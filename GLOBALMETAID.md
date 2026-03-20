# GlobalMetaId 功能说明

## 概述

在 PIN 索引结果中新增了 `GlobalMetaId` 字段，用于存储创建者地址的统一跨链表示。

## 实现细节

### 1. 数据结构变更

在 `pin.PinInscription` 结构体中添加了新字段：

```go
type PinInscription struct {
    // ... 现有字段 ...
    CreateAddress      string          `json:"creator"`       // 原始链地址
    CreateMetaId       string          `json:"createMetaId"`  // SHA256(CreateAddress)
    GlobalMetaId       string          `json:"globalMetaId"`  // IDAddress 格式 🆕
    // ... 其他字段 ...
}
```

### 2. GlobalMetaId 字段说明

- **格式**: IDAddress（统一的跨链地址格式）
- **来源**: 从 `CreateAddress`（创建者的原始链地址）转换而来
- **用途**: 提供跨链统一的创建者标识
- **优势**:
  - 跨链可比较：不同链上的相同公钥会生成相同的 GlobalMetaId
  - 标准化：使用 Bech32 风格的编码，带校验和
  - 可读性：以 `id` 为前缀，后跟版本标识

### 3. IDAddress 格式

GlobalMetaId 使用 IDAddress 格式，例如：

```
idq1sv98gg8x84mzgnlhe0guyj8ffs2yvvjenmhn0j  // P2PKH 地址
idp1qyq8s4zcvwyx9qvuqw8zrx3qcxqwqyujnkdxjm4tc  // P2SH 地址
```

**格式说明**:
- `id`: 固定前缀（HRP）
- `q/p/z/r/y/t`: 版本字符
  - `q` = P2PKH (Pay-to-PubKey-Hash)
  - `p` = P2SH (Pay-to-Script-Hash)  
  - `z` = P2WPKH (Pay-to-Witness-PubKey-Hash)
  - `r` = P2WSH (Pay-to-Witness-Script-Hash)
  - `y` = P2MS (Pay-to-Multisig)
  - `t` = P2TR (Pay-to-Taproot)
- `1`: 分隔符
- 后续部分: Base32 编码的数据 + 6字符校验和

### 4. 转换逻辑

```go
// common/idaddress_util.go
func ConvertToGlobalMetaId(address string) string {
    // 1. 验证地址不为空且不是错误地址
    if address == "" || address == "errorAddr" {
        return ""
    }
    
    // 2. 使用 idaddress 包转换
    //    - 解码原始地址（Base58Check）
    //    - 提取版本和 payload
    //    - 编码为 IDAddress 格式
    idAddr, err := idaddress.ConvertFromBitcoin(address)
    if err != nil {
        log.Printf("[WARN] Failed to convert %s: %v", address, err)
        return ""
    }
    
    return idAddr
}
```

### 5. 支持的地址类型

| 区块链 | 地址类型 | 版本字节 | IDAddress 前缀 |
|--------|----------|----------|----------------|
| Bitcoin 主网 | P2PKH | 0x00 | idq1... |
| Bitcoin 主网 | P2SH | 0x05 | idp1... |
| Bitcoin 测试网 | P2PKH | 0x6F | idq1... |
| Bitcoin 测试网 | P2SH | 0xC4 | idp1... |
| Dogecoin | P2PKH | 0x1E | idq1... |
| Dogecoin | P2SH | 0x16 | idp1... |

### 6. 在索引器中的应用

在三个链的索引器中都已添加 GlobalMetaId 的生成：

**Dogecoin** (`adapter/dogecoin/indexer.go`):
```go
pinInscriptions = append(pinInscriptions, &pin.PinInscription{
    // ... 其他字段 ...
    CreateAddress:      creator,
    CreateMetaId:       common.GetMetaIdByAddress(creator),
    GlobalMetaId:       common.ConvertToGlobalMetaId(creator),  // 🆕
    // ... 其他字段 ...
})
```

**Bitcoin** (`adapter/bitcoin/indexer.go`): 同上

**MVC** (`adapter/microvisionchain/indexer.go`): 同上

### 7. 错误处理

- 如果地址格式不支持（如 Bech32）或转换失败，`GlobalMetaId` 将为空字符串
- 转换失败会记录警告日志，但不会中断索引过程
- 空的 `GlobalMetaId` 不影响其他字段的正常索引

### 8. 使用示例

**查询 PIN 时**:
```json
{
  "id": "3a99a8d2a7802dc7...i0",
  "creator": "DH5yaieqoZN36fDVciNyRueRGvGLR3mr7L",
  "createMetaId": "830a7420e63d76244ff7cbd1c248e94c14463259",
  "globalMetaId": "idq1sv98gg8x84mzgnlhe0guyj8ffs2yvvjenmhn0j",
  "chainName": "doge"
}
```

**跨链查询同一创作者**:
```javascript
// 可以使用 globalMetaId 跨链查询同一创作者的所有内容
db.pins.find({ "globalMetaId": "idq1sv98gg8x84mzgnlhe0guyj8ffs2yvvjenmhn0j" })
// 会返回该创作者在所有链上的 PIN
```

### 9. 验证工具

```go
// 验证 GlobalMetaId 是否有效
func ValidateGlobalMetaId(globalMetaId string) bool {
    return idaddress.ValidateIDAddress(globalMetaId)
}
```

## 优势

1. **跨链统一**: 同一个公钥在不同链上生成相同的 GlobalMetaId
2. **数据分析**: 可以追踪创作者在多个链上的活动
3. **用户体验**: 统一的地址格式，便于识别和比较
4. **向前兼容**: 不影响现有的 `CreateAddress` 和 `CreateMetaId` 字段
5. **错误检测**: IDAddress 带有校验和，可以检测输入错误

## 注意事项

- GlobalMetaId 为可选字段，转换失败不影响索引
- 仅支持 Legacy 地址（Base58Check），暂不支持 SegWit 原生地址（Bech32）
- 跨链对应关系基于相同的公钥哈希（payload）
