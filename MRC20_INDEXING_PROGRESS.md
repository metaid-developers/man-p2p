# MRC20 独立索引进度机制

## 问题背景

在实际使用中，可能会出现以下场景：
1. 主索引（MAN）已经索引了大量区块（比如已经索引到区块 850000）
2. 之后才决定启用 MRC20 模块（mrc20Height = 820000）
3. 如果不做特殊处理，会丢失 820000-850000 之间的所有 MRC20 交易

## 解决方案

为 MRC20 实现**独立的索引进度跟踪机制**，与主索引进度分离：

### 1. 独立进度存储

**存储键**: `{chainName}_mrc20_sync_height`

例如：
- `btc_mrc20_sync_height` - BTC 链的 MRC20 索引进度
- `mvc_mrc20_sync_height` - MVC 链的 MRC20 索引进度
- `doge_mrc20_sync_height` - Doge 链的 MRC20 索引进度

### 2. 进度管理 API

#### GetMrc20IndexHeight
```go
func (pd *PebbleData) GetMrc20IndexHeight(chainName string) int64
```
获取指定链的 MRC20 索引进度。

**返回值**:
- 如果从未索引过，返回 `0`
- 否则返回最后处理的区块高度

#### SaveMrc20IndexHeight
```go
func (pd *PebbleData) SaveMrc20IndexHeight(chainName string, height int64) error
```
保存 MRC20 索引进度。

**调用时机**: 
- 每处理完一个区块的 MRC20 数据后调用
- 即使该区块没有 MRC20 交易，也要记录进度

### 3. 自动补索引机制

**函数**: `Mrc20CatchUpRun()`

**执行时机**: 在主索引完成后自动执行

**工作流程**:

```
1. 遍历所有链（btc, mvc, doge）
2. 对于每条链：
   a. 获取主索引进度（mainHeight）
   b. 获取 MRC20 配置的启动高度（mrc20StartHeight）
   c. 获取 MRC20 当前索引进度（mrc20CurrentHeight）
   
   d. 判断是否需要补索引：
      - 如果 mrc20CurrentHeight < mainHeight
      - 且 mainHeight >= mrc20StartHeight
      - 则需要补索引
   
   e. 执行补索引：
      - from = max(mrc20CurrentHeight, mrc20StartHeight - 1)
      - to = mainHeight
      - 从 from+1 到 to 逐个区块处理
   
   f. 显示进度条和日志
```

**日志示例**:
```
MRC20 catch-up for chain: btc, from: 820000, to: 850000
MRC20 catch-up progress for btc: 820100/850000
MRC20 catch-up progress for btc: 820200/850000
...
MRC20 catch-up completed for chain: btc
```

## 使用场景

### 场景 1: 新项目启用 MRC20

**配置**:
```toml
module = ["mrc20"]

[btc]
initialHeight = 800000
mrc20Height = 820000
```

**执行流程**:
1. 主索引从 800000 开始
2. 到达 820000 后，主索引开始处理 MRC20
3. MRC20 索引进度随主索引同步增长
4. 不需要补索引

### 场景 2: 已有项目后期启用 MRC20

**初始状态**:
- 主索引已到 850000
- MRC20 未启用

**启用 MRC20**:
```toml
module = ["mrc20"]  # 新增

[btc]
mrc20Height = 820000  # 配置启动高度
```

**执行流程**:
1. 主程序启动后
2. 主索引运行 `IndexerRun()` - 可能很快完成（已是最新）
3. **自动执行 `Mrc20CatchUpRun()`**
4. 检测到 MRC20 进度（0）落后于主索引（850000）
5. 自动从 820000 补索引到 850000
6. 完成后，后续新区块 MRC20 和主索引同步处理

**补索引日志**:
```
MRC20 catch-up for chain: btc, from: 820000, to: 850000
[MRC20 btc 820000-850000] 100% |████████████████| (30000/30000)
MRC20 catch-up completed for chain: btc
```

### 场景 3: 多链不同启动高度

**配置**:
```toml
module = ["mrc20"]

[btc]
initialHeight = 800000
mrc20Height = 820000

[mvc]
initialHeight = 86500
mrc20Height = 100000

[doge]
initialHeight = 5000000
mrc20Height = 5050000
```

**执行流程**:
- 每条链独立跟踪进度
- 各自补索引互不影响
- BTC 可能在补索引，MVC 已同步，Doge 还未到启动高度

## 进度查询

### 查看主索引进度
```bash
# BTC
echo "get btc_sync_height" | redis-cli
# 或通过 PebbleDB 查询
# Key: btc_sync_height
```

### 查看 MRC20 索引进度
```bash
# BTC MRC20
# Key: btc_mrc20_sync_height
```

### 判断是否需要补索引
```go
mainHeight := PebbleStore.GetSyncHeight("btc")        // 主索引进度
mrc20Height := PebbleStore.GetMrc20IndexHeight("btc") // MRC20 进度
mrc20Start := common.Config.Btc.Mrc20Height          // 配置的启动高度

if mrc20Height < mainHeight && mainHeight >= mrc20Start {
    fmt.Printf("需要补索引 %d 个区块\n", mainHeight - mrc20Height)
}
```

## 性能考虑

### 补索引性能
- **读取速度**: 从已索引的 PIN 数据读取，无需重新解析区块
- **处理速度**: 只处理 MRC20 相关 PIN（路径匹配 `/ft/mrc20/`）
- **Native Transfer**: 自动检测并处理原生比特币交易中的 MRC20 转账
- **预估时间**: 
  - 假设 10000 个区块
  - 每个区块平均 5 个 MRC20 交易
  - 处理速度约 100 区块/秒
  - 总耗时约 100 秒

### Native Transfer 处理

**重要**: MRC20 补索引会完整处理 Native Transfer（原生转账）

即使某个区块没有任何 MRC20 PIN（`/ft/mrc20/deploy|mint|transfer`），但如果该区块的交易输入花费了之前创建的 MRC20 UTXO，系统会：
1. 检查 `txInList`（该区块所有交易的输入列表）
2. 查询这些输入是否对应 MRC20 UTXO
3. 如果是，自动执行 Native Transfer
4. 更新 UTXO 状态和余额

这确保了补索引和实时索引的行为完全一致。

### 优化建议
1. **批量处理**: 当前逐块处理，可考虑批量读取
2. **并行处理**: 多链可并行补索引
3. **断点续传**: 支持中断后继续，不会重复处理

## 监控和日志

### 关键日志

#### 正常同步
```
2026-01-14 10:00:00 [INFO] MRC20 for chain btc is up to date: 850000
```

#### 补索引开始
```
2026-01-14 10:00:00 [INFO] MRC20 catch-up for chain: btc, from: 820000, to: 850000
```

#### 补索引进度（每 100 个区块）
```
2026-01-14 10:00:10 [INFO] MRC20 catch-up progress for btc: 820100/850000
2026-01-14 10:00:20 [INFO] MRC20 catch-up progress for btc: 820200/850000
```

#### 补索引完成
```
2026-01-14 10:05:00 [INFO] MRC20 catch-up completed for chain: btc
```

### 监控指标

**建议监控**:
- `mrc20_sync_height` - MRC20 索引高度
- `main_sync_height` - 主索引高度
- `mrc20_lag` - MRC20 落后主索引的区块数
- `mrc20_catchup_duration` - 补索引耗时

## 故障恢复

### 补索引中断

**场景**: 补索引进行到一半，程序崩溃

**恢复机制**:
1. 重启程序
2. 再次执行 `Mrc20CatchUpRun()`
3. 从上次记录的 `mrc20_sync_height` 继续
4. 不会重复处理已完成的区块

**示例**:
```
首次运行:
- 补索引 820000 → 825000（处理了 5000 个区块）
- 程序崩溃

重启后:
- 读取 mrc20_sync_height = 825000
- 继续补索引 825001 → 850000
- 无重复处理
```

### 数据不一致检测

**建议**:
定期比对主索引高度和 MRC20 高度：
```bash
#!/bin/bash
MAIN_HEIGHT=$(get_sync_height btc)
MRC20_HEIGHT=$(get_mrc20_sync_height btc)
LAG=$((MAIN_HEIGHT - MRC20_HEIGHT))

if [ $LAG -gt 1000 ]; then
    echo "WARNING: MRC20 lagging by $LAG blocks"
fi
```

## 配置参考

### 完整配置示例
```toml
module = ["metaname", "mrc721", "mrc20", "metaso_notifcation"]

[btc]
initialHeight = 800000
mrc20Height = 820000

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

### 配置说明

- **module**: 模块列表，必须包含 `"mrc20"` 才会启用
- **mrc20Height**: MRC20 开始处理的区块高度
  - 设置为 0 或不设置：不处理该链的 MRC20
  - 设置为具体高度：从该高度开始处理

## 注意事项

1. **首次启用**: 如果主索引已远超 mrc20Height，首次启动会执行较长时间的补索引
2. **存储空间**: MRC20 数据会占用额外的 PebbleDB 存储空间
3. **性能影响**: 补索引期间会占用一定 CPU 和 I/O 资源
4. **数据完整性**: 补索引依赖主索引的 PIN 数据完整性
5. **并发安全**: 同一时刻只有一个补索引任务在运行

## 总结

通过独立的索引进度机制，MRC20 模块实现了：
- ✅ **灵活启用**: 可在任意时刻启用 MRC20，不丢失历史数据
- ✅ **自动补索引**: 无需手动操作，自动处理历史区块
- ✅ **进度独立**: 与主索引进度分离，互不影响
- ✅ **断点续传**: 支持中断恢复，不重复处理
- ✅ **多链支持**: 每条链独立跟踪进度

这使得 MRC20 模块可以作为一个真正的"可选模块"，随时启用、随时停用，而不会影响数据完整性。
