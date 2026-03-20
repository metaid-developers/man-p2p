# MVC链测试指南

本指南帮助你生成密钥对并在MVC链上进行测试转账。

## 快速开始

### 1. 生成密钥对

```bash
cd /srv/dev_project/metaid/man-indexer-v2/idaddress/cmd/mvckey
go build
./mvckey generate
```

输出示例：
```
========================================
新生成的密钥对
========================================

私钥 (Hex):     a1b2c3d4e5f6...
私钥 (WIF):     L5oLkpV3aqBjhki6LmvChTCV6odsp4SXM9...

公钥 (Hex):     02c6047f9441ed7d6d3045406e95c07cd85c778e4b8cef3ca7abac09b95c709ee5
公钥哈希:       751e76e8199196d454941c45d1b3a323f1433bd6

ID地址:         idq1w508d6qejxtdg4y5r3zarvary0c5xw7ky30xwh
MVC地址:        1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2

⚠️  请妥善保管私钥，不要泄露给任何人！
========================================
```

**重要提示：**
- 私钥(Hex)和WIF格式都可以用来恢复钱包
- ID地址和MVC地址指向同一个账户，只是格式不同
- 请妥善保管私钥，丢失将无法找回！

### 2. 地址转换

ID地址和MVC地址可以相互转换：

```bash
# ID地址 -> MVC地址
./mvckey convert idq1w508d6qejxtdg4y5r3zarvary0c5xw7ky30xwh

# MVC地址 -> ID地址
./mvckey convert 1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2
```

### 3. 从私钥恢复地址

如果你已经有私钥，可以恢复地址信息：

```bash
./mvckey info a1b2c3d4e5f6...
```

## MVC链转账测试

### 方法1：使用MVC钱包

1. **导入私钥到钱包**
   - 下载MVC钱包（如ShowPay钱包）
   - 使用WIF格式私钥导入
   - 或者使用Hex私钥导入

2. **充值测试币**
   - 向生成的MVC地址转入少量测试币
   - 等待交易确认（约10分钟）

3. **发送转账**
   - 在钱包中输入目标地址
   - 输入转账金额
   - 确认并发送交易

### 方法2：使用MVC RPC接口

如果你运行了MVC节点，可以使用RPC接口：

#### 导入私钥到节点钱包

```bash
mvc-cli importprivkey "你的WIF私钥" "test_account" false
```

#### 查询余额

```bash
mvc-cli getbalance "test_account"
```

#### 发送转账

```bash
# 发送到MVC地址
mvc-cli sendfrom "test_account" "目标MVC地址" 0.001

# 或发送到ID地址（需要先转换）
ID_ADDR="idq1w508d6qejxtdg4y5r3zarvary0c5xw7ky30xwh"
MVC_ADDR=$(./mvckey convert $ID_ADDR | grep "MVC地址:" | awk '{print $2}')
mvc-cli sendfrom "test_account" "$MVC_ADDR" 0.001
```

### 方法3：使用Go程序发送交易

创建一个简单的转账程序：

```go
package main

import (
    "encoding/hex"
    "fmt"
    "log"
    
    "github.com/bitcoinsv/bsvd/chaincfg"
    "github.com/bitcoinsv/bsvd/rpcclient"
    "github.com/bitcoinsv/bsvutil"
)

func main() {
    // 连接到MVC节点
    client, err := rpcclient.New(&rpcclient.ConnConfig{
        Host:         "127.0.0.1:9882",
        User:         "showpay",
        Pass:         "showpay88..",
        HTTPPostMode: true,
        DisableTLS:   true,
    }, nil)
    if err != nil {
        log.Fatal(err)
    }
    defer client.Shutdown()
    
    // 导入私钥 (WIF格式)
    wif := "你的WIF私钥"
    err = client.ImportPrivKey(wif, "test_account", false)
    if err != nil {
        log.Printf("Import key: %v", err)
    }
    
    // 发送交易
    toAddr, _ := bsvutil.DecodeAddress("目标地址", &chaincfg.MainNetParams)
    txHash, err := client.SendFromMinConf("test_account", toAddr, 0.001, 1)
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Printf("Transaction sent: %s\n", txHash)
}
```

## 交易验证

### 查询交易状态

```bash
# 通过RPC查询
mvc-cli getrawtransaction "交易ID" true

# 或使用区块浏览器
# MVC主网浏览器: https://www.mvcscan.com/
```

### 查询地址余额

```bash
# 使用RPC
mvc-cli getreceivedbyaddress "你的MVC地址" 1

# 或使用mvckey工具配合jq
ADDR="1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2"
mvc-cli listunspent 1 9999999 "[\"$ADDR\"]"
```

## 完整测试流程示例

```bash
#!/bin/bash

# 1. 生成密钥对
cd /srv/dev_project/metaid/man-indexer-v2/idaddress/cmd/mvckey
go build
./mvckey generate > account.txt

# 2. 提取地址
MVC_ADDR=$(grep "MVC地址:" account.txt | awk '{print $2}')
ID_ADDR=$(grep "ID地址:" account.txt | awk '{print $2}')
PRIV_KEY_WIF=$(grep "私钥 (WIF):" account.txt | awk '{print $3}')

echo "MVC地址: $MVC_ADDR"
echo "ID地址:  $ID_ADDR"

# 3. 导入到钱包
mvc-cli importprivkey "$PRIV_KEY_WIF" "test_account" false

# 4. 等待充值（手动充值后继续）
read -p "请向地址 $MVC_ADDR 转入测试币，完成后按回车继续..."

# 5. 检查余额
echo "查询余额..."
mvc-cli getbalance "test_account"

# 6. 发送测试交易
TARGET_ADDR="1BoatSLRHtKNngkdXEeobR76b53LETtpyT"
echo "发送 0.0001 MVC 到 $TARGET_ADDR"
TX_ID=$(mvc-cli sendfrom "test_account" "$TARGET_ADDR" 0.0001)

echo "交易已发送: $TX_ID"
echo "查看交易详情: mvc-cli getrawtransaction $TX_ID true"

# 7. 验证地址转换
echo "验证地址转换..."
CONVERTED=$(./mvckey convert $ID_ADDR | grep "MVC地址:" | awk '{print $2}')
if [ "$CONVERTED" == "$MVC_ADDR" ]; then
    echo "✓ 地址转换验证成功"
else
    echo "✗ 地址转换验证失败"
fi
```

## 注意事项

1. **网络选择**
   - 目前工具默认使用MVC主网参数
   - 如果需要测试网，需要修改代码中的 `chaincfg.MainNetParams` 为测试网参数

2. **手续费**
   - MVC网络的最低手续费约为 50 satoshi/byte
   - 建议使用钱包自动计算手续费

3. **交易确认**
   - MVC区块时间约10分钟
   - 建议等待至少1个确认

4. **安全提醒**
   - 生成的私钥仅用于测试
   - 不要在主网使用测试密钥存储大额资产
   - 测试完成后可以清理账户信息

5. **兼容性**
   - ID地址格式是自定义的，现有MVC钱包不直接支持
   - 需要先转换为标准MVC地址再使用
   - 或者在应用层做转换

## 故障排除

### 问题：编译失败

```bash
# 确保依赖已安装
cd /srv/dev_project/metaid/man-indexer-v2
go mod tidy
go mod download
```

### 问题：连接RPC失败

```bash
# 检查MVC节点是否运行
ps aux | grep mvcd

# 检查RPC配置
cat ~/.mvc/mvc.conf
```

### 问题：导入私钥失败

```bash
# 确认私钥格式正确
# WIF私钥应该以 L 或 K 开头（压缩格式）
# 或以 5 开头（非压缩格式）
```

## 扩展功能

### 批量生成地址

```bash
# 生成10个测试地址
for i in {1..10}; do
    echo "=== 地址 $i ==="
    ./mvckey generate
    echo ""
done
```

### 监控地址余额

```bash
#!/bin/bash
ADDR="你的MVC地址"
while true; do
    BALANCE=$(mvc-cli getreceivedbyaddress "$ADDR" 0)
    echo "$(date): 余额 = $BALANCE MVC"
    sleep 60
done
```

## 参考资料

- [MVC官方文档](https://www.microvisionchain.com/)
- [MVC区块浏览器](https://www.mvcscan.com/)
- [Bitcoin Script参考](https://en.bitcoin.it/wiki/Script)
- [ID地址规范](../ID_ADDRESS_SPEC.md)
