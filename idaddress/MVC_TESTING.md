# MVC链测试 - 完整说明

## 概述

已创建完整的MVC密钥生成和测试工具，可以：
- ✅ 生成安全的私钥和地址
- ✅ ID地址和MVC地址相互转换
- ✅ 支持在MVC链上进行转账测试

## 工具位置

```
/srv/dev_project/metaid/man-indexer-v2/idaddress/cmd/mvckey/
├── main.go           # 主程序
├── test_flow.sh      # 自动化测试脚本
└── README.md         # 详细使用文档
```

## 快速使用

### 1. 一键运行完整测试

```bash
cd /srv/dev_project/metaid/man-indexer-v2/idaddress/cmd/mvckey
./test_flow.sh
```

这会自动：
1. 生成新的密钥对
2. 验证地址转换
3. 验证私钥恢复
4. 保存账户信息
5. 显示后续操作指南

### 2. 手动使用工具

```bash
# 生成密钥对
./mvckey generate

# 地址转换
./mvckey convert idq19fy6p84fmlzqgghzhrmvgu2fd3yxg3qezrq6zx
./mvckey convert qq4yngy7480ugppzu2u0d3r3f9kysezyry6865dyrr

# 从私钥恢复
./mvckey info 22aed27eeee7b52ff1e81e74d257eca723e10f7255b9cd1fd2b7926550140cc2
```

## 测试转账步骤

### 推荐方式：使用MVC钱包

1. **运行工具生成地址**
   ```bash
   ./mvckey generate
   ```
   记录输出的：
   - MVC地址
   - 私钥（WIF格式）

2. **导入到MVC钱包**
   - 下载ShowPay或其他MVC钱包
   - 选择"导入钱包"
   - 粘贴WIF格式私钥

3. **充值测试币**
   - 向MVC地址转入少量测试币（如0.01 MVC）
   - 等待交易确认（约10分钟）

4. **发送测试转账**
   - 在钱包中选择"发送"
   - 输入任意目标地址和金额
   - 确认交易

5. **验证结果**
   - 在区块浏览器查看交易
   - 网址：https://www.mvcscan.com/
   - 搜索交易ID或地址

### 高级方式：使用RPC接口

如果你运行了MVC节点：

```bash
# 1. 导入私钥
mvc-cli importprivkey "你的WIF私钥" "test_account"

# 2. 查询余额
mvc-cli getbalance "test_account"

# 3. 发送转账
mvc-cli sendfrom "test_account" "目标地址" 0.001

# 4. 查询交易
mvc-cli getrawtransaction "交易ID" true
```

## 示例输出

运行 `./test_flow.sh` 的完整输出：

```
==========================================
MVC链测试 - 完整流程示例
==========================================

1️⃣  生成新密钥对...
==========================================
========================================
新生成的密钥对
========================================

私钥 (Hex):     22aed27eeee7b52ff1e81e74d257eca723e10f7255b9cd1fd2b7926550140cc2
私钥 (WIF):     KxP8VFnshzYzhKeRayVvDpTCYribJBnwouUhadFJ9jGQkNWCjkC2

公钥 (Hex):     02514fbfe2ac97e06183416c6a97a436abc8c3227b637f1d17628ebe9827cd8b7a
公钥哈希:       2a49a09ea9dfc40422e2b8f6c471496c48644419

ID地址:         idq19fy6p84fmlzqgghzhrmvgu2fd3yxg3qezrq6zx
MVC地址:        qq4yngy7480ugppzu2u0d3r3f9kysezyry6865dyrr

2️⃣  验证地址转换...
==========================================
测试 ID -> MVC 转换:
ID地址:  idq19fy6p84fmlzqgghzhrmvgu2fd3yxg3qezrq6zx
MVC地址: qq4yngy7480ugppzu2u0d3r3f9kysezyry6865dyrr

✅ 密钥生成和验证完成！

📝 下一步操作：
- 方式1: 使用MVC钱包测试
- 方式2: 使用MVC RPC接口
- 方式3: 在线水龙头获取测试币
```

## 地址格式说明

生成的地址有两种格式，指向同一个账户：

| 格式 | 示例 | 说明 |
|------|------|------|
| **ID地址** | `idq19fy6p84fmlzqgghzhrmvgu2fd3yxg3qezrq6zx` | 自定义格式，使用Bech32变体编码 |
| **MVC地址** | `qq4yngy7480ugppzu2u0d3r3f9kysezyry6865dyrr` | 标准格式，与Bitcoin兼容 |

**重要：**
- 两个地址指向同一个账户
- MVC钱包只识别MVC地址格式
- 使用工具可以在两种格式间转换

## 安全提醒

⚠️ **请注意：**

1. **私钥安全**
   - 私钥是控制资金的唯一凭证
   - 不要截图、不要分享
   - 丢失无法找回

2. **测试用途**
   - 生成的密钥仅用于测试
   - 不要存储大额资金
   - 测试完成后可以清理

3. **文件管理**
   - `test_account_*.json` 包含私钥
   - 不要提交到Git
   - 使用后建议删除

## 故障排除

### 问题1：编译失败

```bash
cd /srv/dev_project/metaid/man-indexer-v2
go mod tidy
cd idaddress/cmd/mvckey
go build
```

### 问题2：地址转换失败

确保地址格式正确：
- ID地址必须以 `id` 开头
- MVC地址使用Base58字符集
- 去除空格和换行符

### 问题3：RPC连接失败

检查：
- MVC节点是否运行：`ps aux | grep mvcd`
- RPC配置：`cat ~/.mvc/mvc.conf`
- 端口是否开放：`netstat -tlnp | grep 9882`

## 相关文档

- [工具使用说明](cmd/mvckey/README.md) - 详细命令文档
- [MVC测试指南](MVC_TEST_GUIDE.md) - 完整测试流程
- [ID地址规范](ID_ADDRESS_SPEC.md) - 技术规范
- [快速开始](QUICKSTART.md) - ID地址系统入门

## 技术支持

如有问题，请查看：
- 工具README：`cmd/mvckey/README.md`
- MVC官方文档：https://www.microvisionchain.com/
- 区块浏览器：https://www.mvcscan.com/

## 总结

现在你可以：

✅ **生成密钥**：运行 `./mvckey generate`  
✅ **转换地址**：在ID和MVC格式间切换  
✅ **测试转账**：使用钱包或RPC进行实际转账  
✅ **验证交易**：在区块浏览器查看结果  

开始测试吧！🚀
