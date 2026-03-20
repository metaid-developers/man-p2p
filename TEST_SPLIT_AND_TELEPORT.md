# MRC20 UTXO 拆分 + Teleport 测试

## 🎯 测试场景

模拟真实使用场景：
1. 有一个 1000 代币的 UTXO
2. 拆分成 2 个 UTXO：600 + 400
3. 将 400 的 UTXO 进行 Teleport 到 DOGE 链
4. 验证整个流程的余额计算正确性

---

## 📋 前置条件

假设你已经完成了 Deploy 和 Mint，有一个包含 1000 个 TEST 代币的 UTXO。

```bash
# 环境变量
btcAddress="你的BTC地址"
dogeAddress="你的DOGE地址"
TICK_ID="你的tick_id"

# 假设你有一个 mint 的 UTXO
MINT_TX_ID="你的mint_tx_id"
MINT_UTXO="${MINT_TX_ID}:0"  # 包含 1000 个代币

# 验证初始余额
curl "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${btcAddress}&tickId=${TICK_ID}&chain=btc"
# 期望: balance=1000
```

---

## 阶段 1: 拆分 UTXO (1000 → 600 + 400)

### PIN 结构

**路径**: `/ft/mrc20/transfer`

**Body**:
```json
{
  "p": "mrc-20",
  "op": "transfer",
  "id": "${TICK_ID}",
  "data": [
    {
      "type": "data",
      "amount": "600",
      "vout": 1
    },
    {
      "type": "data",
      "amount": "400",
      "vout": 2
    }
  ]
}
```

**字段说明**:
- `type`: "data" 表示普通转账（非 teleport）
- `amount`: 转账金额
- `vout`: 输出索引（1 表示第一个找零输出，2 表示第二个找零输出）

### 详细说明

#### 交易结构

这笔交易会创建以下输出：
```
Inputs:
  - UTXO[0]: 1000 个 TEST 代币 (被花费)

Outputs:
  - vout=0: OP_RETURN (MRC20 transfer PIN)
  - vout=1: 600 聪 → 接收者 (或自己)，对应 600 个 TEST 代币
  - vout=2: 400 聪 → 接收者 (或自己)，对应 400 个 TEST 代币
```

#### 创建步骤

```bash
# 1. 创建 transfer PIN 的 JSON 数据
cat > split.json <<EOF
{
  "p": "mrc-20",
  "op": "transfer",
  "id": "${TICK_ID}",
  "data": [
    {
      "type": "data",
      "amount": "600",
      "vout": 1
    },
    {
      "type": "data",
      "amount": "400",
      "vout": 2
    }
  ]
}
EOF

# 2. 创建交易
# ⚠️ 重要：
# - 这笔交易必须花费 MINT_UTXO (1000 个代币的 UTXO)
# - vout=1 接收 600 聪（对应 600 个代币）
# - vout=2 接收 400 聪（对应 400 个代币）
# - 两个输出都可以发给自己（$btcAddress）

# 使用你的工具创建交易，示例：
# metaid-cli create-transfer \
#   --spend-utxo $MINT_UTXO \
#   --output-1 "${btcAddress}:600" \
#   --output-2 "${btcAddress}:400" \
#   --pin-data "$(cat split.json)"

SPLIT_TX_ID="<split_tx_id>"

# 3. ⚠️ 不要立即挖矿，先验证 mempool 阶段
```

### 验证 Mempool 阶段

```bash
# 等待索引器处理
sleep 5

# 查询余额
curl "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${btcAddress}&tickId=${TICK_ID}&chain=btc"
```

**期望输出（Mempool 阶段）**:
```json
{
  "code": 1,
  "data": {
    "balance": "0",          // 原来的 1000 UTXO 变为 TransferPending
    "pendingIn": "1000",     // 2 个新 UTXO (600+400) 在 mempool
    "pendingOut": "1000"     // 原 UTXO 被标记为 pending
  }
}
```

**✅ 验证点（Mempool）**:
- ✅ 原 UTXO (1000) 状态变为 `TransferPending`
- ✅ `pendingOut` = 1000（被花费的 UTXO）
- ✅ `pendingIn` = 1000（2 个新 UTXO: 600 + 400）
- ✅ 总额不变：`balance + pendingOut = pendingIn = 1000`

### 挖矿确认

```bash
# 挖矿确认
bitcoin-cli -regtest generatetoaddress 1 $btcAddress

# 等待索引器处理
sleep 5

# 查询余额
curl "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${btcAddress}&tickId=${TICK_ID}&chain=btc"
```

**期望输出（Block 阶段）**:
```json
{
  "code": 1,
  "data": {
    "balance": "1000",       // 2 个新 UTXO 确认 (600 + 400)
    "pendingIn": "0",        // 清零
    "pendingOut": "0"        // 清零
  }
}
```

**✅ 验证点（Block）**:
- ✅ `balance` = 1000（2 个新 UTXO 的总和）
- ✅ `pendingIn` = 0
- ✅ `pendingOut` = 0
- ✅ 原 UTXO 已被标记为 `Spent`，新的 2 个 UTXO 状态为 `Available`

### 查看新的 UTXOs

```bash
# 查看拆分后的 UTXO
echo "UTXO 1 (600 个代币):"
curl "http://localhost:7777/api/mrc20/tx/history?txId=${SPLIT_TX_ID}&index=1"

echo "UTXO 2 (400 个代币):"
curl "http://localhost:7777/api/mrc20/tx/history?txId=${SPLIT_TX_ID}&index=2"

# 记录 UTXO 信息（用于后续 teleport）
UTXO_600="${SPLIT_TX_ID}:1"  # 600 个代币
UTXO_400="${SPLIT_TX_ID}:2"  # 400 个代币
```

**期望输出**:
```json
// UTXO 1
{
  "txPoint": "<SPLIT_TX_ID>:1",
  "amtChange": "600",
  "status": 0,              // Available
  "toAddress": "<btcAddress>"
}

// UTXO 2
{
  "txPoint": "<SPLIT_TX_ID>:2",
  "amtChange": "400",
  "status": 0,              // Available
  "toAddress": "<btcAddress>"
}
```

---

## 阶段 2: Teleport 其中一个 UTXO (400 → DOGE)

现在我们将 400 个代币的 UTXO teleport 到 DOGE 链。

### 步骤 2.1: 创建 Arrival PIN（DOGE 链）

**路径**: `/ft/mrc20/arrival`

**Body**:
```json
{
  "p": "mrc-20",
  "op": "arrival",
  "id": "${TICK_ID}",
  "chain": "doge",
  "amount": "400",
  "to": "${dogeAddress}",
  "assetOutpoint": "${UTXO_400}"
}
```

**创建步骤**:
```bash
# 1. 创建 arrival.json
cat > arrival.json <<EOF
{
  "p": "mrc-20",
  "op": "arrival",
  "id": "${TICK_ID}",
  "chain": "doge",
  "amount": "400",
  "to": "${dogeAddress}",
  "assetOutpoint": "${UTXO_400}"
}
EOF

# 2. 在 DOGE 链创建 arrival PIN
# metaid-cli create-pin --chain doge --path "/ft/mrc20/arrival" --body "$(cat arrival.json)" --address $dogeAddress
ARRIVAL_PIN_ID="<arrival_pin_id>"

# 3. 挖矿确认（DOGE 链）
dogecoin-cli -regtest generatetoaddress 1 $dogeAddress

# 4. 验证 arrival
curl "http://localhost:7777/api/mrc20/admin/teleport/check-arrival/${ARRIVAL_PIN_ID}"
```

### 步骤 2.2: 创建 Teleport PIN（BTC 链）

**路径**: `/ft/mrc20/transfer`

**Body**:
```json
{
  "p": "mrc-20",
  "op": "transfer",
  "id": "${TICK_ID}",
  "data": [
    {
      "type": "teleport",
      "chain": "doge",
      "amount": "400",
      "coord": "${ARRIVAL_PIN_ID}"
    }
  ]
}
```

**创建步骤**:
```bash
# 1. 创建 teleport.json
cat > teleport.json <<EOF
{
  "p": "mrc-20",
  "op": "transfer",
  "id": "${TICK_ID}",
  "data": [
    {
      "type": "teleport",
      "chain": "doge",
      "amount": "400",
      "coord": "${ARRIVAL_PIN_ID}"
    }
  ]
}
EOF

# 2. 在 BTC 链创建 teleport PIN
# ⚠️ 重要：这笔交易必须花费 UTXO_400
# metaid-cli create-transfer \
#   --spend-utxo $UTXO_400 \
#   --pin-data "$(cat teleport.json)"

TELEPORT_TX_ID="<teleport_tx_id>"

# 3. ⚠️ 不要立即挖矿，先验证 mempool 阶段
```

### 验证 Teleport Mempool 阶段

```bash
# 等待索引器处理
sleep 5
```

**BTC 链（源链）**:
```bash
curl "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${btcAddress}&tickId=${TICK_ID}&chain=btc"
```

**期望输出**:
```json
{
  "code": 1,
  "data": {
    "balance": "600",        // ✅ 只剩 600 (UTXO_600)
    "pendingIn": "0",
    "pendingOut": "400"      // ✅ UTXO_400 变为 TeleportPending
  }
}
```

**DOGE 链（目标链）**:
```bash
curl "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${dogeAddress}&tickId=${TICK_ID}&chain=doge"
```

**期望输出**:
```json
{
  "code": 1,
  "data": {
    "balance": "0",
    "pendingIn": "400",      // ✅ TeleportPendingIn 记录
    "pendingOut": "0"
  }
}
```

**✅ 验证点（Teleport Mempool）**:
- ✅ BTC: `balance` = 600（UTXO_600 仍然 Available）
- ✅ BTC: `pendingOut` = 400（UTXO_400 变为 TeleportPending）
- ✅ DOGE: `pendingIn` = 400（TeleportPendingIn 记录）
- ✅ 总额：600 + 400 + 400 = 1400（等待确认）

### 挖矿确认并验证 Block 阶段

```bash
# 挖矿确认 BTC 交易
bitcoin-cli -regtest generatetoaddress 1 $btcAddress

# 等待索引器处理
sleep 5
```

**BTC 链（源链）**:
```bash
curl "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${btcAddress}&tickId=${TICK_ID}&chain=btc"
```

**期望输出**:
```json
{
  "code": 1,
  "data": {
    "balance": "600",        // ✅ 保持 600
    "pendingIn": "0",
    "pendingOut": "0"        // ✅ 清零
  }
}
```

**DOGE 链（目标链）**:
```bash
curl "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${dogeAddress}&tickId=${TICK_ID}&chain=doge"
```

**期望输出**:
```json
{
  "code": 1,
  "data": {
    "balance": "400",        // ✅ 增加 400
    "pendingIn": "0",        // ✅ 清零
    "pendingOut": "0"
  }
}
```

**✅ 验证点（Teleport Block）**:
- ✅ BTC: `balance` = 600，`pendingOut` = 0
- ✅ DOGE: `balance` = 400，`pendingIn` = 0
- ✅ 总额：600 + 400 = 1000 ✅

---

## 阶段 3: 资产验证

验证整个流程后，资产总量没有变化。

```bash
curl "http://localhost:7777/api/mrc20/admin/verify/supply/${TICK_ID}"
```

**期望输出**:
```json
{
  "code": 1,
  "data": {
    "tickId": "<TICK_ID>",
    "tickName": "TEST",
    "status": "✅ PASSED",
    "totalUtxo": "1000",
    "totalBalance": "1000",
    "totalPendingOut": "0",
    "totalPendingIn": "0",
    "utxoByChain": {
      "btc": "600",          // ✅ BTC 链剩余 600
      "doge": "400"          // ✅ DOGE 链有 400
    },
    "balanceByChain": {
      "btc": "600",
      "doge": "400"
    },
    "utxoCount": 2,          // ✅ 2 个 UTXO
    "errors": [],
    "warnings": []
  }
}
```

**✅ 验证点**:
- ✅ `totalBalance` = 1000（没有变化）
- ✅ `utxoCount` = 2（从 1 个变成 2 个）
- ✅ BTC 链: 600（1 个 UTXO）
- ✅ DOGE 链: 400（1 个 UTXO）
- ✅ 无错误和警告

---

## 📊 完整流程图

```
初始状态:
  BTC: [UTXO: 1000]
  DOGE: []

阶段 1: 拆分 (1000 → 600 + 400)
  ↓ Mempool
  BTC: balance=0, pendingOut=1000, pendingIn=1000
  ↓ Block
  BTC: [UTXO: 600] + [UTXO: 400]

阶段 2: Teleport (400 → DOGE)
  ↓ Mempool
  BTC: balance=600, pendingOut=400
  DOGE: balance=0, pendingIn=400
  ↓ Block
  BTC: [UTXO: 600]
  DOGE: [UTXO: 400]

最终状态:
  BTC: 600 (1 个 UTXO)
  DOGE: 400 (1 个 UTXO)
  总计: 1000 ✅
```

---

## 🔍 详细 UTXO 状态变化

### 拆分阶段

| 时间 | UTXO | TxPoint | Amount | Status | 说明 |
|------|------|---------|--------|--------|------|
| 初始 | UTXO_0 | mint:0 | 1000 | Available | 原始 UTXO |
| Mempool | UTXO_0 | mint:0 | 1000 | TransferPending | 被花费 |
| Mempool | UTXO_1 | split:1 | 600 | Available (mempool) | 新 UTXO 1 |
| Mempool | UTXO_2 | split:2 | 400 | Available (mempool) | 新 UTXO 2 |
| Block | UTXO_0 | mint:0 | 1000 | Spent | 已花费 |
| Block | UTXO_1 | split:1 | 600 | Available | 确认 |
| Block | UTXO_2 | split:2 | 400 | Available | 确认 |

### Teleport 阶段

| 时间 | UTXO | TxPoint | Amount | Status | Chain | 说明 |
|------|------|---------|--------|--------|-------|------|
| 初始 | UTXO_1 | split:1 | 600 | Available | BTC | 保留 |
| 初始 | UTXO_2 | split:2 | 400 | Available | BTC | 准备 teleport |
| Mempool | UTXO_2 | split:2 | 400 | TeleportPending | BTC | 被锁定 |
| Block | UTXO_2 | split:2 | 400 | Spent | BTC | 已花费 |
| Block | UTXO_3 | arrival:X | 400 | Available | DOGE | 新创建 |

---

## 🧪 验证脚本

```bash
#!/bin/bash

# 设置变量
btcAddress="你的BTC地址"
dogeAddress="你的DOGE地址"
TICK_ID="你的tick_id"
MINT_UTXO="你的mint_utxo"

echo "========================================="
echo "测试：UTXO 拆分 + Teleport"
echo "========================================="

# 阶段 1: 拆分
echo -e "\n[阶段 1] 拆分 UTXO (1000 → 600 + 400)"
echo "创建 transfer PIN..."
# 你的 transfer 命令
SPLIT_TX_ID="获取的tx_id"

echo "等待 mempool 处理..."
sleep 5

echo "验证 Mempool 余额:"
curl -s "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${btcAddress}&tickId=${TICK_ID}&chain=btc" | jq '.data'

echo "挖矿确认..."
bitcoin-cli -regtest generatetoaddress 1 $btcAddress
sleep 5

echo "验证 Block 余额:"
curl -s "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${btcAddress}&tickId=${TICK_ID}&chain=btc" | jq '.data'

UTXO_600="${SPLIT_TX_ID}:1"
UTXO_400="${SPLIT_TX_ID}:2"
echo "✅ 拆分完成: UTXO_600=$UTXO_600, UTXO_400=$UTXO_400"

# 阶段 2: Teleport
echo -e "\n[阶段 2] Teleport 400 到 DOGE 链"
echo "创建 Arrival PIN..."
# 你的 arrival 命令
ARRIVAL_PIN_ID="获取的arrival_pin_id"
dogecoin-cli -regtest generatetoaddress 1 $dogeAddress

echo "创建 Teleport PIN..."
# 你的 teleport 命令
TELEPORT_TX_ID="获取的teleport_tx_id"

echo "等待 mempool 处理..."
sleep 5

echo "验证 Mempool 余额:"
echo "BTC 链:"
curl -s "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${btcAddress}&tickId=${TICK_ID}&chain=btc" | jq '.data'
echo "DOGE 链:"
curl -s "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${dogeAddress}&tickId=${TICK_ID}&chain=doge" | jq '.data'

echo "挖矿确认..."
bitcoin-cli -regtest generatetoaddress 1 $btcAddress
sleep 5

echo "验证 Block 余额:"
echo "BTC 链:"
curl -s "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${btcAddress}&tickId=${TICK_ID}&chain=btc" | jq '.data'
echo "DOGE 链:"
curl -s "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${dogeAddress}&tickId=${TICK_ID}&chain=doge" | jq '.data'

# 阶段 3: 验证
echo -e "\n[阶段 3] 资产验证"
curl -s "http://localhost:7777/api/mrc20/admin/verify/supply/${TICK_ID}" | jq '.data | {status, totalBalance, utxoByChain}'

echo -e "\n========================================="
echo "测试完成！"
echo "========================================="
```

---

## ✅ 成功标准

测试通过需要满足：

1. **拆分阶段**
   - ✅ Mempool: `pendingOut` = 1000, `pendingIn` = 1000
   - ✅ Block: `balance` = 1000（2 个 UTXO）
   - ✅ 可以查询到 2 个新 UTXO（600 + 400）

2. **Teleport Mempool 阶段**
   - ✅ BTC: `balance` = 600, `pendingOut` = 400
   - ✅ DOGE: `pendingIn` = 400
   - ✅ 总额正确：600 + 400 + 400 = 1400

3. **Teleport Block 阶段**
   - ✅ BTC: `balance` = 600, `pendingOut` = 0
   - ✅ DOGE: `balance` = 400, `pendingIn` = 0
   - ✅ 总额正确：600 + 400 = 1000

4. **资产验证**
   - ✅ `totalBalance` = 1000
   - ✅ BTC: 600, DOGE: 400
   - ✅ `status` = "✅ PASSED"
   - ✅ 无错误

---

## 🎯 关键点总结

### Transfer (拆分) vs Teleport

| 操作 | type | 目标 | UTXO 状态 | 跨链 |
|------|------|------|-----------|------|
| **Transfer** | "data" | 同链拆分/转账 | TransferPending | ❌ |
| **Teleport** | "teleport" | 跨链转移 | TeleportPending | ✅ |

### PIN 结构对比

**Transfer (拆分)**:
```json
{
  "data": [
    {"type": "data", "amount": "600", "vout": 1},
    {"type": "data", "amount": "400", "vout": 2}
  ]
}
```

**Teleport**:
```json
{
  "data": [
    {"type": "teleport", "amount": "400", "chain": "doge", "coord": "xxx"}
  ]
}
```

---

**准备好测试了吗？** 🚀
