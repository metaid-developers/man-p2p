# Teleport PendingIn 问题诊断指南

## 问题描述
发送方（BTC）有 `pendingOutBalance: 2000`，但接收方（目标链）没有 `pendingInBalance`。

## 已发现的问题

### 1. SaveTeleportPendingIn 错误被忽略 ✅ 已修复
**位置**: `man/mrc20_teleport_v2.go:329-331`

**问题**: 
```go
if err := PebbleStore.SaveTeleportPendingIn(pendingIn); err != nil {
    log.Printf("[TeleportV2] ⚠️  Failed to save PendingIn: %v", err)  // 只打印日志，没有返回错误
}
```

**修复**: 改为返回错误
```go
if err := PebbleStore.SaveTeleportPendingIn(pendingIn); err != nil {
    return fmt.Errorf("failed to save PendingIn: %w", err)
}
```

### 2. AssetOutpoint 验证可能失败
**位置**: `man/mrc20_teleport_v2.go:310-312`

**问题**: Step 2 中会验证 `arrival.AssetOutpoint == tx.SourceOutpoint`，如果不匹配会返回错误，导致 PendingIn 不会被创建。

**可能原因**:
- Arrival 中声明的 `assetOutpoint` 与 Transfer 实际使用的源 UTXO 不一致
- UTXO 查找逻辑选择了错误的 UTXO

## 诊断步骤

### 步骤 1: 检查 TeleportTransaction 状态

查询所有处于 SourceLocked 状态的 Teleport（说明 Step 1 完成但 Step 2 失败）：

```bash
curl "http://localhost:8001/api/mrc20/admin/teleport/v2/list?state=1&limit=100"
```

状态说明：
- `State = 0`: Created（刚创建）
- `State = 1`: SourceLocked（Step 1 完成，PendingOut 已创建）
- `State = 2`: ArrivalVerified（Step 2 完成，PendingIn 已创建）
- `State = -1`: Failed（失败）

### 步骤 2: 检查失败原因

查看 TeleportTransaction 详情：

```bash
curl "http://localhost:8001/api/mrc20/admin/teleport/v2/detail/{teleportId}"
```

关注 `stateHistory` 中的错误信息，特别是包含 "assetOutpoint mismatch" 的错误。

### 步骤 3: 检查 Arrival 和 Transfer 的 AssetOutpoint

对于有问题的 Teleport，检查：

1. **Arrival 声明的 assetOutpoint**：
```bash
# 查询 Arrival
curl "http://localhost:8001/api/mrc20/arrival/{arrivalPinId}"
```

2. **Transfer 实际使用的 sourceOutpoint**：
从 TeleportTransaction 详情中查看 `sourceOutpoint` 字段。

3. **比对两者是否一致**

### 步骤 4: 检查 PendingIn 记录

查询目标链某个地址的 PendingIn：

```bash
curl "http://localhost:8001/api/mrc20/balance/{chain}/{tickId}/{address}"
```

查看响应中的 `pendingInBalance` 和 `pendingInDetails`。

## 常见原因分析

### 原因 1: Transfer 使用了错误的源 UTXO

**症状**: `assetOutpoint mismatch` 错误

**原因**: 
- 用户有多个相同金额的 UTXO
- `findTeleportSourceUtxo` 函数选择了与 Arrival 声明不同的 UTXO

**解决方案**: 
- 用户需要确保 Arrival 声明的 `assetOutpoint` 与 Transfer 交易实际消耗的 UTXO 一致
- 如果有多个 UTXO，Transfer 交易应该明确使用 Arrival 声明的那个 UTXO

### 原因 2: SaveTeleportPendingIn 失败

**症状**: 日志中有 "Failed to save PendingIn" 但状态仍然变为 ArrivalVerified

**原因**: 数据库写入失败（磁盘空间、权限等）

**解决方案**: 
- 检查磁盘空间
- 检查数据库文件权限
- 查看详细错误日志

### 原因 3: Transfer 先到达，Arrival 未触发重新处理

**症状**: Transfer 处于 Created (0) 状态，没有 PendingOut，也没有 PendingIn

**原因**: 双向等待机制中的问题

**解决方案**: 
- 检查 PendingTeleport 队列是否有该 Transfer
- 等待 Arrival 到达后自动触发
- 或手动触发重试：`RetryPendingTeleports()`

## 重新处理

如果确认是代码问题（已修复），需要重新处理已有的 Teleport：

### 方案 1: 手动触发重试（推荐）

等待下一个区块出块，系统会自动调用 `RetryPendingTeleports()` 重新处理。

### 方案 2: API 触发（如果有）

```bash
curl -X POST "http://localhost:8001/api/mrc20/admin/teleport/retry"
```

### 方案 3: 重新索引（最后手段）

如果数据已错误，可能需要：
1. 备份数据库
2. 清理错误的 TeleportTransaction 记录
3. 重新处理相关区块

## 监控建议

在日志中搜索以下关键词：

1. **AssetOutpoint 不匹配**:
```bash
grep "AssetOutpoint mismatch" /path/to/log
grep "assetOutpoint mismatch" /path/to/log
```

2. **PendingIn 创建失败**:
```bash
grep "Failed to save PendingIn" /path/to/log
grep "failed to save PendingIn" /path/to/log
```

3. **Step 2 失败**:
```bash
grep "Step 2:" /path/to/log
grep "Step failed: state=SourceLocked" /path/to/log
```

## 验证修复

修复后，检查：

1. ✅ 新的 Teleport 应该同时有 PendingOut 和 PendingIn
2. ✅ 发送方 `pendingOutBalance` = 接收方 `pendingInBalance`
3. ✅ 完成 Teleport 后，PendingOut 和 PendingIn 都应该被清除，转为实际余额

## 联系信息

如果问题持续存在，请提供：
- TeleportTransaction ID
- Arrival PIN ID
- Transfer Transaction ID
- 相关日志片段
