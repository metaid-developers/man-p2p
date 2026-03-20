# 快速测试 - PIN 结构参考

## 📌 变量定义

测试前先定义这些变量：

```bash
# 地址
btcAddress="<你的_BTC_地址>"
dogeAddress="<你的_DOGE_地址>"

# 代币信息（测试过程中会生成）
TICK_ID=""           # deploy 后获得
DEPLOY_PIN_ID=""     # deploy PIN ID
MINT_PIN_1=""        # 第一个 mint PIN
ASSET_OUTPOINT=""    # 要 teleport 的 UTXO (格式: txid:vout)
ARRIVAL_PIN_ID=""    # arrival PIN ID
```

---

## 1️⃣ Deploy PIN

**路径**: `/ft/mrc20/deploy`

**Body**:
```json
{
  "p": "mrc-20",
  "op": "deploy",
  "tick": "TEST",
  "max": "21000000",
  "lim": "1000",
  "dec": "8"
}
```

**创建命令**:
```bash
# 使用你的工具创建 deploy PIN
# 创建后记录：
DEPLOY_PIN_ID="<pin_id>"
TICK_ID="<genesis_tx_id>"
```

**验证**:
```bash
curl "http://localhost:7777/api/mrc20/tick/info/${TICK_ID}"
```

---

## 2️⃣ Mint PIN（创建 3 次）

**路径**: `/ft/mrc20/mint`

**Body**:
```json
{
  "p": "mrc-20",
  "op": "mint",
  "id": "${TICK_ID}",
  "vout": 0
}
```

**创建命令**:
```bash
# Mint 1
# 创建 mint PIN (会生成 1000 个代币)
MINT_PIN_1="<pin_id_1>"
MINT_TX_1="<tx_id_1>"

# Mint 2
MINT_PIN_2="<pin_id_2>"
MINT_TX_2="<tx_id_2>"

# Mint 3
MINT_PIN_3="<pin_id_3>"
MINT_TX_3="<tx_id_3>"

# 挖矿确认
bitcoin-cli -regtest generatetoaddress 1 $btcAddress
```

**验证**:
```bash
# 应该看到 3000 个 TEST 代币
curl "http://localhost:7777/api/mrc20/address/balance/${btcAddress}?chain=btc"
```

**预期输出**:
```json
{
  "code": 1,
  "data": {
    "list": [{
      "name": "TEST",
      "balance": "3000",
      "pendingInBalance": "0",
      "pendingOutBalance": "0"
    }]
  }
}
```

---

## 3️⃣ Arrival PIN（在 DOGE 链创建）

**路径**: `/ft/mrc20/arrival`

**Body**:
```json
{
  "p": "mrc-20",
  "op": "arrival",
  "id": "${TICK_ID}",
  "chain": "doge",
  "amount": "500",
  "to": "${dogeAddress}",
  "assetOutpoint": "${ASSET_OUTPOINT}"
}
```

**准备工作**:
```bash
# 1. 先获取要转移的 UTXO
# 假设使用第一个 mint 的输出
ASSET_OUTPOINT="${MINT_TX_1}:0"

# 2. 创建 arrival.json
cat > arrival.json <<'EOF'
{
  "p": "mrc-20",
  "op": "arrival",
  "id": "TICK_ID_HERE",
  "chain": "doge",
  "amount": "500",
  "to": "DOGE_ADDRESS_HERE",
  "assetOutpoint": "ASSET_OUTPOINT_HERE"
}
EOF

# 3. 替换占位符
sed -i "s/TICK_ID_HERE/${TICK_ID}/g" arrival.json
sed -i "s/DOGE_ADDRESS_HERE/${dogeAddress}/g" arrival.json
sed -i "s/ASSET_OUTPOINT_HERE/${ASSET_OUTPOINT}/g" arrival.json
```

**创建命令**:
```bash
# 在 DOGE 链创建 arrival PIN
# 使用你的工具创建
ARRIVAL_PIN_ID="<arrival_pin_id>"
ARRIVAL_TX_ID="<arrival_tx_id>"

# 挖矿确认（DOGE 链）
dogecoin-cli -regtest generatetoaddress 1 $dogeAddress
```

**验证**:
```bash
# 检查 arrival 是否被索引
curl "http://localhost:7777/api/mrc20/admin/teleport/check-arrival/${ARRIVAL_PIN_ID}"
```

---

## 4️⃣ Teleport PIN（在 BTC 链创建）

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
      "amount": "500",
      "coord": "${ARRIVAL_PIN_ID}"
    }
  ]
}
```

**准备工作**:
```bash
# 创建 teleport.json
cat > teleport.json <<'EOF'
{
  "p": "mrc-20",
  "op": "transfer",
  "id": "TICK_ID_HERE",
  "data": [
    {
      "type": "teleport",
      "chain": "doge",
      "amount": "500",
      "coord": "ARRIVAL_PIN_ID_HERE"
    }
  ]
}
EOF

# 替换占位符
sed -i "s/TICK_ID_HERE/${TICK_ID}/g" teleport.json
sed -i "s/ARRIVAL_PIN_ID_HERE/${ARRIVAL_PIN_ID}/g" teleport.json
```

**创建命令**:
```bash
# 在 BTC 链创建 teleport PIN
# ⚠️ 重要：这个交易必须花费 ASSET_OUTPOINT 对应的 UTXO
# 使用你的工具创建，指定要花费的 UTXO
TELEPORT_TX_ID="<teleport_tx_id>"

# ⚠️ 不要立即挖矿！先验证 mempool 阶段
```

---

## ✅ 验证检查点

### 检查点 1: Mint 后余额

```bash
curl "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${btcAddress}&tickId=${TICK_ID}&chain=btc"
```

**期望**:
```json
{
  "balance": "3000",
  "pendingIn": "0",
  "pendingOut": "0"
}
```

---

### 检查点 2: Teleport Mempool 阶段

**BTC 链（源链）**:
```bash
curl "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${btcAddress}&tickId=${TICK_ID}&chain=btc"
```

**期望**:
```json
{
  "balance": "2500",      // ✅ 减少了 500
  "pendingIn": "0",
  "pendingOut": "500"     // ✅ 增加了 500
}
```

**DOGE 链（目标链）**:
```bash
curl "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${dogeAddress}&tickId=${TICK_ID}&chain=doge"
```

**期望**:
```json
{
  "balance": "0",
  "pendingIn": "500",     // ✅ 增加了 500
  "pendingOut": "0"
}
```

---

### 检查点 3: Teleport Block 阶段

```bash
# 挖矿确认 BTC 交易
bitcoin-cli -regtest generatetoaddress 1 $btcAddress
sleep 5
```

**BTC 链（源链）**:
```bash
curl "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${btcAddress}&tickId=${TICK_ID}&chain=btc"
```

**期望**:
```json
{
  "balance": "2500",      // ✅ 保持 2500
  "pendingIn": "0",
  "pendingOut": "0"       // ✅ 清零
}
```

**DOGE 链（目标链）**:
```bash
curl "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${dogeAddress}&tickId=${TICK_ID}&chain=doge"
```

**期望**:
```json
{
  "balance": "500",       // ✅ 增加了 500
  "pendingIn": "0",       // ✅ 清零
  "pendingOut": "0"
}
```

---

### 检查点 4: 资产验证

```bash
curl "http://localhost:7777/api/mrc20/admin/verify/supply/${TICK_ID}"
```

**期望**:
```json
{
  "status": "✅ PASSED",
  "totalUtxo": "3000",
  "totalBalance": "3000",
  "totalPendingOut": "0",
  "totalPendingIn": "0",
  "utxoByChain": {
    "btc": "2500",
    "doge": "500"
  },
  "errors": [],
  "warnings": []
}
```

---

## 🔍 日志检查

### 检查余额计算日志

```bash
tail -f app.log | grep "💡"
```

**期望看到**:
```
[API] 💡 使用 UTXO 实时计算余额: address=xxx, chain=btc
[Balance] Calculated from UTXO: chain=btc, address=xxx, tick=TEST, balance=2500, pendingOut=0, pendingIn=0
[TeleportV2] 💡 余额通过UTXO状态实时计算: Balance = Available, PendingOut = TeleportPending
```

### 检查 Teleport 日志

```bash
tail -f app.log | grep "TeleportV2"
```

**期望看到**:
```
[TeleportV2] 🚀 Processing: id=xxx, coord=xxx, txId=xxx
[TeleportV2] ✨ Created new transaction: xxx
[TeleportV2] Pre-check: Verifying arrival exists before locking
[TeleportV2] ✅ Pre-check passed: arrival=xxx
[TeleportV2] ✅ Source UTXO locked: xxx
[TeleportV2] ✅ Arrival verified: coord=xxx
[TeleportV2] 🎉 Teleport completed: id=xxx
```

---

## ⚠️ 常见错误

### 错误 1: "arrival not found (pre-check failed)"

**原因**: Arrival PIN 还未被索引或 coord 不正确

**解决**:
```bash
# 1. 检查 arrival 是否存在
curl "http://localhost:7777/api/mrc20/admin/teleport/check-arrival/${ARRIVAL_PIN_ID}"

# 2. 如果不存在，等待几秒后重试
sleep 5

# 3. 检查索引高度
curl "http://localhost:7777/api/mrc20/admin/index-height/doge"
```

### 错误 2: "source UTXO not found"

**原因**: ASSET_OUTPOINT 不正确或已被花费

**解决**:
```bash
# 检查 UTXO 状态
curl "http://localhost:7777/api/mrc20/tx/history?txId=${MINT_TX_1}&index=0"
```

### 错误 3: "amount mismatch"

**原因**: Teleport 和 Arrival 的 amount 不一致

**解决**: 确保两边的 amount 完全一致（都是 "500"）

---

## 📝 测试脚本（可选）

你可以创建一个自动化测试脚本：

```bash
#!/bin/bash

# 设置变量
btcAddress="你的BTC地址"
dogeAddress="你的DOGE地址"

# 1. Deploy
echo "Creating deploy PIN..."
# 你的 deploy 命令
TICK_ID="获取的tick_id"

# 2. Mint
echo "Minting tokens..."
# 你的 mint 命令（3次）
bitcoin-cli -regtest generatetoaddress 1 $btcAddress

# 3. 验证 Mint
echo "Verifying mint..."
curl "http://localhost:7777/api/mrc20/address/balance/${btcAddress}?chain=btc" | jq '.data.list[0].balance'

# 4. Teleport
ASSET_OUTPOINT="${MINT_TX_1}:0"
echo "Creating arrival PIN..."
# 你的 arrival 命令
ARRIVAL_PIN_ID="获取的arrival_pin_id"
dogecoin-cli -regtest generatetoaddress 1 $dogeAddress

echo "Creating teleport PIN..."
# 你的 teleport 命令

# 5. 验证 Mempool
sleep 5
echo "Checking mempool balances..."
curl "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${btcAddress}&tickId=${TICK_ID}&chain=btc" | jq '.data'
curl "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${dogeAddress}&tickId=${TICK_ID}&chain=doge" | jq '.data'

# 6. 挖矿确认
bitcoin-cli -regtest generatetoaddress 1 $btcAddress
sleep 5

# 7. 验证 Block
echo "Checking final balances..."
curl "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${btcAddress}&tickId=${TICK_ID}&chain=btc" | jq '.data'
curl "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${dogeAddress}&tickId=${TICK_ID}&chain=doge" | jq '.data'

# 8. 资产验证
echo "Verifying total supply..."
curl "http://localhost:7777/api/mrc20/admin/verify/supply/${TICK_ID}" | jq '.data | {status, totalBalance, totalUtxo}'

echo "Test completed!"
```

---

**准备好开始测试了吗？** 🚀
