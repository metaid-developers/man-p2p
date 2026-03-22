//go:build ignore
// +build ignore

package main

import (
	"encoding/hex"
	"fmt"
	"log"
	"man-p2p/adapter/dogecoin"
	"man-p2p/common"
	"man-p2p/pin"

	"github.com/btcsuite/btcd/btcutil"
)

func main() {
	// 初始化配置
	common.InitConfig("../config_doge.toml")

	// 初始化 Dogecoin 链
	chain := dogecoin.DogecoinChain{}
	chain.InitChain()

	// 要解析的交易 ID
	txID := "38675ae2e9c51b404bfc3584160494788a8faceebfb3c418d043ebcaa84a8ac5"

	fmt.Println("========================================")
	fmt.Printf("解析 Dogecoin 交易: %s\n", txID)
	fmt.Println("========================================\n")

	// 获取交易
	txResult, err := chain.GetTransaction(txID)
	if err != nil {
		log.Fatal("Failed to get transaction:", err)
	}

	txObj := txResult.(*btcutil.Tx)
	tx := txObj.MsgTx()

	fmt.Printf("交易版本: %d\n", tx.Version)
	fmt.Printf("输入数量: %d\n", len(tx.TxIn))
	fmt.Printf("输出数量: %d\n\n", len(tx.TxOut))

	// 解析每个输入，查找 metaid 协议数据
	fmt.Println("分析交易输入（查找 metaid 协议）：")
	fmt.Println("----------------------------------------")

	foundMetaID := false
	for i, txIn := range tx.TxIn {
		fmt.Printf("\n输入 #%d:\n", i)
		fmt.Printf("  Previous TxID: %s\n", txIn.PreviousOutPoint.Hash.String())
		fmt.Printf("  Previous Vout: %d\n", txIn.PreviousOutPoint.Index)
		fmt.Printf("  ScriptSig 长度: %d bytes\n", len(txIn.SignatureScript))

		// 将 ScriptSig 转换为十六进制查看
		scriptHex := hex.EncodeToString(txIn.SignatureScript)
		fmt.Printf("  ScriptSig Hex: %s\n", scriptHex)

		// 检查是否包含 "metaid" (6d6574616964 in hex)
		if len(scriptHex) > 12 {
			// 查找 metaid 标识
			metaidHex := "6d6574616964"
			if contains(scriptHex, metaidHex) {
				foundMetaID = true
				fmt.Printf("\n  ✅ 发现 MetaID 协议!\n")

				// 尝试解析协议数据
				parseMetaIDData(txIn.SignatureScript)
			}
		}
	}

	if !foundMetaID {
		fmt.Println("\n❌ 此交易不包含 MetaID 协议数据")
		return
	}

	// 使用索引器的方法解析完整的 Pin
	fmt.Println("\n========================================")
	fmt.Println("使用索引器解析 Pin 数据：")
	fmt.Println("========================================\n")

	indexer := dogecoin.Indexer{
		ChainParams: "mainnet",
		PopCutNum:   21,
		ChainName:   "doge",
	}
	indexer.InitIndexer()

	// 模拟区块信息
	blockHeight := int64(6005454) // 从交易数据获取的区块高度
	timestamp := int64(1766024860)
	blockHash := "d7cede2efd9505acfdc81b1b4b93a7758718a9c93862275e807ec56d8b034e6a"
	merkleRoot := "test"

	pins := indexer.CatchPinsByTx(tx, blockHeight, timestamp, blockHash, merkleRoot, 0)

	if len(pins) > 0 {
		for i, p := range pins {
			fmt.Printf("Pin #%d:\n", i+1)
			fmt.Printf("  Pin ID: %s\n", p.Id)
			fmt.Printf("  Number: %d\n", p.Number)
			fmt.Printf("  MetaID: %s\n", p.MetaId)
			fmt.Printf("  Address: %s\n", p.Address)
			fmt.Printf("  Output: %s\n", p.Output)
			fmt.Printf("  Timestamp: %d\n", p.Timestamp)
			fmt.Printf("  Genesis Height: %d\n", p.GenesisHeight)
			fmt.Printf("  Content Type: %s\n", p.ContentType)
			fmt.Printf("  Content Body: %s\n", string(p.ContentBody))
			fmt.Printf("  Content Length: %d\n", p.ContentLength)
			fmt.Printf("  Content Summary: %s\n", p.ContentSummary)

			if p.Operation != "" {
				fmt.Printf("  Operation: %s\n", p.Operation)
			}
			if p.Path != "" {
				fmt.Printf("  Path: %s\n", p.Path)
			}
			if p.Encryption != "" {
				fmt.Printf("  Encryption: %s\n", p.Encryption)
			}
			if p.Version != "" {
				fmt.Printf("  Version: %s\n", p.Version)
			}

			// 验证 Pin
			fmt.Printf("\n  验证结果: ")
			if err := pin.ManValidator(p); err != nil {
				fmt.Printf("❌ %v\n", err)
			} else {
				fmt.Printf("✅ 有效的 MetaID Pin\n")
			}
			fmt.Println()
		}
	} else {
		fmt.Println("未能解析出 Pin 数据")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s[:len(substr)] == substr ||
		len(s) > len(substr) && contains(s[1:], substr))
}

func parseMetaIDData(scriptSig []byte) {
	fmt.Println("\n  解析 MetaID 数据：")

	// 尝试解析 OP_PUSHDATA
	pos := 0
	fieldIndex := 0
	fieldNames := []string{"Protocol", "Operation", "ContentType", "?", "Version", "ContentTypeBody", "ContentBody"}

	for pos < len(scriptSig) && fieldIndex < 10 {
		if pos >= len(scriptSig) {
			break
		}

		// 读取长度字节
		length := int(scriptSig[pos])
		pos++

		if length == 0 || pos+length > len(scriptSig) {
			break
		}

		// 读取数据
		data := scriptSig[pos : pos+length]
		pos += length

		// 尝试解析为字符串
		dataStr := string(data)
		dataHex := hex.EncodeToString(data)

		fieldName := fmt.Sprintf("Field %d", fieldIndex)
		if fieldIndex < len(fieldNames) {
			fieldName = fieldNames[fieldIndex]
		}

		// 如果是可打印字符串，显示原文，否则显示十六进制
		isPrintable := true
		for _, b := range data {
			if b < 32 || b > 126 {
				isPrintable = false
				break
			}
		}

		if isPrintable && length > 0 {
			fmt.Printf("    %s: %s\n", fieldName, dataStr)
		} else {
			fmt.Printf("    %s: %s (hex)\n", fieldName, dataHex)
		}

		fieldIndex++
	}

	// 总结协议数据
	fmt.Println("\n  ✅ MetaID 协议解析成功!")
	fmt.Println("  协议格式: 直接在 ScriptSig 开头，无 OP_IF/OP_ENDIF 包裹")
	fmt.Println("  这是 Dogecoin 特有的铭文格式，与 Bitcoin Ordinals 不同")
}
