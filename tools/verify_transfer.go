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
	common.Chain = "doge"
	man.InitAdapter(common.Chain, common.Db, common.TestNet, common.Server)

	fmt.Println("=== 验证 MRC20 Transfer 结果 ===")

	// 检查 transfer 交易的输出 UTXO
	transferTxId := "1fa08c6a99378145ab0a959fe8c1443062d353885ba9ccf5d210ad600c353fd9"

	// 检查是否已处理
	fmt.Printf("\n1. 检查 transfer 交易是否已处理\n")
	utxo, err := man.PebbleStore.CheckOperationtx(transferTxId, false)
	if err != nil {
		fmt.Printf("❌ 错误: %v\n", err)
	} else if utxo != nil {
		fmt.Printf("✅ 已处理! UTXO:\n")
		fmt.Printf("   TxPoint: %s\n", utxo.TxPoint)
		fmt.Printf("   Tick: %s\n", utxo.Tick)
		fmt.Printf("   Amount: %s\n", utxo.AmtChange.String())
		fmt.Printf("   Status: %d\n", utxo.Status)
		fmt.Printf("   MrcOption: %s\n", utxo.MrcOption)
	} else {
		fmt.Printf("❌ 未找到 UTXO\n")
	}

	// 检查 arrival 的 UTXO 状态（应该已被花费）
	fmt.Printf("\n2. 检查 arrival UTXO 状态\n")
	arrivalUtxoPoint := "2fdcbba823bf1d6997291550484faf10c39916f01c1aa9d8f9a5d6801326de3e:0"
	arrivalUtxo, err := man.PebbleStore.GetMrc20UtxoByTxPoint(arrivalUtxoPoint, false)
	if err != nil {
		fmt.Printf("❌ 错误: %v\n", err)
	} else if arrivalUtxo != nil {
		statusStr := map[int]string{0: "Available", 1: "TeleportPending", 2: "TransferPending", -1: "Spent"}[arrivalUtxo.Status]
		fmt.Printf("Arrival UTXO %s:\n", arrivalUtxoPoint)
		fmt.Printf("   Status: %d (%s)\n", arrivalUtxo.Status, statusStr)
		fmt.Printf("   Tick: %s\n", arrivalUtxo.Tick)
		fmt.Printf("   Amount: %s\n", arrivalUtxo.AmtChange.String())
	} else {
		fmt.Printf("❌ 未找到 arrival UTXO\n")
	}

	// 检查 transfer 输出的 UTXO（vout=1）
	fmt.Printf("\n3. 检查 transfer 输出 UTXO\n")
	transferOutputPoint := transferTxId + ":1"
	transferUtxo, err := man.PebbleStore.GetMrc20UtxoByTxPoint(transferOutputPoint, false)
	if err != nil {
		fmt.Printf("❌ 错误: %v\n", err)
	} else if transferUtxo != nil {
		statusStr := map[int]string{0: "Available", 1: "TeleportPending", 2: "TransferPending", -1: "Spent"}[transferUtxo.Status]
		fmt.Printf("✅ Transfer 输出 UTXO %s:\n", transferOutputPoint)
		fmt.Printf("   Status: %d (%s)\n", transferUtxo.Status, statusStr)
		fmt.Printf("   Tick: %s\n", transferUtxo.Tick)
		fmt.Printf("   Amount: %s\n", transferUtxo.AmtChange.String())
		fmt.Printf("   Holder: %s\n", transferUtxo.Holder)
		fmt.Printf("   ToAddress: %s\n", transferUtxo.ToAddress)
	} else {
		fmt.Printf("❌ 未找到 transfer 输出 UTXO\n")
	}

	// 检查 MRC20 索引高度
	fmt.Printf("\n4. 检查 MRC20 索引高度\n")
	mrc20Height := man.PebbleStore.GetMrc20IndexHeight("doge")
	fmt.Printf("当前 doge MRC20 索引高度: %d\n", mrc20Height)

	fmt.Println("\n=== 验证完成 ===")
}
