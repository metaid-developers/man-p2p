# Dogecoin 适配器使用指南

## 概述

本适配器为 man-indexer-v2 提供了 Dogecoin 区块链的铭刻数据索引支持。与 Bitcoin 不同，Dogecoin 默认未激活 SegWit，因此铭刻数据存储在 P2SH (Pay-to-Script-Hash) 的 ScriptSig 中，而不是 Witness 数据中。

## 关键特性

- ✅ 支持 Dogecoin 主网、测试网和 Regtest
- ✅ P2SH 铭刻解析（兼容 metaid-cli 的 Doge 铭刻格式）
- ✅ ZMQ 实时通知支持
- ✅ MRC20 原生转账支持
- ✅ 完整的索引器功能

## Dogecoin vs Bitcoin 铭刻差异

| 特性 | Bitcoin | Dogecoin |
|------|---------|----------|
| SegWit 支持 | ✅ 已激活 | ❌ 默认未激活 (Regtest) |
| 地址类型 | P2WSH / Taproot | P2SH (Legacy) |
| 脚本位置 | Witness | ScriptSig |
| 铭刻格式 | Taproot witness | P2SH redeem script |

## 铭刻格式

### Dogecoin P2SH Redeem Script 结构

```
<pubkey> OP_CHECKSIGVERIFY
OP_FALSE OP_IF
  "ord"
  OP_1 <contentType>
  OP_0 <content>
OP_ENDIF
OP_TRUE
```

### 执行流程

1. **签名验证**: `OP_CHECKSIGVERIFY` 验证签名和公钥
2. **铭刻信封**: `OP_FALSE OP_IF...OP_ENDIF` 块在执行时被跳过，仅存储数据
3. **成功标志**: `OP_TRUE` 确保脚本执行成功

## 配置说明

### 1. 配置文件设置

在 `config.toml` 中添加 Dogecoin 配置：

```toml
[doge]
initialHeight = 0              # 起始区块高度
mrc20Height = 0                # MRC20 起始高度
rpcHost = "127.0.0.1:22555"    # Dogecoin RPC 地址
rpcUser = "dogecoin"           # RPC 用户名
rpcPass = "password"           # RPC 密码
rpcHttpPostMode = true
rpcDisableTLS = true
zmqHost = "tcp://127.0.0.1:28555"  # ZMQ 地址
popCutNum = 21                 # PoP 截断数量
```

### 2. 网络配置

#### 主网 (Mainnet)
```toml
rpcHost = "127.0.0.1:22555"
zmqHost = "tcp://127.0.0.1:28555"
```

#### 测试网 (Testnet)
```toml
rpcHost = "127.0.0.1:44555"
zmqHost = "tcp://127.0.0.1:44556"
```

#### Regtest (本地测试)
```toml
rpcHost = "127.0.0.1:18332"
zmqHost = "tcp://127.0.0.1:18444"
```

## 启动方式

### 1. 启动 Dogecoin 节点

#### 使用 Docker (推荐)

```bash
docker run -d --name dogecoin-node \
  -p 22555:22555 \
  -p 28555:28555 \
  -e NETWORK=mainnet \
  -e RPC_USER=dogecoin \
  -e RPC_PASSWORD=password \
  -v dogecoin-data:/dogecoin/data \
  dogecoin/dogecoin
```

#### Regtest 测试环境

```bash
docker run -d --name dogecoin-regtest \
  -p 18332:18332 \
  -p 18444:18444 \
  -e NETWORK=regtest \
  -e RPC_USER=regtest \
  -e RPC_PASSWORD=regtest \
  ich777/dogecoin-core
```

### 2. 启动索引器

```bash
# 单链索引 - Dogecoin
./manindexer -chain doge -config config_doge.toml

# 多链索引 - Bitcoin + Dogecoin
./manindexer -chain btc,doge -config config.toml

# Dogecoin 测试网
./manindexer -chain doge -test 1 -config config.toml

# Dogecoin Regtest
./manindexer -chain doge -test 2 -config config.toml
```

## 命令行参数

```bash
# Dogecoin 专用参数
--doge_height          # Dogecoin 起始区块高度
--doge_rpc_host        # Dogecoin RPC 地址
--doge_rpc_user        # Dogecoin RPC 用户名
--doge_rpc_password    # Dogecoin RPC 密码
--doge_zmqpubrawtx     # Dogecoin ZMQ 地址
```

示例：

```bash
./manindexer \
  -chain doge \
  -doge_rpc_host 127.0.0.1:18332 \
  -doge_rpc_user regtest \
  -doge_rpc_password regtest \
  -doge_zmqpubrawtx tcp://127.0.0.1:18444 \
  -doge_height 0
```

## 测试铭刻

使用 metaid-cli 在 Dogecoin 上创建铭刻：

```bash
# 1. 初始化 Dogecoin 钱包
./metaid-cli init --chain doge

# 2. 同步 UTXO
./metaid-cli sync --chain doge

# 3. 创建铭刻
./metaid-cli inscribe create \
  --chain doge \
  --payload '{"msg": "Hello Dogecoin!"}' \
  --path "/test" \
  --address <your-doge-address> \
  --feerate 100000
```

## 网络参数

### Dogecoin 地址前缀

| 网络 | P2PKH | P2SH | 私钥 |
|------|-------|------|------|
| 主网 | D | 9 | Q |
| 测试网 | n | 2 | 9/c |
| Regtest | m/n | 2 | - |

### 魔数 (Magic Bytes)

- 主网: `0xc0c0c0c0`
- 测试网: `0xfcc1b7dc`
- Regtest: `0xfabfb5da`

## 技术实现细节

### P2SH 地址生成

```go
// 正确方式：NewAddressScriptHash 会自动计算 HASH160
addr, err := btcutil.NewAddressScriptHash(script, network)

// 错误方式：不要手动 hash 两次
scriptHash := btcutil.Hash160(script)
addr, err := btcutil.NewAddressScriptHash(scriptHash, network)  // 错误！
```

### 签名哈希计算

```go
// 对于 P2SH 交易，必须使用 redeemScript 作为 scriptCode
sigHash, err := txscript.CalcSignatureHash(
    redeemScript,           // 使用 redeemScript，不是 P2SH pkScript
    txscript.SigHashAll,
    tx,
    0,
)
```

### 交易版本

```go
// Dogecoin 使用交易版本 2
tx := wire.NewMsgTx(2)
```

## 故障排除

### "no-witness-yet" 错误

**原因**: 尝试在未激活 SegWit 的 Dogecoin Regtest 上使用 P2WSH。

**解决**: DogeBuilder 会自动使用 P2SH 而不是 P2WSH。确保使用 `--chain doge` 参数。

### "Script evaluated without error but finished with a false/empty top stack element"

**原因**:
1. 签名验证失败（sigHash 计算错误）
2. P2SH 地址不匹配（redeemScript hash 错误）
3. 脚本末尾缺少 `OP_TRUE`

**解决**:
- 确保使用 `btcutil.NewAddressScriptHash(script, net)` 正确生成地址
- 确保脚本以 `OP_TRUE` 结尾
- 确保签名使用 redeemScript 计算 sigHash

### WIF 解码错误

**原因**: Dogecoin 私钥使用不同的 WIF 格式。

**解决**: 代码已自动处理 WIF 转换：

```go
if len(privateKey) != 64 {
    decoded, version, err := base58.CheckDecode(privateKey)
    if err == nil && len(decoded) == 33 {
        privateKey = hex.EncodeToString(decoded[:32])
    }
}
```

## API 示例

### 获取区块信息

```bash
curl -X POST http://localhost:8080/api/block/doge/12345
```

### 查询铭刻

```bash
curl -X POST http://localhost:8080/api/pins/doge \
  -H "Content-Type: application/json" \
  -d '{"path": "/test", "limit": 10}'
```

### 获取地址铭刻

```bash
curl -X POST http://localhost:8080/api/address/doge/<doge-address>
```

## 性能优化

### 1. 数据库配置

```toml
[pebble]
dir = "./man_base_data_pebble"
num = 20  # 增加数据库分片数
```

### 2. 同步配置

```toml
[sync]
isFullNode = true  # 完整节点模式，索引更多数据
```

### 3. 费率设置

Dogecoin 费率通常比 Bitcoin 低：

```toml
[doge]
popCutNum = 17  # 测试网可以降低 PoP 截断数
```

## 未来增强

计划中的改进：

1. ✨ **SegWit 支持**: 检测 SegWit 激活的 Dogecoin 网络
2. 🔍 **主网测试**: 在 Dogecoin 主网上测试（需要真实 DOGE）
3. ⚡ **费用优化**: 针对 Dogecoin 的低费用进行优化
4. 📊 **铭刻索引**: 构建 Dogecoin 铭刻专用索引器
5. 🔐 **多重签名**: 支持 P2SH 多重签名铭刻

## 参考资料

- [Bitcoin Script Wiki](https://en.bitcoin.it/wiki/Script)
- [BIP 16: P2SH](https://github.com/bitcoin/bips/blob/master/bip-0016.mediawiki)
- [Dogecoin Core](https://github.com/dogecoin/dogecoin)
- [btcd Documentation](https://github.com/btcsuite/btcd)
- [metaid-cli Dogecoin Support](../metaid-cli/DOGECOIN_SUPPORT.md)

## 支持

如有问题或建议，请提交 Issue 或 Pull Request。
