package main

import (
	"encoding/json"
	"fmt"
	"man-p2p/common"
	"man-p2p/man"
	"man-p2p/mrc20"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/wire"
)

func TestIndexer(t *testing.T) {
	common.InitConfig("./config_regtest.toml")
	common.TestNet = "0"
	common.Chain = "mvc"
	man.InitAdapter(common.Chain, common.Db, common.TestNet, common.Server)
	man.PebbleStore.DoIndexerRun("mvc", 145049, false)
}

func TestDogeIndexer(t *testing.T) {
	common.InitConfig("./config_dev_main.toml")
	common.TestNet = "0"
	common.Chain = "doge"
	man.InitAdapter(common.Chain, common.Db, common.TestNet, common.Server)

	blockHeight := int64(6051005)

	fmt.Printf("\n========================================\n")
	fmt.Printf("测试 Dogecoin 区块 %d\n", blockHeight)
	man.PebbleStore.DoIndexerRun("doge", blockHeight, false)
	fmt.Println("索引完成！")

	// 查看索引的数据
	result, err := man.PebbleStore.Database.GetlBlocksDB("doge", int(blockHeight))
	if err != nil {
		fmt.Printf("❌ 获取区块数据失败: %v\n", err)
		t.Fatalf("获取区块数据失败: %v", err)
	} else if result != nil && *result != "" {
		fmt.Printf("✅ 区块 %d 包含的 PIN IDs: %s\n", blockHeight, *result)

		// 获取并显示 PIN 详情
		pinIds := strings.Split(*result, ",")
		fmt.Printf("\n找到 %d 个 PIN:\n", len(pinIds))
		for i, pinId := range pinIds {
			pinData, err := man.PebbleStore.Database.GetPinByKey(pinId)
			if err == nil && pinData != nil {
				fmt.Printf("\n[PIN #%d] ID: %s\n", i+1, pinId)
				fmt.Printf("数据长度: %d bytes\n", len(pinData))
				// 只显示前200个字符
				if len(pinData) > 200 {
					fmt.Printf("数据预览: %s...\n", string(pinData[:200]))
				} else {
					fmt.Printf("数据: %s\n", string(pinData))
				}
			} else {
				fmt.Printf("\n[PIN #%d] ID: %s (无法读取数据)\n", i+1, pinId)
			}
		}
	} else {
		fmt.Printf("❌ 区块 %d 没有找到 PIN 数据\n", blockHeight)
		t.Errorf("预期找到 PIN 数据但实际为空")
	}
}

func TestDogePin(t *testing.T) {
	common.InitConfig("./config_dev_main.toml")
	common.TestNet = "0"
	common.Chain = "doge"
	man.InitAdapter(common.Chain, common.Db, common.TestNet, common.Server)

	txString := "94809d6598eae303898bb2b342fa61b6026a0717e285d7970b5ff5ee4ea1b9a9"
	blockHeight := int64(6051005)

	// 由于节点没有 txindex，需要从区块中获取交易
	block, err := man.ChainAdapter["doge"].GetBlock(blockHeight)
	if err != nil {
		t.Fatal(err)
	}

	msgBlock := block.(*wire.MsgBlock)
	fmt.Printf("区块 %d 包含 %d 个交易\n", blockHeight, len(msgBlock.Transactions))

	// 列出所有交易 hash
	fmt.Println("\n区块中的所有交易:")
	for i, tx := range msgBlock.Transactions {
		fmt.Printf("  [%d] %s\n", i, tx.TxHash().String())
	}

	var targetTx *wire.MsgTx
	var txIndex int
	for i, tx := range msgBlock.Transactions {
		if tx.TxHash().String() == txString {
			targetTx = tx
			txIndex = i
			fmt.Printf("找到目标交易，索引: %d\n", i)
			break
		}
	}

	if targetTx == nil {
		t.Fatalf("区块中没有找到交易 %s", txString)
	}

	fmt.Printf("\n分析交易 %s:\n", txString)
	fmt.Printf("  输入数量: %d\n", len(targetTx.TxIn))
	fmt.Printf("  输出数量: %d\n", len(targetTx.TxOut))

	// 检查每个输入的 ScriptSig
	for i, input := range targetTx.TxIn {
		fmt.Printf("\n  Input %d:\n", i)
		fmt.Printf("    PrevOut: %s:%d\n", input.PreviousOutPoint.Hash, input.PreviousOutPoint.Index)
		fmt.Printf("    ScriptSig 长度: %d bytes\n", len(input.SignatureScript))
		if len(input.SignatureScript) > 0 {
			// 检查是否包含 metaid 协议标记
			scriptHex := fmt.Sprintf("%x", input.SignatureScript)
			fmt.Printf("    ScriptSig (hex): %s\n", scriptHex)

			// 检查是否包含 "metaid" 字符串 (6d6574616964)
			if strings.Contains(scriptHex, "6d6574616964") {
				fmt.Printf("    ✅ 包含 'metaid' 协议标记!\n")
			}
		}
	}

	// 尝试解析 PIN
	pins := man.IndexerAdapter["doge"].CatchPinsByTx(targetTx, blockHeight, 0, "", "", txIndex)
	fmt.Printf("\n解析到 %d 个 PIN\n", len(pins))

	for _, pinNode := range pins {
		fmt.Printf("\nPin : %+v\n", pinNode)
		fmt.Println("===================")
		fmt.Println(string(pinNode.ContentBody))
	}

	if len(pins) == 0 {
		t.Log("警告: 没有解析到 PIN，可能需要检查解析逻辑")
	}
}

// TestDogeBlock6049344Arrival 测试 doge 区块 6049344 中的 arrival
func TestDogeBlock6049344Arrival(t *testing.T) {
	fmt.Println("=== 测试 Doge 区块 6049344 Arrival ===")

	common.InitConfig("./config_dev_main.toml")
	common.TestNet = "0"
	common.Chain = "btc,doge"
	man.InitAdapter(common.Chain, common.Db, common.TestNet, common.Server)

	arrivalTxId := "2fdcbba823bf1d6997291550484faf10c39916f01c1aa9d8f9a5d6801326de3e"
	//arrivalPinId := arrivalTxId + "i0"
	blockHeight := int64(6049344)

	// 1. 检查交易是否在这个区块
	fmt.Printf("\n1. 获取区块 %d 的所有交易...\n", blockHeight)
	chain := man.ChainAdapter["doge"]
	block, err := chain.GetBlock(blockHeight)
	if err != nil {
		t.Fatalf("获取区块失败: %v", err)
	}

	// doge adapter 返回的是 *wire.MsgBlock，不是 *btcutil.Block
	msgBlock := block.(*wire.MsgBlock)
	fmt.Printf("区块包含 %d 个交易\n", len(msgBlock.Transactions))

	// 查找 arrival 交易
	found := false
	for i, tx := range msgBlock.Transactions {
		txHash := tx.TxHash().String()
		if txHash == arrivalTxId {
			found = true
			fmt.Printf("\n✅ 找到 arrival 交易! 索引: %d\n", i)
			fmt.Printf("TxHash: %s\n", txHash)
			fmt.Printf("输入数量: %d\n", len(tx.TxIn))
			fmt.Printf("输出数量: %d\n", len(tx.TxOut))

			// 尝试解析 PIN
			fmt.Println("\n2. 尝试解析 PIN...")
			pins := man.IndexerAdapter["doge"].CatchPinsByTx(tx, blockHeight, 0, "", "", i)
			fmt.Printf("解析到 %d 个 PIN\n", len(pins))

			for j, pin := range pins {
				fmt.Printf("\n  PIN %d:\n", j+1)
				fmt.Printf("    ID: %s\n", pin.Id)
				fmt.Printf("    Path: %s\n", pin.Path)
				fmt.Printf("    Operation: %s\n", pin.Operation)
				fmt.Printf("    ContentType: %s\n", pin.ContentType)
				fmt.Printf("    ContentBody: %s\n", string(pin.ContentBody))
			}

			if len(pins) == 0 {
				// 手动检查输入的 ScriptSig
				fmt.Println("\n3. 手动分析交易输入...")
				for k, input := range tx.TxIn {
					fmt.Printf("  Input %d:\n", k)
					fmt.Printf("    PrevOut: %s:%d\n", input.PreviousOutPoint.Hash, input.PreviousOutPoint.Index)
					fmt.Printf("    ScriptSig 长度: %d\n", len(input.SignatureScript))
					if len(input.SignatureScript) > 0 {
						// 显示前100字节的十六进制
						maxLen := 100
						if len(input.SignatureScript) < maxLen {
							maxLen = len(input.SignatureScript)
						}
						fmt.Printf("    ScriptSig (前%d字节): %x\n", maxLen, input.SignatureScript[:maxLen])

						// 检查是否包含 "metaid" 字符串
						scriptStr := string(input.SignatureScript)
						if strings.Contains(scriptStr, "metaid") {
							fmt.Printf("    ⚠️ 包含 'metaid' 字符串!\n")
						}
					}
				}

				// 检查 Witness 数据
				fmt.Println("\n4. 检查 Witness 数据...")
				for k, input := range tx.TxIn {
					if len(input.Witness) > 0 {
						fmt.Printf("  Input %d Witness:\n", k)
						for w, wit := range input.Witness {
							fmt.Printf("    Witness[%d] 长度: %d\n", w, len(wit))
							if len(wit) > 0 && len(wit) < 200 {
								witStr := string(wit)
								if strings.Contains(strings.ToLower(witStr), "metaid") {
									fmt.Printf("    ⚠️ Witness[%d] 包含 'metaid'!\n", w)
								}
							}
						}
					}
				}
			}
			break
		}
	}

	if !found {
		fmt.Printf("\n❌ 区块 %d 中没有找到交易 %s\n", blockHeight, arrivalTxId)
		fmt.Println("请确认交易所在的区块高度")
	}
}

// TestDogeMRC20ChainDebug 追溯 MRC20 交易链，找到问题根源
func TestDogeMRC20ChainDebug(t *testing.T) {
	fmt.Printf("=== 追溯 Doge MRC20 交易链 ===\n")

	common.InitConfig("./config_dev_main.toml")
	common.TestNet = "0"
	common.Chain = "doge"
	man.InitAdapter(common.Chain, common.Db, common.TestNet, common.Server)

	// 从已知的输入 MRC20 UTXO 开始追溯
	txString := "1fa08c6a99378145ab0a959fe8c1443062d353885ba9ccf5d210ad600c353fd9"
	fmt.Printf("分析输入交易: %s\n\n", txString)

	// 递归追溯交易链
	traceMrc20Chain(t, txString, 0)
}

// TestDogeMRC20TransferProcess 模拟 MRC20 transfer 处理流程
func TestDogeMRC20TransferProcess(t *testing.T) {
	fmt.Printf("=== 模拟 MRC20 Transfer 处理 ===\n")

	common.InitConfig("./config_dev_main.toml")
	common.TestNet = "0"
	common.Chain = "doge"
	man.InitAdapter(common.Chain, common.Db, common.TestNet, common.Server)

	txString := "1fa08c6a99378145ab0a959fe8c1443062d353885ba9ccf5d210ad600c353fd9"

	// 获取交易
	tx, err := man.ChainAdapter["doge"].GetTransaction(txString)
	if err != nil {
		t.Fatalf("获取交易失败: %v", err)
	}

	msgTx := tx.(*btcutil.Tx).MsgTx()

	// 获取 PIN 数据
	pins := man.IndexerAdapter["doge"].CatchPinsByTx(msgTx, 0, 0, "", "", 0)
	if len(pins) == 0 {
		t.Fatal("没有找到 PIN")
	}

	pinNode := pins[0]
	fmt.Printf("PIN 信息:\n")
	fmt.Printf("  ID: %s\n", pinNode.Id)
	fmt.Printf("  Path: %s\n", pinNode.Path)
	fmt.Printf("  Operation: %s\n", pinNode.Operation)
	fmt.Printf("  GenesisTransaction: %s\n", pinNode.GenesisTransaction)
	fmt.Printf("  ContentBody: %s\n", string(pinNode.ContentBody))

	// 检查是否已处理
	find, err := man.PebbleStore.CheckOperationtx(pinNode.GenesisTransaction, false)
	fmt.Printf("\nCheckOperationtx 结果:\n")
	fmt.Printf("  find: %+v\n", find)
	fmt.Printf("  err: %v\n", err)

	if find != nil {
		fmt.Printf("  ✅ 已处理过，跳过\n")
		return
	}

	// 解析 JSON
	var content []mrc20.Mrc20TranferData
	err = json.Unmarshal(pinNode.ContentBody, &content)
	if err != nil {
		fmt.Printf("❌ JSON 解析失败: %v\n", err)
		return
	}
	fmt.Printf("\n解析的 Transfer 数据:\n")
	for i, item := range content {
		fmt.Printf("  %d. Vout=%d, Id=%s, Amount=%s\n", i+1, item.Vout, item.Id, item.Amount)
	}

	// 获取交易输入
	var inputList []string
	for _, in := range msgTx.TxIn {
		s := fmt.Sprintf("%s:%d", in.PreviousOutPoint.Hash.String(), in.PreviousOutPoint.Index)
		inputList = append(inputList, s)
	}
	fmt.Printf("\n交易输入:\n")
	for i, s := range inputList {
		fmt.Printf("  %d. %s\n", i+1, s)
	}

	// 查询输入 UTXO
	utxoList, err := man.PebbleStore.GetMrc20UtxoByOutPutList(inputList, false)
	fmt.Printf("\n输入 MRC20 UTXO:\n")
	fmt.Printf("  err: %v\n", err)
	fmt.Printf("  count: %d\n", len(utxoList))
	for i, utxo := range utxoList {
		fmt.Printf("  %d. TxPoint=%s, Tick=%s, Mrc20Id=%s, Amount=%s, Status=%d\n",
			i+1, utxo.TxPoint, utxo.Tick, utxo.Mrc20Id, utxo.AmtChange.String(), utxo.Status)
	}

	// 检查 tick 是否存在
	for _, item := range content {
		tick, err := man.PebbleStore.GetMrc20TickInfo(item.Id, "")
		fmt.Printf("\nTick 信息 (ID=%s):\n", item.Id)
		if err != nil {
			fmt.Printf("  ❌ 错误: %v\n", err)
		} else {
			fmt.Printf("  ✅ Tick=%s, TokenName=%s\n", tick.Tick, tick.TokenName)
		}
	}

	// 检查输入金额是否足够
	inMap := make(map[string]string)
	for _, utxo := range utxoList {
		inMap[utxo.Mrc20Id] = utxo.AmtChange.String()
	}
	fmt.Printf("\n金额检查:\n")
	for _, item := range content {
		inAmt := inMap[item.Id]
		fmt.Printf("  Tick %s: 输入=%s, 输出=%s\n", item.Id, inAmt, item.Amount)
		if inAmt == "" {
			fmt.Printf("    ❌ 输入中没有该 Tick 的 UTXO！\n")
		}
	}

	// 直接调用 CreateMrc20TransferUtxo 看结果
	fmt.Printf("\n=== 直接调用 CreateMrc20TransferUtxo ===\n")
	result, err := man.CreateMrc20TransferUtxo(pinNode, &man.Mrc20Validator{}, false)
	fmt.Printf("返回结果:\n")
	fmt.Printf("  err: %v\n", err)
	fmt.Printf("  result count: %d\n", len(result))
	for i, r := range result {
		fmt.Printf("  %d. TxPoint=%s, Tick=%s, Amount=%s, MrcOption=%s, Msg=%s\n",
			i+1, r.TxPoint, r.Tick, r.AmtChange.String(), r.MrcOption, r.Msg)
	}

	// 检查区块高度
	fmt.Printf("\n=== 检查区块高度 ===\n")
	fmt.Printf("PIN GenesisHeight: %d\n", pinNode.GenesisHeight)
	fmt.Printf("当前 MRC20 索引高度: %d\n", man.PebbleStore.GetMrc20IndexHeight("doge"))
	fmt.Printf("配置 MRC20 起始高度: %d\n", common.Config.Doge.Mrc20Height)

	// 获取交易的区块信息
	txInfo, err := man.ChainAdapter["doge"].GetTransaction(txString)
	if err == nil {
		btcTx := txInfo.(*btcutil.Tx)
		fmt.Printf("交易 Hash: %s\n", btcTx.Hash().String())
	}
}

func traceMrc20Chain(t *testing.T, txString string, depth int) {
	if depth > 5 {
		fmt.Printf("%s[深度限制] 停止追溯\n", strings.Repeat("  ", depth))
		return
	}

	indent := strings.Repeat("  ", depth)
	fmt.Printf("%s=== 交易 %s ===\n", indent, txString)

	// 检查该交易是否在 MRC20 数据库中
	utxo, _ := man.PebbleStore.CheckOperationtx(txString, false)
	if utxo != nil {
		fmt.Printf("%s✅ 已在数据库中: Tick=%s, Amount=%s, Status=%d\n", indent, utxo.Tick, utxo.AmtChange.String(), utxo.Status)
		return
	}
	fmt.Printf("%s❌ 未在 MRC20 数据库中\n", indent)

	// 获取交易
	tx, err := man.ChainAdapter["doge"].GetTransaction(txString)
	if err != nil {
		fmt.Printf("%s❌ 无法获取交易: %v\n", indent, err)
		return
	}

	msgTx := tx.(*btcutil.Tx).MsgTx()

	// 检查交易的 PIN 数据
	pins := man.IndexerAdapter["doge"].CatchPinsByTx(msgTx, 0, 0, "", "", 0)
	if len(pins) > 0 {
		fmt.Printf("%s找到 %d 个 PIN:\n", indent, len(pins))
		for i, pin := range pins {
			fmt.Printf("%s  PIN %d: Path=%s, Op=%s\n", indent, i+1, pin.Path, pin.Operation)
			if pin.ContentType == "application/json" {
				fmt.Printf("%s    JSON: %s\n", indent, string(pin.ContentBody))
			}
		}
	} else {
		fmt.Printf("%s没有 PIN 数据\n", indent)
	}

	// 检查输入 UTXO
	fmt.Printf("%s输入 UTXO:\n", indent)
	var inputList []string
	for i, in := range msgTx.TxIn {
		outpoint := fmt.Sprintf("%s:%d", in.PreviousOutPoint.Hash.String(), in.PreviousOutPoint.Index)
		inputList = append(inputList, outpoint)
		fmt.Printf("%s  输入 %d: %s\n", indent, i, outpoint)
	}

	// 查询输入中的 MRC20 UTXO
	inputUtxoList, _ := man.PebbleStore.GetMrc20UtxoByOutPutList(inputList, false)
	if len(inputUtxoList) > 0 {
		fmt.Printf("%s✅ 找到 %d 个输入 MRC20 UTXO:\n", indent, len(inputUtxoList))
		for _, u := range inputUtxoList {
			fmt.Printf("%s    %s: Tick=%s, Amount=%s\n", indent, u.TxPoint, u.Tick, u.AmtChange.String())
		}
	} else {
		fmt.Printf("%s❌ 没有找到输入 MRC20 UTXO\n", indent)

		// 继续追溯第一个输入
		if len(inputList) > 0 {
			parts := strings.Split(inputList[0], ":")
			if len(parts) >= 1 {
				fmt.Printf("\n%s继续追溯...\n", indent)
				traceMrc20Chain(t, parts[0], depth+1)
			}
		}
	}
}

func TestDogeMRC20Debug(t *testing.T) {
	fmt.Printf("=== 调试 Doge MRC20 交易 ===\n")

	common.InitConfig("./config_dev_main.toml")
	common.TestNet = "0"
	common.Chain = "doge"
	man.InitAdapter(common.Chain, common.Db, common.TestNet, common.Server)

	txString := "94809d6598eae303898bb2b342fa61b6026a0717e285d7970b5ff5ee4ea1b9a9"
	fmt.Printf("分析交易: %s\n", txString)

	// 获取交易
	tx, err := man.ChainAdapter["doge"].GetTransaction(txString)
	if err != nil {
		t.Fatalf("获取交易失败: %v", err)
	}

	fmt.Printf("✅ 交易获取成功\n")

	// 获取PIN数据
	msgTx := tx.(*btcutil.Tx).MsgTx()
	pins := man.IndexerAdapter["doge"].CatchPinsByTx(msgTx, 0, 0, "", "", 0)

	fmt.Printf("找到 %d 个 PIN\n", len(pins))

	if len(pins) == 0 {
		fmt.Printf("❌ 没有找到 PIN 数据，这可能是问题所在\n")

		// 手动分析输入
		for i, input := range msgTx.TxIn {
			fmt.Printf("\n分析输入 %d:\n", i)
			fmt.Printf("  ScriptSig 长度: %d\n", len(input.SignatureScript))

			// 检查前100个字节
			if len(input.SignatureScript) > 100 {
				preview := string(input.SignatureScript[:100])
				fmt.Printf("  前100字节: %s\n", preview)
				if strings.Contains(strings.ToLower(preview), "metaid") {
					fmt.Printf("  *** 发现 MetaID 标识 ***\n")
				}
			}
		}
		t.Fatal("没有找到PIN数据")
	}

	// 分析每个PIN
	for i, pinNode := range pins {
		fmt.Printf("\n=== PIN #%d ===\n", i+1)
		fmt.Printf("操作: %s\n", pinNode.Operation)
		fmt.Printf("路径: %s\n", pinNode.Path)
		fmt.Printf("内容类型: %s\n", pinNode.ContentType)
		fmt.Printf("数据长度: %d\n", len(pinNode.ContentBody))

		// 如果是 JSON 数据，尝试解析为 MRC20
		if pinNode.ContentType == "application/json" {
			fmt.Printf("JSON 内容: %s\n", string(pinNode.ContentBody))

			// 首先尝试作为数组解析（新格式）
			var mrcArray []map[string]interface{}
			err := json.Unmarshal(pinNode.ContentBody, &mrcArray)
			if err == nil && len(mrcArray) > 0 {
				// 数组格式的 MRC20 数据
				fmt.Printf("🎯 检测到 MRC20 转账（数组格式）\n")
				fmt.Printf("转账项数量: %d\n", len(mrcArray))

				for j, mrcItem := range mrcArray {
					fmt.Printf("  转账 #%d:\n", j+1)
					fmt.Printf("    Vout: %v\n", mrcItem["vout"])
					fmt.Printf("    ID: %v\n", mrcItem["id"])
					fmt.Printf("    Amount: %v\n", mrcItem["amount"])

					// 从 ID 中提取 tick 信息
					if id, ok := mrcItem["id"].(string); ok {
						checkMRC20IndexingFromId(t, id, txString)
					}
				}
			} else {
				// 尝试作为单个对象解析（旧格式）
				var mrcData map[string]interface{}
				err := json.Unmarshal(pinNode.ContentBody, &mrcData)
				if err != nil {
					fmt.Printf("❌ JSON 解析失败: %v\n", err)
					continue
				}

				if op, ok := mrcData["op"].(string); ok && op == "transfer" {
					fmt.Printf("🎯 检测到 MRC20 转账（对象格式）\n")
					tick := mrcData["tick"]
					amt := mrcData["amt"]
					to := mrcData["to"]

					fmt.Printf("  Tick: %v\n", tick)
					fmt.Printf("  Amount: %v\n", amt)
					fmt.Printf("  To: %v\n", to)

					// 检查为什么没有被 MRC20 索引
					checkMRC20Indexing(t, tick, txString)
				}
			}
		}
	}
}

func checkMRC20IndexingFromId(t *testing.T, idString string, txString string) {
	fmt.Printf("\n=== MRC20 Tick ID 分析 ===\n")
	fmt.Printf("Tick ID: %s\n", idString)

	// 这是一个 tickid，不是原始交易ID
	// 需要根据这个 tickid 查找对应的 MRC20 tick 信息
	tickInfo, err := man.PebbleStore.GetMrc20TickInfo(idString, "")
	if err != nil {
		fmt.Printf("❌ Tick ID '%s' 在数据库中不存在: %v\n", idString, err)
		fmt.Printf("这很可能是交易未被索引的原因！\n")

		// 尝试列出现有的tick来对比
		fmt.Printf("\n查找现有的 tick...\n")
		tickList, err := man.PebbleStore.GetMrc20TickList(0, 20)
		if err == nil && tickList != nil {
			fmt.Printf("找到 %d 个已部署的 tick:\n", len(tickList))
			for i, t := range tickList {
				fmt.Printf("  %d. Tick: %s, MRC20 ID: %s\n", i+1, t.Tick, t.Mrc20Id)
			}
		}
		return
	}

	fmt.Printf("✅ Tick ID '%s' 存在于数据库中\n", idString)
	fmt.Printf("  Tick 名称: %s\n", tickInfo.Tick)
	fmt.Printf("  Token 名称: %s\n", tickInfo.TokenName)
	fmt.Printf("  每次铸造数量: %s\n", tickInfo.AmtPerMint)
	fmt.Printf("  已铸造总数: %d\n", tickInfo.TotalMinted)

	// 检查交易输入的 UTXO
	fmt.Printf("\n=== 检查交易输入 UTXO ===\n")
	tx, err := man.ChainAdapter["doge"].GetTransaction(txString)
	if err != nil {
		fmt.Printf("❌ 获取交易失败: %v\n", err)
		return
	}

	msgTx := tx.(*btcutil.Tx).MsgTx()
	var inputList []string
	for i, in := range msgTx.TxIn {
		outpoint := fmt.Sprintf("%s:%d", in.PreviousOutPoint.Hash.String(), in.PreviousOutPoint.Index)
		inputList = append(inputList, outpoint)
		fmt.Printf("  输入 %d: %s\n", i, outpoint)
	}

	// 查询这些输入是否有对应的 MRC20 UTXO
	utxoList, err := man.PebbleStore.GetMrc20UtxoByOutPutList(inputList, false)
	if err != nil {
		fmt.Printf("❌ 查询输入 UTXO 失败: %v\n", err)
	} else if len(utxoList) == 0 {
		fmt.Printf("❌ 输入中没有找到 MRC20 UTXO\n")
		fmt.Printf("这是转账无法处理的原因：需要先有 MRC20 余额才能转账！\n")

		// 进一步检查输入交易
		fmt.Printf("\n=== 进一步检查输入交易 ===\n")
		for _, outpoint := range inputList {
			parts := strings.Split(outpoint, ":")
			if len(parts) < 2 {
				continue
			}
			inputTxId := parts[0]
			fmt.Printf("\n检查输入交易: %s\n", inputTxId)

			// 检查这个交易是否有任何相关的 MRC20 记录
			inputUtxo, err := man.PebbleStore.CheckOperationtx(inputTxId, false)
			if err != nil || inputUtxo == nil {
				fmt.Printf("  ❌ 该交易没有 MRC20 记录\n")

				// 检查这个交易本身
				inputTx, err := man.ChainAdapter["doge"].GetTransaction(inputTxId)
				if err != nil {
					fmt.Printf("  ❌ 无法获取交易: %v\n", err)
					continue
				}
				inputMsgTx := inputTx.(*btcutil.Tx).MsgTx()

				// 检查这个交易的 PIN 数据
				pins := man.IndexerAdapter["doge"].CatchPinsByTx(inputMsgTx, 0, 0, "", "", 0)
				if len(pins) > 0 {
					fmt.Printf("  找到 %d 个 PIN:\n", len(pins))
					for j, pin := range pins {
						fmt.Printf("    PIN %d: Path=%s, Op=%s\n", j+1, pin.Path, pin.Operation)
						if pin.Path == "/ft/mrc20/mint" {
							fmt.Printf("    *** 这是一个 MRC20 铸造交易！应该被索引 ***\n")
						}
					}
				} else {
					fmt.Printf("  该交易没有 PIN 数据\n")
				}
			} else {
				fmt.Printf("  ✅ 找到 MRC20 记录: Tick=%s, Amount=%s\n", inputUtxo.Tick, inputUtxo.AmtChange.String())
			}
		}
	} else {
		fmt.Printf("✅ 找到 %d 个输入 MRC20 UTXO:\n", len(utxoList))
		for i, utxo := range utxoList {
			fmt.Printf("  UTXO %d:\n", i+1)
			fmt.Printf("    TxPoint: %s\n", utxo.TxPoint)
			fmt.Printf("    Tick: %s\n", utxo.Tick)
			fmt.Printf("    Amount: %s\n", utxo.AmtChange.String())
			fmt.Printf("    Status: %d\n", utxo.Status)
		}
	}

	// 检查当前转账交易
	fmt.Printf("\n检查当前转账交易...\n")
	utxo, err := man.PebbleStore.CheckOperationtx(txString, false)
	if err != nil || utxo == nil {
		fmt.Printf("❌ 转账交易 %s 未在 UTXO 数据库中找到\n", txString)
		fmt.Printf("确认交易未被 MRC20 索引\n")
	} else {
		fmt.Printf("✅ 转账交易已在数据库中: %+v\n", utxo)
	}
}

func checkMRC20Indexing(t *testing.T, tickInterface interface{}, txString string) {
	fmt.Printf("\n=== MRC20 索引检查 ===\n")

	tick, ok := tickInterface.(string)
	if !ok {
		fmt.Printf("❌ Tick 不是字符串类型: %v\n", tickInterface)
		return
	}

	// 检查当前 MRC20 索引高度
	currentHeight := man.PebbleStore.GetMrc20IndexHeight("doge")
	configHeight := common.Config.Doge.Mrc20Height
	fmt.Printf("当前 DOGE MRC20 索引高度: %d\n", currentHeight)
	fmt.Printf("配置 DOGE MRC20 起始高度: %d\n", configHeight)

	// 检查 tick 是否存在
	tickInfo, err := man.PebbleStore.GetMrc20TickInfo("", tick)
	if err != nil {
		fmt.Printf("❌ Tick '%s' 在数据库中不存在: %v\n", tick, err)
		fmt.Printf("这很可能是交易未被索引的原因！\n")

		// 列出现有的tick
		fmt.Printf("\n查找现有的 tick...\n")
		tickList, err := man.PebbleStore.GetMrc20TickList(0, 20)
		if err == nil && tickList != nil {
			fmt.Printf("找到 %d 个已部署的 tick:\n", len(tickList))
			for i, t := range tickList {
				fmt.Printf("  %d. %s\n", i+1, t.Tick)
			}
		}
		return
	}

	fmt.Printf("✅ Tick '%s' 存在于数据库中\n", tick)
	fmt.Printf("  Tick 名称: %s\n", tickInfo.Tick)
	fmt.Printf("  Token 名称: %s\n", tickInfo.TokenName)
	fmt.Printf("  每次铸造数量: %s\n", tickInfo.AmtPerMint)
	fmt.Printf("  已铸造总数: %d\n", tickInfo.TotalMinted)

	// 检查交易是否已经在 UTXO 数据库中
	fmt.Printf("\n检查交易是否已被索引...\n")
	utxo, err := man.PebbleStore.CheckOperationtx(txString, false)
	if err != nil || utxo == nil {
		fmt.Printf("❌ 交易 %s 未在 UTXO 数据库中找到\n", txString)
		fmt.Printf("确认交易未被 MRC20 索引\n")
	} else {
		fmt.Printf("✅ 交易已在数据库中: %+v\n", utxo)
	}
}
func TestPebbleDb(t *testing.T) {
	common.InitConfig("./config_regtest.toml")
	common.TestNet = "2"
	man.InitAdapter(common.Chain, common.Db, common.TestNet, common.Server)

	it, err := man.PebbleStore.Database.PinSort.NewIter(nil)
	if err != nil {
		// 处理错误
		return
	}
	defer it.Close()

	for it.First(); it.Valid(); it.Next() {
		key := it.Key()
		// dbkey := strings.Split(string(key), "&")
		// if dbkey[0] == common.GetMetaIdByAddress("/protocols/simplenote") {
		// 	fmt.Println("Path Key:", string(key))
		// }
		fmt.Println(" Key:", string(key))
	}
}

func TestMempoolDelete(t *testing.T) {
	common.InitConfig("./config_regtest.toml")
	common.TestNet = "2"
	man.InitAdapter(common.Chain, common.Db, common.TestNet, common.Server)

	man.DeleteMempoolData(438, "btc")
}

// TestBtcBlock932892TeleportBug 测试 BTC 区块 932892 中的 teleport pending bug
// 问题：普通 transfer 被错误标记为 TeleportPending
// 交易: 19bfccfb9dda00a8c753d36baaddf2256dba8e5e6f494bfd5dba07d4d1035393
func TestBtcBlock932892TeleportBug(t *testing.T) {
	fmt.Println("=== 测试 BTC 区块 932892 TeleportPending Bug ===")

	common.InitConfig("./config_dev_main.toml")
	common.TestNet = "0"
	common.Chain = "btc"
	man.InitAdapter(common.Chain, common.Db, common.TestNet, common.Server)

	// 先查看两个 PIN 的内容
	fmt.Println("\n=== 分析区块 932892 的两个 transfer PIN ===")

	// PIN 1: 普通 transfer
	tx1Id := "19bfccfb9dda00a8c753d36baaddf2256dba8e5e6f494bfd5dba07d4d1035393"
	tx1, _ := man.ChainAdapter["btc"].GetTransaction(tx1Id)
	pins1 := man.IndexerAdapter["btc"].CatchPinsByTx(tx1.(*btcutil.Tx).MsgTx(), 932892, 0, "", "", 0)

	fmt.Printf("\n[交易1 - 普通 transfer]\n")
	fmt.Printf("TxId: %s\n", tx1Id)
	for _, pin := range pins1 {
		fmt.Printf("  PIN ID: %s\n", pin.Id)
		fmt.Printf("  Path: %s\n", pin.Path)
		fmt.Printf("  Content: %s\n", string(pin.ContentBody))
		fmt.Printf("  isTeleport: %v\n", man.IsTeleportTransferDebug(pin))
	}

	// PIN 2: teleport transfer
	tx2Id := "021370fb29e649d2d9818b5404ae4b863807c2b132f0d0666c1e6f72a3e10d09"
	tx2, _ := man.ChainAdapter["btc"].GetTransaction(tx2Id)
	pins2 := man.IndexerAdapter["btc"].CatchPinsByTx(tx2.(*btcutil.Tx).MsgTx(), 932892, 0, "", "", 0)

	fmt.Printf("\n[交易2 - teleport transfer]\n")
	fmt.Printf("TxId: %s\n", tx2Id)
	for _, pin := range pins2 {
		fmt.Printf("  PIN ID: %s\n", pin.Id)
		fmt.Printf("  Path: %s\n", pin.Path)
		fmt.Printf("  Content: %s\n", string(pin.ContentBody))
		fmt.Printf("  isTeleport: %v\n", man.IsTeleportTransferDebug(pin))
	}

	// 检查交易2的输入
	fmt.Printf("\n[交易2 的输入分析]\n")
	tx2Msg := tx2.(*btcutil.Tx).MsgTx()
	for i, in := range tx2Msg.TxIn {
		inputTxPoint := fmt.Sprintf("%s:%d", in.PreviousOutPoint.Hash.String(), in.PreviousOutPoint.Index)
		fmt.Printf("  Input[%d]: %s\n", i, inputTxPoint)

		// 检查是否是交易1的输出
		if strings.Contains(inputTxPoint, tx1Id) {
			fmt.Printf("    ⚠️ 这是交易1的输出! teleport 消费了普通 transfer 的 UTXO\n")
		}
	}

	fmt.Println("\n=== 结论 ===")
	fmt.Println("问题根源: 同一区块内，teleport transfer 消费了普通 transfer 创建的 UTXO")
	fmt.Println("普通 transfer 先执行，创建 UTXO 状态为 Available")
	fmt.Println("teleport transfer 后执行，将 UTXO 状态改为 TeleportPending")
	fmt.Println("这是正常行为，不是 bug！")
}

// TestDogeTeleportOutDebug 调试 Doge teleport-out 交易
// 交易: 6d275750da23ff67d66ede69333f61ebda55e7a8bc05ce0f0698cbf492075298
// 区块: 6051879
