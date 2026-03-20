# ID地址系统 - 快速开始指南

## 安装

```bash
cd /srv/dev_project/metaid/man-indexer-v2/idaddress
go test    # 运行测试
go install ./cmd/idaddr  # 安装命令行工具
```

## 核心概念

ID地址使用"id"作为人类可读前缀，支持5种地址类型：

| 前缀 | 类型 | 数据长度 | 说明 |
|------|------|----------|------|
| idq1 | P2PKH | 20字节 | 支付到公钥哈希 |
| idp1 | P2SH | 20字节 | 支付到脚本哈希 |
| idz1 | P2WPKH | 20字节 | 见证公钥哈希 |
| idr1 | P2WSH | 32字节 | 见证脚本哈希 |
| idy1 | P2MS | 可变 | 多签地址 |

## Go API 使用

### 1. 基本编码/解码

```go
package main

import (
    "encoding/hex"
    "fmt"
    "manindexer/idaddress"
)

func main() {
    // 编码 P2PKH 地址
    data, _ := hex.DecodeString("751e76e8199196d454941c45d1b3a323f1433bd6")
    addr, _ := idaddress.EncodeIDAddress(idaddress.VersionP2PKH, data)
    fmt.Println("地址:", addr)
    // 输出: idq1w508d6qejxtdg4y5r3zarvary0c5xw7ky30xwh
    
    // 解码地址
    info, _ := idaddress.DecodeIDAddress(addr)
    fmt.Printf("类型: %s\n", idaddress.GetAddressType(info.Version))
    fmt.Printf("数据: %x\n", info.Data)
    
    // 验证地址
    valid := idaddress.ValidateIDAddress(addr)
    fmt.Println("有效:", valid)
}
```

### 2. 从公钥生成地址

```go
pubkey, _ := hex.DecodeString("0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798")

// P2PKH
p2pkhAddr, _ := idaddress.NewP2PKHAddress(pubkey)
fmt.Println("P2PKH:", p2pkhAddr)

// P2WPKH (SegWit)
p2wpkhAddr, _ := idaddress.NewP2WPKHAddress(pubkey)
fmt.Println("P2WPKH:", p2wpkhAddr)
```

### 3. 创建多签地址

```go
// 3个压缩公钥
pubkey1, _ := hex.DecodeString("0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798")
pubkey2, _ := hex.DecodeString("02c6047f9441ed7d6d3045406e95c07cd85c778e4b8cef3ca7abac09b95c709ee5")
pubkey3, _ := hex.DecodeString("03774ae7f858a9411e5ef4246b70c65aac5649980be5c17891bbec17895da008cb")

pubkeys := [][]byte{pubkey1, pubkey2, pubkey3}

// 创建 2-of-3 多签地址
multisigAddr, _ := idaddress.NewP2MSAddress(2, 3, pubkeys)
fmt.Println("多签地址:", multisigAddr)

// 提取多签信息
info, _ := idaddress.DecodeIDAddress(multisigAddr)
m, n, extractedKeys, _ := idaddress.ExtractMultisigInfo(info)
fmt.Printf("需要签名: %d-of-%d\n", m, n)
```

### 4. 跨链地址转换

```go
// Bitcoin -> ID
bitcoinAddr := "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa"
idAddr, _ := idaddress.ConvertFromBitcoin(bitcoinAddr)
fmt.Println("ID地址:", idAddr)

// ID -> Bitcoin
bitcoinAddr, _ = idaddress.ConvertToBitcoin(idAddr, "mainnet")
fmt.Println("Bitcoin地址:", bitcoinAddr)

// ID -> Dogecoin
dogeAddr, _ := idaddress.ConvertToDogecoin(idAddr)
fmt.Println("Dogecoin地址:", dogeAddr)
```

### 5. 使用地址转换器

```go
converter := idaddress.NewAddressConverter("mainnet")

// 批量转换
addresses := []string{
    "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa",
    "invalid_address",
}

results, errors := converter.Batch(addresses)
for i, result := range results {
    if errors[i] == nil {
        fmt.Printf("%s -> %s\n", addresses[i], result)
    } else {
        fmt.Printf("%s: 错误 - %v\n", addresses[i], errors[i])
    }
}
```

## 命令行工具使用

### 编码地址

```bash
# P2PKH
idaddr encode p2pkh 751e76e8199196d454941c45d1b3a323f1433bd6

# P2SH
idaddr encode p2sh 89abcdefabbaabbaabbaabbaabbaabbaabbaabba

# P2WPKH
idaddr encode p2wpkh 751e76e8199196d454941c45d1b3a323f1433bd6

# P2WSH (32字节)
idaddr encode p2wsh 1863143c14c5166804bd19203356da136c985678cd4d27a1b8c6329604903262
```

### 解码地址

```bash
idaddr decode idq1w508d6qejxtdg4y5r3zarvary0c5xw7ky30xwh
```

输出:
```
Address: idq1w508d6qejxtdg4y5r3zarvary0c5xw7ky30xwh
Version: 0
Type: Pay-to-PubKey-Hash
Data: 751e76e8199196d454941c45d1b3a323f1433bd6
Data Length: 20 bytes
```

### 验证地址

```bash
idaddr validate idq1w508d6qejxtdg4y5r3zarvary0c5xw7ky30xwh
```

输出:
```
✓ Address is valid: idq1w508d6qejxtdg4y5r3zarvary0c5xw7ky30xwh
  Type: Pay-to-PubKey-Hash
```

### 地址转换

```bash
# Bitcoin -> ID
idaddr convert 1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa

# ID -> Bitcoin
idaddr convert idq1vt5s0v2uhuna2sjnn84ldu8m2r4m3rccaensxx bitcoin

# ID -> Dogecoin
idaddr convert idq1vt5s0v2uhuna2sjnn84ldu8m2r4m3rccaensxx dogecoin
```

### 创建多签地址

```bash
idaddr multisig -m 2 -n 3 -keys \
"0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798,\
02c6047f9441ed7d6d3045406e95c07cd85c778e4b8cef3ca7abac09b95c709ee5,\
03774ae7f858a9411e5ef4246b70c65aac5649980be5c17891bbec17895da008cb"
```

## 性能指标

基于测试结果（Intel i3-N305）:

| 操作 | 速度 | 内存分配 |
|------|------|----------|
| Base58编码 | 304 ns/op | 136 B/op |
| Base58解码 | 312 ns/op | 120 B/op |
| ID地址编码 | 1739 ns/op | 2184 B/op |
| ID地址解码 | 2425 ns/op | 1376 B/op |
| 地址验证 | 2498 ns/op | 1376 B/op |
| Bitcoin转换 | 2302 ns/op | 2336 B/op |
| 多签地址创建 | 4859 ns/op | 6072 B/op |

**吞吐量估算:**
- **编码**: ~575,000 地址/秒
- **解码**: ~412,000 地址/秒
- **验证**: ~400,000 地址/秒

## 安全注意事项

### 1. 校验和强度
- 使用 BCH 码（6字符，30位）
- 可检测任意 4 个字符错误
- 可检测任意长度突发错误
- 汉明距离至少为 5

### 2. 地址验证
```go
// 始终验证地址
if !idaddress.ValidateIDAddress(addr) {
    return errors.New("无效地址")
}
```

### 3. 数据验证
```go
// 编码前验证数据长度
data := []byte{...}
if len(data) != 20 {  // P2PKH/P2SH/P2WPKH
    return errors.New("数据长度无效")
}
```

### 4. 多签验证
```go
// 验证多签参数
if m <= 0 || n <= 0 || m > n || n > 15 {
    return errors.New("无效的多签参数")
}
```

## 错误处理

### 常见错误

```go
// 1. 无效的地址格式
_, err := idaddress.DecodeIDAddress("invalid")
// err: invalid HRP: must start with 'id'

// 2. 校验和错误
_, err := idaddress.DecodeIDAddress("idq1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq")
// err: checksum verification failed

// 3. 数据长度错误
_, err := idaddress.EncodeIDAddress(idaddress.VersionP2PKH, []byte{0x01})
// err: invalid data length for version 0: expected 20, got 1

// 4. 无效的多签参数
_, err := idaddress.NewP2MSAddress(3, 2, pubkeys)
// err: invalid multisig parameters: m=3, n=2
```

## 测试

```bash
# 运行所有测试
go test -v

# 运行特定测试
go test -v -run TestEncodeDecodeP2PKH

# 运行性能测试
go test -bench=. -benchmem

# 测试覆盖率
go test -cover
```

## 集成示例

### Web API
```go
func handleEncode(w http.ResponseWriter, r *http.Request) {
    data, _ := hex.DecodeString(r.FormValue("data"))
    version := idaddress.VersionP2PKH
    
    addr, err := idaddress.EncodeIDAddress(version, data)
    if err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    
    json.NewEncoder(w).Encode(map[string]string{
        "address": addr,
        "type": idaddress.GetAddressType(version),
    })
}
```

### 数据库存储
```go
type AddressRecord struct {
    IDAddress     string
    BitcoinAddress string
    Version       byte
    Data          []byte
}

func SaveAddress(db *sql.DB, idAddr string) error {
    info, err := idaddress.DecodeIDAddress(idAddr)
    if err != nil {
        return err
    }
    
    bitcoinAddr, _ := idaddress.ConvertToBitcoin(idAddr, "mainnet")
    
    _, err = db.Exec(
        "INSERT INTO addresses (id_address, bitcoin_address, version, data) VALUES (?, ?, ?, ?)",
        idAddr, bitcoinAddr, info.Version, info.Data,
    )
    return err
}
```

## 常见问题

**Q: ID地址与Bech32有什么区别？**
A: ID地址使用自定义HRP("id")和版本系统，支持更多地址类型。

**Q: 可以在生产环境使用吗？**
A: 代码经过完整测试，但建议在生产环境前进行额外审计。

**Q: 如何添加新的区块链支持？**
A: 在converter.go中添加相应的版本字节映射。

**Q: 性能如何优化？**
A: 当前实现已经很高效。可考虑使用缓存或并行处理批量转换。

## 更多资源

- [完整API文档](README.md)
- [技术规范](ID_ADDRESS_SPEC.md)
- [测试示例](examples_test.go)
- [项目主页](https://github.com/yourusername/idaddress)
