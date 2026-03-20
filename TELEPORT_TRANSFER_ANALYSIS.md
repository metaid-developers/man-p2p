# Teleport Transfer 流程分析

## 核心流程概述

Teleport Transfer使用**V2状态机架构**，分为6个步骤：

### Step 1: Lock Source UTXO (锁定源UTXO)
**代码位置**: `mrc20_teleport_v2.go:166-262`

**逻辑**:
1. **Pre-check**: 先验证Arrival是否存在（避免锁定后发现Arrival不存在）
2. 验证Arrival数据匹配（tickId, amount, chain）
3. 查找源UTXO（通过交易输入）
4. 验证UTXO状态必须是Available(0)
5. **标记为TeleportPending(1)** - 这是关键状态变更
6. 写入pending流水（teleport_pending）

**UTXO状态**: Available(0) → **TeleportPending(1)**

**余额影响**:
- 源链Balance: 不变（TeleportPending不计入balance）
- 源链PendingOut: +amount（TeleportPending UTXO计入pendingOut）

### Step 2: Verify Arrival (验证到达并创建目标链PendingIn)
**代码位置**: `mrc20_teleport_v2.go:263-309`

**逻辑**:
1. 获取并验证Arrival记录
2. 验证assetOutpoint匹配
3. **创建TeleportPendingIn记录** - 用于目标链pendingIn计算

**余额影响**:
- 目标链PendingIn: +amount（通过TeleportPendingIn记录）

### Step 3: Mark Source Spent (标记源UTXO为Spent)
**代码位置**: `mrc20_teleport_v2.go:311-340`

**时机**: **区块确认后执行** (GenesisHeight > 0)

**逻辑**:
1. 验证源UTXO状态是TeleportPending(1)
2. **标记为Spent(-1)** - 关键状态变更
3. 不删除UTXO，保留审计记录

**UTXO状态**: TeleportPending(1) → **Spent(-1)**

**余额影响**:
- 源链Balance: 不变
- 源链PendingOut: -amount（TeleportPending UTXO被标记为Spent，不再计入pendingOut）

### Step 4: Create Target UTXO (创建目标UTXO)
**代码位置**: `mrc20_teleport_v2.go:343-403`

**逻辑**:
1. 从Arrival获取目标链信息
2. 生成targetOutpoint: `arrivalTxId:locationIndex`
3. 检查是否已存在（幂等性）
4. **创建新UTXO，状态为Available(0)**

**UTXO状态**: 无 → **Available(0)**

**余额影响**:
- 目标链Balance: +amount（新UTXO计入balance）
- 目标链PendingIn: 仍然存在（TeleportPendingIn记录还未删除）

### Step 5: Update Balances (验证UTXO状态)
**代码位置**: `mrc20_teleport_v2.go:405-450`

**逻辑**:
1. 验证源UTXO是Spent(-1)
2. 验证目标UTXO是Available(0)
3. 验证金额匹配

**说明**: 新架构不需要更新余额表，余额通过UTXO状态实时计算

### Step 6: Finalize (完成Teleport)
**代码位置**: `mrc20_teleport_v2.go:452-560`

**逻辑**:
1. 写入源链流水（teleport_out）
2. 写入目标链流水（teleport_in）
3. 更新Arrival状态为Completed
4. **删除TeleportPendingIn记录** - 关键操作
5. 保存Teleport完成记录
6. 状态转换到Completed

**余额影响**:
- 目标链PendingIn: -amount（TeleportPendingIn记录被删除）

---

## 余额计算逻辑

### 源链余额
```
Balance = sum(UTXO where status=Available)
PendingOut = sum(UTXO where status=TeleportPending) + sum(UTXO where status=TransferPending && AmtChange<0)
```

**Teleport过程**:
- Step1锁定后: Balance不变, PendingOut+amount
- Step3出块后: Balance不变, PendingOut-amount (UTXO变为Spent)

### 目标链余额
```
Balance = sum(UTXO where status=Available)
PendingIn = TeleportPendingIn + TransferPendingIn
```

**Teleport过程**:
- Step2验证后: Balance不变, PendingIn+amount
- Step4创建后: Balance+amount, PendingIn仍+amount (重复计算!)
- Step6完成后: Balance+amount, PendingIn-amount (删除记录)

---

## 🚨 发现的问题

### 问题1: 目标链PendingIn重复计算
**影响阶段**: Step4到Step6之间

**现象**:
- Step4创建目标UTXO（Available状态）→ Balance+amount
- TeleportPendingIn记录仍存在 → PendingIn+amount
- **结果**: 资金被计算两次！

**示例**:
```
初始: Balance=1000, PendingIn=0
Step4后: Balance=1100 (新UTXO), PendingIn=100 (TeleportPendingIn)
Total = 1100 + 100 = 1200 ❌ (实际应该是1100)
Step6后: Balance=1100, PendingIn=0 ✅
```

**根本原因**: TeleportPendingIn应该在Step4创建UTXO时就删除，而不是等到Step6

### 问题2: 源链TeleportPending的balance计算
**影响阶段**: Step1锁定后

**当前逻辑**:
- TeleportPending UTXO: 只计入pendingOut，不计入balance

**问题**:
- 用户看到Balance减少，但实际资金还未离开链
- 与TransferPending的处理不一致（TransferPending同时计入balance和pendingOut）

**预期行为**:
```
原6000 Available → TeleportPending
mempool: Balance=6000 (保持), PendingOut=6000
block: Balance=0 (真正消失), PendingOut=0
```

### 问题3: Mempool阶段的处理
**代码位置**: `mrc20_teleport_v2.go:136-138`

**当前逻辑**:
```go
if pinNode.GenesisHeight <= 0 {
    log.Printf("[TeleportV2] ⏸️  Mempool stage completed, waiting for block confirmation")
    return SaveTeleportTransaction(tx)
}
```

**问题**: Step1-Step2在mempool阶段执行，但源UTXO已锁定为TeleportPending

**影响**:
- 用户在mempool阶段就看到balance减少
- 如果区块回滚，需要回滚UTXO状态

---

## 建议修复

### 修复1: TeleportPendingIn删除时机
**文件**: `mrc20_teleport_v2.go:343-403` (Step 4)

在创建目标UTXO之后，立即删除TeleportPendingIn记录：
```go
// 5. 保存目标UTXO
if err := PebbleStore.SaveMrc20Pin([]mrc20.Mrc20Utxo{newUtxo}); err != nil {
    return fmt.Errorf("save target UTXO failed: %w", err)
}

// 6. 删除TeleportPendingIn记录（避免重复计算）
if err := PebbleStore.DeleteTeleportPendingIn(tx.Coord, tx.ToAddress); err != nil {
    log.Printf("[TeleportV2] ⚠️  Failed to delete PendingIn: %v", err)
}
```

### 修复2: TeleportPending的balance计算
**文件**: `man/mrc20_balance_utxo.go:67-69`

修改TeleportPending的计算逻辑，与TransferPending保持一致：
```go
case mrc20.UtxoStatusTeleportPending:
    // Teleport pending: 资金还在账上，但准备跃迁
    balance.Balance = balance.Balance.Add(utxo.AmtChange)
    balance.PendingOut = balance.PendingOut.Add(utxo.AmtChange)
    balance.PendingUtxos++
```

### 修复3: Mempool阶段保护
考虑在mempool阶段不锁定UTXO，只记录pending transaction流水，等区块确认后再执行状态机。

---

## 测试建议

### 测试场景1: Mempool到Block的完整流程
1. Teleport进入mempool
2. 检查源链: Balance保持, PendingOut正确
3. 检查目标链: Balance不变, PendingIn正确
4. 出块确认
5. 检查源链: Balance减少, PendingOut清零
6. 检查目标链: Balance增加, PendingIn清零

### 测试场景2: 并发Teleport
1. 同时发起多个teleport
2. 验证UTXO状态不会冲突
3. 验证余额计算正确

### 测试场景3: Teleport回滚
1. Teleport在mempool
2. 区块回滚
3. 验证UTXO状态恢复
4. 验证余额计算正确
