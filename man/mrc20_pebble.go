package man

import (
	"fmt"
	"log"
	"man-p2p/mrc20"
	"man-p2p/pin"
	"sort"
	"strings"

	"github.com/bytedance/sonic"
	"github.com/cockroachdb/pebble"
	"github.com/shopspring/decimal"
)

// 测试用 mock 函数，用于替代真实的链上验证
// 正常运行时为 nil，测试时可设置为 mock 函数
var (
	// MockGetTransactionWithCache 用于测试时 mock GetTransactionWithCache 函数
	MockGetTransactionWithCache func(chainName string, txid string) (interface{}, error)
)

// SaveMrc20Pin 保存 MRC20 PIN 数据
// 根据新架构设计：
// - UTXO 表只保留 status=0 (Available) 和 status=1/2 (Pending) 的记录
// - status=-1 (Spent) 的 UTXO 应该通过 DeleteMrc20Utxo 删除，不应该通过此方法保存
// 索引：
// - mrc20_utxo_{txPoint}: UTXO 主记录
// - mrc20_in_{ToAddress}_{mrc20Id}_{txPoint}: 地址收入索引
// - available_utxo_{chain}_{address}_{tickId}_{txPoint}: 可用 UTXO 专用索引 (只存储 status=0)
func (pd *PebbleData) SaveMrc20Pin(utxoList []mrc20.Mrc20Utxo) error {
	batch := pd.Database.MrcDb.NewBatch()
	defer batch.Close()

	for _, utxo := range utxoList {
		// 跳过 status=-1 的记录，Spent UTXO 不应该通过此方法保存
		if utxo.Status == mrc20.UtxoStatusSpent {
			log.Printf("[WARN] SaveMrc20Pin: skipping spent UTXO %s, use DeleteMrc20Utxo instead", utxo.TxPoint)
			continue
		}

		// 检查是否已存在该 UTXO
		// 如果已存在且是 TeleportPending 状态，则跳过（保护跃迁状态不被覆盖）
		key := fmt.Sprintf("mrc20_utxo_%s", utxo.TxPoint)
		existingData, closer, err := pd.Database.MrcDb.Get([]byte(key))
		if err == nil && existingData != nil {
			var existingUtxo mrc20.Mrc20Utxo
			if unmarshalErr := sonic.Unmarshal(existingData, &existingUtxo); unmarshalErr == nil {
				if existingUtxo.Status == mrc20.UtxoStatusTeleportPending {
					// 已有 UTXO 是 TeleportPending 状态，不能覆盖
					// 只允许更新 BlockHeight（从 mempool -1 到实际区块高度）
					closer.Close()
					if utxo.BlockHeight > existingUtxo.BlockHeight {
						log.Printf("[INFO] SaveMrc20Pin: UTXO %s is TeleportPending, only updating BlockHeight from %d to %d",
							utxo.TxPoint, existingUtxo.BlockHeight, utxo.BlockHeight)
						existingUtxo.BlockHeight = utxo.BlockHeight
						data, _ := sonic.Marshal(existingUtxo)
						batch.Set([]byte(key), data, pebble.Sync)
						// 同时更新索引
						if existingUtxo.ToAddress != "" {
							inKey := fmt.Sprintf("mrc20_in_%s_%s_%s", existingUtxo.ToAddress, existingUtxo.Mrc20Id, existingUtxo.TxPoint)
							batch.Set([]byte(inKey), data, pebble.Sync)
						}
					} else {
						log.Printf("[INFO] SaveMrc20Pin: skipping UTXO %s, already TeleportPending with BlockHeight=%d",
							utxo.TxPoint, existingUtxo.BlockHeight)
					}
					continue
				}
			}
			closer.Close()
		} else if err != nil && err != pebble.ErrNotFound {
			log.Printf("[WARN] SaveMrc20Pin: error checking existing UTXO %s: %v", utxo.TxPoint, err)
		}

		// 保存 UTXO 数据
		// Key: mrc20_utxo_{txPoint}
		data, err := sonic.Marshal(utxo)
		if err != nil {
			log.Println("Marshal mrc20 utxo error:", err)
			continue
		}

		err = batch.Set([]byte(key), data, pebble.Sync)
		if err != nil {
			log.Println("Set mrc20 utxo error:", err)
		}

		// 收入索引：所有记录都写入 mrc20_in_{ToAddress}
		// 这个索引用于计算余额（只需扫描 mrc20_in_ 前缀，Status=0 的记录）
		if utxo.ToAddress != "" {
			inKey := fmt.Sprintf("mrc20_in_%s_%s_%s", utxo.ToAddress, utxo.Mrc20Id, utxo.TxPoint)
			err = batch.Set([]byte(inKey), data, pebble.Sync)
			if err != nil {
				log.Println("Set mrc20 income index error:", err)
			}
		}

		// 发送方索引：当 FromAddress 存在时，为发送方创建索引
		// - 如果 FromAddress != ToAddress：总是创建（这是标准转账）
		// - 如果 FromAddress == ToAddress 且 Status 是 Pending：也要创建（这是自转账的待转出状态）
		// 这用于查找该地址待转出的 UTXO，特别是 Status=1,2（Pending）的转账
		if utxo.FromAddress != "" {
			// 标准转账或自转账的 Pending 状态，都需要为发送方创建索引
			shouldCreateOutIndex := (utxo.FromAddress != utxo.ToAddress) ||
				(utxo.FromAddress == utxo.ToAddress && (utxo.Status == mrc20.UtxoStatusTeleportPending || utxo.Status == mrc20.UtxoStatusTransferPending))

			if shouldCreateOutIndex {
				outKey := fmt.Sprintf("mrc20_in_%s_%s_%s", utxo.FromAddress, utxo.Mrc20Id, utxo.TxPoint)
				err = batch.Set([]byte(outKey), data, pebble.Sync)
				if err != nil {
					log.Println("Set mrc20 output index error:", err)
				}
			}
		}

		// 可用 UTXO 专用索引：只有 status=0 的 UTXO 写入此索引
		// 用于快速查询某地址某 tick 的可用 UTXO
		if utxo.Status == mrc20.UtxoStatusAvailable && utxo.ToAddress != "" {
			availableKey := fmt.Sprintf("available_utxo_%s_%s_%s_%s", utxo.Chain, utxo.ToAddress, utxo.Mrc20Id, utxo.TxPoint)
			err = batch.Set([]byte(availableKey), data, pebble.Sync)
			if err != nil {
				log.Println("Set available utxo index error:", err)
			}
		}

		// 区块创建索引：记录该区块创建了哪些 UTXO（用于回滚/重跑）
		// 只在出块后（BlockHeight > 0）时记录，mempool 阶段（BlockHeight = -1）不记录
		if utxo.BlockHeight > 0 && utxo.Chain != "" {
			blockCreatedKey := fmt.Sprintf("block_created_%s_%d_%s", utxo.Chain, utxo.BlockHeight, utxo.TxPoint)
			err = batch.Set([]byte(blockCreatedKey), []byte("1"), pebble.Sync)
			if err != nil {
				log.Println("Set block_created index error:", err)
			}
		}
	}

	return batch.Commit(pebble.Sync)
}

// SaveMrc20Tick 保存 MRC20 代币信息
func (pd *PebbleData) SaveMrc20Tick(tickList []mrc20.Mrc20DeployInfo) error {
	batch := pd.Database.MrcDb.NewBatch()
	defer batch.Close()

	for _, tick := range tickList {
		data, err := sonic.Marshal(tick)
		if err != nil {
			log.Println("Marshal mrc20 tick error:", err)
			continue
		}

		// Key: mrc20_tick_{mrc20Id}
		key := fmt.Sprintf("mrc20_tick_%s", tick.Mrc20Id)
		err = batch.Set([]byte(key), data, pebble.Sync)
		if err != nil {
			log.Println("Set mrc20 tick error:", err)
		}

		// 为 tick 名称建立索引
		// Key: mrc20_tick_name_{tickName}
		tickNameKey := fmt.Sprintf("mrc20_tick_name_%s", tick.Tick)
		err = batch.Set([]byte(tickNameKey), []byte(tick.Mrc20Id), pebble.Sync)
		if err != nil {
			log.Println("Set mrc20 tick name index error:", err)
		}
	}

	return batch.Commit(pebble.Sync)
}

// GetMrc20TickList 获取 MRC20 代币列表（分页）
func (pd *PebbleData) GetMrc20TickList(cursor, limit int) ([]mrc20.Mrc20DeployInfo, error) {
	var result []mrc20.Mrc20DeployInfo

	prefix := "mrc20_tick_"
	iter, err := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: []byte(prefix + "~"),
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	count := 0
	for iter.First(); iter.Valid(); iter.Next() {
		key := string(iter.Key())
		// 跳过 tick_name 索引
		if strings.Contains(key, "tick_name_") {
			continue
		}

		if count < cursor {
			count++
			continue
		}

		if limit > 0 && len(result) >= limit {
			break
		}

		var info mrc20.Mrc20DeployInfo
		err := sonic.Unmarshal(iter.Value(), &info)
		if err != nil {
			log.Println("Unmarshal mrc20 tick error:", err)
			continue
		}

		result = append(result, info)
		count++
	}

	return result, nil
}

// GetMrc20TickInfo 获取 MRC20 代币信息
func (pd *PebbleData) GetMrc20TickInfo(mrc20Id, tickName string) (mrc20.Mrc20DeployInfo, error) {
	var info mrc20.Mrc20DeployInfo

	var key string
	if mrc20Id != "" {
		key = fmt.Sprintf("mrc20_tick_%s", mrc20Id)
	} else if tickName != "" {
		// 先通过名称找到ID
		nameKey := fmt.Sprintf("mrc20_tick_name_%s", tickName)
		idBytes, closer, err := pd.Database.MrcDb.Get([]byte(nameKey))
		if err != nil {
			return info, err
		}
		defer closer.Close()
		mrc20Id = string(idBytes)
		key = fmt.Sprintf("mrc20_tick_%s", mrc20Id)
	} else {
		return info, fmt.Errorf("mrc20Id and tickName are both empty")
	}

	value, closer, err := pd.Database.MrcDb.Get([]byte(key))
	if err != nil {
		return info, err
	}
	defer closer.Close()

	err = sonic.Unmarshal(value, &info)
	return info, err
}

// UpdateMrc20TickInfo 更新 MRC20 代币信息（铸造数量）
func (pd *PebbleData) UpdateMrc20TickInfo(mrc20Id, txPoint string, totalMinted uint64) error {
	info, err := pd.GetMrc20TickInfo(mrc20Id, "")
	if err != nil {
		return err
	}

	info.TotalMinted = totalMinted

	data, err := sonic.Marshal(info)
	if err != nil {
		return err
	}

	key := fmt.Sprintf("mrc20_tick_%s", mrc20Id)
	return pd.Database.MrcDb.Set([]byte(key), data, pebble.Sync)
}

// UpdateMrc20TickHolder 更新持有者数量和交易数量
func (pd *PebbleData) UpdateMrc20TickHolder(mrc20Id string, txNum int64) error {
	info, err := pd.GetMrc20TickInfo(mrc20Id, "")
	if err != nil {
		return err
	}

	info.TxCount += uint64(txNum)

	// TODO: 实现持有者数量统计逻辑

	data, err := sonic.Marshal(info)
	if err != nil {
		return err
	}

	key := fmt.Sprintf("mrc20_tick_%s", mrc20Id)
	return pd.Database.MrcDb.Set([]byte(key), data, pebble.Sync)
}

// CleanMempoolNativeTransfer 清理 mempool 阶段创建的 native transfer 数据
// 在出块时调用，用于清理 mempool 阶段的中间状态，让出块流程可以从头处理
// 1. 恢复 TransferPending 状态的 UTXO 为 Available
// 2. 删除 mempool 阶段创建的接收方 UTXO (BlockHeight=-1)
func (pd *PebbleData) CleanMempoolNativeTransfer(pendingUtxos []*mrc20.Mrc20Utxo) error {
	batch := pd.Database.MrcDb.NewBatch()
	defer batch.Close()

	for _, utxo := range pendingUtxos {
		if utxo.Status != mrc20.UtxoStatusTransferPending {
			continue
		}

		//log.Printf("[DEBUG] CleanMempoolNativeTransfer: cleaning UTXO %s, operationTx=%s", utxo.TxPoint, utxo.OperationTx)

		// 1. 恢复发送方 UTXO 为 Available 状态
		// AmtChange 如果是负数需要取绝对值
		originalAmt := utxo.AmtChange
		if originalAmt.LessThan(decimal.Zero) {
			originalAmt = originalAmt.Neg()
		}

		restoredUtxo := *utxo
		restoredUtxo.Status = mrc20.UtxoStatusAvailable
		restoredUtxo.AmtChange = originalAmt
		restoredUtxo.OperationTx = "" // 清除操作交易

		data, err := sonic.Marshal(restoredUtxo)
		if err != nil {
			log.Printf("[ERROR] CleanMempoolNativeTransfer: marshal error: %v", err)
			continue
		}

		// 保存恢复后的 UTXO
		mainKey := fmt.Sprintf("mrc20_utxo_%s", utxo.TxPoint)
		inKey := fmt.Sprintf("mrc20_in_%s_%s_%s", utxo.ToAddress, utxo.Mrc20Id, utxo.TxPoint)
		availableKey := fmt.Sprintf("available_utxo_%s_%s_%s_%s", utxo.Chain, utxo.ToAddress, utxo.Mrc20Id, utxo.TxPoint)

		batch.Set([]byte(mainKey), data, pebble.Sync)
		batch.Set([]byte(inKey), data, pebble.Sync)
		batch.Set([]byte(availableKey), data, pebble.Sync)

		//log.Printf("[DEBUG] CleanMempoolNativeTransfer: restored UTXO %s to Available, amt=%s", utxo.TxPoint, originalAmt)

		// 2. 删除 mempool 阶段创建的接收方 UTXO
		// 接收方 UTXO 的 TxPoint 格式是 {operationTx}:0, {operationTx}:1, ...
		if utxo.OperationTx != "" {
			// 检查所有可能的输出（拆分可能有多个输出）
			for outputIndex := 0; outputIndex < 10; outputIndex++ {
				receiverTxPoint := fmt.Sprintf("%s:%d", utxo.OperationTx, outputIndex)
				receiverMainKey := fmt.Sprintf("mrc20_utxo_%s", receiverTxPoint)

				// 先检查接收方 UTXO 是否存在且是 mempool 创建的
				value, closer, err := pd.Database.MrcDb.Get([]byte(receiverMainKey))
				if err != nil {
					// 不存在则停止检查更多输出
					if outputIndex > 0 {
						break
					}
					continue
				}

				var receiverUtxo mrc20.Mrc20Utxo
				if err := sonic.Unmarshal(value, &receiverUtxo); err != nil {
					closer.Close()
					continue
				}
				closer.Close()

				// 【修复】跳过 TeleportPending 状态的 UTXO
				// 这些 UTXO 已经被 teleport 处理，不应该被清理
				if receiverUtxo.Status == mrc20.UtxoStatusTeleportPending {
					//log.Printf("[DEBUG] CleanMempoolNativeTransfer: skipping TeleportPending UTXO %s", receiverTxPoint)
					continue
				}

				// 只删除 mempool 创建的 (BlockHeight=-1) 且状态是 Available 的
				if receiverUtxo.BlockHeight == -1 && receiverUtxo.Status == mrc20.UtxoStatusAvailable {
					receiverInKey := fmt.Sprintf("mrc20_in_%s_%s_%s", receiverUtxo.ToAddress, receiverUtxo.Mrc20Id, receiverTxPoint)
					receiverAvailableKey := fmt.Sprintf("available_utxo_%s_%s_%s_%s", receiverUtxo.Chain, receiverUtxo.ToAddress, receiverUtxo.Mrc20Id, receiverTxPoint)

					batch.Delete([]byte(receiverMainKey), pebble.Sync)
					batch.Delete([]byte(receiverInKey), pebble.Sync)
					batch.Delete([]byte(receiverAvailableKey), pebble.Sync)

					//log.Printf("[DEBUG] CleanMempoolNativeTransfer: deleted mempool receiver UTXO %s, toAddr=%s", receiverTxPoint, receiverUtxo.ToAddress)
				}
			}
		}
	}

	return batch.Commit(pebble.Sync)
}

// GetMrc20UtxoByOutPutList 根据输出列表获取 MRC20 UTXO
// 返回可用状态(0)和等待跃迁状态(1)的 UTXO
// pending 状态的 UTXO 可以被 native transfer 或普通 transfer 花费
// 花费后跃迁将失败，用户自行承担后果
func (pd *PebbleData) GetMrc20UtxoByOutPutList(outputList []string, isMempool bool) ([]*mrc20.Mrc20Utxo, error) {
	var result []*mrc20.Mrc20Utxo

	for _, output := range outputList {
		key := fmt.Sprintf("mrc20_utxo_%s", output)
		value, closer, err := pd.Database.MrcDb.Get([]byte(key))
		if err != nil {
			if err == pebble.ErrNotFound {
				continue
			}
			return nil, err
		}

		var utxo mrc20.Mrc20Utxo
		err = sonic.Unmarshal(value, &utxo)
		closer.Close()

		if err != nil {
			log.Println("Unmarshal mrc20 utxo error:", err)
			continue
		}

		// 返回可用(0)、等待跃迁(1)和等待转账确认(2)状态的 UTXO
		// 已消耗(-1)的 UTXO 不返回
		if utxo.Status == mrc20.UtxoStatusAvailable ||
			utxo.Status == mrc20.UtxoStatusTeleportPending ||
			utxo.Status == mrc20.UtxoStatusTransferPending {
			result = append(result, &utxo)
		}
	}

	return result, nil
}

// UpdateMrc20Utxo 更新 MRC20 UTXO（用于转账和状态变更）
// 根据新架构设计：
// - status=0 (Available): 保存到 UTXO 表和 available_utxo 索引
// - status=1/2 (Pending): 保存到 UTXO 表，从 available_utxo 索引删除
// - status=-1 (Spent): 从 UTXO 表和所有索引中删除
func (pd *PebbleData) UpdateMrc20Utxo(utxoList []*mrc20.Mrc20Utxo, isMempool bool) error {
	batch := pd.Database.MrcDb.NewBatch()
	defer batch.Close()

	for _, utxo := range utxoList {
		mainKey := fmt.Sprintf("mrc20_utxo_%s", utxo.TxPoint)
		inKey := fmt.Sprintf("mrc20_in_%s_%s_%s", utxo.ToAddress, utxo.Mrc20Id, utxo.TxPoint)
		availableKey := fmt.Sprintf("available_utxo_%s_%s_%s_%s", utxo.Chain, utxo.ToAddress, utxo.Mrc20Id, utxo.TxPoint)

		if utxo.Status == mrc20.UtxoStatusSpent {
			// Spent UTXO: 从所有索引中删除
			// 根据新架构设计，Spent UTXO 不保留在 UTXO 表中，历史由 Transaction 流水表记录
			err := batch.Delete([]byte(mainKey), pebble.Sync)
			if err != nil {
				log.Println("Delete mrc20 utxo error:", err)
			}

			// 删除 mrc20_in 索引
			err = batch.Delete([]byte(inKey), pebble.Sync)
			if err != nil {
				log.Println("Delete mrc20 income index error:", err)
			}

			// 删除 available_utxo 索引
			err = batch.Delete([]byte(availableKey), pebble.Sync)
			if err != nil {
				log.Println("Delete available utxo index error:", err)
			}

			//log.Printf("[MRC20] Deleted spent UTXO: %s", utxo.TxPoint)
		} else {
			// 非 Spent UTXO: 保存/更新记录
			data, err := sonic.Marshal(utxo)
			if err != nil {
				log.Println("Marshal mrc20 utxo error:", err)
				continue
			}

			// 保存主记录
			err = batch.Set([]byte(mainKey), data, pebble.Sync)
			if err != nil {
				log.Println("Set mrc20 utxo error:", err)
			}

			// 保存 mrc20_in 索引
			if utxo.ToAddress != "" {
				err = batch.Set([]byte(inKey), data, pebble.Sync)
				if err != nil {
					log.Println("Set mrc20 income index error:", err)
				}
			}

			// 处理 available_utxo 索引
			if utxo.Status == mrc20.UtxoStatusAvailable && utxo.ToAddress != "" {
				// Available UTXO: 写入 available_utxo 索引
				err = batch.Set([]byte(availableKey), data, pebble.Sync)
				if err != nil {
					log.Println("Set available utxo index error:", err)
				}
			} else {
				// Pending UTXO: 从 available_utxo 索引删除
				err = batch.Delete([]byte(availableKey), pebble.Sync)
				if err != nil && err != pebble.ErrNotFound {
					log.Println("Delete available utxo index error:", err)
				}
			}
		}
	}

	return batch.Commit(pebble.Sync)
}

// AddMrc20Shovel 添加 MRC20 铲子（防止重复使用PIN）
func (pd *PebbleData) AddMrc20Shovel(shovelList []string, mintPinId, mrc20Id string) error {
	batch := pd.Database.MrcDb.NewBatch()
	defer batch.Close()

	for _, pinId := range shovelList {
		shovel := mrc20.Mrc20Shovel{
			Id:           pinId,
			Mrc20MintPin: mintPinId,
		}

		data, err := sonic.Marshal(shovel)
		if err != nil {
			log.Println("Marshal mrc20 shovel error:", err)
			continue
		}

		// Key: mrc20_shovel_{mrc20Id}_{pinId}
		key := fmt.Sprintf("mrc20_shovel_%s_%s", mrc20Id, pinId)
		err = batch.Set([]byte(key), data, pebble.Sync)
		if err != nil {
			log.Println("Set mrc20 shovel error:", err)
		}
	}

	return batch.Commit(pebble.Sync)
}

// GetMrc20Shovel 获取已使用的铲子
func (pd *PebbleData) GetMrc20Shovel(pinIds []string, mrc20Id string) (map[string]mrc20.Mrc20Shovel, error) {
	result := make(map[string]mrc20.Mrc20Shovel)

	for _, pinId := range pinIds {
		key := fmt.Sprintf("mrc20_shovel_%s_%s", mrc20Id, pinId)
		value, closer, err := pd.Database.MrcDb.Get([]byte(key))
		if err != nil {
			if err == pebble.ErrNotFound {
				continue
			}
			return nil, err
		}

		var shovel mrc20.Mrc20Shovel
		err = sonic.Unmarshal(value, &shovel)
		closer.Close()

		if err != nil {
			log.Println("Unmarshal mrc20 shovel error:", err)
			continue
		}

		result[pinId] = shovel
	}

	return result, nil
}

// CheckOperationtx 检查交易是否已处理（区分mempool和block状态）
func (pd *PebbleData) CheckOperationtx(txId string, isMempool bool) (*mrc20.Mrc20Utxo, error) {
	// 方法1：尝试查找 mrc20_op_tx_ 索引（向后兼容）
	key := fmt.Sprintf("mrc20_op_tx_%s", txId)
	value, closer, err := pd.Database.MrcDb.Get([]byte(key))
	if err == nil {
		defer closer.Close()
		var utxo mrc20.Mrc20Utxo
		err = sonic.Unmarshal(value, &utxo)
		if err == nil {
			// 检查BlockHeight匹配状态：mempool(-1) vs block(>0)
			if isMempool && utxo.BlockHeight == -1 {
				return &utxo, nil
			} else if !isMempool && utxo.BlockHeight > 0 {
				return &utxo, nil
			}
		}
	}

	// 方法2：如果索引不存在，通过扫描 TxPoint 查找
	// 扫描所有 mrc20_utxo_ 前缀的记录，查找匹配的 txId
	prefix := []byte("mrc20_utxo_")
	iter, err := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		var utxo mrc20.Mrc20Utxo
		err = sonic.Unmarshal(iter.Value(), &utxo)
		if err != nil {
			continue
		}

		// 检查 TxPoint 是否包含目标 txId
		// TxPoint 格式: txid:vout 或 txid:vout_out
		isTxPointMatch := strings.HasPrefix(utxo.TxPoint, txId+":")
		isOperationTxMatch := utxo.OperationTx == txId

		if isTxPointMatch || isOperationTxMatch {
			// 检查BlockHeight匹配状态：mempool(-1) vs block(>0)
			if isMempool && utxo.BlockHeight == -1 {
				return &utxo, nil
			} else if !isMempool && utxo.BlockHeight > 0 {
				return &utxo, nil
			}
		}
	}

	return nil, nil
}

// CheckOperationtxByTxPoint 通过 TxPoint 查找特定的 UTXO
func (pd *PebbleData) CheckOperationtxByTxPoint(txPoint string, isMempool bool) (*mrc20.Mrc20Utxo, error) {
	// 直接通过 TxPoint 查找
	key := fmt.Sprintf("mrc20_utxo_%s", txPoint)
	value, closer, err := pd.Database.MrcDb.Get([]byte(key))
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	defer closer.Close()

	var utxo mrc20.Mrc20Utxo
	err = sonic.Unmarshal(value, &utxo)
	if err != nil {
		return nil, err
	}

	return &utxo, nil
}

// CheckOperationtxAll 通过交易 ID 查找该交易的所有 UTXO（区分mempool和block状态）
func (pd *PebbleData) CheckOperationtxAll(txId string, isMempool bool) ([]*mrc20.Mrc20Utxo, error) {
	var result []*mrc20.Mrc20Utxo

	// 扫描所有 mrc20_utxo_ 前缀的记录，查找匹配的 txId
	prefix := []byte("mrc20_utxo_")
	iter, err := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		var utxo mrc20.Mrc20Utxo
		err = sonic.Unmarshal(iter.Value(), &utxo)
		if err != nil {
			continue
		}

		// 检查 TxPoint 是否包含目标 txId
		// TxPoint 格式: txid:vout 或 txid:vout_out
		isTxPointMatch := strings.HasPrefix(utxo.TxPoint, txId+":")
		isOperationTxMatch := utxo.OperationTx == txId

		if isTxPointMatch || isOperationTxMatch {
			// 根据isMempool参数过滤状态
			if isMempool && utxo.BlockHeight == -1 {
				result = append(result, &utxo)
			} else if !isMempool && utxo.BlockHeight >= 0 {
				// block阶段：包括BlockHeight=0和>0的记录
				result = append(result, &utxo)
			}
		}
	}

	return result, nil
}

// GetMrc20ByAddressAndTick 根据地址和代币ID获取余额
// 只扫描 mrc20_in_ 前缀，过滤 Status=0 的记录
func (pd *PebbleData) GetMrc20ByAddressAndTick(address, tickId string) ([]*mrc20.Mrc20Utxo, error) {
	var result []*mrc20.Mrc20Utxo

	// 使用 mrc20_in_ 前缀扫描收入记录
	prefix := fmt.Sprintf("mrc20_in_%s_%s_", address, tickId)
	iter, err := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: []byte(prefix + "~"),
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		var utxo mrc20.Mrc20Utxo
		err := sonic.Unmarshal(iter.Value(), &utxo)
		if err != nil {
			log.Println("Unmarshal mrc20 utxo error:", err)
			continue
		}

		// 只返回可用的 UTXO (Status=0)
		if utxo.Status == mrc20.UtxoStatusAvailable {
			result = append(result, &utxo)
		}
	}

	return result, nil
}

// GetPinListByOutPutList 根据输出列表获取 PIN 列表
func (pd *PebbleData) GetPinListByOutPutList(outputList []string) ([]*pin.PinInscription, error) {
	var result []*pin.PinInscription

	for _, output := range outputList {
		// output 格式: txid:vout
		// 需要根据 txPoint 查找 PIN
		// 在 pebblestore 中应该有相关的索引
		parts := strings.Split(output, ":")
		if len(parts) != 2 {
			continue
		}

		txid := parts[0]
		vout := parts[1]

		// 构建 pinId: txid + "i" + vout
		pinId := fmt.Sprintf("%si%s", txid, vout)

		// 从 PinsDBs 中查找
		pinBytes, err := pd.Database.GetPinByKey(pinId)
		if err != nil {
			if err != pebble.ErrNotFound {
				log.Println("Get pin by id error:", err, pinId)
			}
			continue
		}

		if pinBytes != nil {
			var pinData pin.PinInscription
			err = sonic.Unmarshal(pinBytes, &pinData)
			if err != nil {
				log.Println("Unmarshal pin error:", err)
				continue
			}
			result = append(result, &pinData)
		}
	}

	return result, nil
}

// GetMrc20Balance 获取地址的 MRC20 余额（用于 API）
func (pd *PebbleData) GetMrc20Balance(address, tickId string) (decimal.Decimal, error) {
	utxoList, err := pd.GetMrc20ByAddressAndTick(address, tickId)
	if err != nil {
		return decimal.Zero, err
	}

	balance := decimal.Zero
	for _, utxo := range utxoList {
		if utxo.Status != -1 {
			balance = balance.Add(utxo.AmtChange)
		}
	}

	return balance, nil
}

// GetMrc20UtxoList 获取地址的所有 MRC20 UTXO 列表（可用余额）
// 只扫描 mrc20_in_ 前缀，过滤 Status != -1 的记录
func (pd *PebbleData) GetMrc20UtxoList(address string, start, limit int) ([]*mrc20.Mrc20Utxo, error) {
	var result []*mrc20.Mrc20Utxo

	prefix := fmt.Sprintf("mrc20_in_%s_", address)
	iter, err := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: []byte(prefix + "~"),
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	count := 0
	for iter.First(); iter.Valid(); iter.Next() {
		var utxo mrc20.Mrc20Utxo
		err := sonic.Unmarshal(iter.Value(), &utxo)
		if err != nil {
			log.Println("Unmarshal mrc20 utxo error:", err)
			continue
		}

		// 只返回可用的 UTXO (Status != -1)
		if utxo.Status != mrc20.UtxoStatusSpent {
			if count < start {
				count++
				continue
			}

			if limit > 0 && len(result) >= limit {
				break
			}

			result = append(result, &utxo)
			count++
		}
	}

	return result, nil
}

// GetMrc20TransferHistory 获取 MRC20 转账历史
func (pd *PebbleData) GetMrc20TransferHistory(mrc20Id string, start, limit int) ([]*mrc20.Mrc20Utxo, error) {
	var allUtxos []*mrc20.Mrc20Utxo

	// 扫描所有相关的 UTXO
	prefix := "mrc20_utxo_"
	iter, err := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: []byte(prefix + "~"),
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	// 先收集所有匹配的数据
	for iter.First(); iter.Valid(); iter.Next() {
		var utxo mrc20.Mrc20Utxo
		err := sonic.Unmarshal(iter.Value(), &utxo)
		if err != nil {
			log.Println("Unmarshal mrc20 utxo error:", err)
			continue
		}

		if utxo.Mrc20Id != mrc20Id {
			continue
		}

		allUtxos = append(allUtxos, &utxo)
	}

	// 按时间倒序排序（最新的在前面）
	sort.Slice(allUtxos, func(i, j int) bool {
		return allUtxos[i].Timestamp > allUtxos[j].Timestamp
	})

	// 分页
	if start >= len(allUtxos) {
		return []*mrc20.Mrc20Utxo{}, nil
	}

	end := start + limit
	if limit <= 0 || end > len(allUtxos) {
		end = len(allUtxos)
	}

	return allUtxos[start:end], nil
}

// Mrc20HistoryRecord 历史记录条目（包含方向信息）
type Mrc20HistoryRecord struct {
	TxPoint     string `json:"txPoint"`
	MrcOption   string `json:"mrcOption"`
	Direction   string `json:"direction"` // "in" 或 "out"
	AmtChange   string `json:"amtChange"`
	Status      int    `json:"status"`
	Chain       string `json:"chain"`
	BlockHeight int64  `json:"blockHeight"`
	Timestamp   int64  `json:"timestamp"`
	FromAddress string `json:"fromAddress"`
	ToAddress   string `json:"toAddress"`
	OperationTx string `json:"operationTx"`
	Verify      bool   `json:"verify"`
	Msg         string `json:"msg"` // 验证失败原因
}

// GetMrc20AddressHistory 获取某地址在某 tick 的收支流水历史
// 收入：ToAddress == 该地址（收到代币）
// 支出：FromAddress == 该地址（转出代币给别人）
// statusFilter: nil 表示返回所有状态，非 nil 表示只返回指定状态的记录
// verifyFilter: nil 表示返回所有验证状态，非 nil 表示只返回指定验证状态的记录
func (pd *PebbleData) GetMrc20AddressHistory(mrc20Id, address string, start, limit int, statusFilter *int, verifyFilter *bool) ([]*mrc20.Mrc20Utxo, int, error) {
	records, total, err := pd.GetMrc20AddressHistoryWithDirection(mrc20Id, address, start, limit, statusFilter, verifyFilter)
	if err != nil {
		return nil, 0, err
	}

	// 转换为 Mrc20Utxo 格式（向后兼容）
	var result []*mrc20.Mrc20Utxo
	for _, r := range records {
		utxo := &mrc20.Mrc20Utxo{
			TxPoint:     r.TxPoint,
			MrcOption:   r.MrcOption,
			AmtChange:   decimal.RequireFromString(r.AmtChange),
			Status:      r.Status,
			Chain:       r.Chain,
			BlockHeight: r.BlockHeight,
			Timestamp:   r.Timestamp,
			FromAddress: r.FromAddress,
			ToAddress:   r.ToAddress,
			Mrc20Id:     mrc20Id,
			Verify:      r.Verify,
			Msg:         r.Msg,
		}
		// Direction 通过 Status 传递：out 方向设为 -1
		if r.Direction == "out" {
			utxo.Status = -1
		}
		result = append(result, utxo)
	}

	return result, total, nil
}

// GetMrc20AddressHistoryWithDirection 获取带方向信息的收支流水历史
func (pd *PebbleData) GetMrc20AddressHistoryWithDirection(mrc20Id, address string, start, limit int, statusFilter *int, verifyFilter *bool) ([]*Mrc20HistoryRecord, int, error) {
	var allRecords []*Mrc20HistoryRecord
	recordMap := make(map[string]bool) // 用于去重：key = txPoint + direction

	// 扫描所有该 tick 的 UTXO
	prefix := "mrc20_utxo_"
	iter, err := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: []byte(prefix + "~"),
	})
	if err != nil {
		return nil, 0, err
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		var utxo mrc20.Mrc20Utxo
		err := sonic.Unmarshal(iter.Value(), &utxo)
		if err != nil {
			continue
		}

		// 只处理匹配 tickId 的记录
		if utxo.Mrc20Id != mrc20Id {
			continue
		}

		// 从 TxPoint 提取 txid（格式: txid:vout）
		txid := utxo.TxPoint
		if idx := strings.LastIndex(utxo.TxPoint, ":"); idx > 0 {
			txid = utxo.TxPoint[:idx]
		}

		// 只处理属于该地址的 UTXO（ToAddress == 该地址）
		if utxo.ToAddress != address {
			continue
		}

		// 收入记录：收到这笔代币
		keyIn := utxo.TxPoint + "_in"
		if !recordMap[keyIn] {
			// 应用 statusFilter：如果指定了 status，只返回匹配的记录
			if statusFilter != nil && utxo.Status != *statusFilter {
				// 跳过不匹配的收入记录
			} else if verifyFilter != nil && utxo.Verify != *verifyFilter {
				// 跳过不匹配的验证状态记录
			} else {
				recordMap[keyIn] = true
				allRecords = append(allRecords, &Mrc20HistoryRecord{
					TxPoint:     utxo.TxPoint,
					MrcOption:   utxo.MrcOption,
					Direction:   "in",
					AmtChange:   utxo.AmtChange.String(),
					Status:      utxo.Status,
					Chain:       utxo.Chain,
					BlockHeight: utxo.BlockHeight,
					Timestamp:   utxo.Timestamp,
					FromAddress: utxo.FromAddress,
					ToAddress:   utxo.ToAddress,
					OperationTx: txid, // In: 显示创建这笔收入的交易（TxPoint的txid）
					Verify:      utxo.Verify,
					Msg:         utxo.Msg,
				})
			}
		}

		// 支出记录：当Status为Spent(-1)或TransferPending(2)时显示支出
		// mempool阶段：TransferPending的输入UTXO（AmtChange<0）表示待支出
		// block阶段：Spent表示已支出
		shouldShowOut := false
		if utxo.Status == -1 { // 已消费
			shouldShowOut = true
		} else if utxo.Status == 2 && utxo.AmtChange.IsNegative() { // mempool阶段的输入UTXO
			shouldShowOut = true
		}

		if shouldShowOut {
			// 应用 statusFilter：如果指定了 status，才显示匹配的支出记录
			if (statusFilter == nil || *statusFilter == utxo.Status) && (verifyFilter == nil || utxo.Verify == *verifyFilter) {
				keyOut := utxo.TxPoint + "_out"
				if !recordMap[keyOut] {
					recordMap[keyOut] = true
					// 对于mempool阶段的输入UTXO，AmtChange为负数，需要转为正数显示
					amtChange := utxo.AmtChange
					if utxo.AmtChange.IsNegative() {
						amtChange = utxo.AmtChange.Abs()
					}
					allRecords = append(allRecords, &Mrc20HistoryRecord{
						TxPoint:     utxo.TxPoint,
						MrcOption:   utxo.MrcOption,
						Direction:   "out",
						AmtChange:   amtChange.String(),
						Status:      utxo.Status,
						Chain:       utxo.Chain,
						BlockHeight: utxo.BlockHeight,
						Timestamp:   utxo.Timestamp,
						FromAddress: utxo.FromAddress,
						ToAddress:   utxo.ToAddress,
						OperationTx: utxo.OperationTx, // Out: 显示消费这笔资产的交易
						Verify:      utxo.Verify,
						Msg:         utxo.Msg,
					})
				}
			}
		}
	}

	total := len(allRecords)

	// 按区块高度倒序排序（最新的在前面）
	sort.Slice(allRecords, func(i, j int) bool {
		if allRecords[i].BlockHeight == allRecords[j].BlockHeight {
			// 同一区块，支出在前
			if allRecords[i].Direction != allRecords[j].Direction {
				return allRecords[i].Direction == "out"
			}
		}
		return allRecords[i].BlockHeight > allRecords[j].BlockHeight
	})

	// 分页
	if start >= len(allRecords) {
		return []*Mrc20HistoryRecord{}, total, nil
	}

	end := start + limit
	if limit <= 0 || end > len(allRecords) {
		end = len(allRecords)
	}

	return allRecords[start:end], total, nil
}

// Mrc20Holder 持有者信息
type Mrc20Holder struct {
	Address string          `json:"address"`
	Balance decimal.Decimal `json:"balance"`
}

// GetMrc20Holders 获取 tick 的持有者列表（复用 API 余额接口）
func (pd *PebbleData) GetMrc20Holders(tickId string, start, limit int, searchAddress string) ([]Mrc20Holder, error) {
	// 收集所有持有该 tickId 的地址
	addressSet := make(map[string]bool)

	// 扫描 UTXO 收集地址
	prefix := []byte("mrc20_utxo_")
	iter, err := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		var utxo mrc20.Mrc20Utxo
		if err := sonic.Unmarshal(iter.Value(), &utxo); err != nil {
			continue
		}

		if utxo.Mrc20Id == tickId && utxo.ToAddress != "" {
			if searchAddress == "" || strings.Contains(utxo.ToAddress, searchAddress) {
				addressSet[utxo.ToAddress] = true
			}
		}
	}

	// 使用 API 余额接口计算每个地址的余额（自动处理跨链）
	var holders []Mrc20Holder
	for addr := range addressSet {
		// 调用和 API 相同的方法：GetAllChainsBalances
		allBalances, err := GetAllChainsBalances(addr)
		if err != nil {
			log.Printf("[Holders] Failed to get balances for addr=%s: %v", addr, err)
			continue
		}

		// 累加该地址在所有链上该 tickId 的余额
		totalBalance := decimal.Zero
		for _, balances := range allBalances {
			for _, b := range balances {
				if b.TickId == tickId {
					// 使用 Balance 字段（和 API 返回的一致）
					totalBalance = totalBalance.Add(b.Balance)
				}
			}
		}

		holders = append(holders, Mrc20Holder{
			Address: addr,
			Balance: totalBalance,
		})
	}

	// 按余额降序排序
	sort.Slice(holders, func(i, j int) bool {
		return holders[i].Balance.GreaterThan(holders[j].Balance)
	})

	// 分页
	if start >= len(holders) {
		return []Mrc20Holder{}, nil
	}

	end := start + limit
	if end > len(holders) {
		end = len(holders)
	}

	return holders[start:end], nil
}

// GetMrc20HoldersCount 获取持有者总数（包括曾经持有过的）
// 使用 mrc20_in_ 前缀，统计所有曾经持有过的地址
func (pd *PebbleData) GetMrc20HoldersCount(tickId string, searchAddress string) (int, error) {
	addressSet := make(map[string]bool)

	prefix := "mrc20_in_"
	iter, err := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: []byte(prefix + "~"),
	})
	if err != nil {
		return 0, err
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		var utxo mrc20.Mrc20Utxo
		err := sonic.Unmarshal(iter.Value(), &utxo)
		if err != nil {
			continue
		}

		if utxo.Mrc20Id != tickId {
			continue
		}

		if utxo.ToAddress == "" {
			continue
		}

		// 搜索过滤
		if searchAddress != "" && !strings.Contains(utxo.ToAddress, searchAddress) {
			continue
		}

		// 记录所有曾经持有过的地址
		addressSet[utxo.ToAddress] = true
	}

	return len(addressSet), nil
}

// ================ Teleport 跃迁相关存储方法 ================

// SaveMrc20Arrival 保存 arrival 记录
func (pd *PebbleData) SaveMrc20Arrival(arrival *mrc20.Mrc20Arrival) error {
	data, err := sonic.Marshal(arrival)
	if err != nil {
		return fmt.Errorf("marshal arrival error: %w", err)
	}

	batch := pd.Database.MrcDb.NewBatch()
	defer batch.Close()

	// 主键: arrival_{pinId}
	key := fmt.Sprintf("arrival_%s", arrival.PinId)
	err = batch.Set([]byte(key), data, pebble.Sync)
	if err != nil {
		return fmt.Errorf("save arrival error: %w", err)
	}

	// 索引: arrival_asset_{assetOutpoint} - 用于 teleport 快速查找
	assetKey := fmt.Sprintf("arrival_asset_%s", arrival.AssetOutpoint)
	err = batch.Set([]byte(assetKey), []byte(arrival.PinId), pebble.Sync)
	if err != nil {
		return fmt.Errorf("save arrival asset index error: %w", err)
	}

	// 索引: arrival_pending_{chain}_{tickId}_{pinId} - 用于列出待处理的 arrival
	if arrival.Status == mrc20.ArrivalStatusPending {
		pendingKey := fmt.Sprintf("arrival_pending_%s_%s_%s", arrival.Chain, arrival.TickId, arrival.PinId)
		err = batch.Set([]byte(pendingKey), []byte(arrival.PinId), pebble.Sync)
		if err != nil {
			return fmt.Errorf("save arrival pending index error: %w", err)
		}
	}

	return batch.Commit(pebble.Sync)
}

// GetMrc20ArrivalByPinId 根据 PIN ID 获取 arrival 记录 (coord 查询)
func (pd *PebbleData) GetMrc20ArrivalByPinId(pinId string) (*mrc20.Mrc20Arrival, error) {
	key := fmt.Sprintf("arrival_%s", pinId)
	value, closer, err := pd.Database.MrcDb.Get([]byte(key))
	if err != nil {
		return nil, err
	}
	defer closer.Close()

	var arrival mrc20.Mrc20Arrival
	err = sonic.Unmarshal(value, &arrival)
	if err != nil {
		return nil, fmt.Errorf("unmarshal arrival error: %w", err)
	}

	return &arrival, nil
}

// GetMrc20ArrivalByAssetOutpoint 根据 assetOutpoint 获取 arrival 记录
func (pd *PebbleData) GetMrc20ArrivalByAssetOutpoint(assetOutpoint string) (*mrc20.Mrc20Arrival, error) {
	// 先查找索引
	assetKey := fmt.Sprintf("arrival_asset_%s", assetOutpoint)
	pinIdBytes, closer, err := pd.Database.MrcDb.Get([]byte(assetKey))
	if err != nil {
		return nil, err
	}
	closer.Close()

	// 再查找 arrival 数据
	return pd.GetMrc20ArrivalByPinId(string(pinIdBytes))
}

// UpdateMrc20ArrivalStatus 更新 arrival 状态（跃迁完成时调用）
func (pd *PebbleData) UpdateMrc20ArrivalStatus(pinId string, status mrc20.ArrivalStatus, teleportPinId, teleportChain, teleportTxId string, completedAt int64) error {
	arrival, err := pd.GetMrc20ArrivalByPinId(pinId)
	if err != nil {
		return fmt.Errorf("get arrival error: %w", err)
	}

	arrival.Status = status
	arrival.TeleportPinId = teleportPinId
	arrival.TeleportChain = teleportChain
	arrival.TeleportTxId = teleportTxId
	arrival.CompletedAt = completedAt

	data, err := sonic.Marshal(arrival)
	if err != nil {
		return fmt.Errorf("marshal arrival error: %w", err)
	}

	batch := pd.Database.MrcDb.NewBatch()
	defer batch.Close()

	// 更新主数据
	key := fmt.Sprintf("arrival_%s", pinId)
	err = batch.Set([]byte(key), data, pebble.Sync)
	if err != nil {
		return fmt.Errorf("update arrival error: %w", err)
	}

	// 如果状态不再是 pending，删除 pending 索引
	if status != mrc20.ArrivalStatusPending {
		pendingKey := fmt.Sprintf("arrival_pending_%s_%s_%s", arrival.Chain, arrival.TickId, pinId)
		err = batch.Delete([]byte(pendingKey), pebble.Sync)
		if err != nil && err != pebble.ErrNotFound {
			log.Println("delete arrival pending index error:", err)
		}
	}

	return batch.Commit(pebble.Sync)
}

// GetPendingArrivals 获取待处理的 arrival 列表
func (pd *PebbleData) GetPendingArrivals(chain, tickId string, limit int) ([]*mrc20.Mrc20Arrival, error) {
	var result []*mrc20.Mrc20Arrival

	prefix := fmt.Sprintf("arrival_pending_%s_%s_", chain, tickId)
	iter, err := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: []byte(prefix + "~"),
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	count := 0
	for iter.First(); iter.Valid(); iter.Next() {
		if limit > 0 && count >= limit {
			break
		}

		pinId := string(iter.Value())
		arrival, err := pd.GetMrc20ArrivalByPinId(pinId)
		if err != nil {
			log.Println("get arrival error:", err)
			continue
		}

		result = append(result, arrival)
		count++
	}

	return result, nil
}

// SaveMrc20Teleport 保存 teleport 记录
func (pd *PebbleData) SaveMrc20Teleport(teleport *mrc20.Mrc20Teleport) error {
	data, err := sonic.Marshal(teleport)
	if err != nil {
		return fmt.Errorf("marshal teleport error: %w", err)
	}

	batch := pd.Database.MrcDb.NewBatch()
	defer batch.Close()

	// 主键: teleport_{pinId}
	key := fmt.Sprintf("teleport_%s", teleport.PinId)
	err = batch.Set([]byte(key), data, pebble.Sync)
	if err != nil {
		return fmt.Errorf("save teleport error: %w", err)
	}

	// 索引: teleport_coord_{coord} - 通过 arrival pinId 查找 teleport
	coordKey := fmt.Sprintf("teleport_coord_%s", teleport.Coord)
	err = batch.Set([]byte(coordKey), []byte(teleport.PinId), pebble.Sync)
	if err != nil {
		return fmt.Errorf("save teleport coord index error: %w", err)
	}

	// 索引: teleport_asset_{assetOutpoint} - 通过源 UTXO 查找 teleport
	if teleport.SpentUtxoPoint != "" {
		assetKey := fmt.Sprintf("teleport_asset_%s", teleport.SpentUtxoPoint)
		err = batch.Set([]byte(assetKey), []byte(teleport.PinId), pebble.Sync)
		if err != nil {
			return fmt.Errorf("save teleport asset index error: %w", err)
		}
	}

	return batch.Commit(pebble.Sync)
}

// GetMrc20TeleportByPinId 根据 PIN ID 获取 teleport 记录
func (pd *PebbleData) GetMrc20TeleportByPinId(pinId string) (*mrc20.Mrc20Teleport, error) {
	key := fmt.Sprintf("teleport_%s", pinId)
	value, closer, err := pd.Database.MrcDb.Get([]byte(key))
	if err != nil {
		return nil, err
	}
	defer closer.Close()

	var teleport mrc20.Mrc20Teleport
	err = sonic.Unmarshal(value, &teleport)
	if err != nil {
		return nil, fmt.Errorf("unmarshal teleport error: %w", err)
	}

	return &teleport, nil
}

// GetMrc20TeleportByCoord 根据 coord (arrival pinId) 获取 teleport 记录
func (pd *PebbleData) GetMrc20TeleportByCoord(coord string) (*mrc20.Mrc20Teleport, error) {
	coordKey := fmt.Sprintf("teleport_coord_%s", coord)
	pinIdBytes, closer, err := pd.Database.MrcDb.Get([]byte(coordKey))
	if err != nil {
		return nil, err
	}
	closer.Close()

	return pd.GetMrc20TeleportByPinId(string(pinIdBytes))
}

// CheckTeleportExists 检查某个 arrival 是否已经有对应的 teleport
func (pd *PebbleData) CheckTeleportExists(coord string) bool {
	coordKey := fmt.Sprintf("teleport_coord_%s", coord)
	_, closer, err := pd.Database.MrcDb.Get([]byte(coordKey))
	if err == nil {
		closer.Close()
		return true
	}
	return false
}

// CheckTeleportExistsByAssetOutpoint 检查某个 assetOutpoint 是否已经有对应的 teleport
func (pd *PebbleData) CheckTeleportExistsByAssetOutpoint(assetOutpoint string) bool {
	assetKey := fmt.Sprintf("teleport_asset_%s", assetOutpoint)
	_, closer, err := pd.Database.MrcDb.Get([]byte(assetKey))
	if err == nil {
		closer.Close()
		return true
	}
	return false
}

// GetMrc20UtxoByTxPoint 根据 txPoint 获取单个 UTXO
func (pd *PebbleData) GetMrc20UtxoByTxPoint(txPoint string, checkStatus bool) (*mrc20.Mrc20Utxo, error) {
	key := fmt.Sprintf("mrc20_utxo_%s", txPoint)
	value, closer, err := pd.Database.MrcDb.Get([]byte(key))
	if err != nil {
		return nil, err
	}
	defer closer.Close()

	var utxo mrc20.Mrc20Utxo
	err = sonic.Unmarshal(value, &utxo)
	if err != nil {
		return nil, fmt.Errorf("unmarshal mrc20 utxo error: %w", err)
	}

	// 如果检查状态，只返回可用的 UTXO
	if checkStatus && utxo.Status == -1 {
		return nil, fmt.Errorf("utxo already spent")
	}

	return &utxo, nil
}

// ============== PendingTeleport 相关方法 ==============

// SavePendingTeleport 保存等待 arrival 的 teleport transfer
func (pd *PebbleData) SavePendingTeleport(pending *mrc20.PendingTeleport) error {
	_ = pending // V1 deprecated
	// V1 PendingTeleport structure - deprecated in V2
	// V2 uses new structure stored via man/mrc20_teleport_storage.go
	return fmt.Errorf("SavePendingTeleport (V1) is deprecated, use V2 storage")

	/* V1 implementation
	// 主键: pending_teleport_{pinId}
	key := fmt.Sprintf("pending_teleport_%s", pending.PinId)
	err = batch.Set([]byte(key), data, pebble.Sync)
	if err != nil {
		return fmt.Errorf("save pending teleport error: %w", err)
	}

	// 索引: pending_teleport_coord_{coord} - 通过期望的 arrival pinId 查找
	coordKey := fmt.Sprintf("pending_teleport_coord_%s", pending.Coord)
	err = batch.Set([]byte(coordKey), []byte(pending.PinId), pebble.Sync)
	if err != nil {
		return fmt.Errorf("save pending teleport coord index error: %w", err)
	}

	return batch.Commit(pebble.Sync)
	*/
}

// GetPendingTeleportByCoord 根据 coord (期望的 arrival pinId) 获取等待的 teleport
func (pd *PebbleData) GetPendingTeleportByCoord(coord string) (*mrc20.PendingTeleport, error) {
	coordKey := fmt.Sprintf("pending_teleport_coord_%s", coord)
	pinIdBytes, closer, err := pd.Database.MrcDb.Get([]byte(coordKey))
	if err != nil {
		return nil, err
	}
	closer.Close()

	return pd.GetPendingTeleportByPinId(string(pinIdBytes))
}

// GetPendingTeleportByPinId 根据 PIN ID 获取等待的 teleport
func (pd *PebbleData) GetPendingTeleportByPinId(pinId string) (*mrc20.PendingTeleport, error) {
	key := fmt.Sprintf("pending_teleport_%s", pinId)
	value, closer, err := pd.Database.MrcDb.Get([]byte(key))
	if err != nil {
		return nil, err
	}
	defer closer.Close()

	var pending mrc20.PendingTeleport
	err = sonic.Unmarshal(value, &pending)
	if err != nil {
		return nil, fmt.Errorf("unmarshal pending teleport error: %w", err)
	}

	return &pending, nil
}

// DeletePendingTeleport 删除已完成的 pending teleport
func (pd *PebbleData) DeletePendingTeleport(pinId, coord string) error {
	batch := pd.Database.MrcDb.NewBatch()
	defer batch.Close()

	key := fmt.Sprintf("pending_teleport_%s", pinId)
	err := batch.Delete([]byte(key), pebble.Sync)
	if err != nil {
		return fmt.Errorf("delete pending teleport error: %w", err)
	}

	coordKey := fmt.Sprintf("pending_teleport_coord_%s", coord)
	err = batch.Delete([]byte(coordKey), pebble.Sync)
	if err != nil {
		return fmt.Errorf("delete pending teleport coord index error: %w", err)
	}

	return batch.Commit(pebble.Sync)
}

// GetAllPendingTeleports 获取所有等待的 teleport（用于定期重试）
func (pd *PebbleData) GetAllPendingTeleports() ([]*mrc20.PendingTeleport, error) {
	var result []*mrc20.PendingTeleport

	iter, err := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: []byte("pending_teleport_"),
		UpperBound: []byte("pending_teleport_~"),
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		key := string(iter.Key())
		// 跳过 coord 索引
		if strings.Contains(key, "_coord_") {
			continue
		}

		var pending mrc20.PendingTeleport
		err := sonic.Unmarshal(iter.Value(), &pending)
		if err != nil {
			continue
		}
		result = append(result, &pending)
	}

	return result, nil
}

// ============== TeleportPendingIn 相关方法 (用于跟踪接收方的 PendingInBalance) ==============

// SaveTeleportPendingIn 保存 teleport 接收方的 pending 余额记录
func (pd *PebbleData) SaveTeleportPendingIn(pendingIn *mrc20.TeleportPendingIn) error {
	data, err := sonic.Marshal(pendingIn)
	if err != nil {
		return fmt.Errorf("marshal teleport pending in error: %w", err)
	}

	batch := pd.Database.MrcDb.NewBatch()
	defer batch.Close()

	// 主键: teleport_pending_in_{coord}
	key := fmt.Sprintf("teleport_pending_in_%s", pendingIn.Coord)
	err = batch.Set([]byte(key), data, pebble.Sync)
	if err != nil {
		return fmt.Errorf("save teleport pending in error: %w", err)
	}

	// 索引: teleport_pending_in_addr_{toAddress}_{coord} - 用于按地址查询
	addrKey := fmt.Sprintf("teleport_pending_in_addr_%s_%s", pendingIn.ToAddress, pendingIn.Coord)
	err = batch.Set([]byte(addrKey), []byte(pendingIn.Coord), pebble.Sync)
	if err != nil {
		return fmt.Errorf("save teleport pending in address index error: %w", err)
	}

	return batch.Commit(pebble.Sync)
}

// GetTeleportPendingInByCoord 根据 coord 获取 pending in 记录
func (pd *PebbleData) GetTeleportPendingInByCoord(coord string) (*mrc20.TeleportPendingIn, error) {
	key := fmt.Sprintf("teleport_pending_in_%s", coord)
	value, closer, err := pd.Database.MrcDb.Get([]byte(key))
	if err != nil {
		return nil, err
	}
	defer closer.Close()

	var pendingIn mrc20.TeleportPendingIn
	err = sonic.Unmarshal(value, &pendingIn)
	if err != nil {
		return nil, fmt.Errorf("unmarshal teleport pending in error: %w", err)
	}

	return &pendingIn, nil
}

// GetTeleportPendingInByAddress 获取指定地址的所有 pending in 记录 (用于计算 PendingInBalance)
func (pd *PebbleData) GetTeleportPendingInByAddress(address string) ([]*mrc20.TeleportPendingIn, error) {
	var result []*mrc20.TeleportPendingIn

	prefix := fmt.Sprintf("teleport_pending_in_addr_%s_", address)
	iter, err := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: []byte(prefix + "~"),
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		coord := string(iter.Value())
		pendingIn, err := pd.GetTeleportPendingInByCoord(coord)
		if err != nil {
			log.Println("GetTeleportPendingInByAddress: get pending in error:", err)
			continue
		}
		result = append(result, pendingIn)
	}

	return result, nil
}

// DeleteTeleportPendingIn 删除 pending in 记录 (跃迁完成时调用)
func (pd *PebbleData) DeleteTeleportPendingIn(coord, toAddress string) error {
	batch := pd.Database.MrcDb.NewBatch()
	defer batch.Close()

	// 删除主记录
	key := fmt.Sprintf("teleport_pending_in_%s", coord)
	err := batch.Delete([]byte(key), pebble.Sync)
	if err != nil && err != pebble.ErrNotFound {
		return fmt.Errorf("delete teleport pending in error: %w", err)
	}

	// 删除地址索引
	addrKey := fmt.Sprintf("teleport_pending_in_addr_%s_%s", toAddress, coord)
	err = batch.Delete([]byte(addrKey), pebble.Sync)
	if err != nil && err != pebble.ErrNotFound {
		return fmt.Errorf("delete teleport pending in address index error: %w", err)
	}

	return batch.Commit(pebble.Sync)
}

// ============== TransferPendingIn 相关方法 (用于跟踪普通转账接收方的 PendingInBalance) ==============

// SaveTransferPendingIn 保存普通 transfer/native_transfer 接收方的 pending 余额记录
func (pd *PebbleData) SaveTransferPendingIn(pendingIn *mrc20.TransferPendingIn) error {
	data, err := sonic.Marshal(pendingIn)
	if err != nil {
		return fmt.Errorf("marshal transfer pending in error: %w", err)
	}

	batch := pd.Database.MrcDb.NewBatch()
	defer batch.Close()

	// 主键: transfer_pending_in_{txPoint}
	key := fmt.Sprintf("transfer_pending_in_%s", pendingIn.TxPoint)
	err = batch.Set([]byte(key), data, pebble.Sync)
	if err != nil {
		return fmt.Errorf("save transfer pending in error: %w", err)
	}

	// 索引: transfer_pending_in_addr_{toAddress}_{txPoint} - 用于按地址查询
	addrKey := fmt.Sprintf("transfer_pending_in_addr_%s_%s", pendingIn.ToAddress, pendingIn.TxPoint)
	err = batch.Set([]byte(addrKey), []byte(pendingIn.TxPoint), pebble.Sync)
	if err != nil {
		return fmt.Errorf("save transfer pending in address index error: %w", err)
	}

	return batch.Commit(pebble.Sync)
}

// GetTransferPendingInByTxPoint 根据 txPoint 获取 pending in 记录
func (pd *PebbleData) GetTransferPendingInByTxPoint(txPoint string) (*mrc20.TransferPendingIn, error) {
	key := fmt.Sprintf("transfer_pending_in_%s", txPoint)
	value, closer, err := pd.Database.MrcDb.Get([]byte(key))
	if err != nil {
		return nil, err
	}
	defer closer.Close()

	var pendingIn mrc20.TransferPendingIn
	err = sonic.Unmarshal(value, &pendingIn)
	if err != nil {
		return nil, fmt.Errorf("unmarshal transfer pending in error: %w", err)
	}

	return &pendingIn, nil
}

// GetTransferPendingInByAddress 获取指定地址的所有 transfer pending in 记录 (用于计算 PendingInBalance)
func (pd *PebbleData) GetTransferPendingInByAddress(address string) ([]*mrc20.TransferPendingIn, error) {
	var result []*mrc20.TransferPendingIn

	prefix := fmt.Sprintf("transfer_pending_in_addr_%s_", address)
	iter, err := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: []byte(prefix + "~"),
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		txPoint := string(iter.Value())
		pendingIn, err := pd.GetTransferPendingInByTxPoint(txPoint)
		if err != nil {
			log.Println("GetTransferPendingInByAddress: get pending in error:", err)
			continue
		}
		result = append(result, pendingIn)
	}

	return result, nil
}

// DeleteTransferPendingIn 删除 transfer pending in 记录 (出块确认后调用)
func (pd *PebbleData) DeleteTransferPendingIn(txPoint, toAddress string) error {
	batch := pd.Database.MrcDb.NewBatch()
	defer batch.Close()

	// 删除主记录
	key := fmt.Sprintf("transfer_pending_in_%s", txPoint)
	err := batch.Delete([]byte(key), pebble.Sync)
	if err != nil && err != pebble.ErrNotFound {
		return fmt.Errorf("delete transfer pending in error: %w", err)
	}

	// 删除地址索引
	addrKey := fmt.Sprintf("transfer_pending_in_addr_%s_%s", toAddress, txPoint)
	err = batch.Delete([]byte(addrKey), pebble.Sync)
	if err != nil && err != pebble.ErrNotFound {
		return fmt.Errorf("delete transfer pending in address index error: %w", err)
	}

	return batch.Commit(pebble.Sync)
}

// DeleteTransferPendingInByTxId 根据交易ID删除所有相关的 transfer pending in 记录
func (pd *PebbleData) DeleteTransferPendingInByTxId(txId string) error {
	// 遍历查找以该 txId 开头的所有记录
	prefix := fmt.Sprintf("transfer_pending_in_%s:", txId)
	iter, err := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: []byte(prefix + "~"),
	})
	if err != nil {
		return err
	}
	defer iter.Close()

	batch := pd.Database.MrcDb.NewBatch()
	defer batch.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		// 获取完整的 pendingIn 记录以获取 toAddress
		pendingIn, err := pd.GetTransferPendingInByTxPoint(string(iter.Key()[len("transfer_pending_in_"):]))
		if err != nil {
			continue
		}

		// 删除主记录
		batch.Delete(iter.Key(), pebble.Sync)

		// 删除地址索引
		addrKey := fmt.Sprintf("transfer_pending_in_addr_%s_%s", pendingIn.ToAddress, pendingIn.TxPoint)
		batch.Delete([]byte(addrKey), pebble.Sync)
	}

	return batch.Commit(pebble.Sync)
}

// CleanMempoolMrc20ByTxIds 根据交易hash列表清理mempool阶段的MRC20数据
// 这个函数在出块时调用，删除mempool阶段创建的数据，然后重新处理区块
// 处理逻辑：
// 1. 把发送方的TransferPending UTXO恢复为Available（Status=0, AmtChange恢复正数, OperationTx清空）
// 2. 删除接收方在mempool创建的UTXO（BlockHeight=-1 且 OperationTx=txId）
func (pd *PebbleData) CleanMempoolMrc20ByTxIds(txIds []string) error {
	if len(txIds) == 0 {
		return nil
	}

	// 构建txId集合用于快速查找
	txIdSet := make(map[string]struct{})
	for _, txId := range txIds {
		txIdSet[txId] = struct{}{}
	}

	batch := pd.Database.MrcDb.NewBatch()
	defer batch.Close()

	// 遍历所有UTXO
	prefix := []byte("mrc20_utxo_")
	iter, err := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return err
	}
	defer iter.Close()

	var restoredCount, deletedCount int

	for iter.First(); iter.Valid(); iter.Next() {
		var utxo mrc20.Mrc20Utxo
		if err := sonic.Unmarshal(iter.Value(), &utxo); err != nil {
			continue
		}

		// 检查这个UTXO是否与区块内的交易相关
		if _, ok := txIdSet[utxo.OperationTx]; !ok {
			continue
		}

		mainKey := fmt.Sprintf("mrc20_utxo_%s", utxo.TxPoint)
		inKey := fmt.Sprintf("mrc20_in_%s_%s_%s", utxo.ToAddress, utxo.Mrc20Id, utxo.TxPoint)
		availableKey := fmt.Sprintf("available_utxo_%s_%s_%s_%s", utxo.Chain, utxo.ToAddress, utxo.Mrc20Id, utxo.TxPoint)

		// 情况1: 发送方的TransferPending UTXO（原始UTXO被mempool修改）
		// 特征：Status=TransferPending(2), AmtChange是负数, BlockHeight >= 0
		if utxo.Status == mrc20.UtxoStatusTransferPending && utxo.BlockHeight >= 0 {
			// 恢复为Available状态
			utxo.Status = mrc20.UtxoStatusAvailable
			// 恢复AmtChange为正数
			if utxo.AmtChange.LessThan(decimal.Zero) {
				utxo.AmtChange = utxo.AmtChange.Neg()
			}
			// 清空OperationTx
			utxo.OperationTx = ""

			// 保存恢复后的UTXO
			data, err := sonic.Marshal(utxo)
			if err != nil {
				log.Printf("[ERROR] CleanMempoolMrc20ByTxIds: marshal error: %v", err)
				continue
			}
			batch.Set([]byte(mainKey), data, pebble.Sync)
			batch.Set([]byte(inKey), data, pebble.Sync)
			batch.Set([]byte(availableKey), data, pebble.Sync)
			restoredCount++
			//log.Printf("[DEBUG] CleanMempoolMrc20ByTxIds: restored UTXO %s to Available", utxo.TxPoint)
		}

		// 情况2: 接收方在mempool创建的UTXO
		// 特征：BlockHeight=-1, Status=Available(0)
		if utxo.BlockHeight == -1 && utxo.Status == mrc20.UtxoStatusAvailable {
			// 删除这个UTXO
			batch.Delete([]byte(mainKey), pebble.Sync)
			batch.Delete([]byte(inKey), pebble.Sync)
			batch.Delete([]byte(availableKey), pebble.Sync)
			deletedCount++
			//log.Printf("[DEBUG] CleanMempoolMrc20ByTxIds: deleted mempool UTXO %s", utxo.TxPoint)
		}
	}

	if restoredCount > 0 || deletedCount > 0 {
		log.Printf("[INFO] CleanMempoolMrc20ByTxIds: restored %d UTXOs, deleted %d mempool UTXOs", restoredCount, deletedCount)
	}

	return batch.Commit(pebble.Sync)
}

// ConfirmPendingTransfersByTxIds 根据区块内的交易hash确认TransferPending状态的UTXO
// 这个函数在出块时调用，处理mempool阶段创建的pending数据
// 逻辑：
// 1. 遍历所有TransferPending状态的UTXO
// 2. 检查OperationTx是否在txIdSet中（说明这笔交易在当前区块内）
// 3. 如果是，则：
//   - 标记发送方UTXO为Spent
//   - 更新接收方UTXO的BlockHeight（从-1改为实际区块高度）
//   - 更新余额
func (pd *PebbleData) ConfirmPendingTransfersByTxIds(txIdSet map[string]struct{}, chainName string, blockHeight int64, timestamp int64) error {
	if len(txIdSet) == 0 {
		return nil
	}

	// 遍历所有UTXO，找出TransferPending状态且OperationTx在txIdSet中的
	prefix := []byte("mrc20_utxo_")
	iter, err := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return err
	}
	defer iter.Close()

	// 收集需要处理的转账
	type pendingTransfer struct {
		spentUtxo    mrc20.Mrc20Utxo  // 发送方的UTXO
		receiverUtxo *mrc20.Mrc20Utxo // 接收方的UTXO（可能为nil）
	}
	transfers := make(map[string]*pendingTransfer) // operationTx -> transfer

	for iter.First(); iter.Valid(); iter.Next() {
		var utxo mrc20.Mrc20Utxo
		if err := sonic.Unmarshal(iter.Value(), &utxo); err != nil {
			continue
		}

		// 跳过不相关的链
		if utxo.Chain != chainName {
			continue
		}

		// 检查OperationTx是否在txIdSet中
		if _, ok := txIdSet[utxo.OperationTx]; !ok {
			continue
		}

		// 发送方的TransferPending UTXO
		if utxo.Status == mrc20.UtxoStatusTransferPending {
			opTx := utxo.OperationTx
			if transfers[opTx] == nil {
				transfers[opTx] = &pendingTransfer{}
			}
			transfers[opTx].spentUtxo = utxo
			//log.Printf("[DEBUG] ConfirmPendingTransfersByTxIds: found pending UTXO %s for tx %s", utxo.TxPoint, opTx)
		}

		// 接收方的mempool UTXO（BlockHeight=-1, Status=Available）
		if utxo.BlockHeight == -1 && utxo.Status == mrc20.UtxoStatusAvailable {
			opTx := utxo.OperationTx
			if transfers[opTx] == nil {
				transfers[opTx] = &pendingTransfer{}
			}
			utxoCopy := utxo
			transfers[opTx].receiverUtxo = &utxoCopy
			//log.Printf("[DEBUG] ConfirmPendingTransfersByTxIds: found mempool UTXO %s for tx %s", utxo.TxPoint, opTx)
		}
	}

	if len(transfers) == 0 {
		return nil
	}

	log.Printf("[INFO] ConfirmPendingTransfersByTxIds: confirming %d transfers", len(transfers))

	// 处理每笔转账
	for txId, transfer := range transfers {
		if transfer.spentUtxo.TxPoint == "" {
			log.Printf("[WARN] ConfirmPendingTransfersByTxIds: tx %s has no spent UTXO", txId)
			continue
		}

		// 准备spent和created列表
		spentUtxo := transfer.spentUtxo
		// 恢复AmtChange为正数（mempool阶段可能设为负数）
		if spentUtxo.AmtChange.LessThan(decimal.Zero) {
			spentUtxo.AmtChange = spentUtxo.AmtChange.Neg()
		}
		spentUtxos := []*mrc20.Mrc20Utxo{&spentUtxo}

		var createdUtxos []*mrc20.Mrc20Utxo
		if transfer.receiverUtxo != nil {
			createdUtxos = []*mrc20.Mrc20Utxo{transfer.receiverUtxo}
		} else {
			log.Printf("[WARN] ConfirmPendingTransfersByTxIds: tx %s has no receiver UTXO", txId)
			continue
		}

		// 调用ProcessNativeTransferSuccess处理余额更新
		err := pd.ProcessNativeTransferSuccess(txId, chainName, blockHeight, spentUtxos, createdUtxos)
		if err != nil {
			log.Printf("[ERROR] ConfirmPendingTransfersByTxIds: ProcessNativeTransferSuccess failed for tx %s: %v", txId, err)
		} else {
			log.Printf("[INFO] ConfirmPendingTransfersByTxIds: confirmed transfer tx %s", txId)
		}
	}

	return nil
}
