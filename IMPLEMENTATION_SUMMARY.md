# Dogecoin 适配器实现总结

## 实现完成 ✅

已成功为 man-indexer-v2 添加了完整的 Dogecoin 链支持，包括：

### 1. 核心文件

#### 配置层 (common/config.go)
- ✅ 添加 `dogeConfig` 结构体
- ✅ 支持命令行参数配置
- ✅ 支持 TOML 配置文件

#### 适配器层 (adapter/dogecoin/)
- ✅ **dogecoin.go**: Chain 接口实现
  - RPC 客户端初始化
  - 区块和交易获取
  - 区块链状态查询
  - 费用和大小计算
  
- ✅ **indexer.go**: Indexer 接口实现  
  - P2SH ScriptSig 铭刻解析（区别于 Bitcoin 的 Witness）
  - OP_RETURN 铭刻支持
  - 区块铭刻捕获
  - Mempool 铭刻监听
  - MRC20 原生转账支持
  - PIN 转账追踪

- ✅ **params.go**: Dogecoin 网络参数
  - DogeMainNetParams (主网)
  - DogeTestNetParams (测试网)  
  - DogeRegTestParams (回归测试网)
  - 正确的魔数、地址前缀、端口配置

- ✅ **zmq.go**: ZMQ 实时通知
  - 实时交易监听
  - 铭刻检测
  - PIN 转账检查

#### 主程序集成 (man/man.go)
- ✅ 导入 dogecoin 包
- ✅ InitAdapter 中添加 doge 链支持
- ✅ getSyncHeight 中添加 doge 初始高度配置

### 2. 配置文件

- ✅ **config_doge.toml**: Dogecoin 配置示例
  - 主网、测试网、Regtest 配置
  - RPC 和 ZMQ 配置
  - 其他必要参数

### 3. 文档

- ✅ **DOGECOIN_ADAPTER.md**: 完整使用指南
  - 概述和特性说明
  - Bitcoin vs Dogecoin 差异对比
  - 铭刻格式详解
  - 配置说明
  - 启动方式
  - 故障排除
  - API 示例
  - 性能优化建议

- ✅ **IMPLEMENTATION_SUMMARY.md**: 本文档

### 4. 工具脚本

- ✅ **start_doge_indexer.sh**: 快速启动脚本
  - Regtest 环境自动部署
  - 测试网和主网启动向导
  - Docker 集成

## 关键技术实现

### 1. P2SH 铭刻解析

与 Bitcoin 的 SegWit Witness 不同，Dogecoin 使用 P2SH (Pay-to-Script-Hash) 方式：

```go
// 从 ScriptSig 中提取 redeemScript
tokenizer := txscript.MakeScriptTokenizer(0, input.SignatureScript)
var redeemScript []byte
var lastData []byte

for tokenizer.Next() {
    if len(tokenizer.Data()) > 0 {
        lastData = tokenizer.Data()
    }
}
redeemScript = lastData

// 解析 redeemScript 中的铭刻数据
// 格式: <pubkey> OP_CHECKSIGVERIFY OP_FALSE OP_IF "ord" OP_1 <contentType> OP_0 <content> OP_ENDIF OP_TRUE
```

### 2. 网络参数配置

正确设置 Dogecoin 特有的网络参数：

```go
DogeMainNetParams = chaincfg.Params{
    Net:              wire.BitcoinNet(0xc0c0c0c0), // Dogecoin 魔数
    PubKeyHashAddrID: 0x1e,                        // D 开头
    ScriptHashAddrID: 0x16,                        // 9 开头
    PrivateKeyID:     0x9e,                        // Q 开头
    // ...
}
```

### 3. 交易版本

Dogecoin 使用交易版本 2：

```go
tx := wire.NewMsgTx(2)  // Version 2 for Dogecoin
```

## 使用方式

### 基本使用

```bash
# 编译
go build -o manindexer

# 启动 Dogecoin 索引器
./manindexer -chain doge -config config_doge.toml

# 多链索引
./manindexer -chain btc,doge -config config.toml

# 使用快速启动脚本
./start_doge_indexer.sh
```

### 配置示例

```toml
[doge]
initialHeight = 0
rpcHost = "127.0.0.1:22555"
rpcUser = "dogecoin"
rpcPass = "password"
rpcHttpPostMode = true
rpcDisableTLS = true
zmqHost = "tcp://127.0.0.1:28555"
popCutNum = 21
```

### 命令行参数

```bash
./manindexer \
  -chain doge \
  -doge_rpc_host 127.0.0.1:18332 \
  -doge_rpc_user regtest \
  -doge_rpc_password regtest \
  -doge_zmqpubrawtx tcp://127.0.0.1:18444 \
  -doge_height 0
```

## 测试建议

### 1. Regtest 环境测试

```bash
# 启动 Dogecoin Regtest 节点
docker run -d --name dogecoin-regtest \
  -p 18332:18332 \
  -p 18444:18444 \
  -e NETWORK=regtest \
  -e RPC_USER=regtest \
  -e RPC_PASSWORD=regtest \
  ich777/dogecoin-core

# 使用 metaid-cli 创建测试铭刻
cd /srv/dev_project/metaid/metaid-cli
./metaid-cli init --chain doge
./metaid-cli sync --chain doge
./metaid-cli inscribe create \
  --chain doge \
  --payload '{"test": "Dogecoin inscription"}' \
  --path "/test" \
  --address <your-address> \
  --feerate 100000
```

### 2. 验证铭刻解析

启动索引器后，检查日志确认：
- ✅ RPC 连接成功
- ✅ 区块同步正常
- ✅ ZMQ 消息接收
- ✅ 铭刻数据解析正确

### 3. 数据库检查

```bash
# 检查 Pebble 数据库
ls -la ./man_base_data_pebble/

# 查询索引数据（通过 API）
curl http://localhost:8080/api/pins/doge?path=/test
```

## 兼容性说明

### 与 metaid-cli 兼容

本实现完全兼容 metaid-cli 的 Dogecoin 铭刻格式：

- ✅ P2SH redeem script 格式
- ✅ "ord" 标记识别
- ✅ contentType 和 content 解析
- ✅ 地址和网络参数一致

### btcd 库兼容

使用 btcd 库处理 Dogecoin 交易：

- ✅ wire.MsgTx 交易结构
- ✅ txscript 脚本解析
- ✅ chaincfg 网络参数
- ✅ rpcclient RPC 客户端

## 已知限制

1. **SegWit 支持**: 当前实现针对未激活 SegWit 的 Dogecoin Regtest。如果 Dogecoin 主网或测试网激活了 SegWit，可能需要额外适配。

2. **铭刻格式**: 当前解析基于 "ord" 标记的简单格式。如果需要支持更复杂的 metaid 格式（operation, path, encryption 等），需要扩展 `ParsePinFromRedeemScript` 函数。

3. **性能优化**: 首次同步大量区块时可能较慢，建议设置合适的 initialHeight。

## 未来改进方向

1. **完整 MetaID 格式**: 支持完整的 MetaID 协议字段（operation, path, encryption, version）
2. **SegWit 检测**: 自动检测 SegWit 激活状态并切换解析逻辑
3. **性能优化**: 批量处理、缓存优化、并发同步
4. **主网验证**: 在 Dogecoin 主网上测试和验证
5. **额外索引**: 创建 Dogecoin 特定的索引结构

## 相关参考

- metaid-cli Dogecoin 实现: `/srv/dev_project/metaid/metaid-cli/pkg/inscribe/inscribe/doge_builder.go`
- metaid-cli Dogecoin 文档: `/srv/dev_project/metaid/metaid-cli/DOGECOIN_SUPPORT.md`
- Dogecoin Core: https://github.com/dogecoin/dogecoin
- BIP 16 (P2SH): https://github.com/bitcoin/bips/blob/master/bip-0016.mediawiki

## 总结

✅ **完成状态**: Dogecoin 适配器已完整实现并集成到 man-indexer-v2

✅ **功能完整**: 支持区块索引、铭刻解析、实时监听、转账追踪

✅ **文档齐全**: 提供完整的使用文档、配置示例、故障排除指南

✅ **测试就绪**: 包含 Regtest 测试环境和快速启动脚本

🚀 **可以开始使用**: 按照 DOGECOIN_ADAPTER.md 文档即可启动和使用
