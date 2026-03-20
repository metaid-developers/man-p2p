# MRC20 PIN 结构 - 正确版本 ✅

## ⚠️ 重要说明

MRC20 PIN 由两部分组成：
1. **PIN 元数据**（Path、ContentType 等）
2. **PIN 内容体**（ContentBody - 这是一个 JSON 数组）

---

## 📌 PIN 完整结构

### PIN 元数据（所有操作通用）

```
Path: /ft/mrc20/{operation}
ContentType: application/json 或 text/plain;charset=utf-8
ContentBody: [JSON 数组或对象]
```

---

## 1️⃣ Deploy

### PIN 元数据
```
Path: /ft/mrc20/deploy
ContentType: application/json
```

### ContentBody（JSON 对象）
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

---

## 2️⃣ Mint

### PIN 元数据
```
Path: /ft/mrc20/mint
ContentType: application/json
```

### ContentBody（JSON 对象）
```json
{
  "p": "mrc-20",
  "op": "mint",
  "id": "abc123...",
  "vout": 0
}
```

---

## 3️⃣ Transfer（普通转账/拆分）

### PIN 元数据
```
Path: /ft/mrc20/transfer
ContentType: application/json
```

### ContentBody（JSON 数组）⚠️ 注意是数组！

#### 单个接收者
```json
[
  {
    "id": "abc123...",
    "amount": "100",
    "vout": 1
  }
]
```

#### 拆分（多个输出）
```json
[
  {
    "id": "abc123...",
    "amount": "600",
    "vout": 1
  },
  {
    "id": "abc123...",
    "amount": "400",
    "vout": 2
  }
]
```

#### 多个接收者
```json
[
  {
    "id": "abc123...",
    "amount": "300",
    "vout": 1
  },
  {
    "id": "abc123...",
    "amount": "200",
    "vout": 2
  },
  {
    "id": "abc123...",
    "amount": "500",
    "vout": 3
  }
]
```

**⚠️ 关键字段说明**:
- `id`: MRC20 代币的 tick ID
- `amount`: 转账金额（字符串）
- `vout`: 对应 BTC 交易的输出索引（从 1 开始）

**⚠️ 重要**:
- ContentBody 是一个 JSON 数组，不是对象！
- 没有 `p`、`op` 等字段，这些信息在 Path 中
- 没有外层的 `data` 包装

---

## 4️⃣ Arrival（跨链接收声明）

### PIN 元数据
```
Path: /ft/mrc20/arrival
ContentType: application/json
```

### ContentBody（JSON 对象）
```json
{
  "p": "mrc-20",
  "op": "arrival",
  "id": "abc123...",
  "chain": "doge",
  "amount": "500",
  "to": "nXeVyJ7KxQoYhPQvAFJZ8c9qR5T3Gx8wMz",
  "assetOutpoint": "def456...:0"
}
```

---

## 5️⃣ Teleport（跨链转移）

### PIN 元数据
```
Path: /ft/mrc20/transfer
ContentType: application/json
```

### ContentBody（JSON 数组）⚠️ 注意是数组！

```json
[
  {
    "type": "teleport",
    "id": "abc123...",
    "chain": "doge",
    "amount": "500",
    "coord": "ghi789..."
  }
]
```

**⚠️ 关键字段说明**:
- `type`: 必须是 "teleport"
- `id`: MRC20 代币的 tick ID
- `chain`: 目标链名称
- `amount`: 转移金额（字符串）
- `coord`: arrival PIN 的 ID
- **没有 `vout` 字段**（teleport 会销毁源 UTXO）

---

## 🔄 完整示例对比

### ❌ 错误的 Transfer PIN（我之前给的）

```json
{
  "p": "mrc-20",
  "op": "transfer",
  "id": "abc123...",
  "data": [
    {
      "type": "data",
      "amount": "600",
      "vout": 1
    }
  ]
}
```

### ✅ 正确的 Transfer PIN

**PIN 元数据**:
```
Path: /ft/mrc20/transfer
```

**ContentBody**:
```json
[
  {
    "id": "abc123...",
    "amount": "600",
    "vout": 1
  },
  {
    "id": "abc123...",
    "amount": "400",
    "vout": 2
  }
]
```

---

### ❌ 错误的 Teleport PIN（我之前给的）

```json
{
  "p": "mrc-20",
  "op": "transfer",
  "id": "abc123...",
  "data": [
    {
      "type": "teleport",
      "chain": "doge",
      "amount": "500",
      "coord": "ghi789..."
    }
  ]
}
```

### ✅ 正确的 Teleport PIN

**PIN 元数据**:
```
Path: /ft/mrc20/transfer
```

**ContentBody**:
```json
[
  {
    "type": "teleport",
    "id": "abc123...",
    "chain": "doge",
    "amount": "500",
    "coord": "ghi789..."
  }
]
```

---

## 📊 字段对比表

### Transfer vs Teleport

| 字段 | Transfer | Teleport | 说明 |
|------|----------|----------|------|
| `type` | ❌ 不需要 | ✅ "teleport" | Teleport 必须有 |
| `id` | ✅ 必需 | ✅ 必需 | MRC20 tick ID |
| `amount` | ✅ 必需 | ✅ 必需 | 转账金额 |
| `vout` | ✅ 必需 | ❌ 不需要 | Transfer 需要，Teleport 不需要 |
| `chain` | ❌ 不需要 | ✅ 必需 | 目标链名称 |
| `coord` | ❌ 不需要 | ✅ 必需 | arrival PIN ID |

---

## 🎯 实际使用示例

### 场景 1: 拆分 UTXO (1000 → 600 + 400)

**创建 transfer.json**:
```bash
cat > transfer.json <<'EOF'
[
  {
    "id": "TICK_ID_HERE",
    "amount": "600",
    "vout": 1
  },
  {
    "id": "TICK_ID_HERE",
    "amount": "400",
    "vout": 2
  }
]
EOF

# 替换 TICK_ID
sed -i "s/TICK_ID_HERE/${TICK_ID}/g" transfer.json
```

**创建 PIN**:
```bash
# 使用你的工具创建 PIN
# Path: /ft/mrc20/transfer
# ContentBody: $(cat transfer.json)
# 交易必须有 2 个输出：vout=1 和 vout=2
```

---

### 场景 2: Teleport (400 → DOGE)

**步骤 1: 创建 Arrival (DOGE 链)**

**arrival.json**:
```bash
cat > arrival.json <<'EOF'
{
  "p": "mrc-20",
  "op": "arrival",
  "id": "TICK_ID_HERE",
  "chain": "doge",
  "amount": "400",
  "to": "DOGE_ADDRESS_HERE",
  "assetOutpoint": "ASSET_OUTPOINT_HERE"
}
EOF

# 替换占位符
sed -i "s/TICK_ID_HERE/${TICK_ID}/g" arrival.json
sed -i "s/DOGE_ADDRESS_HERE/${dogeAddress}/g" arrival.json
sed -i "s/ASSET_OUTPOINT_HERE/${UTXO_400}/g" arrival.json
```

**创建 Arrival PIN**:
```bash
# Path: /ft/mrc20/arrival
# ContentBody: $(cat arrival.json)
# 在 DOGE 链创建
```

**步骤 2: 创建 Teleport (BTC 链)**

**teleport.json**:
```bash
cat > teleport.json <<'EOF'
[
  {
    "type": "teleport",
    "id": "TICK_ID_HERE",
    "chain": "doge",
    "amount": "400",
    "coord": "ARRIVAL_PIN_ID_HERE"
  }
]
EOF

# 替换占位符
sed -i "s/TICK_ID_HERE/${TICK_ID}/g" teleport.json
sed -i "s/ARRIVAL_PIN_ID_HERE/${ARRIVAL_PIN_ID}/g" teleport.json
```

**创建 Teleport PIN**:
```bash
# Path: /ft/mrc20/transfer
# ContentBody: $(cat teleport.json)
# 在 BTC 链创建
# ⚠️ 交易必须花费 UTXO_400
```

---

## 🔍 验证 PIN 内容

### 检查 Transfer PIN

```bash
# 1. 读取 PIN 内容
CONTENT=$(cat transfer.json)

# 2. 验证是否是有效的 JSON 数组
echo $CONTENT | jq . > /dev/null && echo "✅ 有效的 JSON" || echo "❌ 无效的 JSON"

# 3. 检查数组长度
echo $CONTENT | jq 'length'

# 4. 检查每个元素的字段
echo $CONTENT | jq '.[] | {id, amount, vout}'

# 5. 验证必需字段
echo $CONTENT | jq '.[] | select(.id == null or .amount == null or .vout == null)' && echo "❌ 缺少必需字段" || echo "✅ 字段完整"
```

### 检查 Teleport PIN

```bash
# 1. 读取 PIN 内容
CONTENT=$(cat teleport.json)

# 2. 验证是否是数组
echo $CONTENT | jq 'type' # 应该输出 "array"

# 3. 检查 type 字段
echo $CONTENT | jq '.[0].type' # 应该输出 "teleport"

# 4. 检查必需字段
echo $CONTENT | jq '.[0] | {type, id, chain, amount, coord}'
```

---

## 📝 快速模板

### Transfer 模板
```bash
cat > transfer.json <<'EOF'
[
  {
    "id": "${TICK_ID}",
    "amount": "${AMOUNT}",
    "vout": ${VOUT}
  }
]
EOF
```

### Teleport 模板
```bash
cat > teleport.json <<'EOF'
[
  {
    "type": "teleport",
    "id": "${TICK_ID}",
    "chain": "${TARGET_CHAIN}",
    "amount": "${AMOUNT}",
    "coord": "${ARRIVAL_PIN_ID}"
  }
]
EOF
```

---

## ✅ 关键点总结

### 正确的理解

1. **Transfer PIN**:
   - Path: `/ft/mrc20/transfer`
   - ContentBody: **JSON 数组** `[{id, amount, vout}, ...]`
   - 没有外层包装，没有 `p`、`op`、`data` 等字段

2. **Teleport PIN**:
   - Path: `/ft/mrc20/transfer` （和普通 transfer 一样）
   - ContentBody: **JSON 数组** `[{type: "teleport", id, chain, amount, coord}]`
   - 通过 `type: "teleport"` 区分

3. **Arrival PIN**:
   - Path: `/ft/mrc20/arrival`
   - ContentBody: **JSON 对象** `{p, op, id, chain, amount, to, assetOutpoint}`

### 常见错误

❌ 在 ContentBody 中包含 `p`、`op` 等元数据（Transfer）
❌ 使用 `data` 字段包装数组
❌ Transfer 用对象而不是数组
❌ Teleport 忘记 `type: "teleport"` 字段
❌ Teleport 包含 `vout` 字段（不需要）

---

**非常抱歉之前给出了错误的信息！** 🙏

现在这个版本是正确的，已经根据实际代码验证过了。
