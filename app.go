package main

import (
	"embed"
	"fmt"
	"log"
	"man-p2p/api"
	"man-p2p/common"
	"man-p2p/man"
	"man-p2p/pebblestore"
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
	common.InitConfig(configPath)
	man.InitAdapter(common.Chain, common.Db, common.TestNet, common.Server)

	// 显示运行模式
	modeInfo := ""
	if common.Config.Sync.Mrc20Only {
		modeInfo = ",mode=MRC20-ONLY"
	}
	log.Printf("ManIndex,chain=%s,fullnode=%v,test=%s,db=%s,server=%s,config=%s%s,metaChain=%s", common.Chain, common.Config.Sync.IsFullNode, common.TestNet, common.Db, common.Server, common.ConfigFile, modeInfo, common.Config.Statistics.MetaChainHost)

	if common.Server == "1" {
		go api.Start(f)
	}
	go man.ZmqRun()

	// MRC20 catch-up disabled in man-p2p phase 1 — asset parsing not enabled this phase
	// man.Mrc20CatchUpRun()

	// Execute statistics; Mrc20Only stat goroutines disabled in man-p2p phase 1
	go pebblestore.StatMetaId(man.PebbleStore.Database)
	go pebblestore.StatPinSort(man.PebbleStore.Database)

	for {
		man.IndexerRun(common.TestNet)

		// MRC20 catch-up disabled in man-p2p phase 1
		// man.Mrc20CatchUpRun()

		man.CheckNewBlock()
		time.Sleep(time.Second * 10)
	}
}
