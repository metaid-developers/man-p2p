//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"man-p2p/common"
	"man-p2p/man"
)

func main() {
	common.InitConfig("./config_dev_main.toml")
	common.TestNet = "0"
	common.Chain = "btc,doge"
	man.InitAdapter(common.Chain, common.Db, common.TestNet, common.Server)

	txId := "6d275750da23ff67d66ede69333f61ebda55e7a8bc05ce0f0698cbf492075298"

	fmt.Println("=== 检查 teleport 交易 ===")
	fmt.Printf("TxId: %s\n\n", txId)

	// 检查 UTXO
	utxoPoint := txId + ":0"
	utxo, err := man.PebbleStore.GetMrc20UtxoByTxPoint(utxoPoint, false)
	if err != nil {
		fmt.Printf("❌ 获取 UTXO 失败: %v\n", err)
	} else if utxo != nil {
		fmt.Printf("UTXO %s:\n", utxoPoint)
		fmt.Printf("  Tick: %s\n", utxo.Tick)
		fmt.Printf("  Amount: %s\n", utxo.AmtChange.String())
		fmt.Printf("  Status: %d\n", utxo.Status)
		fmt.Printf("  MrcOption: %s\n", utxo.MrcOption)
		fmt.Printf("  Holder: %s\n", utxo.Holder)
		fmt.Printf("  ToAddress: %s\n", utxo.ToAddress)
		fmt.Printf("  Chain: %s\n", utxo.Chain)
		fmt.Printf("  BlockHeight: %d\n", utxo.BlockHeight)
		fmt.Printf("  OperationTx: %s\n", utxo.OperationTx)
		fmt.Printf("  Msg: %s\n", utxo.Msg)
	} else {
		fmt.Printf("❌ UTXO 不存在\n")
	}

	// 检查 PendingTeleport
	fmt.Printf("\n=== 检查 PendingTeleport ===\n")
	// coord 格式是 pinId
	pinId := txId + "i0"
	pending, err := man.PebbleStore.GetPendingTeleportByCoord(pinId)
	if err != nil {
		fmt.Printf("通过 coord=%s 未找到: %v\n", pinId, err)
	} else if pending != nil {
		fmt.Printf("找到 PendingTeleport:\n")
		fmt.Printf("  PinId: %s\n", pending.PinId)
		fmt.Printf("  Coord: %s\n", pending.Coord)
		fmt.Printf("  TickId: %s\n", pending.TickId)
		fmt.Printf("  Amount: %s\n", pending.Amount)
		fmt.Printf("  AssetOutpoint: %s\n", pending.AssetOutpoint)
		fmt.Printf("  SourceChain: %s\n", pending.SourceChain)
		fmt.Printf("  TargetChain: %s\n", pending.TargetChain)
		fmt.Printf("  Status: %d\n", pending.Status)
	}

	// 检查 PIN
	fmt.Printf("\n=== 检查 PIN 数据 ===\n")
	pinData, err := man.PebbleStore.Database.GetPinByKey(pinId)
	if err != nil {
		fmt.Printf("❌ 获取 PIN 失败: %v\n", err)
	} else if pinData != nil {
		fmt.Printf("PIN 数据 (前500字节):\n")
		if len(pinData) > 500 {
			fmt.Printf("%s...\n", string(pinData[:500]))
		} else {
			fmt.Printf("%s\n", string(pinData))
		}
	}

	// 检查 :8 的 UTXO
	fmt.Printf("\n=== 检查 :8 UTXO ===\n")
	utxoPoint8 := txId + ":8"
	utxo8, err := man.PebbleStore.GetMrc20UtxoByTxPoint(utxoPoint8, false)
	if err != nil {
		fmt.Printf("❌ 获取 UTXO 失败: %v\n", err)
	} else if utxo8 != nil {
		fmt.Printf("UTXO %s:\n", utxoPoint8)
		fmt.Printf("  Tick: %s\n", utxo8.Tick)
		fmt.Printf("  Amount: %s\n", utxo8.AmtChange.String())
		fmt.Printf("  Status: %d\n", utxo8.Status)
		fmt.Printf("  MrcOption: %s\n", utxo8.MrcOption)
		fmt.Printf("  Holder: %s\n", utxo8.Holder)
		fmt.Printf("  Chain: %s\n", utxo8.Chain)
		fmt.Printf("  BlockHeight: %d\n", utxo8.BlockHeight)
	} else {
		fmt.Printf("❌ UTXO 不存在\n")
	}
}
