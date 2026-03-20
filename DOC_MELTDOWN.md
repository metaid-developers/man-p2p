# 熔化（Meltdown）功能说明

## 概述
“熔化（meltdown）”是 PIN-UTXO 管理中的一种特殊操作，原名“溶解（dissolve）”，现已全局替换为“熔化（meltdown）”。该操作用于将多个 PIN-UTXO 合并销毁，释放 UTXO。

## 熔化判定条件
一笔交易被判定为“熔化交易（meltdown transaction）”需同时满足以下条件：
1. **输入中有 ≥3 个 PIN-UTXO**，且每个金额为 546 聪（StandardPinUtxoValue）。
2. **输出只有 1 个**。
3. **所有 PIN-UTXO 的输入地址与输出地址相同**。

## 相关常量与字段
- `MeltdownMinPinCount`：熔化所需最小 PIN-UTXO 数量（默认为 3）。
- `PinStatusMeltdown`：熔化状态常量。
- `IsMeltdown`：结构体字段，标记是否为熔化交易。

## 主要代码位置
- 判定逻辑：
  - `IsMeltdownTransaction` 方法（bitcoin、dogecoin、microvisionchain 各自的 indexer.go）
- 结构体字段：
  - `PinTransferInfo.IsMeltdown`（pin.go）
- 状态常量：
  - `PinStatusMeltdown`（pin.go）
- 相关注释、文档、变量、方法名等均已统一为“熔化/meltdown”

## 处理流程简述
1. 区块或内存池交易处理时，调用 `IsMeltdownTransaction` 判定是否为熔化。
2. 若为熔化交易，仅更新 PIN 状态为 `PinStatusMeltdown`，不做普通转移处理。
3. 相关数据结构和 API 返回均以 `meltdown` 字段标识。

## 适用链
- Bitcoin
- Dogecoin
- MicroVisionChain

## 变更说明
- 原“溶解（dissolve）”相关内容全部替换为“熔化（meltdown）”。
- 代码、注释、文档、接口字段等均已同步更新。

---

# 熔化（Meltdown）功能详细代码说明

## 1. 判定是否为熔化交易

### 判定函数（以 Go 语言为例）
```go
// IsMeltdownTransaction 检测是否为熔化交易
// 熔化条件：
// 1. 输入有 ≥3 个 PIN-UTXO
// 2. 输出只有 1 个
// 3. 输入和输出地址相同
func (indexer *Indexer) IsMeltdownTransaction(tx *wire.MsgTx, idMap map[string]string) bool {
    // 条件2: 输出只有1个
    if len(tx.TxOut) != 1 {
        return false
    }
    // 获取输出地址
    outAddress := ""
    _, addresses, _, _ := txscript.ExtractPkScriptAddrs(tx.TxOut[0].PkScript, netParams)
    if len(addresses) > 0 {
        outAddress = addresses[0].String()
    }
    if outAddress == "" {
        return false
    }
    // 统计输入中符合条件的 PIN-UTXO 数量
    pinUtxoCount := 0
    allSameAddress := true
    for _, in := range tx.TxIn {
        id := fmt.Sprintf("%s:%d", in.PreviousOutPoint.Hash.String(), in.PreviousOutPoint.Index)
        if fromAddress, ok := idMap[id]; ok {
            // 检查金额是否为 546
            value, err := GetValueByTx(in.PreviousOutPoint.Hash.String(), int(in.PreviousOutPoint.Index))
            if err == nil && value == pin.StandardPinUtxoValue {
                pinUtxoCount++
                // 检查输入地址是否与输出地址相同
                if fromAddress != outAddress {
                    allSameAddress = false
                }
            }
        }
    }
    // 条件1和3
    return pinUtxoCount >= pin.MeltdownMinPinCount && allSameAddress
}
```

- `idMap`：输入UTXO的id到地址的映射。
- `GetValueByTx`：根据UTXO哈希和索引获取金额。
- `pin.StandardPinUtxoValue`：标准PIN金额（546聪）。
- `pin.MeltdownMinPinCount`：最小PIN数量（3）。

## 2. 结构体字段

```go
type PinTransferInfo struct {
    Address     string `json:"address"`
    Output      string `json:"output"`
    OutputValue int64  `json:"outputValue"`
    Offset      uint64 `json:"offset"`
    Location    string `json:"location"`
    FromAddress string `json:"fromAddress"`
    IsMeltdown  bool   `json:"isMeltdown"` // 是否为熔化交易
}
```

## 3. 状态常量

```go
const (
    PinStatusNormal    = 0  // 正常状态
    PinStatusMeltdown  = -2 // 熔化状态 - PIN被合并销毁，UTXO释放
)

const MeltdownMinPinCount = 3 // 熔化最小PIN数量
const StandardPinUtxoValue = 546 // 标准PIN金额
```

## 4. 处理流程代码片段

在处理区块或mempool交易时：

```go
if info.IsMeltdown {
    // 熔化交易：只更新状态为熔化，不更新其他信息
    pinNode.Status = pin.PinStatusMeltdown
} else {
    // 普通转移：更新转移信息
    pinNode.IsTransfered = true
    pinNode.Address = info.Address
    // ... 其他转移逻辑 ...
}
```

## 5. 典型调用流程

1. 遍历区块/内存池所有交易，依次调用 `IsMeltdownTransaction` 判定。
2. 若为熔化交易，生成 `PinTransferInfo` 并设置 `IsMeltdown=true`。
3. 更新 PIN 状态为 `PinStatusMeltdown`，不做普通转移。
4. 相关API、数据结构、接口返回均以 `meltdown` 字段标识。

## 6. 依赖说明
- 需有 UTXO 金额查询函数（如 `GetValueByTx`）。
- 需有 PIN-UTXO 标准金额常量（546聪）。
- 需有 PIN 状态常量、最小PIN数量常量。
- 需有输入UTXO到地址的映射（`idMap`）。

---

如需移植到其他项目，建议先实现上述判定函数、结构体字段和常量，并在交易处理主流程中集成熔化判定与处理逻辑。