# MRC20 Implementation Summary

## 概述

成功在当前项目（man-indexer-v2）中集成了完整的 MRC20 功能。MRC20 是一个基于比特币的代币协议，类似于 BRC20，但具有更强大的功能，特别是 Shovel（铲子）机制。

## 实现内容

### 1. 核心数据结构 (mrc20/)

**文件**: `mrc20/mrc20.go`, `mrc20/util.go`

- **Mrc20Utxo**: UTXO 数据结构，包含代币余额、状态等信息
- **Mrc20DeployInfo**: 代币部署信息，包含 tick、总量、mint 规则等
- **Mrc20Shovel**: 铲子机制，防止 PIN 重复使用
- **PathParse**: 路径解析工具，支持复杂的路径匹配规则

### 2. 验证器 (man/mrc20_validator.go)

实现了三大核心操作的验证逻辑：

#### Deploy 验证
- Tick 名称唯一性检查
- 参数范围验证（长度、数量、精度等）
- 预挖矿地址和数量验证
- PayCheck 支付验证

#### Mint 验证  
- 代币存在性检查
- 跨链限制
- 铸造上限检查
- 区块高度范围验证
- **Shovel 机制验证**：
  - PoP 难度检查 (lvCheck)
  - 创建者检查 (creatorCheck)
  - 路径匹配检查 (pathCheck) - 支持通配符、follow、字段匹配等
  - 数量检查 (countCheck)
- PayCheck 支付验证
- Vout 输出验证

#### Transfer 验证
- JSON 格式检查
- 金额和精度验证
- 余额充足性检查
- 输出有效性验证
- 自动找零计算

### 3. 主处理逻辑 (man/mrc20.go)

**核心流程**:
```
1. 识别 MRC20 相关的 PIN (/ft/mrc20/deploy|mint|transfer)
2. 分类处理：
   - Deploy: 优先处理，创建新代币
   - Mint: 验证并铸造代币
   - Transfer: 显式转账（data-transfer）
3. Native Transfer: 自动检测隐式转账（直接花费 UTXO）
4. 更新统计: 持有者数量、交易数量
```

**关键函数**:
- `Mrc20Handle()`: 主入口
- `deployHandle()`: 处理部署
- `CreateMrc20MintPin()`: 创建铸造 UTXO
- `CreateMrc20TransferUtxo()`: 创建转账 UTXO
- `sendAllAmountToFirstOutput()`: 错误处理，将余额发送到第一个输出

### 4. PebbleDB 存储适配 (man/mrc20_pebble.go)

**数据库键设计**:
```
mrc20_utxo_{txPoint}                        - UTXO 数据
mrc20_tick_{mrc20Id}                        - 代币信息
mrc20_tick_name_{tickName}                  - Tick 名称索引
mrc20_addr_{address}_{mrc20Id}_{txPoint}    - 地址余额索引
mrc20_shovel_{mrc20Id}_{pinId}              - 已使用的铲子
mrc20_op_tx_{txId}                          - 操作交易跟踪
```

**核心方法**:
- `SaveMrc20Pin()`: 保存 UTXO
- `SaveMrc20Tick()`: 保存代币信息
- `GetMrc20TickInfo()`: 查询代币信息
- `GetMrc20UtxoByOutPutList()`: 根据输出列表查询 UTXO
- `GetMrc20ByAddressAndTick()`: 查询地址余额
- `AddMrc20Shovel()`: 添加已使用的铲子
- `GetMrc20Shovel()`: 查询已使用的铲子
- `CheckOperationtx()`: 检查交易是否已处理
- `GetPinListByOutPutList()`: 查询 PIN 列表

### 5. 主流程集成 (man/indexer_pebble.go)

**集成点**:
```go
func (pd *PebbleData) DoIndexerRun(chainName string, height int64, reIndex bool) (err error) {
    // ... 现有处理逻辑 ...
    
    // 处理 MRC20（根据配置启用）
    if isModuleEnabled("mrc20") {
        pd.handleMrc20(chainName, height, pinList, txInList)
    }
    
    return
}
```

**模块检查函数**:
```go
func isModuleEnabled(moduleName string) bool {
    for _, m := range common.Config.Module {
        if m == moduleName {
            return true
        }
    }
    return false
}
```

**MRC20 处理函数**:
```go
func (pd *PebbleData) handleMrc20(chainName string, height int64, pinList *[]*pin.PinInscription, txInList *[]string) {
    // 1. 检查 MRC20 启动高度
    // 2. 筛选 MRC20 相关 PIN
    // 3. 调用 Mrc20Handle 处理
}
```

### 6. 配置支持

**配置文件示例** (config_regtest.toml):
```toml
module = ["metaname", "mrc721", "mrc20", "metaso_notifcation"]

[btc]
initialHeight = 800000
mrc20Height = 820000  # MRC20 开始处理的区块高度

[mvc]
initialHeight = 86500
mrc20Height = 100000

[doge]
initialHeight = 5000000
mrc20Height = 5050000

[pebble]
dir = "./man_base_data_pebble"
num = 16
```

## 关键特性

### 1. Shovel（铲子）机制
防止 PIN 重复使用来铸造代币，支持多种验证方式：
- **PoP 难度**: 要求特定难度的 PoP（前导零数量）
- **创建者**: 只允许特定创建者的 PIN
- **路径匹配**: 支持通配符、follow、JSON 字段匹配等
- **数量限制**: 要求特定数量的有效 PIN

### 2. Native Transfer 自动检测
自动检测原生比特币交易中的 MRC20 转账（直接花费包含 MRC20 的 UTXO），无需显式 transfer PIN。

### 3. UTXO 模型
- 每个 MRC20 操作创建 UTXO
- Status: 1(可用), -1(已花费)
- 支持内存池和链上状态分离
- 转账时输入 UTXO 标记为已花费，创建新的输出 UTXO

### 4. 模块化设计
- 可通过配置文件启用/禁用
- 与现有系统松耦合
- 使用独立的 MrcDb 存储

## 技术亮点

1. **完全兼容现有架构**: 使用 PebbleDB 存储，与现有 PIN 系统无缝集成
2. **高效索引**: 多维度索引设计（地址、代币、UTXO）
3. **批量操作**: 使用 batch 操作提高存储效率
4. **错误处理**: 完善的错误处理和回滚机制
5. **并发安全**: 支持并发操作
6. **灵活验证**: 支持复杂的 Shovel 验证规则

## 代码统计

- **新增文件**: 4 个
  - `mrc20/mrc20.go` (147 行)
  - `mrc20/util.go` (25 行)
  - `man/mrc20.go` (469 行)
  - `man/mrc20_validator.go` (660 行)
  - `man/mrc20_pebble.go` (503 行)

- **修改文件**: 2 个
  - `man/indexer_pebble.go` (添加 MRC20 集成逻辑)
  - `common/config.go` (已有 mrc20Height 配置)

- **总代码量**: ~1800+ 行

## 使用示例

### 启用 MRC20
在配置文件中添加：
```toml
module = ["mrc20"]

[btc]
mrc20Height = 820000
```

### Deploy 代币
```json
{
  "tick": "TEST",
  "tokenName": "Test Token",
  "decimals": "8",
  "amtPerMint": "1000",
  "mintCount": "21000000",
  "pinCheck": {
    "lvl": "4",
    "count": "3"
  }
}
```

### Mint 代币
```json
{
  "id": "mrc20_deploy_pin_id",
  "vout": "0"
}
```

### Transfer 代币
```json
[
  {
    "id": "mrc20_id",
    "amount": "1000.5",
    "vout": 1
  }
]
```

## 测试建议

1. **功能测试**:
   - Deploy: 创建不同参数的代币
   - Mint: 测试各种 Shovel 验证规则
   - Transfer: 测试单输出、多输出、找零等场景

2. **边界测试**:
   - 铸造上限
   - 余额不足
   - 精度超限
   - 跨链限制

3. **性能测试**:
   - 大量 UTXO 查询
   - 批量铸造
   - 并发转账

## 后续优化建议

1. **性能优化**:
   - 添加缓存层（代币信息、余额等）
   - 优化大额转账的 UTXO 选择算法
   - 批量处理优化

2. **功能增强**:
   - 添加 API 接口（查询余额、历史等）
   - 支持更多 Shovel 验证规则
   - 添加统计分析功能

3. **监控告警**:
   - 添加关键指标监控
   - 异常情况告警
   - 性能指标跟踪

## 文档

- [MRC20_CONFIG.md](MRC20_CONFIG.md) - 详细配置指南
- [GLOBALMETAID.md](GLOBALMETAID.md) - 全局 MetaID 文档（已存在）

## 总结

MRC20 模块已完全集成到项目中，支持 Deploy、Mint、Transfer 三大核心功能，以及 Shovel 机制和 Native Transfer 检测。代码已编译通过，可以通过配置文件灵活启用/禁用。整个实现遵循项目现有架构，与 PebbleDB 存储系统无缝集成。
