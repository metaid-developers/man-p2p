# MRC20 Teleport 调试指南

## 新增调试日志

为了诊断 teleport 在 mempool 阶段未被处理的问题，我添加了全面的调试日志系统。

### 日志追踪点

#### 1. Mempool 入口 (`mempool.go`)

```
[Mempool] 🎯 MRC20 PIN detected, calling handleMempoolMrc20
[Mempool] 📨 handleMempoolMrc20: path=..., pinId=..., txId=...
```

**作用**：确认 PIN 是否通过 ZMQ 到达 mempool 处理器

**如果没有这个日志**：
- ZMQ 可能没有接收到交易
- Indexer 的 ZMQ 监听可能有问题
- PIN 路径可能不匹配 `/ft/mrc20/`

---

#### 2. Teleport 检测 (`mrc20.go:transferHandleWithMempool`)

```
[Teleport] 🎯 Detected teleport transfer: pinId=..., txId=...
```

**作用**：确认 transfer PIN 是否被识别为 teleport 类型

**如果没有这个日志**：
- JSON 格式可能不正确
- `type` 字段可能不是 "teleport"
- JSON 解析失败

---

#### 3. Teleport 处理开始 (`mrc20.go:processTeleportTransfer`)

```
[Teleport] 🔄 processTeleportTransfer called: pinId=..., isMempool=true
```

**作用**：确认进入 teleport 处理流程

---

#### 4. CheckTeleportExists 拒绝 (`mrc20.go:validateAndProcessTeleport`)

```
[Teleport] ❌ Rejected: teleport already exists for coord: ...
```

**作用**：检测 arrival coord 是否已被使用过

**如果看到这个日志**：
- 这个 arrival coord 之前已经有完成的 teleport
- 可能是测试时重复使用了相同的 arrival
- 需要使用新的 arrival 交易

---

#### 5. Teleport 验证通过 (`mrc20.go:validateAndProcessTeleport`)

```
[Teleport] ✅ Processing new teleport: coord=..., tickId=..., amount=..., isMempool=true
```

**作用**：确认通过了 coord 重复检查，开始处理

---

#### 6. PendingTeleport 保存 - Arrival 未找到

```
[Teleport] 💾 Saved PendingTeleport (arrival not found): coord=..., asset=...
```

**作用**：Teleport 先到达，Arrival 还未索引，保存 pending 状态

---

#### 7. PendingTeleport 保存 - Mempool 阶段

```
[Teleport] 💾 Saved PendingTeleport (mempool stage, waiting for confirmation): coord=..., teleportMempool=true, arrivalMempool=true/false
```

**作用**：双方都找到了，但在 mempool 阶段，等待区块确认

---

#### 8. Arrival 处理 (`mrc20.go:arrivalHandle`)

```
[Arrival] 📥 Processing arrival: pinId=..., txId=..., chain=...
[Arrival] ✅ Parsed arrival data: assetOutpoint=..., tickId=..., amount=...
```

**作用**：追踪 arrival 的处理流程

---

#### 9. processPendingTeleportForArrival 调用

```
[processPendingTeleportForArrival] 🔍 Checking for pending teleport: arrivalPinId=...
[processPendingTeleportForArrival] ✅ Found pending teleport: pinId=..., coord=...
```

或

```
[processPendingTeleportForArrival] ℹ️ No pending teleport found for arrival ...
```

**作用**：检查 arrival 到达时是否有等待的 teleport

---

## 诊断流程

### 场景 1: Teleport 交易根本没有进入 mempool 处理

**症状**：没有任何 `[Mempool]` 或 `[Teleport]` 日志

**可能原因**：
1. ZMQ 没有接收到交易
2. 交易还未进入节点的 mempool
3. PIN 路径不匹配

**检查方法**：
```bash
# 查看 ZMQ 日志
grep "GenesisTransaction=<你的txid>" /path/to/log

# 检查节点 mempool
bitcoin-cli getrawmempool | grep <txid>
```

---

### 场景 2: 进入了处理但被 coord 重复拒绝

**症状**：看到日志
```
[Teleport] ❌ Rejected: teleport already exists for coord: ...
```

**原因**：这个 arrival 之前已经被使用过

**解决方法**：
1. 使用新的 arrival 交易（不要重复使用 coord）
2. 如果需要清理测试数据：
   ```bash
   # 查询已完成的 teleport
   curl "http://localhost:7777/api/mrc20/admin/teleport/check-arrival/<pinId>"
   ```

---

### 场景 3: 处理进入但 PendingTeleport 未保存

**症状**：看到 `[Teleport] ✅ Processing new teleport` 但没有看到 `💾 Saved PendingTeleport`

**可能原因**：
1. 源 UTXO 查找失败（余额不足、UTXO 不存在）
2. 验证失败（金额、tickId 不匹配）
3. 数据库写入失败

**检查方法**：
```bash
# 查看详细错误日志
grep "validateAndProcessTeleport error" /path/to/log

# 检查源 UTXO 是否存在
curl "http://localhost:7777/api/mrc20/balance/<address>?tick=<tick>"
```

---

### 场景 4: PendingTeleport 保存成功但余额没变化

**症状**：看到 `💾 Saved PendingTeleport` 但余额没有变化

**原因**：可能是 `UpdateMrc20AccountBalance` 失败

**检查方法**：
```bash
# 查看 UpdateMrc20AccountBalance 错误日志
grep "UpdateMrc20AccountBalance failed" /path/to/log

# 查看 pending 余额（新架构应该显示 PendingOut/PendingIn）
curl "http://localhost:7777/api/mrc20/balance/<address>?tick=<tick>"
```

---

## 测试建议

### 1. 完整测试流程

使用新的日志系统测试 teleport：

```bash
# 1. 清理日志
> /path/to/log

# 2. 创建 arrival (在 DOGE 链)
# ... 创建交易 ...

# 3. 创建 teleport (在 BTC 链)
# ... 创建交易 ...

# 4. 观察日志
tail -f /path/to/log | grep -E "\[Mempool\]|\[Teleport\]|\[Arrival\]"

# 5. 检查 pending 列表
curl "http://localhost:7777/api/mrc20/admin/teleport/list-pending"
```

---

### 2. 日志过滤命令

```bash
# 查看所有 teleport 相关日志
grep -E "\[Teleport\]|\[Arrival\]|\[processPendingTeleportForArrival\]" /path/to/log

# 查看特定 txId 的处理流程
grep "txId=<your_txid>" /path/to/log

# 查看特定 coord 的处理流程
grep "coord=<your_coord>" /path/to/log

# 查看所有错误
grep "❌" /path/to/log

# 查看所有成功保存的 PendingTeleport
grep "💾 Saved PendingTeleport" /path/to/log
```

---

## 常见问题排查

### Q1: 为什么 mempool 阶段余额没有变化？

**A**: 检查以下几点：

1. **是否保存了 PendingTeleport？**
   ```bash
   grep "💾 Saved PendingTeleport" /path/to/log
   ```

2. **是否调用了 UpdateMrc20AccountBalance？**
   ```bash
   grep "UpdateMrc20AccountBalance" /path/to/log
   ```

3. **新架构下，余额应该显示在 PendingOut/PendingIn 中**
   - 发送方：`Balance -= amount`, `PendingOut += amount`
   - 接收方：`PendingIn += amount`

4. **检查 API 返回是否包含 pending 字段**
   ```bash
   curl "http://localhost:7777/api/mrc20/balance/<address>?tick=<tick>"
   ```

---

### Q2: Arrival coord 为什么说已经存在？

**A**: 一个 arrival 只能被使用一次。检查：

```bash
# 检查 arrival 是否已被使用
curl "http://localhost:7777/api/mrc20/admin/teleport/check-arrival/<pinId>"

# 检查 assetOutpoint 索引
curl "http://localhost:7777/api/mrc20/admin/teleport/check-asset-index/<assetOutpoint>"
```

**解决方法**：使用新的 arrival 交易

---

### Q3: 如何确认 ZMQ 是否工作正常？

**A**: 检查日志中是否有 mempool PIN 的记录：

```bash
# 查看最近的 mempool PIN
grep "\[Mempool\] 🎯 MRC20 PIN detected" /path/to/log | tail -10

# 如果没有，检查 ZMQ 配置
ps aux | grep zmq
```

---

## 日志级别说明

- `🎯` : 检测到事件（PIN 检测、teleport 检测）
- `✅` : 验证通过、处理成功
- `❌` : 验证失败、处理拒绝
- `💾` : 数据保存操作
- `📨` / `📥` : 消息接收（mempool / arrival）
- `🔄` : 处理流程开始
- `🔍` : 查询检查操作
- `ℹ️` : 信息提示（非错误）

---

## 下一步

如果通过这些日志还无法定位问题，请提供以下信息：

1. **完整的日志输出**（包含特定 txId 的所有相关日志）
2. **Teleport 交易 ID**
3. **Arrival 交易 ID**
4. **测试环境**（regtest / testnet / mainnet）
5. **余额 API 返回**（包含 pending 字段）

这将帮助我们进一步诊断问题的根本原因。
