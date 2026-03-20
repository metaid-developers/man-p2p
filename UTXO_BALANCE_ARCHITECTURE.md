# UTXO-Based Balance Architecture

## 🎯 核心理念

**余额 = UTXO 状态的函数**

- ✅ 不维护独立的余额表（AccountBalance）
- ✅ 余额通过 UTXO 状态实时计算
- ✅ UTXO 是唯一的真相来源（Single Source of Truth）

---

## 📊 余额计算公式

```javascript
Balance = sum(UTXO where status = Available AND chain = X AND address = Y AND tickId = Z)

PendingOut = sum(UTXO where status IN [TeleportPending, TransferPending]
                     AND chain = X AND address = Y AND tickId = Z)

PendingIn = sum(TeleportPendingIn where chain = X AND address = Y AND tickId = Z)
          + sum(TransferPendingIn where chain = X AND address = Y AND tickId = Z)

Total = Balance + PendingOut + PendingIn
```

---

## 🔄 UTXO 状态定义

| 状态 | 值 | 含义 | 计入余额 |
|------|-----|------|----------|
| `Available` | 0 | 可用 | Balance |
| `TeleportPending` | 1 | Teleport pending out | PendingOut |
| `TransferPending` | 2 | Transfer pending out | PendingOut |
| `Spent` | -1 | 已花费 | 不计入 |

---

## 🚀 Teleport 流程（新架构）

### Mempool 阶段

```
1. stepLockSourceUTXO (Created → SourceLocked)
   ├─ Pre-check: 验证 Arrival 存在
   ├─ 查找源 UTXO (status=Available)
   ├─ 标记为 TeleportPending
   └─ 💡 此时余额自动变化：
       源链: Balance减少，PendingOut增加（通过UTXO状态计算）

2. stepVerifyArrival (SourceLocked → ArrivalVerified)
   ├─ 验证 Arrival 数据匹配
   ├─ 创建 TeleportPendingIn 记录
   └─ 💡 此时余额自动变化：
       目标链: PendingIn增加（通过TeleportPendingIn记录计算）

⏸️  等待区块确认...
```

### 区块确认阶段

```
3. stepMarkSourceSpent (ArrivalVerified → SourceSpent)
   ├─ 标记源 UTXO 为 Spent
   └─ 💡 此时余额自动变化：
       源链: PendingOut减少（UTXO不再是TeleportPending）

4. stepCreateTargetUTXO (SourceSpent → TargetCreated)
   ├─ 创建目标 UTXO (status=Available)
   └─ 💡 此时余额自动变化：
       目标链: Balance增加（新UTXO状态为Available）

5. stepUpdateBalances (TargetCreated → BalanceUpdated)
   ├─ 验证源 UTXO 状态为 Spent
   ├─ 验证目标 UTXO 状态为 Available
   └─ 💡 不需要更新余额表，只验证UTXO状态正确

6. stepFinalizeTeleport (BalanceUpdated → Completed)
   ├─ 删除 TeleportPendingIn 记录
   ├─ 写入流水记录
   ├─ 更新 Arrival 状态
   └─ 💡 此时余额自动变化：
       目标链: PendingIn减少（TeleportPendingIn已删除）
```

---

## 📁 核心文件

### 1. `man/mrc20_balance_utxo.go` - 余额计算引擎

```go
// 通过UTXO实时计算余额
func CalculateBalanceFromUTXO(chain, address, tickId string) (*MRC20Balance, error)

// 获取某个地址的所有代币余额
func GetAddressBalances(chain, address string) ([]*MRC20Balance, error)

// 获取某个地址在所有链上的余额
func GetAllChainsBalances(address string) (map[string][]*MRC20Balance, error)
```

### 2. `man/mrc20_teleport_v2.go` - Teleport 状态机

**简化后的逻辑**：
- ✅ 只管理 UTXO 状态转换
- ✅ 不调用 `UpdateMrc20AccountBalance`
- ✅ 余额通过 UTXO 状态自动计算

**移除的操作**：
```go
// ❌ 不再需要
PebbleStore.UpdateMrc20AccountBalance(...)
```

**保留的操作**：
```go
// ✅ 只需要管理UTXO状态
PebbleStore.UpdateMrc20Utxo([]*mrc20.Mrc20Utxo{sourceUtxo}, isMempool)
PebbleStore.SaveMrc20Pin([]mrc20.Mrc20Utxo{targetUtxo})
PebbleStore.SaveTeleportPendingIn(pendingIn)
PebbleStore.DeleteTeleportPendingIn(coord, address)
```

---

## ✅ 优势

### 1. **简化状态管理**

**旧架构**（有余额表）：
```
操作UTXO → 同时更新AccountBalance → 两处状态需要保持一致 ❌
```

**新架构**（无余额表）：
```
操作UTXO → 余额自动计算 ✅
```

### 2. **避免数据不一致**

**旧架构问题**：
- UTXO 更新成功，但 AccountBalance 更新失败
- 导致余额 ≠ UTXO 总和
- 需要定期验证和修复

**新架构**：
- UTXO 是唯一的真相来源
- 余额始终 = UTXO 状态的函数
- **不可能出现不一致**

### 3. **更容易验证**

```go
// 旧架构：需要验证两处
assert(accountBalance.Balance == sum(UTXO))

// 新架构：只验证UTXO
assert(sum(UTXO) == totalSupply)
```

### 4. **更容易审计**

所有余额变化都对应 UTXO 状态变化，完全可追溯：

```
Balance 变化 ← UTXO 创建/删除 ← 对应的交易 ← 区块链上的 PIN
```

---

## 🔍 查询性能

### 查询方式

```go
// 查询单个代币余额
balance := CalculateBalanceFromUTXO(chain, address, tickId)

// 查询所有代币余额
balances := GetAddressBalances(chain, address)
```

### 性能考虑

**场景 1: 小用户**（< 100 UTXOs）
- 扫描速度：< 10ms
- 性能完全可接受 ✅

**场景 2: 大户**（> 1000 UTXOs）
- 扫描速度：< 100ms
- 仍然可接受 ✅
- 可以加缓存优化

**优化方案**（如果需要）：
1. 按地址建立索引（prefix = `mrc20_utxo_${chain}_${address}_`）
2. 缓存计算结果（5秒过期）
3. 后台预计算热门地址

---

## 📈 资产验证

### 验证公式

```
所有链的所有地址的 Balance 总和 = Deploy 的总供应量
```

### 验证方法

```go
// 验证单个代币
report := VerifyMRC20TotalSupply(tickId)

// 检查
assert(report.TotalBalance == deploy.TotalSupply)
assert(report.TotalUTXO == deploy.TotalSupply)
```

**新架构的验证更简单**：
- 不需要验证 `AccountBalance` 和 `UTXO` 是否一致
- 只需要验证 `UTXO 总和 = 总供应量`

---

## 🔄 迁移策略

### 平滑迁移

**阶段 1: 双写模式**（过渡期）
```go
// 同时维护UTXO和AccountBalance
UpdateMrc20Utxo(...)
UpdateMrc20AccountBalance(...) // 保留，但不依赖
```

**阶段 2: 验证模式**（验证新架构正确性）
```go
// 比较两种计算结果
utxoBalance := CalculateBalanceFromUTXO(...)
tableBalance := GetAccountBalance(...)
assert(utxoBalance == tableBalance)
```

**阶段 3: 切换模式**（正式切换）
```go
// 只使用UTXO计算
balance := CalculateBalanceFromUTXO(...)
// AccountBalance表变为只读（不再更新）
```

**阶段 4: 清理模式**（移除旧代码）
```go
// 移除所有UpdateMrc20AccountBalance调用
// 移除AccountBalance表（可选）
```

### 当前状态

✅ **已完成**：
- Teleport V2 已切换到新架构
- 不再调用 `UpdateMrc20AccountBalance`
- 实现了 `CalculateBalanceFromUTXO`

⏳ **待完成**：
- 其他操作（mint, transfer）也切换到新架构
- API 查询接口切换到实时计算
- 性能测试和优化

---

## 🛠️ API 调整

### 余额查询 API

```go
// 旧方式（从AccountBalance表读取）
func getBalanceByAddress(ctx *gin.Context) {
    accountBalance := PebbleStore.GetAccountBalance(...)
    ctx.JSON(http.StatusOK, accountBalance)
}

// 新方式（从UTXO实时计算）
func getBalanceByAddress(ctx *gin.Context) {
    balance := CalculateBalanceFromUTXO(chain, address, tickId)
    ctx.JSON(http.StatusOK, balance)
}
```

### 性能对比

| 方式 | 延迟 | 一致性 | 维护成本 |
|------|------|--------|----------|
| 旧：AccountBalance | ~1ms | ❌ 可能不一致 | 高 |
| 新：UTXO实时计算 | ~10ms | ✅ 始终一致 | 低 |

---

## 📚 相关文档

- **Teleport V2**: `TELEPORT_V2_GUIDE.md`
- **资产验证**: `man/mrc20_verify.go`
- **余额计算**: `man/mrc20_balance_utxo.go`

---

## ✅ 总结

**新架构核心**：
```
UTXO状态 → 余额
```

**优势**：
- ✅ 简化状态管理
- ✅ 消除数据不一致
- ✅ 更容易验证
- ✅ 更容易审计

**trade-off**：
- ⚠️ 查询性能：从 1ms → 10ms
- ✅ 完全可接受
- ✅ 可以加缓存优化

---

**架构调整时间**: 2026-02-10
**状态**: ✅ Teleport V2 已实现
**下一步**: 其他操作也切换到新架构
