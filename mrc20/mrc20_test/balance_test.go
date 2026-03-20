package mrc20_test

import (
	"fmt"
	"testing"

	"github.com/shopspring/decimal"
)

// BalanceSimulator 模拟 MRC20 余额系统
type BalanceSimulator struct {
	// AccountBalance 表：存储已确认余额
	AccountBalance map[string]*AccountBalanceRecord

	// TransferPendingIn 表：存储 mempool 阶段的待转入记录
	TransferPendingIn map[string]*TransferPendingInRecord

	// UTXO 表：存储所有 UTXO
	UTXOs map[string]*UTXO
}

// AccountBalanceRecord 账户余额记录
type AccountBalanceRecord struct {
	Address   string
	TickId    string
	Balance   decimal.Decimal // 已确认余额
	UtxoCount int
}

// TransferPendingInRecord 待转入记录
type TransferPendingInRecord struct {
	TxPoint     string
	TxId        string
	ToAddress   string
	TickId      string
	Amount      decimal.Decimal
	FromAddress string
	BlockHeight int64 // -1 表示 mempool
}

// UTXO 表示一个 MRC20 UTXO
type UTXO struct {
	TxPoint     string
	ToAddress   string
	TickId      string
	Amount      decimal.Decimal
	Status      int // 0=Available, 1=TeleportPending, 2=TransferPending, -1=Spent
	FromAddress string
}

// UTXO 状态常量
const (
	StatusAvailable       = 0
	StatusTeleportPending = 1
	StatusTransferPending = 2
	StatusSpent           = -1
)

// NewBalanceSimulator 创建模拟器
func NewBalanceSimulator() *BalanceSimulator {
	return &BalanceSimulator{
		AccountBalance:    make(map[string]*AccountBalanceRecord),
		TransferPendingIn: make(map[string]*TransferPendingInRecord),
		UTXOs:             make(map[string]*UTXO),
	}
}

// key 生成账户余额的 key
func (s *BalanceSimulator) accountKey(address, tickId string) string {
	return fmt.Sprintf("%s_%s", address, tickId)
}

// SetInitialBalance 设置初始已确认余额（模拟 mint 后的状态）
func (s *BalanceSimulator) SetInitialBalance(address, tickId string, amount decimal.Decimal, utxoTxPoint string) {
	key := s.accountKey(address, tickId)
	s.AccountBalance[key] = &AccountBalanceRecord{
		Address:   address,
		TickId:    tickId,
		Balance:   amount,
		UtxoCount: 1,
	}

	// 创建初始 UTXO
	s.UTXOs[utxoTxPoint] = &UTXO{
		TxPoint:   utxoTxPoint,
		ToAddress: address,
		TickId:    tickId,
		Amount:    amount,
		Status:    StatusAvailable,
	}
}

// SimulateMempoolTransfer 模拟 mempool 阶段的转账
// fromAddress 持有 spentUtxos 中的 UTXO，转账给 recipients
func (s *BalanceSimulator) SimulateMempoolTransfer(
	txId string,
	fromAddress string,
	tickId string,
	spentUtxoPoints []string, // 被花费的 UTXO txPoints
	recipients map[string]decimal.Decimal, // toAddress -> amount
	changeAmount decimal.Decimal, // 找零金额
) error {
	fmt.Printf("\n=== Mempool 阶段: 交易 %s ===\n", txId)

	// 1. 更新被花费的 UTXO 状态为 TransferPending
	totalSpent := decimal.Zero
	for _, txPoint := range spentUtxoPoints {
		utxo, ok := s.UTXOs[txPoint]
		if !ok {
			return fmt.Errorf("UTXO not found: %s", txPoint)
		}
		if utxo.Status != StatusAvailable {
			return fmt.Errorf("UTXO not available: %s, status=%d", txPoint, utxo.Status)
		}
		utxo.Status = StatusTransferPending
		utxo.FromAddress = fromAddress
		totalSpent = totalSpent.Add(utxo.Amount)
		fmt.Printf("  [UTXO] %s 状态变更: Available -> TransferPending (金额: %s)\n", txPoint, utxo.Amount)
	}

	// 2. 为接收方创建新 UTXO 和 TransferPendingIn 记录
	vout := 0
	for toAddr, amount := range recipients {
		newTxPoint := fmt.Sprintf("%s:%d", txId, vout)
		vout++

		// 创建新 UTXO（mempool 阶段就创建，状态为 Available）
		s.UTXOs[newTxPoint] = &UTXO{
			TxPoint:     newTxPoint,
			ToAddress:   toAddr,
			TickId:      tickId,
			Amount:      amount,
			Status:      StatusAvailable,
			FromAddress: fromAddress,
		}

		// 创建 TransferPendingIn 记录
		s.TransferPendingIn[newTxPoint] = &TransferPendingInRecord{
			TxPoint:     newTxPoint,
			TxId:        txId,
			ToAddress:   toAddr,
			TickId:      tickId,
			Amount:      amount,
			FromAddress: fromAddress,
			BlockHeight: -1,
		}
		fmt.Printf("  [PendingIn] %s 收到待确认 %s (来自 %s)\n", toAddr, amount, fromAddress)
	}

	// 3. 为发送方创建找零 UTXO 和 TransferPendingIn 记录
	if changeAmount.GreaterThan(decimal.Zero) {
		changeTxPoint := fmt.Sprintf("%s:%d", txId, vout)

		// 创建找零 UTXO
		s.UTXOs[changeTxPoint] = &UTXO{
			TxPoint:     changeTxPoint,
			ToAddress:   fromAddress,
			TickId:      tickId,
			Amount:      changeAmount,
			Status:      StatusAvailable,
			FromAddress: fromAddress,
		}

		// 找零也需要 TransferPendingIn（这是修复后的逻辑）
		s.TransferPendingIn[changeTxPoint] = &TransferPendingInRecord{
			TxPoint:     changeTxPoint,
			TxId:        txId,
			ToAddress:   fromAddress,
			TickId:      tickId,
			Amount:      changeAmount,
			FromAddress: fromAddress,
			BlockHeight: -1,
		}
		fmt.Printf("  [PendingIn] %s 收到找零待确认 %s\n", fromAddress, changeAmount)
	}

	return nil
}

// SimulateBlockConfirmation 模拟出块确认
func (s *BalanceSimulator) SimulateBlockConfirmation(
	txId string,
	fromAddress string,
	tickId string,
	spentUtxoPoints []string,
	recipients map[string]decimal.Decimal,
	changeAmount decimal.Decimal,
	blockHeight int64,
) error {
	fmt.Printf("\n=== 出块确认: 区块 %d, 交易 %s ===\n", blockHeight, txId)

	// 1. 删除发送方的 spent UTXO 并更新余额 (减少)
	key := s.accountKey(fromAddress, tickId)
	for _, txPoint := range spentUtxoPoints {
		utxo := s.UTXOs[txPoint]
		if utxo != nil {
			// 更新余额
			if _, ok := s.AccountBalance[key]; ok {
				s.AccountBalance[key].Balance = s.AccountBalance[key].Balance.Sub(utxo.Amount)
				s.AccountBalance[key].UtxoCount--
				fmt.Printf("  [Balance] %s 余额减少 %s (扣除已花费 UTXO)\n", fromAddress, utxo.Amount)
			}

			// 删除 spent UTXO
			delete(s.UTXOs, txPoint)
			fmt.Printf("  [UTXO] 删除已花费 UTXO: %s\n", txPoint)
		}
	}

	// 2. 为接收方更新余额 (增加) 并删除 TransferPendingIn
	vout := 0
	for toAddr, amount := range recipients {
		newTxPoint := fmt.Sprintf("%s:%d", txId, vout)
		vout++

		toKey := s.accountKey(toAddr, tickId)
		if _, ok := s.AccountBalance[toKey]; !ok {
			s.AccountBalance[toKey] = &AccountBalanceRecord{
				Address: toAddr,
				TickId:  tickId,
				Balance: decimal.Zero,
			}
		}
		s.AccountBalance[toKey].Balance = s.AccountBalance[toKey].Balance.Add(amount)
		s.AccountBalance[toKey].UtxoCount++
		fmt.Printf("  [Balance] %s 余额增加 %s\n", toAddr, amount)

		// 删除 TransferPendingIn
		delete(s.TransferPendingIn, newTxPoint)
	}

	// 3. 处理找零
	if changeAmount.GreaterThan(decimal.Zero) {
		changeTxPoint := fmt.Sprintf("%s:%d", txId, vout)

		if _, ok := s.AccountBalance[key]; !ok {
			s.AccountBalance[key] = &AccountBalanceRecord{
				Address: fromAddress,
				TickId:  tickId,
				Balance: decimal.Zero,
			}
		}
		s.AccountBalance[key].Balance = s.AccountBalance[key].Balance.Add(changeAmount)
		s.AccountBalance[key].UtxoCount++
		fmt.Printf("  [Balance] %s 余额增加(找零) %s\n", fromAddress, changeAmount)

		// 删除找零的 TransferPendingIn
		delete(s.TransferPendingIn, changeTxPoint)
	}

	return nil
}

// GetAvailableBalance 获取可用余额 (修复后的公式)
// 可用余额 = 已确认余额 + pendingIn - pendingOut
// 这个方法模拟接口的实际逻辑
func (s *BalanceSimulator) GetAvailableBalance(address, tickId string) (available, pendingIn, pendingOut, confirmedBalance decimal.Decimal) {
	key := s.accountKey(address, tickId)

	// 获取已确认余额 (从 AccountBalance 表)
	if record, ok := s.AccountBalance[key]; ok {
		confirmedBalance = record.Balance
	}

	// 计算 pendingIn (从 TransferPendingIn 表)
	// 接口逻辑：GetTransferPendingInByAddress 查询 ToAddress == address 的记录
	for _, pending := range s.TransferPendingIn {
		if pending.ToAddress == address && pending.TickId == tickId {
			pendingIn = pendingIn.Add(pending.Amount)
		}
	}

	// 计算 pendingOut (从 UTXO 表，扫描 mrc20_in_{address}_{tickId} 前缀)
	// 接口逻辑（修复后）：utxo.ToAddress == address && Status == Pending
	// 因为 mrc20_in 索引是按 ToAddress 存储的，被花费的 UTXO 仍然保留原来的 ToAddress
	for _, utxo := range s.UTXOs {
		if utxo.ToAddress == address && utxo.TickId == tickId &&
			(utxo.Status == StatusTransferPending || utxo.Status == StatusTeleportPending) {
			// 使用绝对值
			amtAbs := utxo.Amount
			if amtAbs.LessThan(decimal.Zero) {
				amtAbs = amtAbs.Neg()
			}
			pendingOut = pendingOut.Add(amtAbs)
		}
	}

	// 可用余额 = 已确认余额 + pendingIn - pendingOut
	available = confirmedBalance.Add(pendingIn).Sub(pendingOut)
	if available.LessThan(decimal.Zero) {
		available = decimal.Zero
	}

	return
}

// PrintBalances 打印所有余额状态
func (s *BalanceSimulator) PrintBalances(addresses []string, tickId string) {
	fmt.Println("\n=== 当前余额状态 ===")
	for _, addr := range addresses {
		available, pendingIn, pendingOut, confirmed := s.GetAvailableBalance(addr, tickId)
		fmt.Printf("  %s:\n", addr)
		fmt.Printf("    已确认余额(Balance): %s\n", confirmed)
		fmt.Printf("    待转入(PendingIn):   %s\n", pendingIn)
		fmt.Printf("    待转出(PendingOut):  %s\n", pendingOut)
		fmt.Printf("    可用余额:            %s (= %s + %s - %s)\n",
			available, confirmed, pendingIn, pendingOut)
	}
}

// TestMempoolAndBlockConfirmation 测试 mempool 和出块确认流程
func TestMempoolAndBlockConfirmation(t *testing.T) {
	sim := NewBalanceSimulator()

	// 初始化: A 持有 100 代币
	addrA := "address_A"
	addrB := "address_B"
	tickId := "test_tick_001"

	fmt.Println("========================================")
	fmt.Println("测试场景: A 持有 100，转 10 给 B")
	fmt.Println("========================================")

	// 设置 A 的初始余额
	sim.SetInitialBalance(addrA, tickId, decimal.NewFromInt(100), "init_tx:0")
	fmt.Printf("\n初始化: A 持有 100 代币 (UTXO: init_tx:0)\n")

	// 打印初始状态
	sim.PrintBalances([]string{addrA, addrB}, tickId)

	// ============ Mempool 阶段 ============
	err := sim.SimulateMempoolTransfer(
		"transfer_tx_001",     // txId
		addrA,                 // fromAddress
		tickId,                // tickId
		[]string{"init_tx:0"}, // spentUtxoPoints
		map[string]decimal.Decimal{ // recipients
			addrB: decimal.NewFromInt(10),
		},
		decimal.NewFromInt(90), // changeAmount
	)
	if err != nil {
		t.Fatalf("Mempool transfer failed: %v", err)
	}

	// 打印 mempool 阶段的余额
	fmt.Println("\n【Mempool 阶段预期】")
	fmt.Println("  A: Balance=100, PendingIn=90(找零), PendingOut=100, 可用=100+90-100=90")
	fmt.Println("  B: Balance=0, PendingIn=10, PendingOut=0, 可用=0+10-0=10")

	sim.PrintBalances([]string{addrA, addrB}, tickId)

	// 验证 Mempool 阶段的余额
	availA, pendingInA, pendingOutA, confirmedA := sim.GetAvailableBalance(addrA, tickId)
	availB, pendingInB, pendingOutB, confirmedB := sim.GetAvailableBalance(addrB, tickId)

	// A 的验证
	if !confirmedA.Equal(decimal.NewFromInt(100)) {
		t.Errorf("Mempool: A 已确认余额错误, 期望 100, 实际 %s", confirmedA)
	}
	if !pendingInA.Equal(decimal.NewFromInt(90)) {
		t.Errorf("Mempool: A pendingIn 错误, 期望 90(找零), 实际 %s", pendingInA)
	}
	if !pendingOutA.Equal(decimal.NewFromInt(100)) {
		t.Errorf("Mempool: A pendingOut 错误, 期望 100, 实际 %s", pendingOutA)
	}
	if !availA.Equal(decimal.NewFromInt(90)) {
		t.Errorf("Mempool: A 可用余额错误, 期望 90, 实际 %s", availA)
	}

	// B 的验证
	if !confirmedB.Equal(decimal.Zero) {
		t.Errorf("Mempool: B 已确认余额错误, 期望 0, 实际 %s", confirmedB)
	}
	if !pendingInB.Equal(decimal.NewFromInt(10)) {
		t.Errorf("Mempool: B pendingIn 错误, 期望 10, 实际 %s", pendingInB)
	}
	if !pendingOutB.Equal(decimal.Zero) {
		t.Errorf("Mempool: B pendingOut 错误, 期望 0, 实际 %s", pendingOutB)
	}
	if !availB.Equal(decimal.NewFromInt(10)) {
		t.Errorf("Mempool: B 可用余额错误, 期望 10, 实际 %s", availB)
	}

	fmt.Println("\n✅ Mempool 阶段验证通过!")

	// ============ 出块确认阶段 ============
	err = sim.SimulateBlockConfirmation(
		"transfer_tx_001",
		addrA,
		tickId,
		[]string{"init_tx:0"},
		map[string]decimal.Decimal{
			addrB: decimal.NewFromInt(10),
		},
		decimal.NewFromInt(90),
		100, // blockHeight
	)
	if err != nil {
		t.Fatalf("Block confirmation failed: %v", err)
	}

	// 打印出块后的余额
	fmt.Println("\n【出块后预期】")
	fmt.Println("  A: Balance=90, PendingIn=0, PendingOut=0, 可用=90")
	fmt.Println("  B: Balance=10, PendingIn=0, PendingOut=0, 可用=10")

	sim.PrintBalances([]string{addrA, addrB}, tickId)

	// 验证出块后的余额
	availA, pendingInA, pendingOutA, confirmedA = sim.GetAvailableBalance(addrA, tickId)
	availB, pendingInB, pendingOutB, confirmedB = sim.GetAvailableBalance(addrB, tickId)

	// A 的验证
	if !confirmedA.Equal(decimal.NewFromInt(90)) {
		t.Errorf("出块后: A 已确认余额错误, 期望 90, 实际 %s", confirmedA)
	}
	if !pendingInA.Equal(decimal.Zero) {
		t.Errorf("出块后: A pendingIn 错误, 期望 0, 实际 %s", pendingInA)
	}
	if !pendingOutA.Equal(decimal.Zero) {
		t.Errorf("出块后: A pendingOut 错误, 期望 0, 实际 %s", pendingOutA)
	}
	if !availA.Equal(decimal.NewFromInt(90)) {
		t.Errorf("出块后: A 可用余额错误, 期望 90, 实际 %s", availA)
	}

	// B 的验证
	if !confirmedB.Equal(decimal.NewFromInt(10)) {
		t.Errorf("出块后: B 已确认余额错误, 期望 10, 实际 %s", confirmedB)
	}
	if !pendingInB.Equal(decimal.Zero) {
		t.Errorf("出块后: B pendingIn 错误, 期望 0, 实际 %s", pendingInB)
	}
	if !pendingOutB.Equal(decimal.Zero) {
		t.Errorf("出块后: B pendingOut 错误, 期望 0, 实际 %s", pendingOutB)
	}
	if !availB.Equal(decimal.NewFromInt(10)) {
		t.Errorf("出块后: B 可用余额错误, 期望 10, 实际 %s", availB)
	}

	fmt.Println("\n✅ 出块确认阶段验证通过!")
	fmt.Println("\n========================================")
	fmt.Println("所有测试通过!")
	fmt.Println("========================================")
}

// TestMultipleTransfers 测试多笔连续转账
func TestMultipleTransfers(t *testing.T) {
	sim := NewBalanceSimulator()

	addrA := "address_A"
	addrB := "address_B"
	addrC := "address_C"
	tickId := "test_tick_002"

	fmt.Println("\n========================================")
	fmt.Println("测试场景: A->B 10, B->C 5 (连续转账)")
	fmt.Println("========================================")

	// 初始化: A 持有 100 代币
	sim.SetInitialBalance(addrA, tickId, decimal.NewFromInt(100), "init_tx:0")
	fmt.Println("初始化: A 持有 100 代币")

	// =============== 第一笔交易: A 转 10 给 B ===============
	// Mempool
	sim.SimulateMempoolTransfer(
		"tx_001",
		addrA,
		tickId,
		[]string{"init_tx:0"},
		map[string]decimal.Decimal{addrB: decimal.NewFromInt(10)},
		decimal.NewFromInt(90),
	)

	// 出块确认
	sim.SimulateBlockConfirmation(
		"tx_001",
		addrA,
		tickId,
		[]string{"init_tx:0"},
		map[string]decimal.Decimal{addrB: decimal.NewFromInt(10)},
		decimal.NewFromInt(90),
		100,
	)

	fmt.Println("\n【第一笔交易后状态】")
	sim.PrintBalances([]string{addrA, addrB, addrC}, tickId)

	// =============== 第二笔交易: B 转 5 给 C ===============
	// Mempool
	sim.SimulateMempoolTransfer(
		"tx_002",
		addrB,
		tickId,
		[]string{"tx_001:0"}, // B 的 UTXO 是第一笔交易的第 0 个输出
		map[string]decimal.Decimal{addrC: decimal.NewFromInt(5)},
		decimal.NewFromInt(5),
	)

	fmt.Println("\n【第二笔交易 Mempool 阶段】")
	sim.PrintBalances([]string{addrA, addrB, addrC}, tickId)

	// 验证 B 在 mempool 阶段的余额
	availB, pendingInB, pendingOutB, confirmedB := sim.GetAvailableBalance(addrB, tickId)
	if !confirmedB.Equal(decimal.NewFromInt(10)) {
		t.Errorf("B 已确认余额错误, 期望 10, 实际 %s", confirmedB)
	}
	if !pendingInB.Equal(decimal.NewFromInt(5)) {
		t.Errorf("B pendingIn 错误, 期望 5(找零), 实际 %s", pendingInB)
	}
	if !pendingOutB.Equal(decimal.NewFromInt(10)) {
		t.Errorf("B pendingOut 错误, 期望 10, 实际 %s", pendingOutB)
	}
	if !availB.Equal(decimal.NewFromInt(5)) {
		t.Errorf("B 可用余额错误, 期望 5, 实际 %s", availB)
	}

	// 出块确认
	sim.SimulateBlockConfirmation(
		"tx_002",
		addrB,
		tickId,
		[]string{"tx_001:0"},
		map[string]decimal.Decimal{addrC: decimal.NewFromInt(5)},
		decimal.NewFromInt(5),
		101,
	)

	fmt.Println("\n【第二笔交易出块后状态】")
	sim.PrintBalances([]string{addrA, addrB, addrC}, tickId)

	// 最终验证
	availA, _, _, confirmedA := sim.GetAvailableBalance(addrA, tickId)
	availB, _, _, confirmedB = sim.GetAvailableBalance(addrB, tickId)
	availC, _, _, confirmedC := sim.GetAvailableBalance(addrC, tickId)

	if !availA.Equal(decimal.NewFromInt(90)) {
		t.Errorf("最终 A 余额错误, 期望 90, 实际 %s", availA)
	}
	if !availB.Equal(decimal.NewFromInt(5)) {
		t.Errorf("最终 B 余额错误, 期望 5, 实际 %s", availB)
	}
	if !availC.Equal(decimal.NewFromInt(5)) {
		t.Errorf("最终 C 余额错误, 期望 5, 实际 %s", availC)
	}

	// 验证总量守恒
	total := confirmedA.Add(confirmedB).Add(confirmedC)
	if !total.Equal(decimal.NewFromInt(100)) {
		t.Errorf("总量不守恒, 期望 100, 实际 %s", total)
	}

	fmt.Println("\n✅ 多笔连续转账测试通过!")
	fmt.Println("✅ 总量守恒验证通过: 100 = 90 + 5 + 5")
}

// TestMultipleOutputsTransfer 测试一笔交易多个输出
func TestMultipleOutputsTransfer(t *testing.T) {
	sim := NewBalanceSimulator()

	addrA := "address_A"
	addrB := "address_B"
	addrC := "address_C"
	tickId := "test_tick_003"

	fmt.Println("\n========================================")
	fmt.Println("测试场景: A 同时转 10 给 B, 20 给 C")
	fmt.Println("========================================")

	// 初始化: A 持有 100 代币
	sim.SetInitialBalance(addrA, tickId, decimal.NewFromInt(100), "init_tx:0")
	fmt.Println("初始化: A 持有 100 代币")

	// Mempool 阶段
	sim.SimulateMempoolTransfer(
		"tx_multi_001",
		addrA,
		tickId,
		[]string{"init_tx:0"},
		map[string]decimal.Decimal{
			addrB: decimal.NewFromInt(10),
			addrC: decimal.NewFromInt(20),
		},
		decimal.NewFromInt(70), // 找零
	)

	fmt.Println("\n【Mempool 阶段】")
	fmt.Println("预期: A 可用=100+70-100=70, B 可用=10, C 可用=20")
	sim.PrintBalances([]string{addrA, addrB, addrC}, tickId)

	// 验证
	availA, _, _, _ := sim.GetAvailableBalance(addrA, tickId)
	availB, _, _, _ := sim.GetAvailableBalance(addrB, tickId)
	availC, _, _, _ := sim.GetAvailableBalance(addrC, tickId)

	if !availA.Equal(decimal.NewFromInt(70)) {
		t.Errorf("Mempool: A 可用余额错误, 期望 70, 实际 %s", availA)
	}
	if !availB.Equal(decimal.NewFromInt(10)) {
		t.Errorf("Mempool: B 可用余额错误, 期望 10, 实际 %s", availB)
	}
	if !availC.Equal(decimal.NewFromInt(20)) {
		t.Errorf("Mempool: C 可用余额错误, 期望 20, 实际 %s", availC)
	}

	// 出块确认
	sim.SimulateBlockConfirmation(
		"tx_multi_001",
		addrA,
		tickId,
		[]string{"init_tx:0"},
		map[string]decimal.Decimal{
			addrB: decimal.NewFromInt(10),
			addrC: decimal.NewFromInt(20),
		},
		decimal.NewFromInt(70),
		100,
	)

	fmt.Println("\n【出块后】")
	sim.PrintBalances([]string{addrA, addrB, addrC}, tickId)

	// 最终验证
	availA, _, _, confirmedA := sim.GetAvailableBalance(addrA, tickId)
	availB, _, _, confirmedB := sim.GetAvailableBalance(addrB, tickId)
	availC, _, _, confirmedC := sim.GetAvailableBalance(addrC, tickId)

	if !confirmedA.Equal(decimal.NewFromInt(70)) {
		t.Errorf("出块后: A 余额错误, 期望 70, 实际 %s", confirmedA)
	}
	if !confirmedB.Equal(decimal.NewFromInt(10)) {
		t.Errorf("出块后: B 余额错误, 期望 10, 实际 %s", confirmedB)
	}
	if !confirmedC.Equal(decimal.NewFromInt(20)) {
		t.Errorf("出块后: C 余额错误, 期望 20, 实际 %s", confirmedC)
	}

	// 验证总量守恒
	total := confirmedA.Add(confirmedB).Add(confirmedC)
	if !total.Equal(decimal.NewFromInt(100)) {
		t.Errorf("总量不守恒, 期望 100, 实际 %s", total)
	}

	fmt.Println("\n✅ 多输出转账测试通过!")
}

// TestUserReportedScenario 测试用户报告的具体场景:
// 用户持有61，转出1，应该看到:
// - 原始UTXO(61)变为待支出
// - 新的找零UTXO(60)进入待收入
// - 可用余额 = Balance(61) + pendingIn(60) - pendingOut(61) = 60
func TestUserReportedScenario(t *testing.T) {
	fmt.Println("\n=== 测试用户报告场景: 持有61转出1 ===")

	sim := NewBalanceSimulator()

	userAddress := "user_address"
	receiverAddress := "receiver_address"
	tickId := "test_tick"

	// 初始状态: 用户有一个61的UTXO作为已确认余额
	sim.SetInitialBalance(userAddress, tickId, decimal.NewFromInt(61), "initial_tx:0")

	fmt.Println("\n--- 初始状态 ---")
	available, pendingIn, pendingOut, balance := sim.GetAvailableBalance(userAddress, tickId)
	fmt.Printf("Balance: %s, PendingIn: %s, PendingOut: %s, 可用: %s\n",
		balance, pendingIn, pendingOut, available)

	if !balance.Equal(decimal.NewFromInt(61)) {
		t.Errorf("初始余额错误, 期望 61, 实际 %s", balance)
	}
	if !pendingIn.Equal(decimal.Zero) {
		t.Errorf("初始 pendingIn 应为 0, 实际 %s", pendingIn)
	}
	if !pendingOut.Equal(decimal.Zero) {
		t.Errorf("初始 pendingOut 应为 0, 实际 %s", pendingOut)
	}
	if !available.Equal(decimal.NewFromInt(61)) {
		t.Errorf("初始可用余额错误, 期望 61, 实际 %s", available)
	}

	// 用户发起转账: 转1给接收者，找零60给自己
	fmt.Println("\n--- 用户发起转账: 转1给接收者 ---")
	recipients := map[string]decimal.Decimal{
		receiverAddress: decimal.NewFromInt(1),
	}
	err := sim.SimulateMempoolTransfer(
		"transfer_tx", // txId
		userAddress,   // fromAddress
		tickId,
		[]string{"initial_tx:0"}, // 消费的UTXO
		recipients,               // 接收者
		decimal.NewFromInt(60),   // 找零金额
	)
	if err != nil {
		t.Fatalf("SimulateMempoolTransfer failed: %v", err)
	}

	fmt.Println("\n--- Mempool 状态 (转账待确认) ---")
	available, pendingIn, pendingOut, balance = sim.GetAvailableBalance(userAddress, tickId)
	fmt.Printf("Balance: %s, PendingIn: %s, PendingOut: %s, 可用: %s\n",
		balance, pendingIn, pendingOut, available)

	// 验证关键值:
	// Balance 仍然是 61（数据库中的余额未变，等出块后更新）
	// pendingIn 应该是 60（找零金额，ToAddress=userAddress）
	// pendingOut 应该是 61（原始UTXO金额，ToAddress=userAddress 且被花费）
	// 可用余额 = 61 + 60 - 61 = 60

	if !balance.Equal(decimal.NewFromInt(61)) {
		t.Errorf("Mempool时 Balance 应保持 61, 实际 %s", balance)
	}
	if !pendingIn.Equal(decimal.NewFromInt(60)) {
		t.Errorf("❌ Mempool时 pendingIn 应为 60(找零), 实际 %s", pendingIn)
		t.Log("提示: 检查 SaveMempoolNativeTransferTransaction 中找零是否加入 TransferPendingIn")
	}
	if !pendingOut.Equal(decimal.NewFromInt(61)) {
		t.Errorf("❌ Mempool时 pendingOut 应为 61(花费的UTXO), 实际 %s", pendingOut)
		t.Log("提示: 检查 pendingOut 是否使用 ToAddress 而非 FromAddress")
	}
	if !available.Equal(decimal.NewFromInt(60)) {
		t.Errorf("❌ 可用余额应为 60, 实际 %s", available)
	} else {
		fmt.Println("✅ 可用余额正确: 60")
	}

	// 也验证接收者的状态
	recvAvailable, recvPendingIn, recvPendingOut, recvBalance := sim.GetAvailableBalance(receiverAddress, tickId)
	fmt.Printf("\n接收者 - Balance: %s, PendingIn: %s, PendingOut: %s, 可用: %s\n",
		recvBalance, recvPendingIn, recvPendingOut, recvAvailable)

	if !recvPendingIn.Equal(decimal.NewFromInt(1)) {
		t.Errorf("接收者 pendingIn 应为 1, 实际 %s", recvPendingIn)
	}
	if !recvAvailable.Equal(decimal.NewFromInt(1)) {
		t.Errorf("接收者可用余额应为 1, 实际 %s", recvAvailable)
	}

	// 确认交易出块
	fmt.Println("\n--- 交易确认出块 ---")
	err = sim.SimulateBlockConfirmation(
		"transfer_tx",
		userAddress,
		tickId,
		[]string{"initial_tx:0"},
		recipients,
		decimal.NewFromInt(60),
		100, // blockHeight
	)
	if err != nil {
		t.Fatalf("SimulateBlockConfirmation failed: %v", err)
	}

	available, pendingIn, pendingOut, balance = sim.GetAvailableBalance(userAddress, tickId)
	fmt.Printf("Balance: %s, PendingIn: %s, PendingOut: %s, 可用: %s\n",
		balance, pendingIn, pendingOut, available)

	if !available.Equal(decimal.NewFromInt(60)) {
		t.Errorf("确认后可用余额应为 60, 实际 %s", available)
	}

	fmt.Println("\n✅ 用户报告场景测试通过!")
}
