# UTXO 余额架构测试指南

## 🎯 测试目标

验证新的 UTXO 余额架构在 Regtest 环境下的正确性，包括：
1. 余额查询 API 正确性
2. Teleport 功能正确性
3. 余额实时计算正确性
4. 资产总量不变性

---

## 🔧 环境准备

### 1. 启动索引服务

```bash
# 编译
CGO_ENABLED=1 go build -o man-indexer

# 启动服务（使用 regtest 配置）
./man-indexer -test=2 -config=./config_regtest.toml -server=true
```

**验证服务启动**:
```bash
# 检查服务是否运行
curl http://localhost:7777/api/mrc20/admin/index-height/btc
curl http://localhost:7777/api/mrc20/admin/index-height/doge
```

### 2. 准备测试地址

需要准备以下地址：
- `btcAddress`: BTC 链上的测试地址（用于 deploy 和 mint）
- `dogeAddress`: DOGE 链上的测试地址（用于接收 teleport）

```bash
# 生成 BTC 地址
bitcoin-cli -regtest getnewaddress "" "legacy"
# 示例输出: mwK9V8H8E5FRjNXKqR4z8QxJGV8YP2FrNz

# 生成 DOGE 地址
dogecoin-cli -regtest getnewaddress "" "legacy"
# 示例输出: nXeVyJ7KxQoYhPQvAFJZ8c9qR5T3Gx8wMz
```

---

## 📋 测试流程

### 阶段 1: Deploy 代币

**目标**: 在 BTC 链上创建一个 MRC20 代币

#### PIN 结构

```json
{
  "path": "/ft/mrc20/deploy",
  "contentType": "text/plain;charset=utf-8",
  "body": {
    "p": "mrc-20",
    "op": "deploy",
    "tick": "TEST",
    "max": "21000000",
    "lim": "1000",
    "dec": "8"
  }
}
```

**字段说明**:
- `tick`: 代币名称（4个字符，TEST）
- `max`: 总供应量（21000000）
- `lim`: 单次 mint 上限（1000）
- `dec`: 小数位数（8）

#### 创建 PIN

```bash
# 1. 创建 deploy PIN 的 JSON 数据
cat > deploy.json <<EOF
{
  "p": "mrc-20",
  "op": "deploy",
  "tick": "TEST",
  "max": "21000000",
  "lim": "1000",
  "dec": "8"
}
EOF

# 2. 使用你的 PIN 创建工具创建 PIN
# 这里需要使用你实际的工具，示例：
# metaid-cli create-pin --path "/ft/mrc20/deploy" --body "$(cat deploy.json)" --address $btcAddress

# 3. 获取 PIN ID（deploy 的 ID）
DEPLOY_PIN_ID="<你的_deploy_pin_id>"
TICK_ID="<你的_tick_id>"  # 通常是 deploy PIN 的 genesis transaction ID
```

#### 验证 Deploy

```bash
# 查询 deploy 信息
curl "http://localhost:7777/api/mrc20/tick/info/${TICK_ID}"

# 期望输出：
# {
#   "code": 1,
#   "message": "ok",
#   "data": {
#     "tick": "TEST",
#     "totalSupply": "21000000",
#     "mintLimit": "1000",
#     "decimal": 8,
#     ...
#   }
# }
```

---

### 阶段 2: Mint 代币

**目标**: 在 BTC 链上 mint 一些代币到 btcAddress

#### PIN 结构

```json
{
  "path": "/ft/mrc20/mint",
  "contentType": "text/plain;charset=utf-8",
  "body": {
    "p": "mrc-20",
    "op": "mint",
    "id": "<TICK_ID>",
    "vout": 0
  }
}
```

**字段说明**:
- `id`: deploy 时的 tick ID
- `vout`: deploy PIN 的输出索引（通常是 0）

#### 创建 PIN

```bash
# 1. 创建 mint PIN 的 JSON 数据
cat > mint.json <<EOF
{
  "p": "mrc-20",
  "op": "mint",
  "id": "${TICK_ID}",
  "vout": 0
}
EOF

# 2. 创建 3 个 mint PIN（总共 3000 个代币）
# Mint 1
# metaid-cli create-pin --path "/ft/mrc20/mint" --body "$(cat mint.json)" --address $btcAddress
MINT_PIN_1="<mint_pin_1_id>"

# Mint 2
# metaid-cli create-pin --path "/ft/mrc20/mint" --body "$(cat mint.json)" --address $btcAddress
MINT_PIN_2="<mint_pin_2_id>"

# Mint 3
# metaid-cli create-pin --path "/ft/mrc20/mint" --body "$(cat mint.json)" --address $btcAddress
MINT_PIN_3="<mint_pin_3_id>"

# 3. 挖矿确认这些交易
bitcoin-cli -regtest generatetoaddress 1 $btcAddress
```

#### 验证 Mint

```bash
# 查询 BTC 地址余额（应该有 3000 个 TEST）
curl "http://localhost:7777/api/mrc20/address/balance/${btcAddress}?chain=btc"

# 期望输出：
# {
#   "code": 1,
#   "message": "ok",
#   "data": {
#     "list": [
#       {
#         "id": "<TICK_ID>",
#         "name": "TEST",
#         "chain": "btc",
#         "balance": "3000",
#         "pendingInBalance": "0",
#         "pendingOutBalance": "0"
#       }
#     ],
#     "total": 1
#   }
# }
```

---

### 阶段 3: 验证余额查询

**目标**: 验证 UTXO 实时计算的余额是否正确

#### 测试余额查询 API

```bash
# 1. 查询地址的所有余额
curl "http://localhost:7777/api/mrc20/address/balance/${btcAddress}"

# 2. 查询地址在 BTC 链上的余额
curl "http://localhost:7777/api/mrc20/address/balance/${btcAddress}?chain=btc"

# 3. 查询地址的特定代币余额
curl "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${btcAddress}&tickId=${TICK_ID}&chain=btc"
```

#### 验证点

✅ **Balance 字段**:
- 应该等于 3000（3 次 mint，每次 1000）
- 💡 这个值是通过扫描 UTXO 计算的：`sum(UTXO where status=Available)`

✅ **PendingInBalance 字段**:
- 应该等于 0（没有待转入）

✅ **PendingOutBalance 字段**:
- 应该等于 0（没有待转出）

#### 检查日志

```bash
# 查看日志中的余额计算标记
tail -f app.log | grep "💡"

# 期望看到：
# [API] 💡 使用 UTXO 实时计算余额: address=xxx, chain=btc
# [Balance] Calculated from UTXO: chain=btc, address=xxx, tick=TEST, balance=3000, pendingOut=0, pendingIn=0, utxos=3
```

---

### 阶段 4: Teleport 测试（BTC → DOGE）

**目标**: 测试跨链转移，验证余额实时计算在 mempool 和 block 阶段的正确性

#### 步骤 1: 创建 Arrival PIN（DOGE 链）

**PIN 结构**:
```json
{
  "path": "/ft/mrc20/arrival",
  "contentType": "text/plain;charset=utf-8",
  "body": {
    "p": "mrc-20",
    "op": "arrival",
    "id": "<TICK_ID>",
    "chain": "doge",
    "amount": "500",
    "to": "<dogeAddress>",
    "assetOutpoint": "<要转移的_UTXO_txpoint>"
  }
}
```

**字段说明**:
- `id`: TEST 代币的 tick ID
- `chain`: 目标链（doge）
- `amount`: 转移数量（500）
- `to`: DOGE 链接收地址
- `assetOutpoint`: BTC 链上要转移的 UTXO（格式：`txid:vout`）

**创建 PIN**:
```bash
# 1. 获取 BTC 地址的 UTXO（选择一个包含 ≥500 代币的 UTXO）
curl "http://localhost:7777/api/mrc20/address/balance/${btcAddress}?chain=btc" | jq '.data.list[]'

# 假设找到一个 UTXO: txid:0，包含 1000 个代币
ASSET_OUTPOINT="<mint_tx_id_1>:0"

# 2. 创建 arrival PIN 的 JSON 数据
cat > arrival.json <<EOF
{
  "p": "mrc-20",
  "op": "arrival",
  "id": "${TICK_ID}",
  "chain": "doge",
  "amount": "500",
  "to": "${dogeAddress}",
  "assetOutpoint": "${ASSET_OUTPOINT}"
}
EOF

# 3. 在 DOGE 链创建 arrival PIN
# metaid-cli create-pin --chain doge --path "/ft/mrc20/arrival" --body "$(cat arrival.json)" --address $dogeAddress
ARRIVAL_PIN_ID="<arrival_pin_id>"

# 4. 挖矿确认（DOGE 链）
dogecoin-cli -regtest generatetoaddress 1 $dogeAddress
```

#### 步骤 2: 创建 Teleport PIN（BTC 链）

**PIN 结构**:
```json
{
  "path": "/ft/mrc20/transfer",
  "contentType": "text/plain;charset=utf-8",
  "body": {
    "p": "mrc-20",
    "op": "transfer",
    "id": "<TICK_ID>",
    "data": [
      {
        "type": "teleport",
        "chain": "doge",
        "amount": "500",
        "coord": "<ARRIVAL_PIN_ID>"
      }
    ]
  }
}
```

**字段说明**:
- `type`: 转账类型（teleport）
- `chain`: 目标链（doge）
- `amount`: 转移数量（500，必须与 arrival 一致）
- `coord`: arrival PIN 的 ID

**创建 PIN**:
```bash
# 1. 创建 teleport PIN 的 JSON 数据
cat > teleport.json <<EOF
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
EOF

# 2. 在 BTC 链创建 teleport PIN（使用包含 ASSET_OUTPOINT 的交易）
# metaid-cli create-pin --chain btc --path "/ft/mrc20/transfer" --body "$(cat teleport.json)" --address $btcAddress --spend-utxo $ASSET_OUTPOINT
TELEPORT_TX_ID="<teleport_tx_id>"

# 注意：这个交易必须花费 ASSET_OUTPOINT 对应的 UTXO
```

#### 步骤 3: 验证 Mempool 阶段余额

**等待交易进入 mempool**（不要立即挖矿）:
```bash
# 等待几秒让索引器处理 mempool 交易
sleep 5
```

**查询 BTC 链余额（源链）**:
```bash
curl "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${btcAddress}&tickId=${TICK_ID}&chain=btc"

# 期望输出（Mempool 阶段）：
# {
#   "code": 1,
#   "message": "ok",
#   "data": {
#     "balance": "2500",        # 3000 - 500 = 2500（可用余额减少）
#     "pendingIn": "0",
#     "pendingOut": "500"        # 待转出 500（UTXO 状态变为 TeleportPending）
#   }
# }
```

**查询 DOGE 链余额（目标链）**:
```bash
curl "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${dogeAddress}&tickId=${TICK_ID}&chain=doge"

# 期望输出（Mempool 阶段）：
# {
#   "code": 1,
#   "message": "ok",
#   "data": {
#     "balance": "0",            # 还未创建目标 UTXO
#     "pendingIn": "500",        # 待转入 500（TeleportPendingIn 记录）
#     "pendingOut": "0"
#   }
# }
```

**✅ 验证点（Mempool 阶段）**:
- ✅ 源链 Balance 从 3000 减少到 2500
- ✅ 源链 PendingOut 增加 500
- ✅ 目标链 PendingIn 增加 500
- ✅ 资产总量不变：`2500 + 500 + 500 = 3500`（等待确认）

#### 步骤 4: 挖矿确认并验证 Block 阶段余额

**挖矿确认 BTC 链交易**:
```bash
bitcoin-cli -regtest generatetoaddress 1 $btcAddress

# 等待索引器处理区块
sleep 5
```

**查询 BTC 链余额（源链）**:
```bash
curl "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${btcAddress}&tickId=${TICK_ID}&chain=btc"

# 期望输出（Block 阶段）：
# {
#   "code": 1,
#   "message": "ok",
#   "data": {
#     "balance": "2500",        # 保持 2500（源 UTXO 已标记为 Spent）
#     "pendingIn": "0",
#     "pendingOut": "0"          # PendingOut 清零（UTXO 状态从 TeleportPending → Spent）
#   }
# }
```

**查询 DOGE 链余额（目标链）**:
```bash
curl "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${dogeAddress}&tickId=${TICK_ID}&chain=doge"

# 期望输出（Block 阶段）：
# {
#   "code": 1,
#   "message": "ok",
#   "data": {
#     "balance": "500",          # Balance 增加 500（创建了新的 Available UTXO）
#     "pendingIn": "0",          # PendingIn 清零（TeleportPendingIn 记录已删除）
#     "pendingOut": "0"
#   }
# }
```

**✅ 验证点（Block 阶段）**:
- ✅ 源链 Balance = 2500，PendingOut = 0
- ✅ 目标链 Balance = 500，PendingIn = 0
- ✅ 资产总量不变：`2500 + 500 = 3000`

---

### 阶段 5: 验证资产总量

**目标**: 验证跨链转移后，资产总量没有增发或丢失

#### 查询资产验证报告

```bash
# 验证 TEST 代币的总量
curl "http://localhost:7777/api/mrc20/admin/verify/supply/${TICK_ID}"

# 期望输出：
# {
#   "code": 1,
#   "message": "ok",
#   "data": {
#     "tickId": "<TICK_ID>",
#     "tickName": "TEST",
#     "status": "✅ PASSED",
#     "expectedTotal": "3000",
#     "totalUtxo": "3000",       # UTXO 总和 = 3000
#     "totalBalance": "3000",    # Balance 总和 = 3000
#     "totalPendingOut": "0",
#     "totalPendingIn": "0",
#     "utxoCount": 3,
#     "utxoByChain": {
#       "btc": "2500",           # BTC 链剩余 2500
#       "doge": "500"            # DOGE 链有 500
#     },
#     "balanceByChain": {
#       "btc": "2500",
#       "doge": "500"
#     },
#     "errors": [],
#     "warnings": []
#   }
# }
```

**✅ 验证点**:
- ✅ `totalUtxo` = 3000（UTXO 总和等于已 mint 的数量）
- ✅ `totalBalance` = 3000（Balance 总和等于 UTXO 总和）
- ✅ `totalPendingOut` = 0（无待转出）
- ✅ `totalPendingIn` = 0（无待转入）
- ✅ `status` = "✅ PASSED"（验证通过）
- ✅ `errors` = []（无错误）

---

### 阶段 6: 测试多次 Teleport

**目标**: 测试多次跨链转移，验证余额计算的稳定性

#### 测试场景：BTC → DOGE → BTC

**步骤 1: 第二次 Teleport（DOGE → BTC）**

```bash
# 1. 获取 DOGE 链上的 UTXO
# (假设在 DOGE 链上有一个 500 的 UTXO)
DOGE_UTXO="<doge_utxo_txpoint>"

# 2. 创建 Arrival PIN（BTC 链）
cat > arrival2.json <<EOF
{
  "p": "mrc-20",
  "op": "arrival",
  "id": "${TICK_ID}",
  "chain": "btc",
  "amount": "200",
  "to": "${btcAddress}",
  "assetOutpoint": "${DOGE_UTXO}"
}
EOF

# 在 BTC 链创建 arrival PIN
ARRIVAL_PIN_2="<arrival_pin_2_id>"
bitcoin-cli -regtest generatetoaddress 1 $btcAddress

# 3. 创建 Teleport PIN（DOGE 链）
cat > teleport2.json <<EOF
{
  "p": "mrc-20",
  "op": "transfer",
  "id": "${TICK_ID}",
  "data": [
    {
      "type": "teleport",
      "chain": "btc",
      "amount": "200",
      "coord": "${ARRIVAL_PIN_2}"
    }
  ]
}
EOF

# 在 DOGE 链创建 teleport PIN
TELEPORT_TX_2="<teleport_tx_2_id>"
```

**步骤 2: 验证余额变化**

**Mempool 阶段**:
```bash
# DOGE 链（源链）
curl "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${dogeAddress}&tickId=${TICK_ID}&chain=doge"
# 期望: balance=300, pendingOut=200

# BTC 链（目标链）
curl "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${btcAddress}&tickId=${TICK_ID}&chain=btc"
# 期望: balance=2500, pendingIn=200
```

**Block 阶段**:
```bash
# 挖矿确认
dogecoin-cli -regtest generatetoaddress 1 $dogeAddress
sleep 5

# DOGE 链（源链）
curl "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${dogeAddress}&tickId=${TICK_ID}&chain=doge"
# 期望: balance=300, pendingOut=0

# BTC 链（目标链）
curl "http://localhost:7777/api/mrc20/tick/AddressBalance?address=${btcAddress}&tickId=${TICK_ID}&chain=btc"
# 期望: balance=2700, pendingIn=0
```

**验证资产总量**:
```bash
curl "http://localhost:7777/api/mrc20/admin/verify/supply/${TICK_ID}"
# 期望: totalBalance=3000 (2700 + 300)
```

---

## 📊 测试检查清单

### 余额查询功能
- [ ] `GET /api/mrc20/address/balance/:address` 返回正确
- [ ] `GET /api/mrc20/address/balance/:address?chain=btc` 返回正确
- [ ] `GET /api/mrc20/tick/AddressBalance` 返回正确
- [ ] Balance 字段通过 UTXO 计算正确
- [ ] PendingOut 字段通过 UTXO 状态计算正确
- [ ] PendingIn 字段通过 PendingIn 表计算正确

### Teleport 功能（Mempool 阶段）
- [ ] 源链 Balance 立即减少
- [ ] 源链 PendingOut 立即增加
- [ ] 目标链 PendingIn 立即增加
- [ ] 资产总量不变（Balance + PendingOut + PendingIn = 总量）

### Teleport 功能（Block 阶段）
- [ ] 源链 PendingOut 清零
- [ ] 目标链 Balance 增加
- [ ] 目标链 PendingIn 清零
- [ ] 资产总量不变

### 资产验证
- [ ] UTXO 总和 = 已 mint 的数量
- [ ] Balance 总和 = UTXO 总和
- [ ] PendingOut 总和 = PendingIn 总和（跨链平衡）
- [ ] 验证报告 status = "✅ PASSED"
- [ ] 无错误（errors = []）

### 日志验证
- [ ] 看到 `💡 使用 UTXO 实时计算余额` 日志
- [ ] 看到 `[Balance] Calculated from UTXO` 日志
- [ ] 看到 `[TeleportV2]` 相关日志
- [ ] 无错误日志

---

## 🐛 常见问题排查

### 问题 1: 余额不正确

**检查**:
```bash
# 1. 查看 UTXO 详情
curl "http://localhost:7777/api/mrc20/tx/history?txId=<tx_id>&index=0"

# 2. 查看 UTXO 状态
grep "UTXO status" app.log | grep "<tx_id>"

# 3. 检查余额计算日志
grep "Calculated from UTXO" app.log | tail -20
```

### 问题 2: Teleport 卡住

**检查**:
```bash
# 1. 查看 Teleport 状态
curl "http://localhost:7777/api/mrc20/admin/teleport/v2/list"

# 2. 查看 Teleport 详情
curl "http://localhost:7777/api/mrc20/admin/teleport/v2/detail/<teleport_id>"

# 3. 查看错误日志
grep "TeleportV2.*❌" app.log | tail -20
```

### 问题 3: 资产验证失败

**检查**:
```bash
# 1. 查看验证报告
curl "http://localhost:7777/api/mrc20/admin/verify/supply/${TICK_ID}" | jq .

# 2. 查看 errors 和 warnings
curl "http://localhost:7777/api/mrc20/admin/verify/supply/${TICK_ID}" | jq '.data.errors, .data.warnings'

# 3. 检查所有 UTXO
grep "mrc20_utxo" app.log | grep "$TICK_ID"
```

---

## ✅ 成功标准

测试通过的标准：

1. ✅ 所有余额查询 API 返回正确结果
2. ✅ Mempool 阶段余额实时变化正确
3. ✅ Block 阶段余额最终状态正确
4. ✅ 多次 Teleport 后余额仍然正确
5. ✅ 资产验证报告显示 "✅ PASSED"
6. ✅ 无资产增发或丢失
7. ✅ 日志中出现 `💡 使用 UTXO 实时计算余额` 标记
8. ✅ 无错误日志

---

## 📝 测试报告模板

测试完成后，请填写以下报告：

```markdown
# UTXO 余额架构测试报告

**测试时间**: 2026-02-XX
**测试环境**: BTC + DOGE Regtest
**测试代币**: TEST (总量: 3000)

## 测试结果

### 1. 余额查询功能
- [ ] ✅ / ❌ 余额查询 API 正确
- [ ] ✅ / ❌ Balance 计算正确
- [ ] ✅ / ❌ PendingOut 计算正确
- [ ] ✅ / ❌ PendingIn 计算正确

### 2. Teleport 功能
- [ ] ✅ / ❌ Mempool 阶段余额变化正确
- [ ] ✅ / ❌ Block 阶段余额变化正确
- [ ] ✅ / ❌ 多次 Teleport 正确

### 3. 资产验证
- [ ] ✅ / ❌ 资产总量不变
- [ ] ✅ / ❌ 验证报告通过

## 发现的问题

1. ...
2. ...

## 结论

[ ] ✅ 测试通过，可以部署
[ ] ❌ 测试失败，需要修复

```

---

**祝测试顺利！** 🎉
