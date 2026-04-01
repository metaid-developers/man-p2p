package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"log"
	"man-p2p/api"
	"man-p2p/common"
	"man-p2p/man"
	"man-p2p/p2p"
	"man-p2p/pebblestore"
	"os"
	"time"
)

var (
	//go:embed web/static/* web/template/* web/template/home/* web/template/public/*
	f embed.FS
)

func main() {
	banner := `
    __  ___  ___     _   __
   /  |/  / /   |   / | / / v2.0.1
  / /|_/ / / /| |  /  |/ / 
 / /  / / / ___ | / /|  /  
/_/  /_/ /_/  |_|/_/ |_/                   
 `
	fmt.Println(banner)
	configPath := "./config.toml"
	if common.ConfigFile != "" {
		configPath = common.ConfigFile
	}
	var p2pConfigFile string
	var p2pDataDir string
	flag.StringVar(&p2pConfigFile, "p2p-config", "", "path to p2p sync config JSON file")
	flag.StringVar(&p2pDataDir, "data-dir", "", "path to PebbleDB data directory")
	common.InitConfig(configPath)
	if p2pConfigFile != "" {
		if err := p2p.LoadConfig(p2pConfigFile); err != nil {
			log.Printf("warn: failed to load p2p config: %v", err)
		}
	}
	chainSourceEnabled := p2p.GetConfig().ChainSourceEnabled()
	man.InitRuntime(common.Chain, common.Db, common.TestNet, common.Server, chainSourceEnabled)

	// P2P initialization
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dataDir := p2pDataDir
	if dataDir == "" {
		dataDir = "./man_p2p_data"
	}
	os.MkdirAll(dataDir, 0700)

	configureP2PCallbacks()

	if err := p2p.InitHost(ctx, dataDir); err != nil {
		log.Printf("warn: p2p host init failed: %v", err)
	} else {
		if err := p2p.InitGossip(ctx); err != nil {
			log.Printf("warn: p2p gossip init failed: %v", err)
		} else if err := p2p.InitPresence(ctx); err != nil {
			log.Printf("warn: p2p presence init failed: %v", err)
		}
		p2p.RegisterSyncHandler()
		p2p.StartStorageMonitor(ctx, dataDir)
		log.Printf("P2P node started: %s", p2p.Node.ID())
	}

	// 显示运行模式
	modeInfo := ""
	if common.Config.Sync.Mrc20Only {
		modeInfo = ",mode=MRC20-ONLY"
	}
	log.Printf("ManIndex,chain=%s,fullnode=%v,test=%s,db=%s,server=%s,config=%s%s,metaChain=%s", common.Chain, common.Config.Sync.IsFullNode, common.TestNet, common.Db, common.Server, common.ConfigFile, modeInfo, common.Config.Statistics.MetaChainHost)

	if common.Server == "1" {
		go api.Start(f)
	}
	if chainSourceEnabled {
		go man.ZmqRun()
	}

	// MRC20 catch-up disabled in man-p2p phase 1 — asset parsing not enabled this phase
	// man.Mrc20CatchUpRun()

	// Execute statistics; Mrc20Only stat goroutines disabled in man-p2p phase 1
	go pebblestore.StatMetaId(man.PebbleStore.Database)
	go pebblestore.StatPinSort(man.PebbleStore.Database)

	for {
		if chainSourceEnabled {
			man.IndexerRun(common.TestNet)

			// MRC20 catch-up disabled in man-p2p phase 1
			// man.Mrc20CatchUpRun()

			man.CheckNewBlock()
		}
		time.Sleep(time.Second * 10)
	}
}
