package man

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"man-p2p/adapter"
	"man-p2p/common"
	"man-p2p/mrc20"
	"man-p2p/pebblestore"
	"man-p2p/pin"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/shopspring/decimal"
)

// TestMRC20TransferPinFlow 测试完整的 MRC20 Transfer PIN 流程
// 流程: handleMempoolPin (模拟 mempool) → handleMrc20 (模拟出块)
func TestMRC20TransferPinFlow(t *testing.T) {
	// 初始化配置（不需要真实的链适配器）
	common.InitConfig("../config_dev_regtest.toml")
	common.TestNet = "0"
	common.Chain = "doge"
	// 不调用 InitAdapter，使用 mock

	// 创建临时数据库
	tmpDir, err := os.MkdirTemp("", "mrc20_integration_test_*")
	if err != nil {
		t.Fatalf("创建临时目录失败: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	fmt.Printf("使用临时目录: %s\n", tmpDir)

	// 初始化数据库
	db, err := pebblestore.NewDataBase(tmpDir, 1)
	if err != nil {
		t.Fatalf("初始化数据库失败: %v", err)
	}
	defer db.Close()

	PebbleStore = &PebbleData{
		Database: db,
	}

	// 确保 mrc20 模块启用
	common.Config.Module = []string{"mrc20"}

	// 测试参数
	chainName := "doge"
	senderAddress := "DDnnM5GP3o87EDL42PifPMzrtB7xSZhhVg"
	// 注意：接收方和找零地址是 mock PkScript 解析出来的，不是预设的
	// 这是测试时 P2PKH 脚本解析的结果（Dogecoin 地址）
	receiverAddress := "D5ERdEN1gsouFSs7zsq7VYJxyWP6dP28H1" // mock PkScript 解析出的接收方地址
	changeAddress := "D8AHjVFmQfUXegi8QFmAirHJoXn8FMtHLu"   // mock PkScript 解析出的找零地址
	tickId := "test_tick_id_001"
	tickName := "TEST"
	// 使用有效的 hex 格式 txid（32字节）
	sourceTxId := "0000000000000000000000000000000000000000000000000000000000000001"
	sourceUtxoTxPoint := sourceTxId + ":0"
	transferPinTx := "0000000000000000000000000000000000000000000000000000000000000002"

	// 设置 mock GetTransactionWithCache
	// Transfer PIN 交易的输入是 source_mint_tx:0，输出是 receiver 和 sender（找零）
	mockTxCache := make(map[string]*btcutil.Tx)
	mockTxCache[transferPinTx] = createMockTransferTx(t, sourceUtxoTxPoint)

	MockGetTransactionWithCache = func(chain string, txid string) (interface{}, error) {
		if tx, ok := mockTxCache[txid]; ok {
			return tx, nil
		}
		return nil, fmt.Errorf("mock: transaction not found: %s", txid)
	}
	defer func() { MockGetTransactionWithCache = nil }() // 测试结束后清理

	fmt.Println("\n========================================")
	// 设置 mock IndexerAdapter
	if IndexerAdapter == nil {
		IndexerAdapter = make(map[string]adapter.Indexer)
	}
	IndexerAdapter[chainName] = &MockIndexerAdapter{
		receiverAddress: receiverAddress,
		senderAddress:   senderAddress,
	}

	fmt.Println("MRC20 Transfer PIN 完整流程测试")
	fmt.Println("========================================")

	// ========================================
	// Step 1: 前置数据 - 必须先有 Tick 和初始余额/UTXO
	// ========================================
	fmt.Println("\n📝 Step 1: 创建前置数据（Tick、初始余额、源 UTXO）")

	// 1.1 创建 Tick 信息
	tickInfo := &mrc20.Mrc20DeployInfo{
		Tick:    tickName,
		Mrc20Id: tickId,
		Chain:   chainName,
	}
	PebbleStore.SaveMrc20Tick([]mrc20.Mrc20DeployInfo{*tickInfo})
	fmt.Printf("  ✓ Tick 已创建: %s (%s)\n", tickName, tickId)

	// 1.2 创建初始余额
	initialBalance := &mrc20.Mrc20AccountBalance{
		Address:          senderAddress,
		TickId:           tickId,
		Tick:             tickName,
		Balance:          decimal.NewFromInt(45),
		Chain:            chainName,
		LastUpdateHeight: 100,
		UtxoCount:        1,
	}
	PebbleStore.SaveMrc20AccountBalance(initialBalance)
	fmt.Printf("  ✓ 初始余额: %s = 45\n", senderAddress)

	// 1.3 创建源 UTXO（用于 Transfer）
	sourceUtxo := mrc20.Mrc20Utxo{
		Tick:        tickName,
		Mrc20Id:     tickId,
		TxPoint:     sourceUtxoTxPoint,
		PointValue:  546,
		BlockHeight: 100,
		MrcOption:   mrc20.OptionMint,
		ToAddress:   senderAddress,
		AmtChange:   decimal.NewFromInt(45),
		Status:      mrc20.UtxoStatusAvailable,
		Chain:       chainName,
		Verify:      true,
	}
	PebbleStore.SaveMrc20Pin([]mrc20.Mrc20Utxo{sourceUtxo})
	fmt.Printf("  ✓ 源 UTXO: %s (amt=45)\n", sourceUtxoTxPoint)

	// 验证初始状态
	bal, _ := PebbleStore.GetMrc20AccountBalance(chainName, senderAddress, tickId)
	if bal != nil {
		fmt.Printf("  ✓ 验证初始余额: %s\n", bal.Balance.String())
	}

	// ========================================
	// Step 2: 模拟 Mempool - 调用 handleMempoolPin
	// ========================================
	fmt.Println("\n📝 Step 2: 调用 handleMempoolPin 模拟 mempool")

	// 构造 Transfer PIN 数据
	// Transfer PIN 格式: [{"id": tickId, "amount": "...", "vout": N}, ...]
	transferContent := []map[string]interface{}{
		{"id": tickId, "amount": "1", "vout": 0},  // output 0 给接收方
		{"id": tickId, "amount": "44", "vout": 1}, // output 1 找零给发送方
	}
	contentBody, _ := json.Marshal(transferContent)

	transferPin := &pin.PinInscription{
		Id:                 "pin_" + transferPinTx + "i0",
		GenesisTransaction: transferPinTx,
		Path:               "/ft/mrc20/transfer",
		ContentBody:        contentBody,
		ChainName:          chainName,
		Address:            senderAddress,
		GenesisHeight:      -1, // mempool
		CreateAddress:      senderAddress,
		Output:             sourceUtxoTxPoint, // 关键：指定花费的 UTXO
	}

	fmt.Printf("  Transfer PIN Tx: %s\n", transferPinTx)
	fmt.Printf("  转账: %s -> %s, 金额: 1, 找零: 44\n", senderAddress, receiverAddress)

	// 调用 handleMempoolPin
	handleMempoolPin(transferPin)

	// 验证 mempool 后状态
	fmt.Println("\n  🔍 验证 mempool 后状态:")
	utxo, _ := PebbleStore.GetMrc20UtxoByTxPoint(sourceUtxoTxPoint, false)
	if utxo != nil {
		fmt.Printf("    源 UTXO 状态: Status=%d (期望=%d TransferPending)\n", utxo.Status, mrc20.UtxoStatusTransferPending)
	}

	bal, _ = PebbleStore.GetMrc20AccountBalance(chainName, senderAddress, tickId)
	if bal != nil {
		fmt.Printf("    发送方余额: Balance=%s, PendingOut=%s, PendingIn=%s\n",
			bal.Balance.String(), bal.PendingOut.String(), bal.PendingIn.String())
	}

	// ========================================
	// Step 3: 模拟区块确认 - 调用 handleMrc20
	// ========================================
	fmt.Println("\n📝 Step 3: 调用 handleMrc20 模拟出块确认")

	blockHeight := int64(200)

	// 构造区块中的 PIN 列表 (同一个 Transfer PIN，但高度已确认)
	transferPinConfirmed := &pin.PinInscription{
		Id:                 "pin_" + transferPinTx + "i0",
		GenesisTransaction: transferPinTx,
		Path:               "/ft/mrc20/transfer",
		ContentBody:        contentBody,
		ChainName:          chainName,
		Address:            senderAddress,
		GenesisHeight:      blockHeight,
		CreateAddress:      senderAddress,
		Output:             sourceUtxoTxPoint,
	}

	pinList := &[]*pin.PinInscription{transferPinConfirmed}
	txInList := &[]string{sourceUtxoTxPoint} // 源 UTXO 被花费

	fmt.Printf("  区块高度: %d\n", blockHeight)
	fmt.Printf("  txInList: %v\n", *txInList)

	// 调用 handleMrc20
	PebbleStore.handleMrc20(chainName, blockHeight, pinList, txInList)

	// ========================================
	// Step 4: 验证最终状态
	// ========================================
	fmt.Println("\n📝 Step 4: 验证最终状态")

	// 验证发送方余额
	senderBal, _ := PebbleStore.GetMrc20AccountBalance(chainName, senderAddress, tickId)
	if senderBal != nil {
		fmt.Printf("  发送方最终余额: Balance=%s, PendingIn=%s, PendingOut=%s\n",
			senderBal.Balance.String(), senderBal.PendingIn.String(), senderBal.PendingOut.String())
	} else {
		fmt.Println("  发送方余额: 未找到")
	}

	// 验证接收方余额
	receiverBal, _ := PebbleStore.GetMrc20AccountBalance(chainName, receiverAddress, tickId)
	if receiverBal != nil {
		fmt.Printf("  接收方最终余额: Balance=%s, PendingIn=%s, PendingOut=%s\n",
			receiverBal.Balance.String(), receiverBal.PendingIn.String(), receiverBal.PendingOut.String())
	} else {
		fmt.Println("  接收方余额: 未找到 (可能是 bug)")
	}

	// 验证找零地址余额
	changeBal, _ := PebbleStore.GetMrc20AccountBalance(chainName, changeAddress, tickId)
	if changeBal != nil {
		fmt.Printf("  找零地址余额: Balance=%s, PendingIn=%s, PendingOut=%s\n",
			changeBal.Balance.String(), changeBal.PendingIn.String(), changeBal.PendingOut.String())
	} else {
		fmt.Println("  找零地址余额: 未找到 (可能是 bug)")
	}

	// 验证源 UTXO 状态
	finalUtxo, _ := PebbleStore.GetMrc20UtxoByTxPoint(sourceUtxoTxPoint, false)
	if finalUtxo != nil {
		fmt.Printf("  源 UTXO 最终状态: Status=%d (期望=%d Spent)\n", finalUtxo.Status, mrc20.UtxoStatusSpent)
	}

	// 验证结果
	fmt.Println("\n🔍 验证结果:")

	// 源发送方的余额应该是 0（因为 UTXO 被消耗了）
	if senderBal != nil {
		expectedSenderBalance := decimal.NewFromInt(0)
		if !senderBal.Balance.Equal(expectedSenderBalance) {
			t.Errorf("❌ 源发送方余额错误: 期望=%s, 实际=%s", expectedSenderBalance.String(), senderBal.Balance.String())
			fmt.Printf("  ❌ 源发送方余额错误: 期望=%s, 实际=%s\n", expectedSenderBalance.String(), senderBal.Balance.String())
		} else {
			fmt.Printf("  ✅ 源发送方余额正确: %s（UTXO 已花费）\n", senderBal.Balance.String())
		}
	}

	// 找零地址期望: 44
	if changeBal != nil {
		expectedChangeBalance := decimal.NewFromInt(44)
		if !changeBal.Balance.Equal(expectedChangeBalance) {
			t.Errorf("❌ 找零余额错误: 期望=%s, 实际=%s", expectedChangeBalance.String(), changeBal.Balance.String())
			fmt.Printf("  ❌ 找零余额错误: 期望=%s, 实际=%s\n", expectedChangeBalance.String(), changeBal.Balance.String())
		} else {
			fmt.Printf("  ✅ 找零余额正确: %s\n", changeBal.Balance.String())
		}
	} else {
		t.Errorf("❌ 找零地址余额未找到")
	}

	// 接收方期望: 1
	if receiverBal != nil {
		expectedReceiverBalance := decimal.NewFromInt(1)
		if !receiverBal.Balance.Equal(expectedReceiverBalance) {
			t.Errorf("❌ 接收方余额错误: 期望=%s, 实际=%s", expectedReceiverBalance.String(), receiverBal.Balance.String())
			fmt.Printf("  ❌ 接收方余额错误: 期望=%s, 实际=%s\n", expectedReceiverBalance.String(), receiverBal.Balance.String())
		} else {
			fmt.Printf("  ✅ 接收方余额正确: %s\n", receiverBal.Balance.String())
		}
	} else {
		t.Errorf("❌ 接收方余额未找到")
	}

	// 源 UTXO 应该是 Spent
	if finalUtxo != nil && finalUtxo.Status != mrc20.UtxoStatusSpent {
		t.Errorf("❌ 源 UTXO 状态错误: 期望=%d (Spent), 实际=%d", mrc20.UtxoStatusSpent, finalUtxo.Status)
		fmt.Printf("  ❌ 源 UTXO 状态错误: 期望=%d (Spent), 实际=%d\n", mrc20.UtxoStatusSpent, finalUtxo.Status)
	} else {
		fmt.Printf("  ✅ 源 UTXO 状态正确: Spent\n")
	}

	fmt.Println("\n========================================")
}

// createMockTransferTx 创建模拟的 Transfer 交易
// 交易的输入是 sourceUtxoTxPoint，输出有2个：output 0 给接收方，output 1 给发送方（找零）
func createMockTransferTx(t *testing.T, sourceUtxoTxPoint string) *btcutil.Tx {
	msgTx := wire.NewMsgTx(wire.TxVersion)

	// 解析源 UTXO 点 - 格式 "txid:vout"
	// 源 UTXO txid: 0000000000000000000000000000000000000000000000000000000000000001
	hashBytes, _ := hex.DecodeString("0100000000000000000000000000000000000000000000000000000000000000")
	prevHash, _ := chainhash.NewHash(hashBytes)
	outPoint := wire.NewOutPoint(prevHash, 0)
	txIn := wire.NewTxIn(outPoint, nil, nil)
	msgTx.AddTxIn(txIn)

	// 创建有效的 P2PKH PkScript（20 字节 pubkey hash）
	// OP_DUP OP_HASH160 <20-byte-hash> OP_EQUALVERIFY OP_CHECKSIG
	// 使用不同的 pubkey hash 来生成不同的地址

	// Output 0 的 PkScript: 给接收方
	pkScriptReceiver := []byte{
		0x76,                                                                                                                   // OP_DUP
		0xa9,                                                                                                                   // OP_HASH160
		0x14,                                                                                                                   // Push 20 bytes
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14, // 20 bytes pubkey hash -> 接收方
		0x88, // OP_EQUALVERIFY
		0xac, // OP_CHECKSIG
	}

	// Output 1 的 PkScript: 找零给发送方（使用不同的 hash）
	pkScriptSender := []byte{
		0x76,                                                                                                                   // OP_DUP
		0xa9,                                                                                                                   // OP_HASH160
		0x14,                                                                                                                   // Push 20 bytes
		0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30, 0x31, 0x32, 0x33, 0x34, // 20 bytes pubkey hash -> 发送方（不同）
		0x88, // OP_EQUALVERIFY
		0xac, // OP_CHECKSIG
	}

	// Output 0: 给接收方 (546 satoshi)
	txOut0 := wire.NewTxOut(546, pkScriptReceiver)
	msgTx.AddTxOut(txOut0)

	// Output 1: 找零给发送方 (546 satoshi)
	txOut1 := wire.NewTxOut(546, pkScriptSender)
	msgTx.AddTxOut(txOut1)

	return btcutil.NewTx(msgTx)
}

// MockIndexerAdapter 是测试用的索引器适配器
type MockIndexerAdapter struct {
	receiverAddress string
	senderAddress   string
}

func (m *MockIndexerAdapter) InitIndexer() {}

func (m *MockIndexerAdapter) CatchPins(blockHeight int64) (*[]*pin.PinInscription, *[]string, *map[string]string) {
	return nil, nil, nil
}

func (m *MockIndexerAdapter) CatchPinsByTx(msgTx interface{}, blockHeight int64, timestamp int64, blockHash string, merkleRoot string, txIndex int) []*pin.PinInscription {
	return nil
}

func (m *MockIndexerAdapter) CatchMempoolPins(txList []interface{}) ([]*pin.PinInscription, []string) {
	return nil, nil
}

func (m *MockIndexerAdapter) CatchTransfer(idMap map[string]string) map[string]*pin.PinTransferInfo {
	return nil
}

func (m *MockIndexerAdapter) GetAddress(pkScript []byte) string {
	return ""
}

func (m *MockIndexerAdapter) ZmqRun(chanMsg chan pin.MempollChanMsg) {}

func (m *MockIndexerAdapter) GetBlockTxHash(blockHeight int64) ([]string, []string) {
	return nil, nil
}

func (m *MockIndexerAdapter) ZmqHashblock() {}

// CatchNativeMrc20Transfer 模拟原生转账捕获
// 这是测试的关键函数：模拟区块确认时的原生转账处理
func (m *MockIndexerAdapter) CatchNativeMrc20Transfer(blockHeight int64, utxoList []*mrc20.Mrc20Utxo, mrc20TransferPinTx map[string]struct{}) []*mrc20.Mrc20Utxo {
	// 在真实场景中，这个函数会：
	// 1. 检查 UTXO 是否被花费
	// 2. 如果被花费，创建新的 UTXO 给输出地址
	// 这里我们模拟这个逻辑
	fmt.Printf("[MockIndexer] CatchNativeMrc20Transfer: height=%d, utxoList=%d\n", blockHeight, len(utxoList))

	// 本测试不测试 native transfer，直接返回空
	// Transfer PIN 的处理由 transferHandleWithMempool 完成
	return nil
}

func (m *MockIndexerAdapter) CatchMempoolNativeMrc20Transfer(txList []interface{}, utxoList []*mrc20.Mrc20Utxo, mrc20TransferPinTx map[string]struct{}) []*mrc20.Mrc20Utxo {
	return nil
}
