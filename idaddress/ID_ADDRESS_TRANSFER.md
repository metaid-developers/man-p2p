# 向ID地址转账说明

## 核心概念

### ❌ 不能直接转账

**MVC链和标准钱包不识别ID地址格式，无法直接向ID地址转账。**

```
❌ 错误操作:
钱包 → idq19fy6p84fmlzqgghzhrmvgu2fd3yxg3qezrq6zx
      ↑
      无法识别的格式
```

### ✅ 正确方式

**ID地址和MVC地址指向同一个账户！**

```
✓ 正确操作:
1. ID地址转换为MVC地址
   idq19fy6p84fmlzqgghzhrmvgu2fd3yxg3qezrq6zx
   ↓ (转换)
   qq4yngy7480ugppzu2u0d3r3f9kysezyry6865dyrr
   
2. 向MVC地址转账
   钱包 → qq4yngy7480ugppzu2u0d3r3f9kysezyry6865dyrr
          ↑
          标准格式，钱包可识别
```

## 原理说明

### 地址关系

```
┌──────────────────────────────────────┐
│         同一个私钥                    │
└──────────────────┬───────────────────┘
                   │
                   ▼
           ┌──────────────┐
           │   公钥哈希    │
           │   (Hash160)  │
           └──────┬───────┘
                  │
        ┌─────────┴─────────┐
        ▼                   ▼
┌──────────────┐    ┌──────────────┐
│  ID地址      │    │  MVC地址     │
│  (自定义编码) │    │  (标准编码)  │
│  idq1...     │    │  qq4y...     │
└──────────────┘    └──────────────┘
        │                   │
        └─────────┬─────────┘
                  ▼
        指向同一个账户！
```

**关键点：**
- 同一个Hash160可以生成不同格式的地址
- ID地址使用Bech32变体编码
- MVC地址使用Base58编码
- 两个地址控制同一笔资金

## 使用方法

### 方法1：使用prepare命令（推荐）

```bash
./mvckey prepare idq19fy6p84fmlzqgghzhrmvgu2fd3yxg3qezrq6zx
```

**输出：**
```
========================================
转账地址准备
========================================

✓ 检测到ID地址格式
  原始地址: idq19fy6p84fmlzqgghzhrmvgu2fd3yxg3qezrq6zx

✓ 已转换为MVC地址
  转账地址: qq4yngy7480ugppzu2u0d3r3f9kysezyry6865dyrr

========================================
📝 钱包转账步骤
========================================
1. 复制上面的 MVC地址
2. 在MVC钱包中选择'发送'
3. 粘贴MVC地址作为收款地址
4. 输入转账金额
5. 确认并发送
```

### 方法2：手动转换

```bash
# 1. 转换地址
./mvckey convert idq19fy6p84fmlzqgghzhrmvgu2fd3yxg3qezrq6zx

# 输出:
# ID地址:  idq19fy6p84fmlzqgghzhrmvgu2fd3yxg3qezrq6zx
# MVC地址: qq4yngy7480ugppzu2u0d3r3f9kysezyry6865dyrr

# 2. 复制MVC地址用于转账
```

## 转账实操

### 使用MVC钱包

**步骤：**

1. **准备收款地址**
   ```bash
   ./mvckey prepare idq19fy6p84fmlzqgghzhrmvgu2fd3yxg3qezrq6zx
   ```
   记录输出的MVC地址：`qq4yngy7480ugppzu2u0d3r3f9kysezyry6865dyrr`

2. **打开钱包转账**
   - 打开ShowPay或其他MVC钱包
   - 点击"发送"或"转账"
   - 在收款地址栏粘贴：`qq4yngy7480ugppzu2u0d3r3f9kysezyry6865dyrr`
   - 输入金额（如 0.01 MVC）
   - 确认并发送

3. **验证交易**
   - 等待交易确认（约10分钟）
   - 访问 https://www.mvcscan.com/
   - 搜索MVC地址或交易ID查看状态

### 使用RPC接口

```bash
# 准备地址
MVC_ADDR=$(./mvckey convert idq19fy6p84fmlzqgghzhrmvgu2fd3yxg3qezrq6zx | grep "MVC地址:" | awk '{print $2}')

# 发送转账
mvc-cli sendfrom "my_account" "$MVC_ADDR" 0.001

# 查看交易
mvc-cli listtransactions "my_account" 10
```

## 完整示例

### 场景：Alice向Bob的ID地址转账

**Bob的地址信息：**
- ID地址：`idq19fy6p84fmlzqgghzhrmvgu2fd3yxg3qezrq6zx`
- MVC地址：`qq4yngy7480ugppzu2u0d3r3f9kysezyry6865dyrr`

**Alice的操作步骤：**

```bash
# 1. Bob分享他的ID地址给Alice
# ID地址: idq19fy6p84fmlzqgghzhrmvgu2fd3yxg3qezrq6zx

# 2. Alice使用工具转换
./mvckey prepare idq19fy6p84fmlzqgghzhrmvgu2fd3yxg3qezrq6zx

# 3. Alice看到转换后的MVC地址
# 转账地址: qq4yngy7480ugppzu2u0d3r3f9kysezyry6865dyrr

# 4. Alice在钱包中转账到这个MVC地址
# 打开钱包 → 发送 → 粘贴 qq4yngy7480ugppzu2u0d3r3f9kysezyry6865dyrr → 输入金额 → 发送

# 5. Bob可以用任一地址查询余额
./mvckey convert idq19fy6p84fmlzqgghzhrmvgu2fd3yxg3qezrq6zx  # 获取MVC地址
# 然后在钱包或区块浏览器查询
```

## 常见问题

### Q1: 为什么不能直接识别ID地址？

**A:** ID地址是我们自定义的编码格式，用于提供更好的用户体验（如更短的地址、更强的校验等）。但MVC链是Bitcoin的分叉，遵循Bitcoin的地址标准（Base58编码）。标准钱包和节点只能识别Base58格式。

### Q2: 转换会影响安全性吗？

**A:** 不会。两个地址使用相同的公钥哈希（Hash160），指向同一个账户。转换只是改变编码格式，不改变底层数据。

### Q3: 转账到MVC地址后，能用ID地址查询吗？

**A:** 能！因为两个地址指向同一个账户。你可以：
```bash
# 将ID地址转换为MVC地址
./mvckey convert idq19fy6p84fmlzqgghzhrmvgu2fd3yxg3qezrq6zx

# 用转换后的MVC地址在区块浏览器查询
# 访问: https://www.mvcscan.com/address/qq4yngy7480ugppzu2u0d3r3f9kysezyry6865dyrr
```

### Q4: 能让钱包支持ID地址吗？

**A:** 理论上可以，但需要：
1. 修改钱包代码，添加ID地址识别和转换逻辑
2. 或者开发自定义钱包
3. 或者使用应用层中间件自动转换

对于当前使用，最简单的方式是使用转换工具。

### Q5: 如果收款方只给了ID地址怎么办？

**A:** 使用 `mvckey prepare` 命令：
```bash
./mvckey prepare <收到的ID地址>
# 工具会自动转换并显示如何转账
```

## 工具命令速查

```bash
# 生成密钥对（同时生成ID和MVC地址）
./mvckey generate

# 地址转换（双向）
./mvckey convert <address>

# 准备转账（推荐）
./mvckey prepare <to_address>

# 从私钥恢复地址
./mvckey info <privkey_hex>
```

## 总结

| 操作 | 支持情况 | 解决方案 |
|------|---------|---------|
| **直接向ID地址转账** | ❌ 不支持 | 先转换为MVC地址 |
| **向MVC地址转账** | ✅ 支持 | 直接使用 |
| **ID地址转MVC地址** | ✅ 支持 | `mvckey convert` |
| **MVC地址转ID地址** | ✅ 支持 | `mvckey convert` |
| **查询ID地址余额** | ⚠️ 间接支持 | 转换后查询MVC地址 |

**记住：ID地址和MVC地址是同一个账户的不同表示形式！**

## 最佳实践

1. **分享地址时**
   - 同时提供ID地址和MVC地址
   - 或提示对方使用转换工具

2. **接收转账时**
   - 优先给出MVC地址（通用性更好）
   - 或使用 `prepare` 命令准备转账说明

3. **查询余额时**
   - 使用MVC地址在区块浏览器查询
   - 或先用工具转换ID地址

4. **开发应用时**
   - 在应用层自动处理地址转换
   - 对用户隐藏转换细节
   - 提供统一的用户体验
