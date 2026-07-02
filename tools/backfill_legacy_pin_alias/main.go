package main

import (
	"flag"
	"log"

	"man-p2p/common"
	"man-p2p/man"
)

func main() {
	configPath := flag.String("config", "./config.toml", "path to config.toml")
	chainName := flag.String("chain", "mvc", "chain to backfill legacy pin aliases for")
	limit := flag.Int("limit", 0, "maximum aliases to create; 0 means no limit")
	flag.Parse()

	common.InitConfig(*configPath)
	man.InitRuntime(*chainName, common.Db, common.TestNet, "0", true)
	defer func() {
		if man.PebbleStore != nil && man.PebbleStore.Database != nil {
			_ = man.PebbleStore.Database.Close()
		}
	}()

	stats, err := man.PebbleStore.BackfillLegacyPinAliases(*chainName, *limit)
	if err != nil {
		log.Fatalf("backfill legacy pin aliases failed: %v", err)
	}

	log.Printf("legacy pin alias backfill finished: chain=%s scanned=%d created=%d errors=%d", *chainName, stats.Scanned, stats.Created, stats.Errors)
}
