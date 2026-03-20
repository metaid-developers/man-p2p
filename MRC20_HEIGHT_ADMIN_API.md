# MRC20 索引高度管理接口

新增了两个管理接口来动态管理 MRC20 索引高度，可以用于回滚重新索引等操作。

## 接口说明

### 1. 获取索引高度
**GET** `/api/mrc20/admin/index-height/{chain}`

获取指定链的当前 MRC20 索引高度信息。

#### 参数
- `chain`: 链名称，支持 `btc`, `bitcoin`, `doge`, `dogecoin`, `mvc`

#### 响应示例
```json
{
    "code": 1,
    "message": "ok",
    "data": {
        "chain": "doge",
        "currentHeight": 6049500,
        "configHeight": 6049343
    }
}
```

#### 使用示例
```bash
curl http://localhost:7777/api/mrc20/admin/index-height/doge
```

---

### 2. 设置索引高度  
**POST** `/api/mrc20/admin/index-height/{chain}`

设置指定链的 MRC20 索引高度（用于回滚重新索引）。

#### 参数
- `chain`: 链名称，支持 `btc`, `bitcoin`, `doge`, `dogecoin`, `mvc`

#### 请求体
```json
{
    "height": 6049400,
    "token": "your_admin_token",
    "reason": "回滚重新索引包含hide操作的MRC20交易"
}
```

#### 字段说明
- `height` (必填): 要设置的新高度
- `token` (可选): 管理员令牌，如果配置了 `adminToken` 则必填
- `reason` (可选): 操作原因，会记录在日志中

#### 响应示例
```json
{
    "code": 1,
    "message": "ok", 
    "data": {
        "chain": "doge",
        "oldHeight": 6049500,
        "newHeight": 6049400,
        "reason": "回滚重新索引包含hide操作的MRC20交易",
        "message": "MRC20 index height updated successfully"
    }
}
```

#### 使用示例
```bash
curl -X POST http://localhost:7777/api/mrc20/admin/index-height/doge \
  -H "Content-Type: application/json" \
  -d '{
    "height": 6049400,
    "token": "your_admin_token", 
    "reason": "回滚重新索引包含hide操作的MRC20交易"
  }'
```

---

## 安全说明

1. **Token 验证**: 如果配置文件中设置了 `adminToken`，则必须在请求中提供正确的 token
2. **操作日志**: 所有高度修改操作都会记录在服务器日志中
3. **参数验证**: 
   - 高度不能为负数
   - 链名称必须是支持的类型
4. **影响范围**: 修改高度后，索引器会从新的高度开始重新索引

---

## 使用场景

### 场景1: 回滚重新索引特定交易
```bash
# 1. 先查询当前高度
curl http://localhost:7777/api/mrc20/admin/index-height/doge

# 2. 设置回滚到指定高度（比问题交易所在区块高度稍低）
curl -X POST http://localhost:7777/api/mrc20/admin/index-height/doge \
  -H "Content-Type: application/json" \
  -d '{
    "height": 6049400,
    "reason": "回滚重新索引交易94809d6598eae303898bb2b342fa61b6026a0717e285d7970b5ff5ee4ea1b9a9"
  }'

# 3. 重启索引器服务，它会从新高度开始重新索引
```

### 场景2: 修复索引进度异常
```bash
# 如果发现索引进度异常，可以手动调整到正确的高度
curl -X POST http://localhost:7777/api/mrc20/admin/index-height/btc \
  -H "Content-Type: application/json" \
  -d '{
    "height": 855000,
    "reason": "修复索引进度异常"
  }'
```

---

## 注意事项

1. **数据一致性**: 回滚高度时，已索引的高度范围内的数据不会自动删除，需要配合数据清理工具使用
2. **服务重启**: 修改高度后建议重启索引器服务以确保生效
3. **备份建议**: 重要操作前建议备份数据库
4. **监控告警**: 建议对高度回滚操作设置监控告警