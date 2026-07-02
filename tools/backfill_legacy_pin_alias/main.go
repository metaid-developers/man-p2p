package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"man-p2p/adapter/microvisionchain"
	"man-p2p/adapter/opcat"
	"man-p2p/common"
	"man-p2p/man"
	"man-p2p/pebblestore"
	"man-p2p/pin"
)

type aliasPairsFlag []string

func (f *aliasPairsFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *aliasPairsFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("empty alias pair")
	}
	*f = append(*f, value)
	return nil
}

type resolveResult struct {
	CanonicalPinID string
	Height         int64
}

type catchPinsFunc func(blockHeight int64) (*[]*pin.PinInscription, *[]string, *map[string]string)

func main() {
	flags := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	configPath := flags.String("config", "./config.toml", "path to config.toml")
	chainName := flags.String("chain", "mvc", "chain to backfill legacy pin aliases for")
	limit := flags.Int("limit", 0, "maximum aliases to create; 0 means no limit")
	resolveLegacyPinID := flags.String("resolve", "", "resolve one legacy pin id to its canonical pin id")
	writeResolved := flags.Bool("write-resolved", false, "when used with --resolve, also write the resolved alias into the pin_alias DB")
	fromHeight := flags.Int64("from-height", 0, "inclusive start height for --resolve; 0 means chain initial height")
	toHeight := flags.Int64("to-height", 0, "inclusive end height for --resolve; 0 means chain best height")
	var setAliasPairs aliasPairsFlag
	flags.Var(&setAliasPairs, "set", "write one alias pair as legacyPinID=canonicalPinID; repeatable")
	flags.Parse(os.Args[1:])

	os.Args = []string{os.Args[0]}
	common.InitConfig(*configPath)
	chain := strings.ToLower(strings.TrimSpace(*chainName))
	resolvedAlias := strings.TrimSpace(*resolveLegacyPinID)

	if resolvedAlias != "" {
		result, err := resolveLegacyPinIDByChain(chain, resolvedAlias, *fromHeight, *toHeight)
		if err != nil {
			log.Fatalf("resolve legacy pin alias failed: %v", err)
		}
		log.Printf("resolved legacy pin alias: alias=%s canonical=%s height=%d", resolvedAlias, result.CanonicalPinID, result.Height)
		if *writeResolved {
			setAliasPairs = append(setAliasPairs, resolvedAlias+"="+result.CanonicalPinID)
		} else if len(setAliasPairs) == 0 {
			return
		}
	}

	if len(setAliasPairs) > 0 {
		pairs, err := parseAliasPairs(setAliasPairs)
		if err != nil {
			log.Fatalf("parse alias pairs failed: %v", err)
		}
		if err := writeAliasPairs(pairs); err != nil {
			log.Fatalf("write alias pairs failed: %v", err)
		}
		keys := make([]string, 0, len(pairs))
		for alias := range pairs {
			keys = append(keys, alias)
		}
		sort.Strings(keys)
		for _, alias := range keys {
			log.Printf("legacy pin alias applied: alias=%s canonical=%s", alias, pairs[alias])
		}
		log.Printf("legacy pin alias apply finished: count=%d", len(pairs))
		return
	}
	man.InitRuntime(chain, common.Db, common.TestNet, "0", true)
	defer func() {
		if man.PebbleStore != nil && man.PebbleStore.Database != nil {
			_ = man.PebbleStore.Database.Close()
		}
	}()

	stats, err := man.PebbleStore.BackfillLegacyPinAliases(chain, *limit)
	if err != nil {
		log.Fatalf("backfill legacy pin aliases failed: %v", err)
	}

	log.Printf("legacy pin alias backfill finished: chain=%s scanned=%d created=%d errors=%d", chain, stats.Scanned, stats.Created, stats.Errors)
}

func parseAliasPairs(rawPairs []string) (map[string]string, error) {
	pairs := make(map[string]string, len(rawPairs))
	for _, rawPair := range rawPairs {
		alias, canonical, err := splitAliasPair(rawPair)
		if err != nil {
			return nil, err
		}
		if existing, ok := pairs[alias]; ok && existing != canonical {
			return nil, fmt.Errorf("conflicting canonical pin ids for alias %s: %s != %s", alias, existing, canonical)
		}
		pairs[alias] = canonical
	}
	return pairs, nil
}

func splitAliasPair(rawPair string) (string, string, error) {
	alias, canonical, ok := strings.Cut(strings.TrimSpace(rawPair), "=")
	if !ok {
		return "", "", fmt.Errorf("invalid alias pair %q, expected legacyPinID=canonicalPinID", rawPair)
	}
	alias = strings.TrimSpace(alias)
	canonical = strings.TrimSpace(canonical)
	if alias == "" || canonical == "" {
		return "", "", fmt.Errorf("invalid alias pair %q, alias and canonical pin id are required", rawPair)
	}
	if alias == canonical {
		return "", "", fmt.Errorf("invalid alias pair %q, alias and canonical pin id must differ", rawPair)
	}
	return alias, canonical, nil
}

func writeAliasPairs(pairs map[string]string) error {
	db, err := pebblestore.NewDataBase(common.Config.Pebble.Dir, common.Config.Pebble.Num)
	if err != nil {
		return err
	}
	defer db.Close()
	return db.BatchSetPinAliases(pairs)
}

func resolveLegacyPinIDByChain(chainName, legacyPinID string, fromHeight, toHeight int64) (resolveResult, error) {
	switch chainName {
	case "mvc":
		chain := &microvisionchain.MicroVisionChain{}
		chain.InitChain()
		indexer := &microvisionchain.Indexer{
			ChainParams: "mainnet",
			PopCutNum:   common.Config.Mvc.PopCutNum,
			ChainName:   chainName,
		}
		indexer.InitIndexer()
		return scanLegacyPinID(legacyPinID, common.Config.Mvc.InitialHeight, chain.GetBestHeight(), fromHeight, toHeight, indexer.CatchPins)
	case "opcat":
		chain := &opcat.OpcatChain{}
		chain.InitChain()
		indexer := &opcat.Indexer{
			ChainParams: "mainnet",
			PopCutNum:   common.Config.Opcat.PopCutNum,
			ChainName:   chainName,
		}
		indexer.InitIndexer()
		return scanLegacyPinID(legacyPinID, common.Config.Opcat.InitialHeight, chain.GetBestHeight(), fromHeight, toHeight, indexer.CatchPins)
	default:
		return resolveResult{}, fmt.Errorf("resolve mode is unsupported for chain %q", chainName)
	}
}

func scanLegacyPinID(targetLegacyPinID string, initialHeight, bestHeight, fromHeight, toHeight int64, catchPins catchPinsFunc) (resolveResult, error) {
	targetLegacyPinID = strings.TrimSpace(targetLegacyPinID)
	if targetLegacyPinID == "" {
		return resolveResult{}, errors.New("legacy pin id is required")
	}
	if catchPins == nil {
		return resolveResult{}, errors.New("catchPins function is required")
	}
	if initialHeight <= 0 {
		return resolveResult{}, fmt.Errorf("invalid initial height: %d", initialHeight)
	}
	if bestHeight < initialHeight {
		return resolveResult{}, fmt.Errorf("invalid height range: initial=%d best=%d", initialHeight, bestHeight)
	}
	if fromHeight <= 0 || fromHeight < initialHeight {
		fromHeight = initialHeight
	}
	if toHeight <= 0 || toHeight > bestHeight {
		toHeight = bestHeight
	}
	if fromHeight > toHeight {
		return resolveResult{}, fmt.Errorf("invalid resolve range: from=%d to=%d", fromHeight, toHeight)
	}

	for height := fromHeight; height <= toHeight; height++ {
		if (height-fromHeight)%1000 == 0 {
			log.Printf("resolve scan progress: target=%s height=%d/%d", targetLegacyPinID, height, toHeight)
		}
		pins, _, _ := catchPins(height)
		if pins == nil {
			continue
		}
		for _, pinNode := range *pins {
			if pinNode == nil || pinNode.LegacyPinId != targetLegacyPinID {
				continue
			}
			return resolveResult{
				CanonicalPinID: pinNode.Id,
				Height:         height,
			}, nil
		}
	}
	return resolveResult{}, fmt.Errorf("legacy pin alias not found: %s (range %d-%d)", targetLegacyPinID, fromHeight, toHeight)
}
