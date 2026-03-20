# MRC20 完整索引方案设计

## 一、设计目标

1. **准确性**：正确跟踪所有 MRC20 操作，包括 mint、transfer、teleport
2. **性能**：余额查询 O(1) 复杂度，历史查询支持分页
3. **一致性**：teleport 双向验证后才生效，保证源链和目标链状态一致
4. **可追溯**：完整的交易流水记录，支持余额变化追溯

## 二、核心数据表设计

### 2.1 UTXO 表 (现有，需调整)

**表名**: `Mrc20Utxo`

**用途**: 记录当前可用的 MRC20 UTXO，是状态查询的核心

**设计理念**: 
- UTXO 表 = 当前状态表，**只保留 status=0 (Available) 的 UTXO**
- Spent UTXO 从表中删除，历史由 Transaction 流水表记录
- Pending UTXO (teleport) 暂时保留，双向确认后删除

**字段**:
```go
type Mrc20Utxo struct {
    Tick        string          // tick 名称
    Mrc20Id     string          // tick ID (部署 PIN ID)
    TxPoint     string          // UTXO 标识 "txid:vout"
    PointValue  uint64          // UTXO 的 satoshi 值
    PinId       string          // 关联的 PIN ID
    PinContent  string          // PIN 内容
    Verify      bool            // 是否验证通过
    BlockHeight int64           // 区块高度
    MrcOption   string          // 操作类型: mint/transfer/teleport
    FromAddress string          // 发送方地址
    ToAddress   string          // 接收方地址
    Msg         string          // 备注信息
    AmtChange   decimal.Decimal // UTXO 金额
    Status      int             // UTXO 状态
    Chain       string          // 所属链
    Index       int             // 在交易中的索引
    Timestamp   int64           // 时间戳
    OperationTx string          // 产生/消耗此 UTXO 的交易
}
```

**状态定义**:
- `0`: Available - 可用，可被花费（UTXO 表中的记录都是此状态）
- `1`: TeleportPending - teleport 发起但未完成（源链 UTXO 临时状态）
- `2`: TransferPending - 普通 transfer 在 mempool 中（临时状态）
- `-1`: Spent - 已被花费（不再存储在 UTXO 表中，由 Transaction 表记录）

**索引键**:
- 主键: `utxo_{txPoint}` - 完整 UTXO 记录
- 地址索引: `addr_utxo_{address}_{tickId}_{txPoint}` - 按地址查询 UTXO

**生命周期**:
1. **创建**: Mint/Transfer 确认后，写入 UTXO 表 (status=0)
2. **Pending**: Mempool 阶段或 Teleport 等待时 (status=1/2)，仍保留在表中
3. **消耗**: Transfer/Teleport 确认后，**从 UTXO 表删除**，记录到 Transaction 流水表

**设计说明**:
- UTXO 表只保留当前可用和 pending 的记录，数据量可控
- 查询可用 UTXO 时，直接扫描 UTXO 表（过滤 status=0），无需额外索引
- Spent UTXO 的历史通过 Transaction 流水表的 SpentUtxos 字段追溯
- Teleport 特殊处理：源链 UTXO 先变为 pending(1)，双向确认后删除

---

### 2.2 账户余额表 (新增)

**表名**: `Mrc20AccountBalance`

**用途**: 快速查询地址的余额状态，O(1) 复杂度

**字段**:
```go
type Mrc20AccountBalance struct {
    Address          string          // 地址
    TickId           string          // tick ID
    Tick             string          // tick 名称
    Balance          decimal.Decimal // 已确认的可用余额 (status=0 的 UTXO 总和)
    PendingOut       decimal.Decimal // 待转出余额 (teleport pending 源链)
    PendingIn        decimal.Decimal // 待转入余额 (teleport pending 目标链)
    Chain            string          // 链名称
    LastUpdateTx     string          // 最后更新的交易 ID
    LastUpdateHeight int64           // 最后更新的区块高度
    LastUpdateTime   int64           // 最后更新时间戳
    UtxoCount        int             // UTXO 数量 (可用状态)
}
```

**索引键**:
- 主键: `balance_{chain}_{address}_{tickId}`
- 查询键: `balance_addr_{address}` (返回该地址所有 tick)

**更新时机**:
1. **Mint**: 接收方 Balance += amount, UtxoCount++
2. **Transfer (确认)**: 
   - 发送方: Balance -= amount, UtxoCount--
   - 接收方: Balance += amount, UtxoCount++
3. **Teleport Transfer (发起)**:
   - 源链发送方: Balance -= amount, PendingOut += amount, UtxoCount--
4. **Arrival (发起)**:
   - 目标链接收方: PendingIn += amount
5. **Teleport Complete (双向确认)**:
   - 源链发送方: PendingOut -= amount
   - 目标链接收方: PendingIn -= amount, Balance += amount, UtxoCount++

**设计优势**:
- 查询余额时无需扫描 UTXO 表
- 清晰区分 available、pendingOut、pendingIn
- 支持快速统计持有人数量

---

### 2.3 交易流水表 (新增)

**表名**: `Mrc20Transaction`

**用途**: 记录所有已确认的 MRC20 交易，支持历史查询和对账

**字段**:
```go
type Mrc20Transaction struct {
    TxId         string          // 交易 ID (主交易，对于 teleport 是 teleport tx)
    TxIndex      int64           // 交易序号 (全局递增，用于排序)
    PinId        string          // PIN ID
    TickId       string          // tick ID
    Tick         string          // tick 名称
    TxType       string          // 交易类型: mint/transfer/teleport_out/teleport_in
    FromAddress  string          // 发送方地址 (mint 时为空)
    ToAddress    string          // 接收方地址
    Amount       decimal.Decimal // 交易金额
    Chain        string          // 交易所在链
    BlockHeight  int64           // 区块高度
    Timestamp    int64           // 时间戳
    
    // Teleport 相关字段
    RelatedTxId   string         // 关联交易 ID (teleport 的 arrival tx)
    RelatedChain  string         // 关联链 (teleport 的目标/源链)
    RelatedPinId  string         // 关联 PIN ID (arrival/teleport PIN)
    TeleportCoord string         // teleport 协调 ID (arrival PIN ID)
    
    // UTXO 追溯
    SpentUtxos    []string       // 消耗的 UTXO (txPoint 数组，JSON 存储)
    CreatedUtxos  []string       // 创建的 UTXO (txPoint 数组，JSON 存储)
    
    // 其他
    Msg          string          // 备注信息
    Status       int             // 交易状态: 1=成功, -1=失败
}
```

**索引键**:
- 主键: `tx_{txId}_{chain}_{txIndex}`
- 地址发送索引: `tx_from_{fromAddress}_{tickId}_{txIndex}`
- 地址接收索引: `tx_to_{toAddress}_{tickId}_{txIndex}`
- 全局索引: `tx_all_{tickId}_{txIndex}` (用于 tick 的所有交易)
- Teleport 协调索引: `tx_coord_{teleportCoord}` (关联 teleport 和 arrival)

**TxType 定义**:
- `mint`: 铸造
- `transfer`: 普通转账
- `teleport_out`: 跨链转出 (源链记录)
- `teleport_in`: 跨链转入 (目标链记录)

**写入时机**:
- **Mint/Transfer**: 交易确认时立即写入
- **Teleport**: **仅在双向验证完成后**写入，同时写入源链 (teleport_out) 和目标链 (teleport_in) 两条记录

**设计说明**:
- TxIndex 全局递增，确保严格时序
- 双向 teleport 通过 TeleportCoord 关联
- SpentUtxos/CreatedUtxos 支持完整的 UTXO 追溯
- 只记录成功的交易，失败的操作不写入流水

---

### 2.4 Teleport 状态表 (现有，需调整)

**表名**: `Mrc20Teleport`, `Mrc20Arrival`, `PendingTeleport`

**用途**: 跟踪跨链跃迁的双向验证状态

#### Mrc20Teleport (已完成的 teleport)
```go
type Mrc20Teleport struct {
    PinId          string          // teleport transfer PIN ID
    TxId           string          // teleport 交易 ID
    TickId         string          // tick ID
    Tick           string          // tick 名称
    Amount         decimal.Decimal // 跃迁金额
    Coord          string          // arrival PIN ID
    FromAddress    string          // 发送方地址
    SourceChain    string          // 源链
    TargetChain    string          // 目标链
    SpentUtxoPoint string          // 消耗的源链 UTXO
    Status         int             // 0=pending, 1=completed, -1=failed
    BlockHeight    int64           // 区块高度
    Timestamp      int64           // 时间戳
    CompletedAt    int64           // 完成时间 (双向确认时间)
}
```

#### Mrc20Arrival (arrival 记录)
```go
type Mrc20Arrival struct {
    PinId         string          // arrival PIN ID (作为 coord)
    TxId          string          // arrival 交易 ID
    AssetOutpoint string          // 源链 UTXO 标识
    Amount        decimal.Decimal // 金额
    TickId        string          // tick ID
    Tick          string          // tick 名称
    LocationIndex int             // 接收 output 索引
    ToAddress     string          // 接收地址
    Chain         string          // 目标链 (arrival 所在链)
    SourceChain   string          // 源链
    Status        ArrivalStatus   // pending/completed/invalid
    BlockHeight   int64           // 区块高度
    Timestamp     int64           // 时间戳
    TeleportPinId string          // 关联的 teleport PIN ID
    TeleportTxId  string          // 关联的 teleport 交易 ID
    TeleportChain string          // teleport 所在链
    CompletedAt   int64           // 完成时间
}
```

#### PendingTeleport (等待 arrival 的 teleport)
```go
type PendingTeleport struct {
    PinId         string // transfer PIN ID
    TxId          string // transfer 交易 ID
    Coord         string // 期望的 arrival PIN ID
    TickId        string // tick ID
    Amount        string // 跃迁金额
    AssetOutpoint string // 源链 UTXO
    TargetChain   string // 目标链
    FromAddress   string // 发送方
    SourceChain   string // 源链
    BlockHeight   int64  // 区块高度
    Timestamp     int64  // 时间戳
    Status        int    // 0=pending, 1=completed, -1=invalid
    RawContent    []byte // 原始内容
}
```

**设计说明**:
- 三张表协同工作，追踪 teleport 的完整生命周期
- PendingTeleport 是临时表，完成后删除
- Teleport 和 Arrival 是永久记录，用于审计和查询

---

## 三、索引流程设计

### 3.1 Mint 流程

```
1. 验证 mint 参数 (tick, amount, pop 等)
2. 创建新 UTXO:
   - Status = 0 (Available)
   - MrcOption = "mint" / "pre-mint"
   - ToAddress = minter address
   - AmtChange = mint amount
   - 写入 UTXO 表: utxo_{txPoint}
3. 更新账户余额:
   - Balance += mint amount
   - UtxoCount++
4. 写入交易流水:
   - TxType = "mint"
   - ToAddress = minter
   - CreatedUtxos = [new utxo txpoint]
5. 更新 tick 统计:
   - TotalMinted++
   - Holders++ (如果是新持有者)
```

### 3.2 Transfer 流程

#### 3.2.1 普通 Transfer
```从 available_utxo 索引查找发送方的可用 UTXO (快速查询)
   - 验证余额充足
2. Mempool 阶段:
   - 源 UTXO: Status = 2 (TransferPending)
   - 从 available_utxo 索引删除
   - 创建新 UTXO: Status = 2 (TransferPending)
3. 确认阶段:
   - 源 UTXO: Status = -1 (Spent)
   - 新 UTXO: Status = 0 (Available)
   - 新 UTXO 写入 available_utxo 索引
   - 源 UTXO: Status = -1 (Spent)
   - 新 UTXO: Status = 0 (Available)
4. 更新账户余额:
   - From: Balance -= amount, UtxoCount--
   - To: Balance += amount, UtxoCount++
5. 写入交易流水:
   - TxType = "transfer"
   - FromAddress, ToAddress, Amount
   - SpentUtxos = [spent utxo]
   - CreatedUtxos = [new utxo]
```

#### 3.2.2 Native Transfer (无 PIN)
```
与普通 transfer 类似，但:
- MrcOption = "native-transfer"
- PinId = ""
- 直接通过交易输入花费 UTXO
```

### 3.3 Teleport 流程 (核心改进)

**关键原则**: **双向验证后才生效，才写流水和更新余额**

#### 3.3.1 Teleport Transfer (源链发起)

```
1. 验证输入:
   - 解析 teleport transfer 数据 (coord, chain, amount)
   - 查找源 UTXO (status=0)
   - 验证余额
2. 检查 arrival 是否已存在:
   
   Case A: Arrival 已存在 (arrival 先出块)
   ========================================
   a. 验证 arrival 参数匹配:
      - arrival.AssetOutpoint == source UTXO txpoint
      - arrival.Amount == teleport amount
      - arrival.TickId == teleport tickId
   
   b. 双向从 available_utxo 索引删除 (如果之前是 pending 则已删除)
      - 目标链创建新 UTXO: Status = 0 (Available)
      - 目标链 UTXO 写入 available_utxo 索引
   
   c. 更新账户余额:
      - 源链发送方: PendingOut -= amount (从 arrival 阶段的 pending 变为确认)
      - 目标链接收方: PendingIn -= amount, Balance += amount
   
   d. 写入交易流水 (双链):
      - 源链写入: TxType = "teleport_out"
      - 目标链写入: TxType = "teleport_in"
   
   e. 更新 arrival 状态: Status = Completed
   f. 保存 teleport 记录: Status = 1 (completed)
   g. 删除 TeleportPendingIn 记录
   
   Case B: Arrival 不存在 (teleport 先出块)
   ========================================
   a. 源链 UTXO: Status = 1 (TeleportPending)
      - 从 available_utxo 索引删除
   ========================================
   a. 源链 UTXO: Status = 1 (TeleportPending)
   
   b. 更新账户余额:
      - 源链发送方: Balance -= amount, PendingOut += amount
   
   c. 保存 PendingTeleport 记录:
      - 等待 arrival 出现
      - 记录 coord, assetOutpoint, amount 等
   
   d. **不写入流水** (等待双向确认)
```

#### 3.3.2 Arrival (目标链发起)

```
1. 验证输入:
   - 解析 arrival 数据 (assetOutpoint, amount, tickId)
   - 验证源链 UTXO 存在
2. 检查 teleport transfer 是否已存在:
   
   Case A: Teleport 已存在 (teleport 先出块，pending 状态)
   ========================================
   a. 从 PendingTeleport 获取 teleport 信息
   
   b. 验证参数匹配:
      - pending.AssetOutpoint == arrival.AssetOutpoint
      - pending.Amount == arrival.Amount
      - pending.TickId == arrival.TickId
   (源链 UTXO 在 pending 时已从 available_utxo 删除)
      - 目标链创建新 UTXO: Status = 0 (Available)
      - 目标链 UTXO 写入 available_utxo 索引
   c. 双向验证通过，执行跃迁:
      - 源链 UTXO: Status = 1 -> -1 (Spent)
      - 目标链创建新 UTXO: Status = 0 (Available)
   
   d. 更新账户余额:
      - 源链发送方: PendingOut -= amount
      - 目标链接收方: PendingIn -= amount, Balance += amount
   
   e. 写入交易流水 (双链):
      - 源链写入: TxType = "teleport_out"
      - 目标链写入: TxType = "teleport_in"
   
   f. 更新 arrival 状态: Status = Completed
   g. 更新 teleport 状态: Status = 1 (completed)
   h. 删除 PendingTeleport
   i. 删除 TeleportPendingIn
   
   Case B: Teleport 不存在 (arrival 先出块)
   ========================================
   a. 创建 arrival 记录: Status = Pending
   
   b. 更新账户余额:
      - 目标链接收方: PendingIn += amount
   
   c. 保存 TeleportPendingIn 记录:
      - 用于计算接收方的 PendingInBalance
   
   d. **不写入流水** (等待双向确认)
```

#### 3.3.3 关键改进点

1. **UTXO 状态严格控制**:
   - Teleport 发起: status = 1 (pending)
   - 双向确认后: status = -1 (spent)
   - 目标链创建: status = 0 (available)

2. **余额只在双向确认后更新**:
   - 发起阶段: 源链 Balance -> PendingOut, 目标链 PendingIn++
   - 确认阶段: 源链 PendingOut--, 目标链 PendingIn-- Balance++

3. **流水只在双向确认后写入**:
   - 避免写入未完成的跃迁记录
   - 保证流水表的准确性

4. **支持任意顺序**:
   - Teleport 先出块 -> 等待 Arrival
   - Arrival 先出块 -> 等待 Teleport
   - 双方出块顺序不影响最终结果

---

## 四、查询接口设计

### 4.1 余额查询

```go
// GetAccountBalance 查询地址余额
// 输入: address, tickId, chain
// 输出: Mrc20AccountBalance
// 复杂度: O(1)
func GetAccountBalance(address, tickId, chain string) (*Mrc20AccountBalance, error)

// GetAccountAllBalances 查询地址所有 tick 余额
// 输入: address, chain
// 输出: []Mrc20AccountBalance
// 复杂度: O(n) n=持有的 tick 数量
func GetAccountAllBalances(address, chain string) ([]*Mrc20AccountBalance, error)
```

### 4.2 交易历史查询

```go
// GetTransactionHistory 查询交易历史
// 输入: address, tickId, txType (可选), limit, offset
// 输出: []Mrc20Transaction
// 排序: 按 TxIndex 降序 (最新的在前)
func GetTransactionHistory(params HistoryQueryParams) ([]*Mrc20Transaction, int64, error)

type HistoryQueryParams struct {
    Address  string   // 地址 (必填)
    TickId   string   // tick ID (可选，空表示所有)
    TxTypes  []string // 交易类型过滤 (可选，空表示所有)
    Chain    string   // 链名称 (必填)
    Limit    int      // 分页大小
    Offset   int      // 分页偏移
}
```

**查询逻辑**:
1. 同时查询 `tx_from_{address}` 和 `tx_to_{address}` 索引
2. 合并结果，按 TxIndex 排序
3. 支持 txType 过滤 (mint/transfer/teleport_out/teleport_in)
4. 返回总数，支持分页

### 4.3 UTXO 查询

```go
// GetAvailableUtxos 查询可用 UTXO
// 性能: 直接从 available_utxo 索引读取，无需过滤
// 输入: address, tickId, chain
// 输出: []Mrc20Utxo (status=0)
func GetAvailableUtxos(address, tickId, chain string) ([]*Mrc20Utxo, error)

// GetUtxoByTxPoint 查询特定 UTXO
// 输入: txPoint, includeSpent
// 输出: Mrc20Utxo
func GetUtxoByTxPoint(txPoint string, includeSpent bool) (*Mrc20Utxo, error)
```

### 4.4 Teleport 查询

```go
// GetTeleportByCoord 查询 teleport 记录
// 输入: coord (arrival PIN ID)
// 输出: Mrc20Teleport, Mrc20Arrival
func GetTeleportByCoord(coord string) (*Mrc20Teleport, *Mrc20Arrival, error)

// GetPendingTeleports 查询待确认的 teleport
// 输入: address (可选), chain (可选)
// 输出: []PendingTeleport
func GetPendingTeleports(address, chain string) ([]*PendingTeleport, error)
```

---

## 五、数据一致性保证

### 5.1 原子性

**同一交易的所有数据更新必须原子执行**:
1. UTXO 状态更新
2. 账户余额更新
3. 交易流水写入
4. 索引更新

**实现方式**: Pebble batch 操作

### 5.2 双向确认

**Teleport 必须双向确认后才生效**:
- 在源链和目标链都出块前，状态为 pending
- 双向确认后，同时更新两条链的数据
- 保证源链 out 和目标链 in 的金额一致

### 5.3 余额一致性

**余额 = UTXO 聚合**:
```
Balance = Sum(UTXO.AmtChange WHERE status=0)
PendingOut = Sum(UTXO.AmtChange WHERE status=1)
PendingIn = Sum(TeleportPendingIn.Amount)

注：UTXO 表中只有 status=0 和临时的 status=1/2
```

**定期校验**:
- 后台任务定期重新计算余额
- 与 AccountBalance 表对比
- 发现不一致时告警和修复

### 5.4 重组处理

**区块重组时的回滚**:
1. 删除受影响区块的所有交易流水
2. 回滚 UTXO 状态
3. 重新计算账户余额
4. 重新索引正确的区块

---

## 六、性能优化

### 6.1 索引策略

**多维度索引**:
- 按地址索引: 快速查询某地址的余额和历史
- 按 tick 索引: 快速统计 t
- **可用 UTXO 专用索引**: 独立维护 status=0 的 UTXO 列表，避免查询时扫描所有历史 UTXO

**可用 UTXO 索引优化**:
- 索引键格式: `available_utxo_{chain}_{address}_{tickId}_{txPoint}`
- 只存储 available (status=0) 的 UTXO
- UTXO 状态变更时同步更新此索引:
  - 创建 available UTXO → 写入索引
  - UTXO 变为 pending/spent → 从索引删除
  - Pending UTXO 回到 available → 重新写入索引
- 查询性能: O(n) n=可用UTXO数量，通常远小于所有历史 UTXO 数量ick 的持有人和交易量
- 按区块高度索引: 支持区块重组回滚

### 6.2 缓存策略

**热点数据缓存**:
- 账户余额 (5 分钟过期)
- 最近交易 (1 分钟过期)
- Tick 统计数据 (10 分钟过期)

### 6.3 分页策略

**交易历史分页**:
- 基于 TxIndex 的 offset/limit
- 默认每页 50 条
- 最大每页 100 条

---

## 七、迁移方案

### 7.1 数据迁移
**迁移原则**:
- **不处理 Teleport 数据**: 迁移时只处理 mint 和 transfer 操作，teleport 相关的历史数据可以忽略或标记为遗留数据
- **增量迁移**: 新的 teleport 操作从上线后开始完整记录
- **部分历史**: 交易流水表只记录能从现有数据推断出的历史，无法准确还原的交易可以省略

1. **生成 AccountBalance 表**:
   ```
   遍历所有 UTXO:
     按 (address, tickId, chain) 分组
     累加 status=0 的 AmtChange -> Balance
     统计 UTXO 数量 (status=0)
     PendingOut 和 PendingIn 初始为 0 (旧数据不考虑)
   ```

2. **生成 Available UTXO 索引**:
   ```
   遍历所有 UTXO:
     如果 status == 0:
       写入 available_utxo_{chain}_{address}_{tickId}_{txPoint}
   ```

3. **生成 Transaction 流水表** (可选的部分历史):
   ```
   方案 A - 仅从 UTXO 推断:
     遍历所有 UTXO:
       根据 MrcOption 生成对应的交易记录
       - mint: 生成 mint 记录
       - transfer: 如果能找到对应的 spent UTXO，生成 transfer 记录
       - 无法准确还原的交易: 跳过
   
   方案 B - 从 PIN 重建:
     重新遍历所有区块的 MRC20 PIN:
       按时间顺序处理
       生成 TxIndex (全局递增)
       只处理 mint 和 transfer
       跳过无法验证的交易
   
   方案 C - 不迁移历史流水:
     Transaction 表从迁移时刻开始记录
     旧数据通过 UTXO 表查询
   ```

**推荐方案**: 方案 C 或方案 A
- 方案 C 最简单，避免复杂的历史数据重建
- 方案 A 可以保留部分历史，但可能不完整有已完成的 teleport:
     验证双向记录完整性
     补充缺失的流水记录
   ```

### 7.2 兼容性

**API 兼容**:
- 保留现有 API 接口
- 新增接口使用新的查询逻辑
- 逐步废弃旧接口

**数据兼容**:
- UTXO 表结构不变，新增字段向后兼容
- 新表与旧数据共存
- 提供对照工具验证数据一致性

---

## 八、测试验证

### 8.1 单元测试

- 每种操作的 UTXO 状态转换
- 余额计算的正确性
- Teleport 双向匹配逻辑

### 8.2 集成测试

- 完整的 mint -> transfer -> teleport 流程
- 不同顺序的 teleport/arrival 处理
- 区块重组的数据回滚

### 8.3 压力测试

- 大量 UTXO 的余额查询性能
- 高频交易的流水写入性能
- 并发 teleport 的正确性

---

## 九、总结

### 9.1 核心改进

1. **账户余额表**: O(1) 查询性能，清晰的 pending 状态
2. **交易流水表**: 完整的历史记录，支持追溯和对账
3. **Teleport 双向验证**: 只在双向确认后生效，保证一致性
4. **UTXO 状态机**: 严格的状态转换，防止重复花费

### 9.2 数据流

```
Mint:
  -> 创建 UTXO (status=0)
  -> 更新 AccountBalance (Balance++)
  -> 写入 Transaction (mint)

Transfer:
  -> 花费源 UTXO (从 UTXO 表删除)
  -> 创建新 UTXO (status=0)
  -> 更新 AccountBalance (from Balance--, to Balance++)
  -> 写入 Transaction (transfer, 记录 SpentUtxos/CreatedUtxos)

Teleport (完整流程):
  1. Teleport Transfer 发起:
     -> 源 UTXO (status=0 -> 1, 保留在表中)
     -> 更新 AccountBalance (Balance--, PendingOut++, UtxoCount--)
     -> 保存 PendingTeleport
  
  2. Arrival 发起:
     -> 创建 Arrival (status=pending)
     -> 更新 AccountBalance (目标链 PendingIn++)
     -> 保存 TeleportPendingIn
  
  3. 双向确认:
     -> 源 UTXO (从 UTXO 表删除)
     -> 目标 UTXO (status=0, 写入 UTXO 表)
     -> 更新 AccountBalance:
        源链: PendingOut--
        目标链: PendingIn--, Balance++, UtxoCount++
     -> 写入 Transaction (源链 teleport_out, 目标链 teleport_in)
     -> 更新 Teleport/Arrival 状态
     -> 删除 Pending 记录
```

### 9.3 方案优势

1. **数据准确**: 双向验证机制保证跨链一致性
2. **查询高效**: 余额 O(1)，历史分页查询
3. **易于维护**: 清晰的状态机，完整的流水记录
4. **可扩展**: 支持未来更多操作类型

---

## 十、实施计划

1. **阶段 1**: 设计评审 (本文档)
2. **阶段 2**: 实现新数据结构和索引逻辑
3. **阶段 3**: 编写迁移工具
4. **阶段 4**: 单元测试和集成测试
5. **阶段 5**: 小规模数据验证
6. **阶段 6**: 全量数据迁移
7. **阶段 7**: 上线监控

**预计时间**: 2-3 周

---

**文档版本**: v1.0  
**创建日期**: 2026-01-23  
**状态**: 待评审
