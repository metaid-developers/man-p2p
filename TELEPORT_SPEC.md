# MRC20 Teleport 跨链跃迁规范

## 1. 概述

Teleport 是 MRC20 协议的跨链资产转移功能，允许用户将 MRC20 资产从一条链（源链）跃迁到另一条链（目标链）。

### 1.1 核心概念

| 概念 | 说明 |
|------|------|
| **源链 (Source Chain)** | MRC20 资产当前所在的链 |
| **目标链 (Target Chain)** | MRC20 资产要跃迁到的链 |
| **Arrival** | 在目标链上声明的跨链接收请求 |
| **Teleport Transfer** | 在源链上执行的跨链转账操作 |
| **Coord** | Arrival PIN 的 ID，用于关联 teleport 和 arrival |

### 1.2 跨链流程

```
目标链                                    源链
  │                                        │
  ├─ 1. 用户发起 arrival                    │
  │    (声明 assetOutpoint)                │
  │                                        │
  │                                        ├─ 2. 用户发起 teleport transfer
  │                                        │    (包含 coord 指向 arrival)
  │                                        │    (交易输入必须包含 assetOutpoint)
  │                                        │
  │                                        ├─ 3. 源链 UTXO 状态: -1 (已消耗)
  │                                        │
  ├─ 4. 目标链创建新 UTXO ←─────────────────┤
  │    (状态: 0, 可用)                      │
  │                                        │
  └─ 5. Arrival 状态: completed            │
```

---

## 2. 数据结构

### 2.1 UTXO 状态定义

```go
const (
    UtxoStatusAvailable       = 0  // 可用
    UtxoStatusTeleportPending = 1  // 等待跃迁中
    UtxoStatusSpent           = -1 // 已消耗
)
```

| 状态 | 值 | 说明 |
|------|---|------|
| Available | 0 | UTXO 可用，可参与任何操作 |
| TeleportPending | 1 | UTXO 处于等待跃迁状态（teleport transfer 已出块，等待 arrival） |
| Spent | -1 | UTXO 已消耗（普通转账/原生转账/跃迁完成） |

### 2.2 Arrival 状态定义

```go
const (
    ArrivalStatusPending   = 0 // 等待中 - 等待源链的 teleport
    ArrivalStatusCompleted = 1 // 已完成 - 跃迁成功
    ArrivalStatusInvalid   = 2 // 无效 - 验证失败
)
```

### 2.3 Arrival 数据格式

```json
{
  "assetOutpoint": "txid:vout",    // 源链 MRC20 UTXO 的 outpoint
  "amount": "100",                  // 跃迁金额（必须等于 UTXO 全额）
  "tickId": "deploy_pin_id",        // MRC20 ID
  "locationIndex": 0,               // 目标链接收资产的 output 索引
  "metadata": ""                    // 可选的元数据
}
```

### 2.4 Teleport Transfer 数据格式

```json
{
  "type": "teleport",
  "id": "deploy_pin_id",            // MRC20 ID
  "amount": "100",                  // 跃迁金额
  "coord": "arrival_pin_id",        // 目标链 arrival 的 PIN ID
  "chain": "mvc"                    // 目标链名称
}
```

---

## 3. 处理逻辑

### 3.1 Teleport Transfer 处理流程

```
                    teleport transfer 出块
                            │
                            ▼
            ┌───────────────────────────────┐
            │  1. 验证必填字段              │
            │     - coord (arrival PIN ID)  │
            │     - id (tickId)             │
            │     - amount                  │
            │     - chain (目标链)          │
            └───────────────┬───────────────┘
                            │
                            ▼
            ┌───────────────────────────────┐
            │  2. 检查 teleport 是否重复    │
            │     (同一个 coord 只能有一个) │
            └───────────────┬───────────────┘
                            │
                            ▼
            ┌───────────────────────────────┐
            │  3. 从交易输入获取 MRC20 UTXO │
            │     - 匹配 tickId             │
            │     - 匹配 amount             │
            │     - 验证所有者              │
            └───────────────┬───────────────┘
                            │
                            ▼
            ┌───────────────────────────────┐
            │  4. 检查 UTXO 状态            │
            └───────────────┬───────────────┘
                            │
           ┌────────────────┼────────────────┐
           │                │                │
           ▼                ▼                ▼
     Status = 0       Status = 1       Status = -1
      (可用)       (已在等待跃迁)       (已消耗)
           │                │                │
           │                │                │
           ▼                ▼                ▼
      继续处理          ❌ 失败          ❌ 失败
           │         (不允许重复)      (已被花费)
           │
           ▼
    ┌──────────────────────────────────┐
    │  5. 检查 arrival 是否存在        │
    └──────────────┬───────────────────┘
                   │
      ┌────────────┴────────────┐
      │                         │
      ▼                         ▼
 arrival 存在              arrival 不存在
      │                         │
      ▼                         ▼
 验证数据匹配              UTXO.Status = 1
 执行跃迁                  保存 PendingTeleport
 UTXO.Status = -1          等待 arrival
 创建目标链 UTXO
```

### 3.2 Arrival 处理流程

```
                      arrival 出块
                           │
                           ▼
           ┌───────────────────────────────┐
           │  1. 验证必填字段              │
           │     - assetOutpoint           │
           │     - amount                  │
           │     - tickId                  │
           └───────────────┬───────────────┘
                           │
                           ▼
           ┌───────────────────────────────┐
           │  2. 检查重复的 arrival        │
           │     (同一 assetOutpoint)      │
           └───────────────┬───────────────┘
                           │
                           ▼
           ┌───────────────────────────────┐
           │  3. 获取源链 UTXO 并检查状态  │
           └───────────────┬───────────────┘
                           │
          ┌────────────────┼────────────────┐
          │                │                │
          ▼                ▼                ▼
    Status = 0       Status = 1       Status = -1
     (可用)         (等待跃迁)         (已消耗)
          │                │                │
          ▼                ▼                ▼
    保存 arrival     保存 arrival     检查 teleport 记录
   (status=pending)  (status=pending)        │
          │                │         ┌──────┴──────┐
          │                │         │             │
          │                ▼         ▼             ▼
          │         处理 pending   有记录        无记录
          │          teleport        │             │
          │                │         ▼             ▼
          │                │     arrival       arrival
          │                │    completed      invalid
          │                │                 (跃迁失败)
          └────────────────┴──────────────────────┘
```

### 3.3 Native Transfer / 普通 Transfer 对 Pending UTXO 的处理

**关键设计**：Pending 状态 (Status=1) 的 UTXO 可以被 native transfer 或普通 MRC20 transfer 花费。

```
用户发起 native transfer / 普通 transfer
                    │
                    ▼
        ┌──────────────────────────────┐
        │  GetMrc20UtxoByOutPutList    │
        │  返回 Status=0 和 Status=1   │
        │  的 UTXO                     │
        └──────────────┬───────────────┘
                       │
                       ▼
        ┌──────────────────────────────┐
        │  正常执行转账                │
        │  UTXO.Status = -1            │
        └──────────────┬───────────────┘
                       │
                       ▼
        ┌──────────────────────────────┐
        │  如果 UTXO 原来是 pending    │
        │  后续 arrival 检查时会发现   │
        │  UTXO 已被消耗，跃迁失败     │
        └──────────────────────────────┘
```

**设计理由**：
- 用户有权自主决定是否继续跃迁
- 如果用户在 teleport 后反悔，可以通过花费 pending UTXO 取消跃迁
- arrival 端会检测到失败并标记为 invalid

---

## 4. 场景分析

### 4.1 正常跃迁流程（arrival 先出块）

```
时间线：
T1: 目标链 - arrival 出块 → arrival.status = pending
T2: 源链   - teleport transfer 出块
            → 验证 arrival 存在且匹配
            → UTXO.status = -1
            → 创建目标链新 UTXO
            → arrival.status = completed
```

### 4.2 正常跃迁流程（teleport 先出块）

```
时间线：
T1: 源链   - teleport transfer 出块
            → arrival 不存在
            → UTXO.status = 1 (pending)
            → 保存 PendingTeleport

T2: 目标链 - arrival 出块
            → 发现 UTXO.status = 1
            → 查找 PendingTeleport
            → 执行跃迁
            → UTXO.status = -1
            → 创建目标链新 UTXO
            → arrival.status = completed
```

### 4.3 跃迁失败（用户中途花费 UTXO）

```
时间线：
T1: 源链   - teleport transfer 出块
            → UTXO.status = 1 (pending)

T2: 源链   - 用户发起 native transfer
            → pending UTXO 被花费
            → UTXO.status = -1
            → 新 UTXO 给其他地址

T3: 目标链 - arrival 出块
            → 发现 UTXO.status = -1
            → 检查无 teleport 记录
            → arrival.status = invalid (跃迁失败)
```

### 4.4 重复 teleport（被拒绝）

```
时间线：
T1: 源链   - teleport transfer A 出块
            → UTXO.status = 1 (pending)

T2: 源链   - teleport transfer B（同一 UTXO）出块
            → 检查 UTXO.status = 1
            → ❌ 失败：不允许同一 UTXO 有多个 teleport
```

### 4.5 Teleport 验证失败（回退处理）

```
场景：teleport transfer 出块，但验证失败（非 arrival 未出块）

失败原因示例：
- coord 字段为空
- 交易输入中没有匹配的 MRC20 UTXO
- UTXO 已经在 pending 状态（有另一个跃迁在等待）
- arrival 已存在但数据不匹配（tickId/金额/目标链/assetOutpoint）
- arrival 已存在但状态不是 pending

处理流程：
T1: 源链   - teleport transfer 出块
            → 交易输入包含 MRC20 UTXO
            → 验证失败（如上述原因）
            → 调用 handleFailedTeleportInputs
            → 输入 UTXO.status = -1 (已消耗)
            → 创建新 UTXO (MrcOption="teleport-failed")
            → 新 UTXO 发送到第一个有效输出地址

结果：
- 原 UTXO 被标记为已消耗（与链上一致）
- 新 UTXO 返回给用户（到第一个输出地址）
- 资产不丢失，可追溯失败原因
```

**注意**：arrival 未出块 **不是** 失败情况！此时 UTXO 会变为 `pending(1)` 状态，等待 arrival 出块后自动配对完成跃迁。

---

## 5. 数据存储

### 5.1 UTXO 索引

| Key 格式 | Value | 说明 |
|----------|-------|------|
| `mrc20_utxo_{txpoint}` | Mrc20Utxo JSON | UTXO 主记录 |
| `mrc20_addr_{addr}_{mrc20id}_{txpoint}` | Mrc20Utxo JSON | 地址索引 |

### 5.2 Arrival 索引

| Key 格式 | Value | 说明 |
|----------|-------|------|
| `arrival_{pinId}` | Mrc20Arrival JSON | Arrival 主记录 |
| `arrival_asset_{assetOutpoint}` | pinId | 通过 assetOutpoint 查找 |
| `arrival_pending_{chain}_{tickId}_{pinId}` | pinId | 待处理列表 |

### 5.3 Teleport 索引

| Key 格式 | Value | 说明 |
|----------|-------|------|
| `teleport_{pinId}` | Mrc20Teleport JSON | Teleport 主记录 |
| `teleport_coord_{coord}` | pinId | 通过 coord 查找 |
| `teleport_asset_{assetOutpoint}` | pinId | 通过 assetOutpoint 查找 |

### 5.4 PendingTeleport 索引

| Key 格式 | Value | 说明 |
|----------|-------|------|
| `pending_teleport_{pinId}` | PendingTeleport JSON | Pending 主记录 |
| `pending_teleport_coord_{coord}` | pinId | 通过 coord 查找 |

---

## 6. 验证规则

### 6.1 Teleport Transfer 验证

| 验证项 | 规则 |
|--------|------|
| 必填字段 | coord, id, amount, chain 必须存在 |
| 重复检查 | 同一 coord 只能有一个 teleport |
| 输入验证 | 交易输入必须包含匹配的 MRC20 UTXO |
| UTXO 状态 | Status 必须为 0 (Available) |
| 所有者验证 | 发送者必须是 UTXO 的所有者 |
| 金额匹配 | teleport 金额必须等于 UTXO 全额 |

### 6.2 Arrival 验证

| 验证项 | 规则 |
|--------|------|
| 必填字段 | assetOutpoint, amount, tickId 必须存在 |
| 重复检查 | 同一 assetOutpoint 只能有一个 arrival |
| tickId 验证 | 必须与源 UTXO 的 tickId 匹配 |
| 金额验证 | 必须等于源 UTXO 的全部金额 |

---

## 7. 错误处理

### 7.1 Teleport Transfer 错误

| 错误 | 原因 | 处理 |
|------|------|------|
| `coord is required` | 缺少 coord 字段 | 拒绝，回退处理 |
| `teleport already exists` | 重复的 teleport | 拒绝，回退处理 |
| `UTXO is already in teleport pending state` | UTXO 已有 pending teleport | 拒绝，回退处理 |
| `UTXO is not available` | UTXO 已被消耗 | 拒绝，回退处理 |
| `not authorized to spend UTXO` | 非 UTXO 所有者 | 拒绝，回退处理 |
| `no matching MRC20 UTXO found` | 输入中无匹配 UTXO | 拒绝，回退处理 |

### 7.2 Teleport 失败的回退处理

**重要**：当 teleport transfer 验证失败时，交易在链上已经花费了输入中的 UTXO。为保持索引与链上状态一致，需要进行回退处理：

```
Teleport 验证失败
       │
       ▼
┌──────────────────────────────────────┐
│  handleFailedTeleportInputs          │
│                                      │
│  1. 获取交易的第一个有效输出地址     │
│  2. 获取交易输入中的所有 MRC20 UTXO │
│  3. 将这些 UTXO 标记为已消耗(-1)    │
│  4. 创建新 UTXO 给第一个输出地址    │
│     (MrcOption = "teleport-failed") │
└──────────────────────────────────────┘
```

**处理逻辑**：
- 输入的 MRC20 UTXO → `Status = -1`，`Msg = "teleport failed: {原因}"`
- 新 UTXO → `Status = 0`，`MrcOption = "teleport-failed"`，发送到第一个有效输出地址

**这确保了**：
1. UTXO 状态与链上一致（输入已花费）
2. 资产不会丢失（转到用户控制的地址）
3. 可追溯失败原因（Msg 中记录）

### 7.2 Arrival 错误

| 错误 | 原因 | 处理 |
|------|------|------|
| `assetOutpoint is required` | 缺少必填字段 | 标记 invalid |
| `arrival already exists` | 重复的 arrival | 标记 invalid |
| `tickId mismatch` | tickId 不匹配 | 标记 invalid |
| `amount must be the full UTXO amount` | 金额不等于全额 | 标记 invalid |
| `UTXO already spent by other operation` | UTXO 被其他操作花费 | 标记 invalid |

---

## 8. API 接口

### 8.1 查询 Arrival

```
GET /mrc20/arrival/{pinId}
```

### 8.2 查询 Teleport

```
GET /mrc20/teleport/{pinId}
```

### 8.3 查询 Pending Teleport

```
GET /mrc20/pending-teleport/{coord}
```

---

## 9. 注意事项

1. **全额跃迁**：跃迁必须转移 UTXO 的全部金额，不支持部分跃迁
2. **单次跃迁**：同一 UTXO 只能有一个 teleport transfer
3. **Pending 可花费**：Pending 状态的 UTXO 可以被其他操作花费，用户自行承担跃迁失败后果
4. **不清理记录**：即使跃迁失败，PendingTeleport 记录也不会被删除（状态标记为 invalid）
5. **双向等待**：arrival 和 teleport 可以以任意顺序出块

---

## 10. 版本历史

| 版本 | 日期 | 说明 |
|------|------|------|
| 1.0 | 2026-01-19 | 初始版本，实现完整的 teleport 跃迁功能 |
