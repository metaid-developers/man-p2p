#!/bin/bash

echo "=========================================="
echo "MVC链测试 - 完整流程示例"
echo "=========================================="
echo ""

# 进入工具目录
cd /srv/dev_project/metaid/man-indexer-v2/idaddress/cmd/mvckey

# 检查工具是否存在
if [ ! -f "./mvckey" ]; then
    echo "正在编译工具..."
    go build
    if [ $? -ne 0 ]; then
        echo "❌ 编译失败"
        exit 1
    fi
    echo "✓ 编译成功"
fi

echo "1️⃣  生成新密钥对..."
echo "=========================================="
./mvckey generate | tee keypair.txt
echo ""

# 提取地址
MVC_ADDR=$(grep "MVC地址:" keypair.txt | awk '{print $2}')
ID_ADDR=$(grep "ID地址:" keypair.txt | awk '{print $2}')
PRIV_KEY_HEX=$(grep "私钥 (Hex):" keypair.txt | awk '{print $3}')
PRIV_KEY_WIF=$(grep "私钥 (WIF):" keypair.txt | awk '{print $3}')

echo ""
echo "2️⃣  验证地址转换..."
echo "=========================================="
echo "测试 ID -> MVC 转换:"
./mvckey convert $ID_ADDR
echo ""

echo "测试 MVC -> ID 转换:"
./mvckey convert $MVC_ADDR
echo ""

echo "3️⃣  验证私钥恢复..."
echo "=========================================="
./mvckey info $PRIV_KEY_HEX
echo ""

echo "4️⃣  保存账户信息..."
echo "=========================================="
cat > test_account_$(date +%Y%m%d_%H%M%S).json <<EOF
{
  "generated_at": "$(date -Iseconds)",
  "private_key": {
    "hex": "$PRIV_KEY_HEX",
    "wif": "$PRIV_KEY_WIF"
  },
  "addresses": {
    "id": "$ID_ADDR",
    "mvc": "$MVC_ADDR"
  },
  "note": "测试账户 - 请勿用于生产环境"
}
EOF

ACCOUNT_FILE="test_account_$(date +%Y%m%d_%H%M%S).json"
echo "✓ 账户信息已保存到: $ACCOUNT_FILE"
echo ""

echo "=========================================="
echo "✅ 密钥生成和验证完成！"
echo "=========================================="
echo ""
echo "📋 账户信息摘要："
echo "   MVC地址: $MVC_ADDR"
echo "   ID地址:  $ID_ADDR"
echo ""
echo "🔑 私钥 (请妥善保管)："
echo "   Hex格式: $PRIV_KEY_HEX"
echo "   WIF格式: $PRIV_KEY_WIF"
echo ""
echo "=========================================="
echo "📝 下一步操作："
echo "=========================================="
echo ""
echo "方式1: 使用MVC钱包测试"
echo "  1. 导入WIF私钥到MVC钱包"
echo "  2. 向 MVC 地址充值: $MVC_ADDR"
echo "  3. 使用钱包发送转账"
echo ""
echo "方式2: 使用MVC RPC接口"
echo "  # 导入私钥"
echo "  mvc-cli importprivkey \"$PRIV_KEY_WIF\" \"test\" false"
echo ""
echo "  # 查询余额"
echo "  mvc-cli getbalance \"test\""
echo ""
echo "  # 发送转账"
echo "  mvc-cli sendfrom \"test\" \"目标地址\" 0.001"
echo ""
echo "方式3: 在线水龙头获取测试币"
echo "  访问 MVC 测试网水龙头获取免费测试币"
echo "  地址: $MVC_ADDR"
echo ""
echo "=========================================="
echo "🔍 查看交易："
echo "=========================================="
echo "  MVC区块浏览器: https://www.mvcscan.com/"
echo "  搜索地址: $MVC_ADDR"
echo ""

# 清理临时文件
rm -f keypair.txt

echo "完成！"
