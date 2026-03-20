# Teleport Pending Balance 修复

## 🐛 问题描述

**症状**：
- Teleport 执行后，只有发送方的 `pendingOutBalance` 增加
- 接收方没有 `pendingInBalance`
- 资产卡在发送方，无法完成跨链转移

**影响范围**：
- 所有使用 V2 架构的 teleport 交易
- 当 Arrival PIN 还未被索引时触发

---

## 🔍 根本原因

### 原始流程（有缺陷）

```
Step 1: stepLockSourceUTXO (Created → SourceLocked)
  ├─ 锁定源 UTXO ✅
  ├─ 更新源链余额: Balance -= amount, PendingOut += amount ✅
  └─ 写入 pending 流水 ✅

Step 2: stepVerifyArrival (SourceLocked → ArrivalVerified)
  ├─ 尝试获取 Arrival
  ├─ ❌ 如果 Arrival 不存在 → 返回错误
  ├─ 状态机停止，标记为 Failed
  └─ ❌ 不会回滚 Step 1 的余额变更
```

**问题**：
1. Step 1 已经更新了源链的 `PendingOut`
2. Step 2 失败（Arrival 不存在），状态机停止
3. `shouldRollback()` 只在 `state >= TeleportStateTargetCreated` 时才回滚
4. 源链的 `PendingOut` 已增加，但目标链的 `PendingIn` 从未创建
5. **结果**：资产卡在 pending 状态，无法完成也无法回滚

---

## ✅ 修复方案

### 修复后的流程

```
Step 1: stepLockSourceUTXO (Created → SourceLocked)
  ├─ ⭐ 新增：Pre-check - 验证 Arrival 存在
  │   ├─ 检查 Arrival 是否存在
  │   ├─ 验证 Arrival 状态为 Pending
  │   ├─ 验证 tickId, amount, chain 匹配
  │   └─ 如果任何验证失败 → 立即返回错误（不锁定资产）
  │
  ├─ ✅ Pre-check 通过后，才执行锁定操作
  ├─ 锁定源 UTXO
  ├─ 更新源链余额: Balance -= amount, PendingOut += amount
  └─ 写入 pending 流水

Step 2: stepVerifyArrival (SourceLocked → ArrivalVerified)
  ├─ 再次确认 Arrival 存在（防御性编程）
  ├─ 验证 assetOutpoint 匹配
  ├─ 创建目标链 TeleportPendingIn
  ├─ 更新目标链余额: PendingIn += amount
  └─ 写入目标链 pending 流水
```

**关键改进**：
- ✅ 在锁定资产**之前**先验证 Arrival 存在
- ✅ 如果 Arrival 不存在，**不会锁定任何资产**
- ✅ 避免了资产卡在 pending 状态的问题

---

## 📝 修改内容

### 文件：`man/mrc20_teleport_v2.go`

#### 修改 1: stepLockSourceUTXO - 添加 Pre-check

```go
// stepLockSourceUTXO 步骤1: 锁定源UTXO
func stepLockSourceUTXO(tx *mrc20.TeleportTransaction, ...) error {
    // ⚠️ 关键修复：在锁定资产前，先验证Arrival存在
    log.Printf("[TeleportV2] Pre-check: Verifying arrival exists before locking")
    arrival, err := PebbleStore.GetMrc20ArrivalByPinId(tx.Coord)
    if err != nil {
        return fmt.Errorf("arrival not found (pre-check failed): %w", err)
    }

    // 验证 Arrival 状态和数据匹配
    if arrival.Status != mrc20.ArrivalStatusPending {
        return fmt.Errorf("arrival not pending: status=%d", arrival.Status)
    }
    if arrival.TickId != tx.TickId { ... }
    if !arrival.Amount.Equal(tx.Amount) { ... }
    if arrival.Chain != tx.TargetChain { ... }

    log.Printf("[TeleportV2] ✅ Pre-check passed")

    // Pre-check 通过后，才执行锁定操作
    sourceUtxo, err := findTeleportSourceUtxo(...)
    // ... 后续锁定逻辑
}
```

#### 修改 2: stepVerifyArrival - 简化逻辑

```go
// stepVerifyArrival 步骤2: 验证Arrival（创建目标链PendingIn）
func stepVerifyArrival(tx *mrc20.TeleportTransaction, ...) error {
    // 再次确认 Arrival 存在（防御性编程）
    arrival, err := PebbleStore.GetMrc20ArrivalByPinId(tx.Coord)

    // 验证 assetOutpoint 匹配（这个在step1无法验证）
    if arrival.AssetOutpoint != tx.SourceOutpoint {
        return fmt.Errorf("assetOutpoint mismatch: ...")
    }

    // 创建目标链 PendingIn 和更新余额
    // ...
}
```

---

## 🔄 处理已卡住的 Teleport

如果已经有 teleport 卡在了 `SourceLocked` 状态：

### 方案 1: 等待自动恢复

系统会自动重试（每个区块处理后）：
1. 检查 Arrival 是否已被索引
2. 如果 Arrival 存在，继续执行 Step 2
3. 创建目标链 PendingIn

### 方案 2: 手动查看状态

```bash
# 查看卡住的 teleport
curl "http://localhost:7777/api/mrc20/admin/teleport/v2/list"

# 查看详情
curl "http://localhost:7777/api/mrc20/admin/teleport/v2/detail/:id"
```

检查：
- `state`: 如果是 `1` (SourceLocked)，说明卡在了 Step 2
- `stateHistory`: 查看失败原因
- `coord`: Arrival PIN ID

### 方案 3: 检查 Arrival

```bash
# 检查 Arrival 是否存在
curl "http://localhost:7777/api/mrc20/admin/teleport/check-arrival/:coord"
```

如果 Arrival 已存在：
- 等待下一个区块，系统会自动重试
- 或重启服务，触发 `RetryStuckTeleports()`

如果 Arrival 不存在：
- 检查链上交易是否真的存在
- 检查 Arrival 交易是否被正确索引
- 可能需要手动 reindex

---

## 🧪 测试验证

### 测试场景 1: 正常流程

```
1. 创建 Arrival PIN (DOGE)
2. 等待 Arrival 被索引
3. 创建 Teleport PIN (BTC)
4. 验证：
   ✅ 源链 pendingOutBalance 增加
   ✅ 目标链 pendingInBalance 增加
   ✅ 区块确认后完成
```

### 测试场景 2: Arrival 不存在

```
1. 创建 Teleport PIN (BTC)
2. Arrival 还未创建/索引
3. 验证：
   ❌ Teleport 失败（pre-check failed）
   ✅ 源链余额不变（未锁定）
   ✅ 不会卡在 pending 状态
```

### 测试场景 3: 断点续传

```
1. 创建 Arrival 和 Teleport (mempool)
2. Teleport 执行到 SourceLocked 状态
3. 服务重启
4. 验证：
   ✅ 从 SourceLocked 继续执行
   ✅ 成功完成 teleport
```

---

## 📊 监控

### 关键日志

```bash
# 查看 pre-check 日志
grep "Pre-check" /path/to/log

# 查看 pre-check 失败
grep "pre-check failed" /path/to/log

# 查看卡住的 teleport
grep "SourceLocked" /path/to/log | grep -v "ArrivalVerified"
```

### 健康检查

定期运行：
```bash
# 查看卡住的 teleport
curl "http://localhost:7777/api/mrc20/admin/teleport/v2/list" | \
  jq '.data.list[] | select(.state == 1) | {id, coord, createdAt}'
```

如果有长时间卡在 `state=1` 的记录，检查对应的 Arrival 是否存在。

---

## ✅ 修复效果

**修复前**：
- ❌ 可能卡在 `SourceLocked` 状态
- ❌ 源链 PendingOut 增加，但目标链无 PendingIn
- ❌ 需要手动干预

**修复后**：
- ✅ 在锁定前先验证 Arrival 存在
- ✅ 如果 Arrival 不存在，立即失败（不锁定资产）
- ✅ 如果 Arrival 存在，同时更新双方 pending 余额
- ✅ 不会出现单边 pending 的情况

---

## 🚀 部署建议

1. **灰度测试**：
   - 先在 regtest 环境测试
   - 验证正常流程和异常流程
   - 确认不会产生单边 pending

2. **生产部署**：
   - 备份数据库
   - 部署新版本
   - 观察日志中的 "Pre-check" 相关信息
   - 运行资产验证：`/api/mrc20/admin/verify/all`

3. **回滚计划**：
   - 如果发现问题，设置 `UseTeleportV2 = false` 回退到 V1
   - V1 和 V2 可以共存

---

**修复时间**：2026-02-10
**影响版本**：V2.0
**修复状态**：✅ 已修复
