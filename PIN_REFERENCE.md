# MRC20 PIN 结构快速参考

## 📌 所有 MRC20 操作的 PIN 结构

---

## 1. Deploy (部署代币)

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

**字段说明**:
- `tick`: 代币名称（4个字符）
- `max`: 总供应量
- `lim`: 单次 mint 上限
- `dec`: 小数位数

---

## 2. Mint (铸造代币)

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

**字段说明**:
- `id`: deploy 时的 tick ID
- `vout`: deploy PIN 的输出索引（通常是 0）

---

## 3. Transfer - 普通转账/拆分

**路径**: `/ft/mrc20/transfer`

### 3.1 给别人转账（单个接收者）

**Body**:
```json
{
  "p": "mrc-20",
  "op": "transfer",
  "id": "${TICK_ID}",
  "data": [
    {
      "type": "data",
      "amount": "100",
      "vout": 1
    }
  ]
}
```

**交易结构**:
```
Inputs: 花费一个 UTXO (比如 1000 个代币)
Outputs:
  - vout=0: OP_RETURN (PIN)
  - vout=1: 接收者地址，收到 100 个代币
  - vout=2: (可选) 找零地址，收到 900 个代币
```

### 3.2 拆分 UTXO（给自己）

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

**交易结构**:
```
Inputs: 花费一个 UTXO (1000 个代币)
Outputs:
  - vout=0: OP_RETURN (PIN)
  - vout=1: 自己地址，收到 600 个代币
  - vout=2: 自己地址，收到 400 个代币
```

### 3.3 多个接收者

**Body**:
```json
{
  "p": "mrc-20",
  "op": "transfer",
  "id": "${TICK_ID}",
  "data": [
    {
      "type": "data",
      "amount": "100",
      "vout": 1
    },
    {
      "type": "data",
      "amount": "200",
      "vout": 2
    },
    {
      "type": "data",
      "amount": "700",
      "vout": 3
    }
  ]
}
```

**交易结构**:
```
Inputs: 花费一个 UTXO (1000 个代币)
Outputs:
  - vout=0: OP_RETURN (PIN)
  - vout=1: 接收者A，收到 100 个代币
  - vout=2: 接收者B，收到 200 个代币
  - vout=3: 自己（找零），收到 700 个代币
```

**⚠️ 重要规则**:
1. **vout 必须连续**: 从 1 开始，依次递增
2. **总金额必须匹配**: 所有 amount 之和 = 花费的 UTXO 金额
3. **vout 对应 BTC 交易输出**: vout=1 对应 BTC 交易的第 1 个输出（索引 1）
4. **接收者地址**: 在 BTC 交易中指定，不在 PIN 中

---

## 4. Arrival (跨链接收声明)

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
  "assetOutpoint": "${BTC_UTXO_TXPOINT}"
}
```

**字段说明**:
- `id`: 代币 tick ID
- `chain`: 目标链名称（arrival PIN 所在的链）
- `amount`: 要接收的数量
- `to`: 接收地址（目标链地址）
- `assetOutpoint`: 源链的 UTXO (格式: `txid:vout`)

**⚠️ 重要**:
- Arrival PIN 创建在**目标链**（比如 DOGE）
- `assetOutpoint` 是**源链**（比如 BTC）上的 UTXO
- 必须先创建 Arrival，再创建 Teleport

---

## 5. Teleport (跨链转移)

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

**字段说明**:
- `type`: "teleport"（表示跨链）
- `chain`: 目标链名称
- `amount`: 转移数量（必须与 arrival 一致）
- `coord`: arrival PIN 的 ID

**⚠️ 重要**:
- Teleport PIN 创建在**源链**（比如 BTC）
- 这笔交易必须花费 `assetOutpoint` 对应的 UTXO
- `amount` 必须与 Arrival 的 `amount` 完全一致
- `coord` 是 Arrival PIN 的 ID（不是 TX ID）

---

## 📊 操作对比表

| 操作 | 路径 | op | type | 同链 | 跨链 | 创建链 |
|------|------|-----|------|------|------|--------|
| Deploy | `/ft/mrc20/deploy` | deploy | - | ✅ | ❌ | 初始链 |
| Mint | `/ft/mrc20/mint` | mint | - | ✅ | ❌ | 同链 |
| Transfer | `/ft/mrc20/transfer` | transfer | data | ✅ | ❌ | 同链 |
| Arrival | `/ft/mrc20/arrival` | arrival | - | ❌ | ✅ | **目标链** |
| Teleport | `/ft/mrc20/transfer` | transfer | teleport | ❌ | ✅ | **源链** |

---

## 🔄 完整流程示例

### 场景：BTC → DOGE 跨链转移 500 个代币

#### 步骤 1: 在 DOGE 链创建 Arrival

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

**在 DOGE 链创建这个 PIN**，获得 `ARRIVAL_PIN_ID`

#### 步骤 2: 在 BTC 链创建 Teleport

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
      "coord": "ARRIVAL_PIN_ID"
    }
  ]
}
```

**在 BTC 链创建这个 PIN**，交易必须花费 `def456...:0` 这个 UTXO

---

## 🎯 常见组合场景

### 场景 1: 拆分后 Teleport

```bash
# 1. 拆分 1000 → 600 + 400
{
  "op": "transfer",
  "data": [
    {"type": "data", "amount": "600", "vout": 1},
    {"type": "data", "amount": "400", "vout": 2}
  ]
}

# 2. Teleport 400 到 DOGE
# 先在 DOGE 创建 arrival
{
  "op": "arrival",
  "chain": "doge",
  "amount": "400",
  "assetOutpoint": "split_tx:2"
}

# 再在 BTC 创建 teleport
{
  "op": "transfer",
  "data": [
    {"type": "teleport", "chain": "doge", "amount": "400", "coord": "arrival_pin_id"}
  ]
}
```

### 场景 2: 给多人转账

```bash
# 1000 代币拆分给 3 个人
{
  "op": "transfer",
  "data": [
    {"type": "data", "amount": "300", "vout": 1},  # 接收者A
    {"type": "data", "amount": "200", "vout": 2},  # 接收者B
    {"type": "data", "amount": "100", "vout": 3},  # 接收者C
    {"type": "data", "amount": "400", "vout": 4}   # 自己（找零）
  ]
}
```

### 场景 3: 部分 Teleport + 找零

```bash
# 1000 代币: 400 teleport, 600 找零
{
  "op": "transfer",
  "data": [
    {
      "type": "teleport",
      "chain": "doge",
      "amount": "400",
      "coord": "arrival_pin_id"
    },
    {
      "type": "data",
      "amount": "600",
      "vout": 2  # 注意：teleport 没有 vout，所以这里是 vout=2
    }
  ]
}
```

**⚠️ 注意**:
- Teleport 项没有 `vout` 字段
- 找零项使用 `vout=2`（因为 vout=1 被 teleport 的 UTXO 销毁逻辑占用）

---

## 💡 关键规则总结

### Transfer (type=data)
1. ✅ **必须有 vout**: 对应 BTC 交易输出索引
2. ✅ **vout 连续**: 从 1 开始递增
3. ✅ **同链操作**: 在同一条链上
4. ✅ **UTXO 状态**: TransferPending → Spent

### Teleport (type=teleport)
1. ✅ **不需要 vout**: teleport 会销毁源 UTXO
2. ✅ **需要 coord**: 指向 arrival PIN
3. ✅ **跨链操作**: 从源链到目标链
4. ✅ **UTXO 状态**: TeleportPending → Spent
5. ✅ **需要 Arrival**: 必须先创建 arrival

### Arrival
1. ✅ **在目标链创建**: 声明要接收的资产
2. ✅ **必须先于 Teleport**: Teleport 会验证 arrival 存在
3. ✅ **assetOutpoint**: 指向源链的 UTXO
4. ✅ **amount 必须匹配**: 与 teleport 的 amount 一致

---

## 📝 模板

### 创建 transfer.json

```bash
cat > transfer.json <<EOF
{
  "p": "mrc-20",
  "op": "transfer",
  "id": "TICK_ID",
  "data": [
    {
      "type": "data",
      "amount": "AMOUNT",
      "vout": VOUT
    }
  ]
}
EOF
```

### 创建 arrival.json

```bash
cat > arrival.json <<EOF
{
  "p": "mrc-20",
  "op": "arrival",
  "id": "TICK_ID",
  "chain": "TARGET_CHAIN",
  "amount": "AMOUNT",
  "to": "TARGET_ADDRESS",
  "assetOutpoint": "SOURCE_UTXO"
}
EOF
```

### 创建 teleport.json

```bash
cat > teleport.json <<EOF
{
  "p": "mrc-20",
  "op": "transfer",
  "id": "TICK_ID",
  "data": [
    {
      "type": "teleport",
      "chain": "TARGET_CHAIN",
      "amount": "AMOUNT",
      "coord": "ARRIVAL_PIN_ID"
    }
  ]
}
EOF
```

---

**准备好了吗？** 🎯
