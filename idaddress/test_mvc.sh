#!/bin/bash

# MVC测试脚本 - 密钥生成和转账测试

set -e

echo "========================================="
echo "MVC 链测试工具"
echo "========================================="
echo ""

# 编译工具
echo "1. 编译测试工具..."
cd cmd/mvctest
go build -o mvctest
if [ $? -ne 0 ]; then
    echo "❌ 编译失败"
    exit 1
fi
echo "✓ 编译成功"
echo ""

# 生成密钥对
echo "2. 生成新的密钥对..."
./mvctest generate > keypair.txt
cat keypair.txt
echo ""

# 提取地址信息
MVC_ADDR=$(grep "MVC地址:" keypair.txt | awk '{print $2}')
ID_ADDR=$(grep "ID地址:" keypair.txt | awk '{print $2}')
PRIV_KEY=$(grep "私钥 (Hex):" keypair.txt | awk '{print $3}')

echo "========================================="
echo "生成的地址信息"
echo "========================================="
echo "MVC地址: $MVC_ADDR"
echo "ID地址:  $ID_ADDR"
echo ""

# 保存到文件
cat > test_account.txt <<EOF
MVC测试账户信息
生成时间: $(date)

MVC地址: $MVC_ADDR
ID地址:  $ID_ADDR
私钥:    $PRIV_KEY

⚠️  这是测试账户，请勿用于正式环境
EOF

echo "✓ 账户信息已保存到 test_account.txt"
echo ""

echo "========================================="
echo "下一步操作"
echo "========================================="
echo "1. 向此地址转入测试币:"
echo "   MVC地址: $MVC_ADDR"
echo ""
echo "2. 查询余额:"
echo "   ./mvctest balance $MVC_ADDR"
echo ""
echo "3. 发送转账 (需要先有余额):"
echo "   ./mvctest send $PRIV_KEY <目标地址> <金额(satoshi)>"
echo ""
echo "示例:"
echo "   ./mvctest send $PRIV_KEY 1BoatSLRHtKNngkdXEeobR76b53LETtpyT 100000"
echo ""

# 清理
rm keypair.txt

echo "完成!"
