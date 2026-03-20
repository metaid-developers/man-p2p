# MRC20 UTXO 查询指南

## 🎯 查询未花费 UTXO 的方法

在测试和使用 MRC20 时，经常需要查询某个地址的未花费 UTXO，用于：
- 选择要转账的 UTXO
- 选择要 Teleport 的 UTXO
- 验证余额计算

---

## 📍 方法 1: 使用调试 API（推荐）

### API 接口

```bash
GET /api/mrc20/debug/utxo-status/:address/:tickId
```

### 使用示例

```bash
# 查询 BTC 地址在 TEST 代币下的所有 UTXO
curl "http://localhost:7777/api/mrc20/debug/utxo-status/${btcAddress}/${TICK_ID}"
```

### 响应示例

```json
{
  "code": 1,
  "message": "ok",
  "data": {
    "address": "mwK9V8H8E5FRjNXKqR4z8QxJGV8YP2FrNz",
    "tickId": "abc123...",
    "count": 3,
    "utxos": [
      {
        "txPoint": "tx1:0",
        "amount": "1000",
        "status": 0,
        "statusName": "Available",
        "fromAddress": "...",
        "toAddress": "mwK9V8H8E5FRjNXKqR4z8QxJGV8YP2FrNz",
        "blockHeight": 100
      },
      {
        "txPoint": "tx2:1",
        "amount": "500",
        "status": 0,
        "statusName": "Available",
        "fromAddress": "...",
        "toAddress": "mwK9V8H8E5FRjNXKqR4z8QxJGV8YP2FrNz",
        "blockHeight": 150
      },
      {
        "txPoint": "tx3:0",
        "amount": "200",
        "status": -1,
        "statusName": "Spent",
        "fromAddress": "...",
        "toAddress": "mwK9V8H8E5FRjNXKqR4z8QxJGV8YP2FrNz",
        "blockHeight": 120
      }
    ]
  }
}
```

### UTXO 状态说明

| status | statusName | 含义 | 是否可用 |
|--------|------------|------|---------|
| 0 | Available | 可用 | ✅ 可以花费 |
| 1 | TeleportPending | Teleport 待确认 | ❌ 已锁定 |
| 2 | TransferPending | Transfer 待确认 | ❌ 已锁定 |
| -1 | Spent | 已花费 | ❌ 已花费 |

---

## 📍 方法 2: 使用 jq 过滤未花费 UTXO

### 只显示可用的 UTXO（status=0）

```bash
curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${btcAddress}/${TICK_ID}" | \
  jq '.data.utxos[] | select(.status == 0)'
```

**输出**:
```json
{
  "txPoint": "tx1:0",
  "amount": "1000",
  "status": 0,
  "statusName": "Available",
  "fromAddress": "...",
  "toAddress": "mwK9V8H8E5FRjNXKqR4z8QxJGV8YP2FrNz",
  "blockHeight": 100
}
{
  "txPoint": "tx2:1",
  "amount": "500",
  "status": 0,
  "statusName": "Available",
  "fromAddress": "...",
  "toAddress": "mwK9V8H8E5FRjNXKqR4z8QxJGV8YP2FrNz",
  "blockHeight": 150
}
```

### 只显示 txPoint 和 amount（简洁模式）

```bash
curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${btcAddress}/${TICK_ID}" | \
  jq '.data.utxos[] | select(.status == 0) | {txPoint, amount}'
```

**输出**:
```json
{
  "txPoint": "tx1:0",
  "amount": "1000"
}
{
  "txPoint": "tx2:1",
  "amount": "500"
}
```

### 按金额排序

```bash
# 从大到小
curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${btcAddress}/${TICK_ID}" | \
  jq '.data.utxos | map(select(.status == 0)) | sort_by(.amount | tonumber) | reverse'

# 从小到大
curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${btcAddress}/${TICK_ID}" | \
  jq '.data.utxos | map(select(.status == 0)) | sort_by(.amount | tonumber)'
```

### 统计可用 UTXO 总额

```bash
curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${btcAddress}/${TICK_ID}" | \
  jq '[.data.utxos[] | select(.status == 0) | .amount | tonumber] | add'
```

**输出**: `1500` (1000 + 500)

---

## 📍 方法 3: 跨链查询所有 UTXO

### 查询所有链上的 UTXO

```bash
# 定义函数
query_all_chains_utxos() {
  local address=$1
  local tickId=$2

  echo "========== BTC Chain =========="
  curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${address}/${tickId}" | \
    jq -r '.data.utxos[] | select(.status == 0) | "\(.txPoint): \(.amount)"'

  echo ""
  echo "========== DOGE Chain =========="
  # 假设你有 DOGE 地址
  # curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${dogeAddress}/${tickId}" | \
  #   jq -r '.data.utxos[] | select(.status == 0) | "\(.txPoint): \(.amount)"'
}

# 使用
query_all_chains_utxos $btcAddress $TICK_ID
```

---

## 🔧 实用脚本

### 脚本 1: 查找指定金额的 UTXO

```bash
#!/bin/bash
# find_utxo.sh - 查找大于等于指定金额的 UTXO

address=$1
tickId=$2
minAmount=$3

if [ -z "$address" ] || [ -z "$tickId" ] || [ -z "$minAmount" ]; then
  echo "Usage: $0 <address> <tickId> <minAmount>"
  exit 1
fi

echo "查找 >= ${minAmount} 的 UTXO..."
curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${address}/${tickId}" | \
  jq --arg min "$minAmount" \
    '.data.utxos[] | select(.status == 0 and (.amount | tonumber) >= ($min | tonumber))'
```

**使用**:
```bash
# 查找 >= 500 的 UTXO
./find_utxo.sh $btcAddress $TICK_ID 500
```

### 脚本 2: 选择最优 UTXO

```bash
#!/bin/bash
# select_best_utxo.sh - 选择最接近目标金额的 UTXO

address=$1
tickId=$2
targetAmount=$3

if [ -z "$address" ] || [ -z "$tickId" ] || [ -z "$targetAmount" ]; then
  echo "Usage: $0 <address> <tickId> <targetAmount>"
  exit 1
fi

echo "选择最接近 ${targetAmount} 的 UTXO..."
curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${address}/${tickId}" | \
  jq --arg target "$targetAmount" \
    '[.data.utxos[] | select(.status == 0 and (.amount | tonumber) >= ($target | tonumber))] |
     sort_by(.amount | tonumber) |
     first'
```

**使用**:
```bash
# 需要 500，选择最小的满足条件的 UTXO
./select_best_utxo.sh $btcAddress $TICK_ID 500
```

### 脚本 3: 列出所有可用 UTXO（表格形式）

```bash
#!/bin/bash
# list_utxos.sh - 以表格形式列出所有可用 UTXO

address=$1
tickId=$2

if [ -z "$address" ] || [ -z "$tickId" ]; then
  echo "Usage: $0 <address> <tickId>"
  exit 1
fi

echo "可用 UTXO 列表："
echo "----------------------------------------"
printf "%-40s %15s %12s\n" "TxPoint" "Amount" "BlockHeight"
echo "----------------------------------------"

curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${address}/${tickId}" | \
  jq -r '.data.utxos[] |
         select(.status == 0) |
         "\(.txPoint) \(.amount) \(.blockHeight)"' | \
  while read txpoint amount height; do
    printf "%-40s %15s %12s\n" "$txpoint" "$amount" "$height"
  done

echo "----------------------------------------"

# 统计总额
total=$(curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${address}/${tickId}" | \
  jq '[.data.utxos[] | select(.status == 0) | .amount | tonumber] | add')

echo "总计: $total"
```

**输出示例**:
```
可用 UTXO 列表：
----------------------------------------
TxPoint                                  Amount    BlockHeight
----------------------------------------
abc123...:0                                1000          100
def456...:1                                 500          150
ghi789...:2                                 300          180
----------------------------------------
总计: 1800
```

---

## 🎯 测试场景应用

### 场景 1: 选择要拆分的 UTXO

```bash
# 1. 查询所有可用 UTXO
curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${btcAddress}/${TICK_ID}" | \
  jq '.data.utxos[] | select(.status == 0)'

# 2. 选择一个大于 1000 的 UTXO 进行拆分
UTXO_TO_SPLIT=$(curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${btcAddress}/${TICK_ID}" | \
  jq -r '[.data.utxos[] | select(.status == 0 and (.amount | tonumber) >= 1000)] | first | .txPoint')

echo "选中的 UTXO: $UTXO_TO_SPLIT"

# 3. 创建拆分 transfer
# ...
```

### 场景 2: 选择要 Teleport 的 UTXO

```bash
# 要 teleport 500 个代币
TELEPORT_AMOUNT=500

# 1. 查找 >= 500 的最小 UTXO
UTXO_TO_TELEPORT=$(curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${btcAddress}/${TICK_ID}" | \
  jq -r --arg amt "$TELEPORT_AMOUNT" \
    '[.data.utxos[] | select(.status == 0 and (.amount | tonumber) >= ($amt | tonumber))] |
     sort_by(.amount | tonumber) |
     first |
     .txPoint')

echo "选中用于 Teleport 的 UTXO: $UTXO_TO_TELEPORT"

# 2. 创建 Arrival (在目标链)
ASSET_OUTPOINT=$UTXO_TO_TELEPORT
# ...

# 3. 创建 Teleport (在源链)
# ...
```

### 场景 3: 验证拆分结果

```bash
# 拆分前
echo "拆分前的 UTXO:"
curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${btcAddress}/${TICK_ID}" | \
  jq '.data.utxos[] | select(.status == 0) | {txPoint, amount}'

# 执行拆分...
# 创建 transfer PIN，拆分 1000 → 600 + 400

# 挖矿确认
bitcoin-cli -regtest generatetoaddress 1 $btcAddress
sleep 5

# 拆分后
echo "拆分后的 UTXO:"
curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${btcAddress}/${TICK_ID}" | \
  jq '.data.utxos[] | select(.status == 0) | {txPoint, amount}'

# 应该看到 2 个新 UTXO: 600 和 400
```

---

## 📊 UTXO 状态监控

### 实时监控脚本

```bash
#!/bin/bash
# monitor_utxos.sh - 实时监控 UTXO 状态变化

address=$1
tickId=$2
interval=${3:-5}  # 默认 5 秒刷新

if [ -z "$address" ] || [ -z "$tickId" ]; then
  echo "Usage: $0 <address> <tickId> [interval_seconds]"
  exit 1
fi

while true; do
  clear
  echo "=========================================="
  echo "UTXO 状态监控"
  echo "时间: $(date '+%Y-%m-%d %H:%M:%S')"
  echo "=========================================="

  # 统计各状态的 UTXO
  available=$(curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${address}/${tickId}" | \
    jq '[.data.utxos[] | select(.status == 0)] | length')

  teleportPending=$(curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${address}/${tickId}" | \
    jq '[.data.utxos[] | select(.status == 1)] | length')

  transferPending=$(curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${address}/${tickId}" | \
    jq '[.data.utxos[] | select(.status == 2)] | length')

  spent=$(curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${address}/${tickId}" | \
    jq '[.data.utxos[] | select(.status == -1)] | length')

  echo ""
  echo "状态统计:"
  echo "  Available:        $available"
  echo "  TeleportPending:  $teleportPending"
  echo "  TransferPending:  $transferPending"
  echo "  Spent:            $spent"
  echo ""

  # 显示可用 UTXO
  echo "可用 UTXO:"
  curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${address}/${tickId}" | \
    jq -r '.data.utxos[] | select(.status == 0) | "  \(.txPoint): \(.amount)"'

  # 显示 Pending UTXO
  if [ "$teleportPending" -gt 0 ]; then
    echo ""
    echo "Teleport Pending:"
    curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${address}/${tickId}" | \
      jq -r '.data.utxos[] | select(.status == 1) | "  \(.txPoint): \(.amount)"'
  fi

  if [ "$transferPending" -gt 0 ]; then
    echo ""
    echo "Transfer Pending:"
    curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${address}/${tickId}" | \
      jq -r '.data.utxos[] | select(.status == 2) | "  \(.txPoint): \(.amount)"'
  fi

  echo ""
  echo "按 Ctrl+C 退出"
  sleep $interval
done
```

**使用**:
```bash
# 每 5 秒刷新
./monitor_utxos.sh $btcAddress $TICK_ID 5
```

---

## 📝 快速参考

### 常用命令

```bash
# 1. 查看所有 UTXO（包括已花费）
curl "http://localhost:7777/api/mrc20/debug/utxo-status/${address}/${tickId}"

# 2. 只看可用的
curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${address}/${tickId}" | \
  jq '.data.utxos[] | select(.status == 0)'

# 3. 只看 txPoint 和 amount
curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${address}/${tickId}" | \
  jq '.data.utxos[] | select(.status == 0) | {txPoint, amount}'

# 4. 统计可用余额
curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${address}/${tickId}" | \
  jq '[.data.utxos[] | select(.status == 0) | .amount | tonumber] | add'

# 5. 查找最大的 UTXO
curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${address}/${tickId}" | \
  jq '[.data.utxos[] | select(.status == 0)] | sort_by(.amount | tonumber) | reverse | first'

# 6. 查找最小的 UTXO
curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${address}/${tickId}" | \
  jq '[.data.utxos[] | select(.status == 0)] | sort_by(.amount | tonumber) | first'

# 7. 保存到文件
curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${address}/${tickId}" | \
  jq '.data.utxos[] | select(.status == 0)' > available_utxos.json
```

### 在脚本中使用

```bash
#!/bin/bash

# 查询并保存到变量
UTXOS_JSON=$(curl -s "http://localhost:7777/api/mrc20/debug/utxo-status/${btcAddress}/${TICK_ID}")

# 获取可用 UTXO 数量
AVAILABLE_COUNT=$(echo "$UTXOS_JSON" | jq '[.data.utxos[] | select(.status == 0)] | length')

# 获取第一个可用 UTXO 的 txPoint
FIRST_UTXO=$(echo "$UTXOS_JSON" | jq -r '[.data.utxos[] | select(.status == 0)] | first | .txPoint')

# 获取所有可用 UTXO 的 txPoint（数组）
ALL_UTXOS=($(echo "$UTXOS_JSON" | jq -r '.data.utxos[] | select(.status == 0) | .txPoint'))

echo "可用 UTXO 数量: $AVAILABLE_COUNT"
echo "第一个 UTXO: $FIRST_UTXO"
echo "所有 UTXO: ${ALL_UTXOS[@]}"
```

---

## ✅ 总结

### 核心 API

```bash
GET /api/mrc20/debug/utxo-status/:address/:tickId
```

### 关键信息

- **status=0**: 可用，可以花费
- **status=1**: TeleportPending，已锁定
- **status=2**: TransferPending，已锁定
- **status=-1**: Spent，已花费

### 常见用途

1. ✅ 选择要转账的 UTXO
2. ✅ 选择要 Teleport 的 UTXO
3. ✅ 验证拆分结果
4. ✅ 监控 UTXO 状态变化
5. ✅ 统计可用余额

---

**准备好查询 UTXO 了吗？** 🔍
