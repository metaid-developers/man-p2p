# MVC密钥生成和测试工具

这个工具可以帮助你生成密钥对，并在MVC链上测试转账。

## 功能特性

✅ 生成安全的私钥和公钥对  
✅ 同时生成ID地址和MVC地址  
✅ 支持Hex和WIF两种私钥格式  
✅ ID地址与MVC地址相互转换  
✅ 从私钥恢复地址信息  

## 快速开始

### 1. 编译工具

```bash
cd /srv/dev_project/metaid/man-indexer-v2/idaddress/cmd/mvckey
go build
```

### 2. 生成密钥对

```bash
./mvckey generate
```

输出示例：
```
========================================
新生成的密钥对
========================================

私钥 (Hex):     22aed27eeee7b52ff1e81e74d257eca723e10f7255b9cd1fd2b7926550140cc2
私钥 (WIF):     KxP8VFnshzYzhKeRayVvDpTCYribJBnwouUhadFJ9jGQkNWCjkC2

公钥 (Hex):     02514fbfe2ac97e06183416c6a97a436abc8c3227b637f1d17628ebe9827cd8b7a
公钥哈希:       2a49a09ea9dfc40422e2b8f6c471496c48644419

ID地址:         idq19fy6p84fmlzqgghzhrmvgu2fd3yxg3qezrq6zx
MVC地址:        qq4yngy7480ugppzu2u0d3r3f9kysezyry6865dyrr

⚠️  请妥善保管私钥，不要泄露给任何人！
```

### 3. 运行完整测试流程

```bash
./test_flow.sh
```

这个脚本会：
- 生成新密钥对
- 验证地址转换功能
- 验证私钥恢复功能
- 保存账户信息到JSON文件
- 显示后续操作指南

## 使用命令

### 生成密钥对

```bash
./mvckey generate
```

生成新的随机密钥对，包括：
- 私钥（Hex和WIF格式）
- 公钥（压缩格式）
- 公钥哈希（Hash160）
- ID地址（自定义格式）
- MVC地址（标准格式）

### 地址转换

```bash
# ID地址 -> MVC地址
./mvckey convert idq19fy6p84fmlzqgghzhrmvgu2fd3yxg3qezrq6zx

# MVC地址 -> ID地址
./mvckey convert qq4yngy7480ugppzu2u0d3r3f9kysezyry6865dyrr
```

### 准备转账（重要！）

**⚠️ MVC链和钱包不能直接识别ID地址！**

如果要向ID地址转账，使用 `prepare` 命令自动转换：

```bash
./mvckey prepare idq19fy6p84fmlzqgghzhrmvgu2fd3yxg3qezrq6zx
```

工具会自动：
- 转换ID地址为MVC地址
- 显示转账步骤说明
- 提供钱包和RPC转账方法

详见：[ID地址转账说明](../../ID_ADDRESS_TRANSFER.md)

### 从私钥恢复地址

```bash
./mvckey info 22aed27eeee7b52ff1e81e74d257eca723e10f7255b9cd1fd2b7926550140cc2
```

## MVC链上测试转账

### 方式1：使用MVC钱包

1. **下载并安装MVC钱包**（如ShowPay钱包）

2. **导入私钥**
   - 选择"导入钱包"
   - 输入WIF格式私钥
   - 钱包会自动识别地址

3. **充值测试币**
   - 复制MVC地址
   - 向该地址转入测试币
   - 等待交易确认（约10分钟）

4. **发送转账**
   - 在钱包中点击"发送"
   - 输入目标地址和金额
   - 确认并发送交易

### 方式2：使用MVC RPC接口

需要运行MVC节点，配置文件 `~/.mvc/mvc.conf`：

```ini
server=1
rpcuser=your_username
rpcpassword=your_password
rpcallowip=127.0.0.1
```

使用命令行操作：

```bash
# 导入私钥
mvc-cli importprivkey "KxP8VFnshzYzhKeRayVvDpTCYribJBnwouUhadFJ9jGQkNWCjkC2" "test"

# 查询余额
mvc-cli getbalance "test"

# 列出未花费输出
mvc-cli listunspent 1 9999999 '["qq4yngy7480ugppzu2u0d3r3f9kysezyry6865dyrr"]'

# 发送转账
mvc-cli sendfrom "test" "目标地址" 0.001

# 查询交易状态
mvc-cli getrawtransaction "交易ID" true
```

### 方式3：使用Go代码发送交易

参考 [MVC_TEST_GUIDE.md](../../MVC_TEST_GUIDE.md) 中的Go示例代码。

## 文件说明

- `main.go` - 主程序代码
- `test_flow.sh` - 自动化测试脚本
- `test_account_*.json` - 生成的账户信息文件（请勿提交到版本控制）

## 安全提示

⚠️ **重要提醒：**

1. **妥善保管私钥**
   - 私钥是唯一控制资金的凭证
   - 丢失私钥 = 丢失资金
   - 不要截图、不要发送给他人

2. **测试环境使用**
   - 本工具生成的密钥仅用于测试
   - 不要在生产环境使用随机生成的密钥
   - 生产环境建议使用硬件钱包

3. **网络安全**
   - 在安全的网络环境下生成密钥
   - 避免在公共网络或不可信设备上使用

4. **备份策略**
   - 记录私钥（Hex或WIF格式）
   - 多处备份，妥善保存
   - 考虑使用助记词（BIP39）方案

## 技术细节

### 密钥生成

```go
// 1. 生成32字节随机私钥
privKeyBytes := make([]byte, 32)
rand.Read(privKeyBytes)

// 2. 计算公钥（secp256k1椭圆曲线）
privKey, pubKey := bsvec.PrivKeyFromBytes(bsvec.S256(), privKeyBytes)

// 3. 压缩公钥（33字节）
pubKeyBytes := pubKey.SerializeCompressed()

// 4. 计算Hash160（SHA256 + RIPEMD160）
hash160 := Hash160(pubKeyBytes)

// 5. 生成地址
// ID地址：使用Bech32变体编码
// MVC地址：使用Base58Check编码
```

### 地址格式

**ID地址：**
- 格式：`id` + 版本字符 + 数据 + 校验和
- 示例：`idq19fy6p84fmlzqgghzhrmvgu2fd3yxg3qezrq6zx`
- 字符集：32个字符（小写字母+数字）
- 校验和：6字符BCH码

**MVC地址：**
- 格式：Base58Check编码
- 示例：`qq4yngy7480ugppzu2u0d3r3f9kysezyry6865dyrr`
- 版本字节：0x00（主网P2PKH）
- 校验和：4字节双SHA256

### 依赖库

```go
github.com/bitcoinsv/bsvd/bsvec     // secp256k1椭圆曲线
github.com/bitcoinsv/bsvd/chaincfg  // 链配置参数
github.com/bitcoinsv/bsvutil        // MVC地址工具
golang.org/x/crypto/ripemd160       // RIPEMD160哈希
```

## 故障排除

### 编译错误

```bash
# 更新依赖
cd /srv/dev_project/metaid/man-indexer-v2
go mod tidy

# 重新编译
cd idaddress/cmd/mvckey
go build
```

### 地址格式错误

确保：
- ID地址以 `id` 开头
- MVC地址使用Base58字符集
- 没有额外的空格或换行符

### RPC连接失败

检查：
- MVC节点是否运行
- RPC配置是否正确
- 防火墙设置
- 网络连接

## 相关资源

- [ID地址规范](../../ID_ADDRESS_SPEC.md)
- [MVC测试指南](../../MVC_TEST_GUIDE.md)
- [ID地址快速开始](../../QUICKSTART.md)
- [MVC官方文档](https://www.microvisionchain.com/)
- [MVC区块浏览器](https://www.mvcscan.com/)

## 示例账户

测试时会生成如下格式的JSON文件：

```json
{
  "generated_at": "2025-12-25T13:49:14+08:00",
  "private_key": {
    "hex": "22aed27eeee7b52ff1e81e74d257eca723e10f7255b9cd1fd2b7926550140cc2",
    "wif": "KxP8VFnshzYzhKeRayVvDpTCYribJBnwouUhadFJ9jGQkNWCjkC2"
  },
  "addresses": {
    "id": "idq19fy6p84fmlzqgghzhrmvgu2fd3yxg3qezrq6zx",
    "mvc": "qq4yngy7480ugppzu2u0d3r3f9kysezyry6865dyrr"
  },
  "note": "测试账户 - 请勿用于生产环境"
}
```

**⚠️ 请勿将包含私钥的文件提交到版本控制系统！**

添加到 `.gitignore`：
```
test_account_*.json
*.key
*.wif
```

## 许可证

本工具是 man-indexer-v2 项目的一部分，遵循项目主许可证。
