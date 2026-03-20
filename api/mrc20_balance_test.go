package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"man-p2p/man"
	"man-p2p/mrc20"
	"man-p2p/pebblestore"
)

// TestBalanceAPIWithRealDB 使用真实数据库测试 getBalanceByAddress 接口
// 场景：用户持有61，转出1给别人，找零60给自己
// 期望：可用余额 = 61 + 60 - 61 = 60
func TestBalanceAPIWithRealDB(t *testing.T) {
	// 创建临时数据库目录
	tmpDir, err := os.MkdirTemp("", "mrc20_balance_test_*")
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

	// 初始化 PebbleStore
	man.PebbleStore = &man.PebbleData{
		Database: db,
	}

	// 测试参数
	userAddress := "bc1qtest_user_address"
	receiverAddress := "bc1qtest_receiver_address"
	tickId := "test_tick_id_12345678"
	tickName := "TEST"
	chain := "btc"

	fmt.Println("\n========================================")
	fmt.Println("测试场景: 用户持有61，转出1，找零60")
	fmt.Println("========================================")

	// Step 1: 创建初始状态 - 用户有61的已确认余额和一个可用UTXO
	fmt.Println("\n📝 Step 1: 设置初始状态（用户持有61）")

	initialBalance := &mrc20.Mrc20AccountBalance{
		Address:          userAddress,
		TickId:           tickId,
		Tick:             tickName,
		Balance:          decimal.NewFromInt(61),
		PendingOut:       decimal.Zero,
		PendingIn:        decimal.Zero,
		Chain:            chain,
		LastUpdateTx:     "initial_mint_tx",
		LastUpdateHeight: 100,
		LastUpdateTime:   1704067200,
		UtxoCount:        1,
	}
	if err := man.PebbleStore.SaveMrc20AccountBalance(initialBalance); err != nil {
		t.Fatalf("保存初始余额失败: %v", err)
	}

	// 创建初始 UTXO (Status=0, Available)
	initialUtxo := mrc20.Mrc20Utxo{
		Tick:        tickName,
		Mrc20Id:     tickId,
		TxPoint:     "initial_tx:0",
		PointValue:  546,
		BlockHeight: 100,
		MrcOption:   "mint",
		FromAddress: "",
		ToAddress:   userAddress,
		AmtChange:   decimal.NewFromInt(61),
		Status:      mrc20.UtxoStatusAvailable, // 0 = Available
		Chain:       chain,
		Index:       0,
		Timestamp:   1704067200,
	}
	if err := man.PebbleStore.SaveMrc20Pin([]mrc20.Mrc20Utxo{initialUtxo}); err != nil {
		t.Fatalf("保存初始 UTXO 失败: %v", err)
	}

	// 验证初始状态
	fmt.Println("\n🔍 验证初始状态...")
	testAPI(t, userAddress, chain, tickId, map[string]string{
		"balance":    "61",
		"pendingIn":  "0",
		"pendingOut": "0",
	}, "初始状态")

	// Step 2: 模拟 mempool 转账 - 用户转出1给接收者，找零60给自己
	fmt.Println("\n📝 Step 2: 模拟 mempool 转账（转1，找零60）")

	// 2.1 更新原始 UTXO 状态为 TransferPending
	spentUtxo := mrc20.Mrc20Utxo{
		Tick:        tickName,
		Mrc20Id:     tickId,
		TxPoint:     "initial_tx:0",
		PointValue:  546,
		BlockHeight: 100,
		MrcOption:   "transfer",
		FromAddress: userAddress, // 发送方
		ToAddress:   userAddress, // 原始接收者（不变）
		AmtChange:   decimal.NewFromInt(61),
		Status:      mrc20.UtxoStatusTransferPending, // 2 = TransferPending
		Chain:       chain,
		Index:       0,
		Timestamp:   1704067200,
		OperationTx: "transfer_tx",
	}
	if err := man.PebbleStore.SaveMrc20Pin([]mrc20.Mrc20Utxo{spentUtxo}); err != nil {
		t.Fatalf("更新 UTXO 状态失败: %v", err)
	}

	// 2.2 创建接收者的 TransferPendingIn 记录
	receiverPendingIn := &mrc20.TransferPendingIn{
		TxPoint:     "transfer_tx:0",
		TxId:        "transfer_tx",
		ToAddress:   receiverAddress,
		TickId:      tickId,
		Tick:        tickName,
		Amount:      decimal.NewFromInt(1),
		Chain:       chain,
		FromAddress: userAddress,
		TxType:      "native_transfer",
		BlockHeight: -1, // mempool
		Timestamp:   1704070800,
	}
	if err := man.PebbleStore.SaveTransferPendingIn(receiverPendingIn); err != nil {
		t.Fatalf("保存接收者 PendingIn 失败: %v", err)
	}

	// 2.3 创建用户的找零 TransferPendingIn 记录（关键！）
	changePendingIn := &mrc20.TransferPendingIn{
		TxPoint:     "transfer_tx:1",
		TxId:        "transfer_tx",
		ToAddress:   userAddress, // 找零给自己
		TickId:      tickId,
		Tick:        tickName,
		Amount:      decimal.NewFromInt(60), // 找零金额
		Chain:       chain,
		FromAddress: userAddress,
		TxType:      "native_transfer",
		BlockHeight: -1, // mempool
		Timestamp:   1704070800,
	}
	if err := man.PebbleStore.SaveTransferPendingIn(changePendingIn); err != nil {
		t.Fatalf("保存找零 PendingIn 失败: %v", err)
	}

	// 2.4 创建找零 UTXO (Status=0, Available, 但在 mempool 中)
	changeUtxo := mrc20.Mrc20Utxo{
		Tick:        tickName,
		Mrc20Id:     tickId,
		TxPoint:     "transfer_tx:1",
		PointValue:  546,
		BlockHeight: -1, // mempool
		MrcOption:   "native_transfer",
		FromAddress: userAddress,
		ToAddress:   userAddress,
		AmtChange:   decimal.NewFromInt(60),
		Status:      mrc20.UtxoStatusAvailable, // 找零 UTXO 是可用的
		Chain:       chain,
		Index:       1,
		Timestamp:   1704070800,
		OperationTx: "transfer_tx",
	}
	if err := man.PebbleStore.SaveMrc20Pin([]mrc20.Mrc20Utxo{changeUtxo}); err != nil {
		t.Fatalf("保存找零 UTXO 失败: %v", err)
	}

	// 2.5 创建接收者的 UTXO
	receiverUtxo := mrc20.Mrc20Utxo{
		Tick:        tickName,
		Mrc20Id:     tickId,
		TxPoint:     "transfer_tx:0",
		PointValue:  546,
		BlockHeight: -1,
		MrcOption:   "native_transfer",
		FromAddress: userAddress,
		ToAddress:   receiverAddress,
		AmtChange:   decimal.NewFromInt(1),
		Status:      mrc20.UtxoStatusAvailable,
		Chain:       chain,
		Index:       0,
		Timestamp:   1704070800,
		OperationTx: "transfer_tx",
	}
	if err := man.PebbleStore.SaveMrc20Pin([]mrc20.Mrc20Utxo{receiverUtxo}); err != nil {
		t.Fatalf("保存接收者 UTXO 失败: %v", err)
	}

	// Step 3: 验证 mempool 阶段的余额
	fmt.Println("\n🔍 Step 3: 验证 mempool 阶段的余额")
	fmt.Println("期望: Balance=0 (UTXO被花费后), PendingIn=60 (找零), PendingOut=61 (被花费)")
	fmt.Println("可用余额 = Balance + PendingIn = 0 + 60 = 60")

	testAPI(t, userAddress, chain, tickId, map[string]string{
		"balance":    "0",  // 已确认可用余额 = 61 - 61 = 0
		"pendingIn":  "60", // 找零
		"pendingOut": "61", // 被花费的 UTXO
	}, "Mempool 阶段 - 用户")

	// 验证接收者
	testAPI(t, receiverAddress, chain, tickId, map[string]string{
		"balance":    "0", // 无已确认余额（只有 pendingIn）
		"pendingIn":  "1", // 待收入
		"pendingOut": "0",
	}, "Mempool 阶段 - 接收者")

	// Step 4: 模拟区块确认 (正确行为)
	// 正确的区块确认应该:
	// 1. 原 UTXO (61) 标记为 Spent
	// 2. 创建新 UTXO: 找零 (60) + 接收者 (1)
	// 3. 删除 PendingIn 记录
	fmt.Println("\n📝 Step 4: 模拟区块确认 (正确行为)")

	// 4.1 原 UTXO 标记为 Spent
	err = man.PebbleStore.MarkUtxoAsSpent("initial_tx:0", userAddress, tickId, chain, 101)
	if err != nil {
		t.Fatalf("标记 UTXO 为 Spent 失败: %v", err)
	}

	// 4.2 找零 UTXO 确认 (BlockHeight 从 -1 变为 101)
	changeUtxoConfirmed := mrc20.Mrc20Utxo{
		Tick:        tickName,
		Mrc20Id:     tickId,
		TxPoint:     "transfer_tx:1",
		PointValue:  546,
		BlockHeight: 101, // 已确认
		MrcOption:   "native_transfer",
		FromAddress: userAddress,
		ToAddress:   userAddress,
		AmtChange:   decimal.NewFromInt(60),
		Status:      mrc20.UtxoStatusAvailable, // 可用
		Chain:       chain,
		Index:       1,
		Timestamp:   1704070800,
		OperationTx: "transfer_tx",
	}
	if err := man.PebbleStore.SaveMrc20Pin([]mrc20.Mrc20Utxo{changeUtxoConfirmed}); err != nil {
		t.Fatalf("保存找零 UTXO 失败: %v", err)
	}

	// 4.3 接收者 UTXO 确认
	receiverUtxoConfirmed := mrc20.Mrc20Utxo{
		Tick:        tickName,
		Mrc20Id:     tickId,
		TxPoint:     "transfer_tx:0",
		PointValue:  546,
		BlockHeight: 101, // 已确认
		MrcOption:   "native_transfer",
		FromAddress: userAddress,
		ToAddress:   receiverAddress,
		AmtChange:   decimal.NewFromInt(1),
		Status:      mrc20.UtxoStatusAvailable,
		Chain:       chain,
		Index:       0,
		Timestamp:   1704070800,
		OperationTx: "transfer_tx",
	}
	if err := man.PebbleStore.SaveMrc20Pin([]mrc20.Mrc20Utxo{receiverUtxoConfirmed}); err != nil {
		t.Fatalf("保存接收者 UTXO 失败: %v", err)
	}

	// 4.4 删除 PendingIn 记录
	man.PebbleStore.DeleteTransferPendingIn("transfer_tx:0", receiverAddress)
	man.PebbleStore.DeleteTransferPendingIn("transfer_tx:1", userAddress)

	// 4.5 重算余额
	man.PebbleStore.RecalculateBalance(chain, userAddress, tickId)
	man.PebbleStore.RecalculateBalance(chain, receiverAddress, tickId)

	fmt.Println("\n🔍 Step 4: 验证区块确认后的余额")
	fmt.Println("期望: 用户 Balance=60, 接收者 Balance=1")

	testAPI(t, userAddress, chain, tickId, map[string]string{
		"balance":    "60", // 找零
		"pendingIn":  "0",
		"pendingOut": "0",
	}, "区块确认后 - 用户")

	testAPI(t, receiverAddress, chain, tickId, map[string]string{
		"balance":    "1",
		"pendingIn":  "0",
		"pendingOut": "0",
	}, "区块确认后 - 接收者")

	fmt.Println("\n✅ 所有测试通过!")
}

// testAPI 调用 getAddressBalance 接口并验证结果
func testAPI(t *testing.T, address, chain, tickId string, expected map[string]string, desc string) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// 注册路由
	router.GET("/api/mrc20/tick/AddressBalance", getAddressBalance)

	// 创建请求
	url := fmt.Sprintf("/api/mrc20/tick/AddressBalance?address=%s&chain=%s&tickId=%s", address, chain, tickId)
	req, _ := http.NewRequest("GET", url, nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("[%s] HTTP 状态码错误: %d", desc, w.Code)
		return
	}

	var response struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Balance    string `json:"balance"`
			PendingIn  string `json:"pendingIn"`
			PendingOut string `json:"pendingOut"`
		} `json:"data"`
	}

	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Errorf("[%s] 解析响应失败: %v, body: %s", desc, err, w.Body.String())
		return
	}

	fmt.Printf("  [%s] 结果: Balance=%s, PendingIn=%s, PendingOut=%s\n",
		desc, response.Data.Balance, response.Data.PendingIn, response.Data.PendingOut)

	// 验证结果
	if expected["balance"] != response.Data.Balance {
		t.Errorf("[%s] ❌ Balance 错误: 期望=%s, 实际=%s",
			desc, expected["balance"], response.Data.Balance)
	}
	if expected["pendingIn"] != response.Data.PendingIn {
		t.Errorf("[%s] ❌ PendingIn 错误: 期望=%s, 实际=%s",
			desc, expected["pendingIn"], response.Data.PendingIn)
	}
	if expected["pendingOut"] != response.Data.PendingOut {
		t.Errorf("[%s] ❌ PendingOut 错误: 期望=%s, 实际=%s",
			desc, expected["pendingOut"], response.Data.PendingOut)
	}
}

// TestBalanceByAddressAPI 测试 /address/balance/:address 接口
func TestBalanceByAddressAPI(t *testing.T) {
	// 创建临时数据库目录
	tmpDir, err := os.MkdirTemp("", "mrc20_balance_list_test_*")
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

	// 初始化 PebbleStore
	man.PebbleStore = &man.PebbleData{
		Database: db,
	}

	// 测试参数
	userAddress := "bc1qtest_user_balance_list"
	receiverAddress := "bc1qtest_receiver_balance_list"
	tickId := "test_tick_balance_list"
	tickName := "TLIST"
	chain := "btc"

	fmt.Println("\n========================================")
	fmt.Println("测试 /address/balance/:address 接口")
	fmt.Println("========================================")

	// 设置初始数据
	initialBalance := &mrc20.Mrc20AccountBalance{
		Address:          userAddress,
		TickId:           tickId,
		Tick:             tickName,
		Balance:          decimal.NewFromInt(61),
		PendingOut:       decimal.Zero,
		PendingIn:        decimal.Zero,
		Chain:            chain,
		LastUpdateHeight: 100,
		UtxoCount:        1,
	}
	man.PebbleStore.SaveMrc20AccountBalance(initialBalance)

	// 创建 Pending UTXO
	pendingUtxo := mrc20.Mrc20Utxo{
		Tick:        tickName,
		Mrc20Id:     tickId,
		TxPoint:     "initial_tx:0",
		FromAddress: userAddress,
		ToAddress:   userAddress,
		AmtChange:   decimal.NewFromInt(61),
		Status:      mrc20.UtxoStatusTransferPending,
		Chain:       chain,
	}
	man.PebbleStore.SaveMrc20Pin([]mrc20.Mrc20Utxo{pendingUtxo})

	// 创建找零 PendingIn
	changePendingIn := &mrc20.TransferPendingIn{
		TxPoint:     "transfer_tx:1",
		TxId:        "transfer_tx",
		ToAddress:   userAddress,
		TickId:      tickId,
		Tick:        tickName,
		Amount:      decimal.NewFromInt(60),
		Chain:       chain,
		FromAddress: userAddress,
		BlockHeight: -1,
	}
	man.PebbleStore.SaveTransferPendingIn(changePendingIn)

	// 接收者 PendingIn
	receiverPendingIn := &mrc20.TransferPendingIn{
		TxPoint:     "transfer_tx:0",
		TxId:        "transfer_tx",
		ToAddress:   receiverAddress,
		TickId:      tickId,
		Tick:        tickName,
		Amount:      decimal.NewFromInt(1),
		Chain:       chain,
		FromAddress: userAddress,
		BlockHeight: -1,
	}
	man.PebbleStore.SaveTransferPendingIn(receiverPendingIn)

	// 测试接口
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/mrc20/address/balance/:address", getBalanceByAddress)

	// 测试用户余额
	fmt.Println("\n🔍 测试用户余额列表接口")
	fmt.Println("期望: balance=0（已确认余额61被花费后=0）, pendingIn=60（找零）, pendingOut=61（被花费）")
	url := fmt.Sprintf("/api/mrc20/address/balance/%s?chain=%s", userAddress, chain)
	req, _ := http.NewRequest("GET", url, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	fmt.Printf("  响应: %s\n", w.Body.String())

	if w.Code != http.StatusOK {
		t.Errorf("HTTP 状态码错误: %d", w.Code)
	}

	// 解析用户响应并验证
	var userResp struct {
		Code int `json:"code"`
		Data struct {
			List []struct {
				Balance           string `json:"balance"`
				PendingInBalance  string `json:"pendingInBalance"`
				PendingOutBalance string `json:"pendingOutBalance"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &userResp); err != nil {
		t.Errorf("解析用户响应失败: %v", err)
	} else if len(userResp.Data.List) > 0 {
		// 验证 balance 是已确认可用余额（61 - 61 = 0，因为 UTXO 被花费了）
		if userResp.Data.List[0].Balance != "0" {
			t.Errorf("❌ 用户 balance 应为 0（UTXO 已被花费），实际=%s", userResp.Data.List[0].Balance)
		} else {
			fmt.Println("  ✅ balance=0（UTXO 被花费后正确）")
		}
		if userResp.Data.List[0].PendingInBalance != "60" {
			t.Errorf("❌ 用户 pendingInBalance 应为 60，实际=%s", userResp.Data.List[0].PendingInBalance)
		} else {
			fmt.Println("  ✅ pendingInBalance=60（找零正确）")
		}
		if userResp.Data.List[0].PendingOutBalance != "61" {
			t.Errorf("❌ 用户 pendingOutBalance 应为 61，实际=%s", userResp.Data.List[0].PendingOutBalance)
		} else {
			fmt.Println("  ✅ pendingOutBalance=61（被花费金额正确）")
		}
	}

	// 验证接收者
	fmt.Println("\n🔍 测试接收者余额列表接口")
	fmt.Println("期望: balance=0（没有已确认余额）, pendingIn=1（待转入）, pendingOut=0")
	url = fmt.Sprintf("/api/mrc20/address/balance/%s?chain=%s", receiverAddress, chain)
	req, _ = http.NewRequest("GET", url, nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	fmt.Printf("  响应: %s\n", w.Body.String())

	// 解析接收者响应并验证
	var receiverResp struct {
		Code int `json:"code"`
		Data struct {
			List []struct {
				Balance           string `json:"balance"`
				PendingInBalance  string `json:"pendingInBalance"`
				PendingOutBalance string `json:"pendingOutBalance"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &receiverResp); err != nil {
		t.Errorf("解析接收者响应失败: %v", err)
	} else if len(receiverResp.Data.List) > 0 {
		// 接收者 balance 应为 0（没有已确认余额）
		if receiverResp.Data.List[0].Balance != "0" {
			t.Errorf("❌ 接收者 balance 应为 0（无已确认余额），实际=%s", receiverResp.Data.List[0].Balance)
		} else {
			fmt.Println("  ✅ balance=0（无已确认余额正确）")
		}
		if receiverResp.Data.List[0].PendingInBalance != "1" {
			t.Errorf("❌ 接收者 pendingInBalance 应为 1，实际=%s", receiverResp.Data.List[0].PendingInBalance)
		} else {
			fmt.Println("  ✅ pendingInBalance=1（待转入正确）")
		}
	}

	fmt.Println("\n✅ 测试完成!")
}

// TestBugReproduction_HandleNativTransferWronglyRestoresUTXO 复现生产环境BUG
// 场景：用户持有40，使用 Transfer PIN 转出1
// BUG：handleNativTransfer 错误地把 TransferPending UTXO 恢复为 Available
func TestBugReproduction_HandleNativTransferWronglyRestoresUTXO(t *testing.T) {
	// 创建临时数据库目录
	tmpDir, err := os.MkdirTemp("", "mrc20_bug_test_*")
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

	man.PebbleStore = &man.PebbleData{
		Database: db,
	}

	// 测试参数
	userAddress := "DDnnM5GP3o87EDL42PifPMzrtB7xSZhhVg"
	receiverAddress := "DPJekPpbkdefXcvUwQJrhjwz9zuS4VTkGo"
	tickId := "0c5f575e39b1e640a0457df39939562fac899da658effaa14b740b072f863d13i0"
	tickName := "TEST"
	chain := "doge"

	fmt.Println("\n========================================")
	fmt.Println("BUG 复现: handleNativTransfer 错误恢复 Transfer PIN UTXO")
	fmt.Println("========================================")

	// Step 1: 初始状态 - 用户有40的余额
	fmt.Println("\n📝 Step 1: 设置初始状态（用户持有40）")
	initialBalance := &mrc20.Mrc20AccountBalance{
		Address:          userAddress,
		TickId:           tickId,
		Tick:             tickName,
		Balance:          decimal.NewFromInt(40),
		Chain:            chain,
		LastUpdateHeight: 100,
		UtxoCount:        1,
	}
	man.PebbleStore.SaveMrc20AccountBalance(initialBalance)

	initialUtxo := mrc20.Mrc20Utxo{
		Tick:        tickName,
		Mrc20Id:     tickId,
		TxPoint:     "source_tx:0",
		PointValue:  546,
		BlockHeight: 100,
		MrcOption:   "mint",
		ToAddress:   userAddress,
		AmtChange:   decimal.NewFromInt(40),
		Status:      mrc20.UtxoStatusAvailable,
		Chain:       chain,
	}
	man.PebbleStore.SaveMrc20Pin([]mrc20.Mrc20Utxo{initialUtxo})

	testAPI(t, userAddress, chain, tickId, map[string]string{
		"balance":    "40",
		"pendingIn":  "0",
		"pendingOut": "0",
	}, "初始状态")

	// Step 2: mempool 处理 Transfer PIN - 用户转出1
	fmt.Println("\n📝 Step 2: mempool 处理 Transfer PIN（转1给接收者）")

	// 原 UTXO 标记为 TransferPending
	pendingUtxo := mrc20.Mrc20Utxo{
		Tick:        tickName,
		Mrc20Id:     tickId,
		TxPoint:     "source_tx:0",
		PointValue:  546,
		BlockHeight: 100,
		MrcOption:   "transfer",
		FromAddress: userAddress,
		ToAddress:   userAddress,
		AmtChange:   decimal.NewFromInt(40),
		Status:      mrc20.UtxoStatusTransferPending,
		Chain:       chain,
		OperationTx: "transfer_pin_tx",
	}
	man.PebbleStore.SaveMrc20Pin([]mrc20.Mrc20Utxo{pendingUtxo})

	// 创建新 UTXO (接收者)
	receiverUtxo := mrc20.Mrc20Utxo{
		Tick:        tickName,
		Mrc20Id:     tickId,
		TxPoint:     "transfer_pin_tx:0",
		PointValue:  546,
		BlockHeight: -1,
		MrcOption:   "transfer",
		FromAddress: userAddress,
		ToAddress:   receiverAddress,
		AmtChange:   decimal.NewFromInt(1),
		Status:      mrc20.UtxoStatusAvailable,
		Chain:       chain,
		OperationTx: "transfer_pin_tx",
	}
	man.PebbleStore.SaveMrc20Pin([]mrc20.Mrc20Utxo{receiverUtxo})

	// 创建找零 UTXO
	changeUtxo := mrc20.Mrc20Utxo{
		Tick:        tickName,
		Mrc20Id:     tickId,
		TxPoint:     "transfer_pin_tx:1",
		PointValue:  546,
		BlockHeight: -1,
		MrcOption:   "transfer",
		FromAddress: userAddress,
		ToAddress:   userAddress,
		AmtChange:   decimal.NewFromInt(39),
		Status:      mrc20.UtxoStatusAvailable,
		Chain:       chain,
		OperationTx: "transfer_pin_tx",
	}
	man.PebbleStore.SaveMrc20Pin([]mrc20.Mrc20Utxo{changeUtxo})

	// 创建 PendingIn 记录
	man.PebbleStore.SaveTransferPendingIn(&mrc20.TransferPendingIn{
		TxPoint:     "transfer_pin_tx:0",
		TxId:        "transfer_pin_tx",
		ToAddress:   receiverAddress,
		TickId:      tickId,
		Amount:      decimal.NewFromInt(1),
		Chain:       chain,
		FromAddress: userAddress,
		BlockHeight: -1,
	})
	man.PebbleStore.SaveTransferPendingIn(&mrc20.TransferPendingIn{
		TxPoint:     "transfer_pin_tx:1",
		TxId:        "transfer_pin_tx",
		ToAddress:   userAddress,
		TickId:      tickId,
		Amount:      decimal.NewFromInt(39),
		Chain:       chain,
		FromAddress: userAddress,
		BlockHeight: -1,
	})

	testAPI(t, userAddress, chain, tickId, map[string]string{
		"balance":    "40",
		"pendingIn":  "39",
		"pendingOut": "40",
	}, "Mempool 阶段")

	// Step 3: 模拟 BUG - handleNativTransfer 错误地恢复 UTXO
	fmt.Println("\n📝 Step 3: 模拟 BUG - handleNativTransfer 错误恢复 UTXO")
	fmt.Println("生产环境中 CleanMempoolNativeTransfer 会把 TransferPending 恢复为 Available")

	// 错误行为：恢复原 UTXO 为 Available
	wronglyRestoredUtxo := mrc20.Mrc20Utxo{
		Tick:        tickName,
		Mrc20Id:     tickId,
		TxPoint:     "source_tx:0",
		PointValue:  546,
		BlockHeight: 100,
		MrcOption:   "mint",
		ToAddress:   userAddress,
		AmtChange:   decimal.NewFromInt(40),
		Status:      mrc20.UtxoStatusAvailable, // 错误！应该是 Spent
		Chain:       chain,
	}
	man.PebbleStore.SaveMrc20Pin([]mrc20.Mrc20Utxo{wronglyRestoredUtxo})

	// 删除 mempool 产物 (模拟 CleanMempoolNativeTransfer)
	man.PebbleStore.DeleteTransferPendingIn("transfer_pin_tx:0", receiverAddress)
	man.PebbleStore.DeleteTransferPendingIn("transfer_pin_tx:1", userAddress)

	// 重算余额
	man.PebbleStore.RecalculateBalance(chain, userAddress, tickId)

	fmt.Println("\n🔍 Step 3: 验证 BUG 结果")
	fmt.Println("期望: Balance=39 (正确行为)")
	fmt.Println("实际: Balance=40+39=79 或更多 (BUG - 原 UTXO 被恢复)")

	// 获取实际余额
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/mrc20/tick/AddressBalance", getAddressBalance)
	url := fmt.Sprintf("/api/mrc20/tick/AddressBalance?address=%s&chain=%s&tickId=%s", userAddress, chain, tickId)
	req, _ := http.NewRequest("GET", url, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	fmt.Printf("  实际响应: %s\n", w.Body.String())

	// 这个测试展示了 BUG：余额不是 39
	var response struct {
		Data struct {
			Balance string `json:"balance"`
		} `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &response)

	if response.Data.Balance == "39" {
		fmt.Println("\n✅ 没有 BUG (余额正确为39)")
	} else {
		fmt.Printf("\n❌ BUG 复现成功: 余额=%s (应该是39)\n", response.Data.Balance)
		fmt.Println("原因: handleNativTransfer 错误地把 Transfer PIN 的 UTXO 恢复为 Available")
	}
}
