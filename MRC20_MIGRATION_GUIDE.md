# MRC20 数据迁移工具

## 功能

从 MongoDB 导出 MRC20 数据，导入到 PebbleDB，并从指定区块高度继续索引。

## 使用场景

1. **从旧系统迁移**：已有 MongoDB 中的 MRC20 数据，需要迁移到 PebbleDB
2. **指定起点索引**：从特定区块高度开始索引新的 MRC20 交易
3. **数据备份恢复**：从备份的 MongoDB 数据恢复

## 前提条件

1. MongoDB 中存在以下集合：
   - `mrc20_tick` - 代币信息
   - `mrc20_utxo` - UTXO 余额
   - `mrc20_shovel` - 已使用的铲子
   - `mrc20_operation_tx` - 操作交易记录

2. 每个文档包含 `chain` 字段（btc/mvc/doge）

## 编译

```bash
cd /srv/dev_project/metaid/man-indexer-v2
go build -o mrc20_migration tools/mrc20_migration.go
```

## 使用步骤

### 1. 干运行（查看统计信息）

先执行干运行，查看将要迁移的数据量：

```bash
./mrc20_migration \
  --mongo "mongodb://localhost:27017" \
  --db "metaso" \
  --pebble "./man_base_data_pebble" \
  --chain "btc" \
  --height 850000 \
  --dry-run
```

**输出示例**：
```
=== MRC20 Data Migration Tool ===
MongoDB: mongodb://localhost:27017/metaso
PebbleDB: ./man_base_data_pebble
Chain: btc
Continue from height: 850000
Dry run: true
=================================

✓ Connected to MongoDB
✓ Opened PebbleDB

[1/5] Migrating MRC20 Tick (token info)...
✓ Migrated 125 ticks

[2/5] Migrating MRC20 UTXO (balances)...
✓ Migrated 45623 UTXOs (8912 unique addresses)

[3/5] Migrating MRC20 Shovel (used PINs)...
✓ Migrated 12456 shovels

[4/5] Migrating MRC20 Operation TX...
✓ Migrated 8934 operation records

[5/5] Setting MRC20 index height...

=== Migration Statistics ===
Ticks:      125
UTXOs:      45623
Addresses:  8912
Shovels:    12456
Operations: 8934
Duration:   5.234s
============================

✓ Dry run completed. No data was written.
  Run without --dry-run to perform actual migration.
```

### 2. 执行迁移

确认统计信息无误后，执行实际迁移：

```bash
./mrc20_migration \
  --mongo "mongodb://localhost:27017" \
  --db "metaso" \
  --pebble "./man_base_data_pebble" \
  --chain "btc" \
  --height 850000
```

### 3. 验证迁移结果

迁移完成后，可以验证数据：

```bash
# 查看 MRC20 索引高度
echo "get btc_mrc20_sync_height" | pebble_dump_tool --db ./man_base_data_pebble/meta

# 或者启动索引器查看日志
./manindexer --config config_btc.toml
```

### 4. 启动索引

迁移完成后，启动主索引器：

```bash
./manindexer --config config.toml
```

程序会：
1. 检测到 MRC20 进度为 850000
2. 主索引如果已到 850100，会自动补索引 850000→850100
3. 然后主索引和 MRC20 同步进行

## 命令行参数

| 参数 | 说明 | 默认值 | 必填 |
|------|------|--------|------|
| `--mongo` | MongoDB 连接 URI | mongodb://localhost:27017 | 否 |
| `--db` | MongoDB 数据库名 | metaso | 否 |
| `--pebble` | PebbleDB 目录 | ./man_base_data_pebble | 否 |
| `--chain` | 链名称 (btc/mvc/doge) | btc | 否 |
| `--height` | 继续索引的起始高度 | 0 | **是** |
| `--dry-run` | 干运行模式（不写入数据） | false | 否 |
| `--batch` | 批量写入大小 | 1000 | 否 |

## 迁移的数据结构

### 1. MRC20 Tick (代币信息)

**MongoDB 集合**: `mrc20_tick`

**PebbleDB 键**:
- `mrc20_tick_{mrc20Id}` → 完整的 tick 数据
- `mrc20_tick_name_{tickName}` → mrc20Id（名称索引）

**字段**:
```json
{
  "_id": "mrc20_id",
  "tick": "TEST",
  "token_name": "Test Token",
  "decimals": 8,
  "amt_per_mint": "1000",
  "mint_count": "21000000",
  "total_minted": "5000000",
  "holders": 123,
  "tx_count": 456,
  "chain": "btc",
  "deploy_height": 820000
}
```

### 2. MRC20 UTXO (余额)

**MongoDB 集合**: `mrc20_utxo`

**PebbleDB 键**:
- `mrc20_utxo_{txPoint}` → UTXO 数据
- `mrc20_addr_{address}_{mrc20Id}_{txPoint}` → 地址索引

**字段**:
```json
{
  "tx_point": "txid:vout",
  "mrc20_id": "...",
  "to_address": "bc1q...",
  "amt_change": "1000.00000000",
  "status": 1,
  "mrc_option": "mint",
  "block_height": 820100,
  "timestamp": 1234567890,
  "chain": "btc"
}
```

### 3. MRC20 Shovel (已使用的铲子)

**MongoDB 集合**: `mrc20_shovel`

**PebbleDB 键**:
- `mrc20_shovel_{mrc20Id}_{pinId}` → "1"

**字段**:
```json
{
  "mrc20_id": "...",
  "pin_id": "...",
  "chain": "btc"
}
```

### 4. MRC20 Operation TX

**MongoDB 集合**: `mrc20_operation_tx`

**PebbleDB 键**:
- `mrc20_op_tx_{txId}` → "1"

**字段**:
```json
{
  "tx_id": "...",
  "chain": "btc"
}
```

## 注意事项

### 1. 数据完整性

- 迁移前确保 MongoDB 数据完整
- 建议先在测试环境验证
- 迁移时停止主索引器，避免并发写入

### 2. 起始高度选择

`--height` 参数应该设置为：
- **MongoDB 数据的最后区块高度 + 1**
- 例如 MongoDB 数据到 850000，设置 `--height 850000`
- 这样新索引从 850000 开始，不会重复或遗漏

### 3. 性能优化

- `--batch` 参数控制批量大小（默认 1000）
- 数据量大时可以调整：`--batch 5000`
- 预计速度：10000 UTXO/秒

### 4. 磁盘空间

确保有足够的磁盘空间：
- 假设 50000 个 UTXO，每个约 500 字节
- 需要约 25MB 空间（加上索引约 50MB）

### 5. MongoDB 字段映射

如果 MongoDB 字段名与代码不一致，需要修改 `convertTickData` 和 `convertUtxoData` 函数。

## 故障恢复

### 迁移中断

如果迁移过程中断：
1. 删除 PebbleDB 中的部分数据：`rm -rf ./man_base_data_pebble/mrc`
2. 重新执行迁移命令
3. PebbleDB 会覆盖已存在的键

### 验证数据

```bash
# 统计 UTXO 数量
pebble_tool --db ./man_base_data_pebble/mrc --command scan --start "mrc20_utxo_" --end "mrc20_utxo_~" | wc -l

# 查看某个代币信息
pebble_tool --db ./man_base_data_pebble/mrc --command get --key "mrc20_tick_name_TEST"
```

### 回滚

如果迁移失败，可以：
1. 删除 PebbleDB 数据：`rm -rf ./man_base_data_pebble/mrc`
2. 删除进度标记：删除 `btc_mrc20_sync_height` 键
3. 从零开始索引（或重新迁移）

## 完整示例

### 场景：从 MongoDB 迁移 BTC 链的 MRC20 数据

```bash
# 1. 查看 MongoDB 数据
mongo metaso --eval "db.mrc20_tick.count({chain: 'btc'})"
mongo metaso --eval "db.mrc20_utxo.count({chain: 'btc'})"

# 2. 确定最后的区块高度（假设 850000）
mongo metaso --eval "db.mrc20_utxo.find({chain: 'btc'}).sort({block_height: -1}).limit(1).pretty()"

# 3. 停止索引器
killall manindexer

# 4. 干运行验证
./mrc20_migration --chain btc --height 850000 --dry-run

# 5. 执行迁移
./mrc20_migration --chain btc --height 850000

# 6. 验证迁移结果
# （查看日志输出的统计信息）

# 7. 启动索引器
./manindexer --config config.toml
```

输出日志示例：
```
2026-01-14 10:00:00 [INFO] ManIndex started
2026-01-14 10:00:01 [INFO] IndexerRun for chain: btc, from: 850100, to: 850200
2026-01-14 10:00:02 [INFO] MRC20 catch-up for chain: btc, from: 850000, to: 850100
[MRC20 btc 850000-850100] 100% |████████████████| (100/100)
2026-01-14 10:00:12 [INFO] MRC20 catch-up completed for chain: btc
2026-01-14 10:00:12 [INFO] MRC20 for chain btc is up to date: 850100
```

## 多链迁移

如果要迁移多条链：

```bash
# BTC
./mrc20_migration --chain btc --height 850000

# MVC
./mrc20_migration --chain mvc --height 100000

# Doge
./mrc20_migration --chain doge --height 5060000
```

每条链的数据独立存储，互不影响。

## 技术细节

### 批量写入

工具使用 PebbleDB 的 `Batch` API：
- 每处理 N 条数据（默认 1000）提交一次
- 减少磁盘 I/O，提高性能
- 保证原子性（批次内全部成功或全部失败）

### 数据转换

MongoDB → PebbleDB 的转换流程：
1. 从 MongoDB 读取 BSON 文档
2. 转换为 Go struct（mrc20.Mrc20Utxo 等）
3. 序列化为 JSON（使用 sonic）
4. 写入 PebbleDB

### 索引构建

迁移工具自动构建所有必需的索引：
- Tick name index（按名称查询代币）
- Address index（查询地址余额）
- TxPoint index（查询 UTXO）

## 支持

如有问题，请检查：
1. MongoDB 连接是否正常
2. PebbleDB 目录权限是否正确
3. 磁盘空间是否充足
4. 日志中的错误信息

或者查看源码：`tools/mrc20_migration.go`
