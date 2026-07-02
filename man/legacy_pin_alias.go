package man

import (
	"fmt"
	"log"
	"strings"

	"man-p2p/pin"

	"github.com/bitcoinsv/bsvd/wire"
	"github.com/bytedance/sonic"
)

type LegacyPinAliasBackfillStats struct {
	Scanned int
	Created int
	Errors  int
}

func (pd *PebbleData) resolveCanonicalPinID(pinID string) string {
	pinID = strings.TrimSpace(pinID)
	if pinID == "" || pd == nil || pd.Database == nil {
		return pinID
	}
	canonicalID, err := pd.Database.GetPinAlias(pinID)
	if err != nil || canonicalID == "" {
		return pinID
	}
	return canonicalID
}

func (pd *PebbleData) BackfillLegacyPinAliases(chainName string, limit int) (stats LegacyPinAliasBackfillStats, err error) {
	if pd == nil || pd.Database == nil {
		return stats, fmt.Errorf("pebble database not initialized")
	}
	chainName = strings.ToLower(strings.TrimSpace(chainName))
	if !supportsLegacyPinAliasChain(chainName) {
		return stats, fmt.Errorf("legacy pin alias backfill is unsupported for chain %q", chainName)
	}
	if _, ok := ChainAdapter[chainName]; !ok {
		return stats, fmt.Errorf("chain adapter not initialized for %q", chainName)
	}

	for _, shard := range pd.Database.PinsDBs {
		if shard == nil {
			continue
		}
		it, iterErr := shard.NewIter(nil)
		if iterErr != nil {
			return stats, iterErr
		}
		for it.First(); it.Valid(); it.Next() {
			var pinNode pin.PinInscription
			if unmarshalErr := sonic.Unmarshal(it.Value(), &pinNode); unmarshalErr != nil {
				stats.Errors++
				continue
			}
			if pinNode.ChainName != chainName || pinNode.Id == "" || pinNode.GenesisTransaction == "" || pinNode.GenesisHeight < 0 {
				continue
			}

			stats.Scanned++
			legacyPinID, resolveErr := resolveLegacyPinAlias(&pinNode)
			if resolveErr != nil {
				stats.Errors++
				log.Printf("[WARN] resolve legacy pin alias failed for %s: %v", pinNode.Id, resolveErr)
				continue
			}
			if legacyPinID == "" || legacyPinID == pinNode.Id {
				continue
			}

			existing, aliasErr := pd.Database.GetPinAlias(legacyPinID)
			if aliasErr == nil && existing == pinNode.Id {
				continue
			}
			if setErr := pd.Database.SetPinAlias(legacyPinID, pinNode.Id); setErr != nil {
				it.Close()
				return stats, setErr
			}
			stats.Created++
			if limit > 0 && stats.Created >= limit {
				it.Close()
				return stats, nil
			}
		}
		if closeErr := it.Close(); closeErr != nil {
			return stats, closeErr
		}
	}
	return stats, nil
}

func supportsLegacyPinAliasChain(chainName string) bool {
	switch strings.ToLower(strings.TrimSpace(chainName)) {
	case "mvc", "opcat":
		return true
	default:
		return false
	}
}

func resolveLegacyPinAlias(pinNode *pin.PinInscription) (string, error) {
	if pinNode == nil || pinNode.Id == "" || pinNode.GenesisTransaction == "" {
		return "", nil
	}
	if !supportsLegacyPinAliasChain(pinNode.ChainName) {
		return "", nil
	}
	suffix, ok := strings.CutPrefix(pinNode.Id, pinNode.GenesisTransaction)
	if !ok || suffix == "" {
		return "", fmt.Errorf("pin id %q does not match genesis tx %q", pinNode.Id, pinNode.GenesisTransaction)
	}

	tx, err := ChainAdapter[pinNode.ChainName].GetTransaction(pinNode.GenesisTransaction)
	if err != nil {
		return "", err
	}

	msgTxProvider, ok := tx.(interface{ MsgTx() *wire.MsgTx })
	if !ok {
		return "", fmt.Errorf("unsupported transaction type %T for chain %s", tx, pinNode.ChainName)
	}
	legacyTxID := msgTxProvider.MsgTx().TxHash().String()
	if legacyTxID == "" || legacyTxID == pinNode.GenesisTransaction {
		return "", nil
	}
	return legacyTxID + suffix, nil
}
