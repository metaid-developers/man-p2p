//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"log"
	"os"
	"strconv"

	"man-p2p/common"
	"man-p2p/man"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run tools/index_single_block.go <block_height> [chain_name]")
		fmt.Println("Example: go run tools/index_single_block.go 6050998 doge")
		os.Exit(1)
	}

	height, err := strconv.ParseInt(os.Args[1], 10, 64)
	if err != nil || height <= 0 {
		log.Fatalf("无效的区块高度: %s", os.Args[1])
	}

	chainName := "doge"
	if len(os.Args) >= 3 {
		chainName = os.Args[2]
	}

	// 初始化配置（参考 app_test.go）
	common.InitConfig("./config_dev_main.toml")
	common.TestNet = "0"
	common.Chain = chainName

	fmt.Printf("=== 单块索引测试工具 ===\n")
	fmt.Printf("链: %s, 区块高度: %d\n", chainName, height)

	// 初始化 Man 模块
	man.InitAdapter(common.Chain, common.Db, common.TestNet, common.Server)
	fmt.Println("Man 模块已初始化")

	// 执行单块索引（reIndex=true 跳过高度检查）
	fmt.Printf("开始索引区块 %d...\n", height)
	err = man.PebbleStore.DoIndexerRun(chainName, height, true)
	if err != nil {
		log.Fatalf("索引区块失败: %v", err)
	}

	fmt.Printf("区块 %d 索引完成！\n", height)
}
