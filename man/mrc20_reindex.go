package man

import (
	"fmt"
	"log"
	"man-p2p/mrc20"
	"strings"

	"github.com/bytedance/sonic"
	"github.com/cockroachdb/pebble"
	"github.com/shopspring/decimal"
)

// ============== 区块级索引 Key 格式 ==============
// block_created_{chain}_{height}_{txPoint} → 1  (该区块创建的UTXO)
// block_spent_{chain}_{height}_{txPoint} → 1    (该区块消耗的UTXO)

// ============== 标记 UTXO 为已消费（替代删除） ==============

// MarkUtxoAsSpent 标记 UTXO 为已消费（不删除，只更新状态）
// 同时写入 block_spent 索引用于回滚
func (pd *PebbleData) MarkUtxoAsSpent(txPoint, address, tickId, chain string, spentAtHeight int64) error {
	// 读取 UTXO
	utxo, err := pd.GetMrc20UtxoByTxPoint(txPoint, false)
	if err != nil || utxo == nil {
		log.Printf("[WARN] MarkUtxoAsSpent: UTXO not found: %s", txPoint)
		return nil // 不存在的 UTXO 直接跳过
	}

	// 已经标记为 spent 的跳过（幂等）
	if utxo.Status == mrc20.UtxoStatusSpent {
		return nil
	}

	batch := pd.Database.MrcDb.NewBatch()
	defer batch.Close()

	// 更新 UTXO 状态
	utxo.Status = mrc20.UtxoStatusSpent
	utxo.SpentAtHeight = spentAtHeight

	data, err := sonic.Marshal(utxo)
	if err != nil {
		return err
	}

	// 更新主记录
	mainKey := []byte(fmt.Sprintf("mrc20_utxo_%s", txPoint))
	batch.Set(mainKey, data, pebble.Sync)

	// 更新 mrc20_in 索引（保留但更新状态）
	if utxo.ToAddress != "" {
		inKey := []byte(fmt.Sprintf("mrc20_in_%s_%s_%s", utxo.ToAddress, utxo.Mrc20Id, txPoint))
		batch.Set(inKey, data, pebble.Sync)
	}

	// 删除 available_utxo 索引（spent 后不再可用）
	if chain != "" {
		availableKey := []byte(fmt.Sprintf("available_utxo_%s_%s_%s_%s", chain, address, tickId, txPoint))
		batch.Delete(availableKey, pebble.Sync)
	}

	// 写入 block_spent 索引（用于回滚）
	blockSpentKey := []byte(fmt.Sprintf("block_spent_%s_%d_%s", chain, spentAtHeight, txPoint))
	batch.Set(blockSpentKey, []byte("1"), pebble.Sync)

	return batch.Commit(pebble.Sync)
}

// ============== 区块索引操作 ==============

// SaveBlockCreatedUtxo 记录区块创建的 UTXO 索引
func (pd *PebbleData) SaveBlockCreatedUtxo(chain string, height int64, txPoint string) error {
	key := []byte(fmt.Sprintf("block_created_%s_%d_%s", chain, height, txPoint))
	return pd.Database.MrcDb.Set(key, []byte("1"), pebble.Sync)
}

// GetBlockCreatedUtxos 获取某区块创建的所有 UTXO TxPoint
func (pd *PebbleData) GetBlockCreatedUtxos(chain string, height int64) ([]string, error) {
	prefix := []byte(fmt.Sprintf("block_created_%s_%d_", chain, height))
	iter, err := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var txPoints []string
	for iter.First(); iter.Valid(); iter.Next() {
		// key 格式: block_created_{chain}_{height}_{txPoint}
		key := string(iter.Key())
		// 提取 txPoint 部分
		prefixLen := len(fmt.Sprintf("block_created_%s_%d_", chain, height))
		if len(key) > prefixLen {
			txPoint := key[prefixLen:]
			txPoints = append(txPoints, txPoint)
		}
	}
	return txPoints, nil
}

// GetBlockSpentUtxos 获取某区块消耗的所有 UTXO TxPoint
func (pd *PebbleData) GetBlockSpentUtxos(chain string, height int64) ([]string, error) {
	prefix := []byte(fmt.Sprintf("block_spent_%s_%d_", chain, height))
	iter, err := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var txPoints []string
	for iter.First(); iter.Valid(); iter.Next() {
		key := string(iter.Key())
		prefixLen := len(fmt.Sprintf("block_spent_%s_%d_", chain, height))
		if len(key) > prefixLen {
			txPoint := key[prefixLen:]
			txPoints = append(txPoints, txPoint)
		}
	}
	return txPoints, nil
}

// ============== 区块清理（回滚） ==============

// CleanBlock 清理某区块的所有 MRC20 数据（用于回滚或重跑）
// 返回受影响的地址列表（需要重算余额）
func (pd *PebbleData) CleanBlock(chain string, height int64) (affectedAddresses map[string]struct{}, err error) {
	affectedAddresses = make(map[string]struct{})

	// 1. 删除该区块创建的 UTXO
	createdTxPoints, err := pd.GetBlockCreatedUtxos(chain, height)
	if err != nil {
		return nil, fmt.Errorf("GetBlockCreatedUtxos error: %v", err)
	}

	for _, txPoint := range createdTxPoints {
		utxo, err := pd.GetMrc20UtxoByTxPoint(txPoint, false)
		if err != nil || utxo == nil {
			continue
		}

		// 记录受影响的地址
		if utxo.ToAddress != "" {
			affectedAddresses[fmt.Sprintf("%s_%s_%s", chain, utxo.ToAddress, utxo.Mrc20Id)] = struct{}{}
		}
		if utxo.FromAddress != "" {
			affectedAddresses[fmt.Sprintf("%s_%s_%s", chain, utxo.FromAddress, utxo.Mrc20Id)] = struct{}{}
		}

		// 删除 UTXO 及相关索引
		if err := pd.deleteUtxoCompletely(utxo); err != nil {
			log.Printf("[WARN] CleanBlock: deleteUtxoCompletely failed for %s: %v", txPoint, err)
		}

		// 删除 block_created 索引
		blockCreatedKey := []byte(fmt.Sprintf("block_created_%s_%d_%s", chain, height, txPoint))
		pd.Database.MrcDb.Delete(blockCreatedKey, pebble.Sync)
	}

	// 2. 恢复该区块消耗的 UTXO
	spentTxPoints, err := pd.GetBlockSpentUtxos(chain, height)
	if err != nil {
		return affectedAddresses, fmt.Errorf("GetBlockSpentUtxos error: %v", err)
	}

	for _, txPoint := range spentTxPoints {
		utxo, err := pd.GetMrc20UtxoByTxPoint(txPoint, false)
		if err != nil || utxo == nil {
			continue
		}

		// 记录受影响的地址
		if utxo.ToAddress != "" {
			affectedAddresses[fmt.Sprintf("%s_%s_%s", chain, utxo.ToAddress, utxo.Mrc20Id)] = struct{}{}
		}

		// 恢复 UTXO 状态
		if err := pd.restoreSpentUtxo(utxo); err != nil {
			log.Printf("[WARN] CleanBlock: restoreSpentUtxo failed for %s: %v", txPoint, err)
		}

		// 删除 block_spent 索引
		blockSpentKey := []byte(fmt.Sprintf("block_spent_%s_%d_%s", chain, height, txPoint))
		pd.Database.MrcDb.Delete(blockSpentKey, pebble.Sync)
	}

	// 3. 删除该区块的 Transaction 记录
	if err := pd.deleteBlockTransactions(chain, height); err != nil {
		log.Printf("[WARN] CleanBlock: deleteBlockTransactions failed: %v", err)
	}

	return affectedAddresses, nil
}

// deleteUtxoCompletely 完全删除 UTXO 及所有索引
func (pd *PebbleData) deleteUtxoCompletely(utxo *mrc20.Mrc20Utxo) error {
	batch := pd.Database.MrcDb.NewBatch()
	defer batch.Close()

	// 删除主记录
	mainKey := []byte(fmt.Sprintf("mrc20_utxo_%s", utxo.TxPoint))
	batch.Delete(mainKey, pebble.Sync)

	// 删除 mrc20_in 索引
	if utxo.ToAddress != "" {
		inKey := []byte(fmt.Sprintf("mrc20_in_%s_%s_%s", utxo.ToAddress, utxo.Mrc20Id, utxo.TxPoint))
		batch.Delete(inKey, pebble.Sync)
	}
	if utxo.FromAddress != "" && utxo.FromAddress != utxo.ToAddress {
		outKey := []byte(fmt.Sprintf("mrc20_in_%s_%s_%s", utxo.FromAddress, utxo.Mrc20Id, utxo.TxPoint))
		batch.Delete(outKey, pebble.Sync)
	}

	// 删除 available_utxo 索引
	if utxo.Chain != "" {
		availableKey := []byte(fmt.Sprintf("available_utxo_%s_%s_%s_%s", utxo.Chain, utxo.ToAddress, utxo.Mrc20Id, utxo.TxPoint))
		batch.Delete(availableKey, pebble.Sync)
	}

	return batch.Commit(pebble.Sync)
}

// restoreSpentUtxo 恢复已消费的 UTXO 为可用状态
func (pd *PebbleData) restoreSpentUtxo(utxo *mrc20.Mrc20Utxo) error {
	if utxo.Status != mrc20.UtxoStatusSpent {
		return nil // 非 spent 状态无需恢复
	}

	batch := pd.Database.MrcDb.NewBatch()
	defer batch.Close()

	// 恢复状态
	utxo.Status = mrc20.UtxoStatusAvailable
	utxo.SpentAtHeight = 0

	data, err := sonic.Marshal(utxo)
	if err != nil {
		return err
	}

	// 更新主记录
	mainKey := []byte(fmt.Sprintf("mrc20_utxo_%s", utxo.TxPoint))
	batch.Set(mainKey, data, pebble.Sync)

	// 更新 mrc20_in 索引
	if utxo.ToAddress != "" {
		inKey := []byte(fmt.Sprintf("mrc20_in_%s_%s_%s", utxo.ToAddress, utxo.Mrc20Id, utxo.TxPoint))
		batch.Set(inKey, data, pebble.Sync)
	}

	// 恢复 available_utxo 索引
	if utxo.Chain != "" && utxo.ToAddress != "" {
		availableKey := []byte(fmt.Sprintf("available_utxo_%s_%s_%s_%s", utxo.Chain, utxo.ToAddress, utxo.Mrc20Id, utxo.TxPoint))
		batch.Set(availableKey, data, pebble.Sync)
	}

	return batch.Commit(pebble.Sync)
}

// deleteBlockTransactions 删除某区块的所有 Transaction 记录
func (pd *PebbleData) deleteBlockTransactions(chain string, height int64) error {
	// 扫描 mrc20_tx_ 前缀，找到 BlockHeight 匹配的记录
	prefix := []byte("mrc20_tx_")
	iter, err := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return err
	}
	defer iter.Close()

	batch := pd.Database.MrcDb.NewBatch()
	defer batch.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		var tx mrc20.Mrc20Transaction
		if err := sonic.Unmarshal(iter.Value(), &tx); err != nil {
			continue
		}

		if tx.Chain == chain && tx.BlockHeight == height {
			batch.Delete(iter.Key(), pebble.Sync)
		}
	}

	return batch.Commit(pebble.Sync)
}

// ============== 余额重算 ==============

// RecalculateBalance 从 UTXO 重新计算余额
func (pd *PebbleData) RecalculateBalance(chain, address, tickId string) error {
	// 扫描 mrc20_in_{address}_{tickId}_ 前缀
	prefix := fmt.Sprintf("mrc20_in_%s_%s_", address, tickId)
	iter, err := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: []byte(prefix + "~"),
	})
	if err != nil {
		return err
	}
	defer iter.Close()

	balance := decimal.Zero
	utxoCount := 0
	var lastUpdateTx string
	var lastUpdateHeight int64
	var lastUpdateTime int64
	var tick string

	for iter.First(); iter.Valid(); iter.Next() {
		var utxo mrc20.Mrc20Utxo
		if err := sonic.Unmarshal(iter.Value(), &utxo); err != nil {
			continue
		}

		// 只统计该地址作为 ToAddress 且状态为可用的 UTXO
		if utxo.ToAddress == address && utxo.Status == mrc20.UtxoStatusAvailable {
			balance = balance.Add(utxo.AmtChange)
			utxoCount++
			tick = utxo.Tick

			// 记录最后更新信息
			if utxo.BlockHeight > lastUpdateHeight {
				lastUpdateHeight = utxo.BlockHeight
				lastUpdateTx = utxo.OperationTx
				lastUpdateTime = utxo.Timestamp
			}
		}
	}

	// 保存重算后的余额
	accountBalance := &mrc20.Mrc20AccountBalance{
		Address:          address,
		TickId:           tickId,
		Tick:             tick,
		Balance:          balance,
		Chain:            chain,
		UtxoCount:        utxoCount,
		LastUpdateTx:     lastUpdateTx,
		LastUpdateHeight: lastUpdateHeight,
		LastUpdateTime:   lastUpdateTime,
	}

	return pd.SaveMrc20AccountBalance(accountBalance)
}

// RecalculateBalances 批量重算余额
// addressKeys 格式: chain_address_tickId
func (pd *PebbleData) RecalculateBalances(addressKeys map[string]struct{}) error {
	for key := range addressKeys {
		// 解析 key: chain_address_tickId
		parts := splitAddressKey(key)
		if len(parts) != 3 {
			log.Printf("[WARN] RecalculateBalances: invalid key format: %s", key)
			continue
		}

		chain, address, tickId := parts[0], parts[1], parts[2]
		if err := pd.RecalculateBalance(chain, address, tickId); err != nil {
			log.Printf("[ERROR] RecalculateBalance failed for %s: %v", key, err)
			// 继续处理其他地址
		}
	}
	return nil
}

// splitAddressKey 分割地址 key (chain_address_tickId)
func splitAddressKey(key string) []string {
	// 找到第一个 _ 和最后一个 _ 来分割
	// 因为 address 和 tickId 都可能包含特殊字符
	firstUnderscore := -1
	lastUnderscore := -1
	for i := 0; i < len(key); i++ {
		if key[i] == '_' {
			if firstUnderscore == -1 {
				firstUnderscore = i
			}
			lastUnderscore = i
		}
	}

	if firstUnderscore == -1 || lastUnderscore == -1 || firstUnderscore == lastUnderscore {
		return nil
	}

	chain := key[:firstUnderscore]
	address := key[firstUnderscore+1 : lastUnderscore]
	tickId := key[lastUnderscore+1:]

	return []string{chain, address, tickId}
}

// ============== 幂等重跑 ==============

// ReindexBlock 幂等重跑某区块
// 1. 清理该区块的数据
// 2. 重新处理区块
// 3. 重算受影响地址的余额
func (pd *PebbleData) ReindexBlock(chain string, height int64) error {
	log.Printf("[REINDEX] Starting reindex for chain=%s, height=%d", chain, height)

	// Step 0: 先检查并修复该区块相关的 pending 状态 UTXO
	// 这是关键：如果 UTXO 是 TransferPending 状态，说明 mempool 处理过但区块确认丢失了
	// CleanBlock 之前需要先把它们恢复为 Available，这样 CleanBlock 才不会跳过
	pendingFixed := 0
	prefix := []byte("mrc20_utxo_")
	iter, iterErr := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if iterErr == nil {
		for iter.First(); iter.Valid(); iter.Next() {
			var utxo mrc20.Mrc20Utxo
			if err := sonic.Unmarshal(iter.Value(), &utxo); err != nil {
				continue
			}
			// 只处理指定链
			if utxo.Chain != chain {
				continue
			}
			// 找到 pending 状态且 BlockHeight 已确认的 UTXO（说明 mempool→block 转换出了问题）
			if (utxo.Status == mrc20.UtxoStatusTransferPending || utxo.Status == mrc20.UtxoStatusTeleportPending) &&
				utxo.BlockHeight > 0 && utxo.BlockHeight < height {
				// 恢复为 Available 状态，让 DoIndexerRun 可以重新处理
				log.Printf("[REINDEX] Fixing pending UTXO before reindex: %s, status=%d, blockHeight=%d",
					utxo.TxPoint, utxo.Status, utxo.BlockHeight)
				utxo.Status = mrc20.UtxoStatusAvailable
				data, _ := sonic.Marshal(&utxo)
				mainKey := []byte(fmt.Sprintf("mrc20_utxo_%s", utxo.TxPoint))
				pd.Database.MrcDb.Set(mainKey, data, pebble.Sync)
				if utxo.ToAddress != "" {
					inKey := []byte(fmt.Sprintf("mrc20_in_%s_%s_%s", utxo.ToAddress, utxo.Mrc20Id, utxo.TxPoint))
					pd.Database.MrcDb.Set(inKey, data, pebble.Sync)
				}
				pendingFixed++
			}
		}
		iter.Close()
	}
	if pendingFixed > 0 {
		log.Printf("[REINDEX] Fixed %d pending UTXOs before reindex", pendingFixed)
	}

	// Step 1: 清理该区块的数据
	affectedAddresses, err := pd.CleanBlock(chain, height)
	if err != nil {
		return fmt.Errorf("CleanBlock failed: %v", err)
	}
	log.Printf("[REINDEX] Cleaned block, affected addresses: %d", len(affectedAddresses))

	// Step 2: 重新处理区块（通过 DoIndexerRun）
	err = pd.DoIndexerRun(chain, height, true)
	if err != nil {
		return fmt.Errorf("DoIndexerRun failed: %v", err)
	}
	log.Printf("[REINDEX] Re-processed block")

	// Step 3: 重算受影响地址的余额
	if err := pd.RecalculateBalances(affectedAddresses); err != nil {
		log.Printf("[WARN] RecalculateBalances failed: %v", err)
	}
	log.Printf("[REINDEX] Recalculated balances")

	return nil
}

// ReindexBlockRange 重跑区块范围
func (pd *PebbleData) ReindexBlockRange(chain string, startHeight, endHeight int64) error {
	for h := startHeight; h <= endHeight; h++ {
		if err := pd.ReindexBlock(chain, h); err != nil {
			return fmt.Errorf("ReindexBlock failed at height %d: %v", h, err)
		}
	}
	return nil
}

// ReindexFromHeight 从指定高度重跑（真正幂等）
// 1. 删除所有 BlockHeight >= targetHeight 的 UTXO
// 2. 恢复所有 SpentAtHeight >= targetHeight 的 UTXO（从 Spent 改回 Available）
// 3. 清理余额缓存和 pending 记录
// 4. 设置索引高度为 targetHeight - 1
// 调用后需要重启服务让主循环从 targetHeight 开始重新索引
func (pd *PebbleData) ReindexFromHeight(chain string, targetHeight int64) (stats map[string]int, err error) {
	stats = map[string]int{
		"deleted":        0, // 删除的 UTXO（BlockHeight >= targetHeight）
		"restored":       0, // 恢复的 UTXO（SpentAtHeight >= targetHeight）
		"pendingFixed":   0, // 修复的 pending UTXO
		"balanceCleared": 0, // 清理的余额记录
	}

	log.Printf("[REINDEX_FROM] Starting reindex from height %d for chain %s", targetHeight, chain)

	// Step 1: 扫描所有 UTXO，根据 BlockHeight 和 SpentAtHeight 处理
	affectedAddresses := make(map[string]struct{})

	prefix := []byte("mrc20_utxo_")
	iter, iterErr := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if iterErr != nil {
		return stats, fmt.Errorf("failed to create iterator: %v", iterErr)
	}

	// 收集要删除和修改的 key
	var toDelete [][]byte
	var toUpdate []struct {
		key   []byte
		value []byte
		utxo  *mrc20.Mrc20Utxo
	}

	for iter.First(); iter.Valid(); iter.Next() {
		var utxo mrc20.Mrc20Utxo
		if err := sonic.Unmarshal(iter.Value(), &utxo); err != nil {
			continue
		}

		// 只处理指定链
		if utxo.Chain != chain {
			continue
		}

		// 情况 1: UTXO 是在 targetHeight 或之后创建的 → 删除
		if utxo.BlockHeight >= targetHeight {
			key := make([]byte, len(iter.Key()))
			copy(key, iter.Key())
			toDelete = append(toDelete, key)

			// 记录受影响的地址
			if utxo.ToAddress != "" {
				affectedAddresses[fmt.Sprintf("%s_%s_%s", chain, utxo.ToAddress, utxo.Mrc20Id)] = struct{}{}
			}
			if utxo.FromAddress != "" {
				affectedAddresses[fmt.Sprintf("%s_%s_%s", chain, utxo.FromAddress, utxo.Mrc20Id)] = struct{}{}
			}
			continue
		}

		// 情况 2: UTXO 是在 targetHeight 或之后被消费的 → 恢复为 Available
		if utxo.SpentAtHeight >= targetHeight && utxo.Status == mrc20.UtxoStatusSpent {
			utxo.Status = mrc20.UtxoStatusAvailable
			utxo.SpentAtHeight = 0
			data, _ := sonic.Marshal(&utxo)

			key := make([]byte, len(iter.Key()))
			copy(key, iter.Key())

			utxoCopy := utxo // 拷贝一份
			toUpdate = append(toUpdate, struct {
				key   []byte
				value []byte
				utxo  *mrc20.Mrc20Utxo
			}{key, data, &utxoCopy})

			// 记录受影响的地址
			if utxo.ToAddress != "" {
				affectedAddresses[fmt.Sprintf("%s_%s_%s", chain, utxo.ToAddress, utxo.Mrc20Id)] = struct{}{}
			}
			continue
		}

		// 情况 3: UTXO 是 pending 状态但 BlockHeight 已确认 → 恢复为 Available
		// （mempool 处理后重启导致的状态不一致）
		if (utxo.Status == mrc20.UtxoStatusTransferPending || utxo.Status == mrc20.UtxoStatusTeleportPending) &&
			utxo.BlockHeight > 0 && utxo.BlockHeight < targetHeight {
			utxo.Status = mrc20.UtxoStatusAvailable
			data, _ := sonic.Marshal(&utxo)

			key := make([]byte, len(iter.Key()))
			copy(key, iter.Key())

			utxoCopy := utxo
			toUpdate = append(toUpdate, struct {
				key   []byte
				value []byte
				utxo  *mrc20.Mrc20Utxo
			}{key, data, &utxoCopy})

			if utxo.ToAddress != "" {
				affectedAddresses[fmt.Sprintf("%s_%s_%s", chain, utxo.ToAddress, utxo.Mrc20Id)] = struct{}{}
			}
			stats["pendingFixed"]++
		}
	}
	iter.Close()

	// Step 2: 执行删除
	for _, key := range toDelete {
		// 解析出 txPoint
		keyStr := string(key)
		if len(keyStr) > len("mrc20_utxo_") {
			txPoint := keyStr[len("mrc20_utxo_"):]

			// 获取 UTXO 详情（为了删除相关索引）
			utxo, _ := pd.GetMrc20UtxoByTxPoint(txPoint, false)
			if utxo != nil {
				// 删除主记录
				pd.Database.MrcDb.Delete(key, pebble.Sync)

				// 删除 mrc20_in 索引
				if utxo.ToAddress != "" {
					inKey := []byte(fmt.Sprintf("mrc20_in_%s_%s_%s", utxo.ToAddress, utxo.Mrc20Id, txPoint))
					pd.Database.MrcDb.Delete(inKey, pebble.Sync)
				}

				// 删除 available_utxo 索引
				if utxo.ToAddress != "" {
					availableKey := []byte(fmt.Sprintf("available_utxo_%s_%s_%s_%s", chain, utxo.ToAddress, utxo.Mrc20Id, txPoint))
					pd.Database.MrcDb.Delete(availableKey, pebble.Sync)
				}

				// 删除 block_created 索引
				if utxo.BlockHeight > 0 {
					blockCreatedKey := []byte(fmt.Sprintf("block_created_%s_%d_%s", chain, utxo.BlockHeight, txPoint))
					pd.Database.MrcDb.Delete(blockCreatedKey, pebble.Sync)
				}

				stats["deleted"]++
			}
		}
	}

	// Step 3: 执行更新（恢复）
	for _, item := range toUpdate {
		// 更新主记录
		pd.Database.MrcDb.Set(item.key, item.value, pebble.Sync)

		// 更新 mrc20_in 索引
		if item.utxo.ToAddress != "" {
			inKey := []byte(fmt.Sprintf("mrc20_in_%s_%s_%s", item.utxo.ToAddress, item.utxo.Mrc20Id, item.utxo.TxPoint))
			pd.Database.MrcDb.Set(inKey, item.value, pebble.Sync)
		}

		// 添加 available_utxo 索引（因为恢复为 Available）
		if item.utxo.ToAddress != "" && item.utxo.Status == mrc20.UtxoStatusAvailable {
			availableKey := []byte(fmt.Sprintf("available_utxo_%s_%s_%s_%s", chain, item.utxo.ToAddress, item.utxo.Mrc20Id, item.utxo.TxPoint))
			pd.Database.MrcDb.Set(availableKey, []byte("1"), pebble.Sync)
		}

		// 删除 block_spent 索引（因为不再是 spent）
		if item.utxo.SpentAtHeight == 0 {
			// 需要扫描删除可能的 block_spent 记录
			// 由于我们不知道原来的 SpentAtHeight，先跳过这个清理
		}

		stats["restored"]++
	}

	// Step 4: 清理 block_created 和 block_spent 索引（>= targetHeight）
	for h := targetHeight; h <= targetHeight+10000; h++ {
		// 删除 block_created
		createdPrefix := []byte(fmt.Sprintf("block_created_%s_%d_", chain, h))
		createdIter, _ := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
			LowerBound: createdPrefix,
			UpperBound: append(createdPrefix, 0xff),
		})
		if createdIter != nil {
			hasData := false
			for createdIter.First(); createdIter.Valid(); createdIter.Next() {
				hasData = true
				key := make([]byte, len(createdIter.Key()))
				copy(key, createdIter.Key())
				pd.Database.MrcDb.Delete(key, pebble.Sync)
			}
			createdIter.Close()
			if !hasData && h > targetHeight+100 {
				break // 没有更多数据了
			}
		}

		// 删除 block_spent
		spentPrefix := []byte(fmt.Sprintf("block_spent_%s_%d_", chain, h))
		spentIter, _ := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
			LowerBound: spentPrefix,
			UpperBound: append(spentPrefix, 0xff),
		})
		if spentIter != nil {
			for spentIter.First(); spentIter.Valid(); spentIter.Next() {
				key := make([]byte, len(spentIter.Key()))
				copy(key, spentIter.Key())
				pd.Database.MrcDb.Delete(key, pebble.Sync)
			}
			spentIter.Close()
		}
	}

	// Step 5: 清理 Transaction 流水记录（>= targetHeight）
	txPrefix := []byte(fmt.Sprintf("tx_%s_", chain))
	txIter, _ := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: txPrefix,
		UpperBound: append(txPrefix, 0xff),
	})
	if txIter != nil {
		for txIter.First(); txIter.Valid(); txIter.Next() {
			var tx mrc20.Mrc20Transaction
			if err := sonic.Unmarshal(txIter.Value(), &tx); err != nil {
				continue
			}
			if tx.BlockHeight >= targetHeight {
				key := make([]byte, len(txIter.Key()))
				copy(key, txIter.Key())
				pd.Database.MrcDb.Delete(key, pebble.Sync)
			}
		}
		txIter.Close()
	}

	// Step 5.1: 清理 PendingTeleport 表
	// 删除源链或目标链等于 chain 且 BlockHeight >= targetHeight 或 status=0 的记录
	log.Printf("[REINDEX_FROM] Cleaning PendingTeleport table for chain %s", chain)
	pendingTeleportCleared := 0
	pendingTeleportPrefix := []byte("pending_teleport_")
	pendingTeleportIter, _ := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: pendingTeleportPrefix,
		UpperBound: []byte("pending_teleport_~"),
	})
	if pendingTeleportIter != nil {
		for pendingTeleportIter.First(); pendingTeleportIter.Valid(); pendingTeleportIter.Next() {
			keyStr := string(pendingTeleportIter.Key())
			// 跳过 coord 索引
			if strings.HasPrefix(keyStr, "pending_teleport_coord_") {
				continue
			}
			var pending mrc20.PendingTeleport
			if err := sonic.Unmarshal(pendingTeleportIter.Value(), &pending); err != nil {
				continue
			}
			// V1 PendingTeleport structure - deprecated
			// V2 doesn't store BlockHeight or Status in the same way
			// Skip cleanup of V1 pending teleports during reindex
			// They will be cleaned up by CleanExpiredPendingTeleports()
			log.Printf("[Reindex] Skipping V1 PendingTeleport cleanup: coord=%s (use V2 cleanup)", pending.Coord)
		}
		pendingTeleportIter.Close()
	}
	stats["pendingTeleportCleared"] = pendingTeleportCleared
	log.Printf("[REINDEX_FROM] Cleared %d PendingTeleport records", pendingTeleportCleared)

	// Step 5.2: 清理 Arrival 表
	// 删除 Chain 等于 chain 且 BlockHeight >= targetHeight 或 status=0 的记录
	log.Printf("[REINDEX_FROM] Cleaning Arrival table for chain %s", chain)
	arrivalCleared := 0
	arrivalPrefix := []byte("arrival_")
	arrivalIter, _ := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: arrivalPrefix,
		UpperBound: []byte("arrival_~"),
	})
	if arrivalIter != nil {
		for arrivalIter.First(); arrivalIter.Valid(); arrivalIter.Next() {
			keyStr := string(arrivalIter.Key())
			// 跳过索引 key
			if strings.HasPrefix(keyStr, "arrival_asset_") || strings.HasPrefix(keyStr, "arrival_pending_") {
				continue
			}
			var arrival mrc20.Mrc20Arrival
			if err := sonic.Unmarshal(arrivalIter.Value(), &arrival); err != nil {
				continue
			}
			// 如果属于当前链，且 BlockHeight >= targetHeight 或 status=pending
			if arrival.Chain == chain && (arrival.BlockHeight >= targetHeight || arrival.Status == 0) {
				// 删除主记录
				pd.Database.MrcDb.Delete(arrivalIter.Key(), pebble.Sync)
				// 删除 asset 索引
				assetKey := fmt.Sprintf("arrival_asset_%s", arrival.AssetOutpoint)
				pd.Database.MrcDb.Delete([]byte(assetKey), pebble.Sync)
				// 删除 pending 索引
				pendingKey := fmt.Sprintf("arrival_pending_%s_%s_%s", arrival.Chain, arrival.TickId, arrival.PinId)
				pd.Database.MrcDb.Delete([]byte(pendingKey), pebble.Sync)
				arrivalCleared++
			}
		}
		arrivalIter.Close()
	}
	stats["arrivalCleared"] = arrivalCleared
	log.Printf("[REINDEX_FROM] Cleared %d Arrival records", arrivalCleared)

	// Step 5.3: 清理 TeleportPendingIn 表
	// 删除 Chain 等于 chain 且 BlockHeight >= targetHeight 或 BlockHeight == -1 的记录
	log.Printf("[REINDEX_FROM] Cleaning TeleportPendingIn table for chain %s", chain)
	teleportPendingInCleared := 0
	teleportPendingInPrefix := []byte("teleport_pending_in_")
	teleportPendingInIter, _ := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: teleportPendingInPrefix,
		UpperBound: []byte("teleport_pending_in_~"),
	})
	if teleportPendingInIter != nil {
		for teleportPendingInIter.First(); teleportPendingInIter.Valid(); teleportPendingInIter.Next() {
			keyStr := string(teleportPendingInIter.Key())
			// 跳过 addr 索引
			if strings.HasPrefix(keyStr, "teleport_pending_in_addr_") {
				continue
			}
			var pendingIn mrc20.TeleportPendingIn
			if err := sonic.Unmarshal(teleportPendingInIter.Value(), &pendingIn); err != nil {
				continue
			}
			// 如果属于当前链（目标链），且 BlockHeight >= targetHeight 或 BlockHeight == -1（mempool）
			if pendingIn.Chain == chain && (pendingIn.BlockHeight >= targetHeight || pendingIn.BlockHeight == -1) {
				// 删除主记录
				pd.Database.MrcDb.Delete(teleportPendingInIter.Key(), pebble.Sync)
				// 删除 addr 索引
				addrKey := fmt.Sprintf("teleport_pending_in_addr_%s_%s", pendingIn.ToAddress, pendingIn.Coord)
				pd.Database.MrcDb.Delete([]byte(addrKey), pebble.Sync)
				teleportPendingInCleared++
			}
		}
		teleportPendingInIter.Close()
	}
	stats["teleportPendingInCleared"] = teleportPendingInCleared
	log.Printf("[REINDEX_FROM] Cleared %d TeleportPendingIn records", teleportPendingInCleared)

	// Step 5.4: 清理 TransferPendingIn 表
	// 删除 Chain 等于 chain 且 BlockHeight >= targetHeight 或 BlockHeight == -1 的记录
	log.Printf("[REINDEX_FROM] Cleaning TransferPendingIn table for chain %s", chain)
	transferPendingInCleared := 0
	transferPendingInPrefix := []byte("transfer_pending_in_")
	transferPendingInIter, _ := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: transferPendingInPrefix,
		UpperBound: []byte("transfer_pending_in_~"),
	})
	if transferPendingInIter != nil {
		for transferPendingInIter.First(); transferPendingInIter.Valid(); transferPendingInIter.Next() {
			keyStr := string(transferPendingInIter.Key())
			// 跳过 addr 索引
			if strings.HasPrefix(keyStr, "transfer_pending_in_addr_") {
				continue
			}
			var pendingIn mrc20.TransferPendingIn
			if err := sonic.Unmarshal(transferPendingInIter.Value(), &pendingIn); err != nil {
				continue
			}
			// 如果属于当前链，且 BlockHeight >= targetHeight 或 BlockHeight == -1（mempool）
			if pendingIn.Chain == chain && (pendingIn.BlockHeight >= targetHeight || pendingIn.BlockHeight == -1) {
				// 删除主记录
				pd.Database.MrcDb.Delete(transferPendingInIter.Key(), pebble.Sync)
				// 删除 addr 索引
				addrKey := fmt.Sprintf("transfer_pending_in_addr_%s_%s", pendingIn.ToAddress, pendingIn.TxPoint)
				pd.Database.MrcDb.Delete([]byte(addrKey), pebble.Sync)
				transferPendingInCleared++
			}
		}
		transferPendingInIter.Close()
	}
	stats["transferPendingInCleared"] = transferPendingInCleared
	log.Printf("[REINDEX_FROM] Cleared %d TransferPendingIn records", transferPendingInCleared)

	// Step 6: 重算受影响地址的余额
	log.Printf("[REINDEX_FROM] Recalculating balances for %d addresses", len(affectedAddresses))
	for addrKey := range affectedAddresses {
		parts := splitAddressKey(addrKey)
		if len(parts) == 3 {
			pd.RecalculateBalance(parts[0], parts[1], parts[2])
			stats["balanceCleared"]++
		}
	}

	// Step 7: 设置索引高度为 targetHeight - 1
	newHeight := targetHeight - 1
	if newHeight < 0 {
		newHeight = 0
	}
	pd.SaveMrc20IndexHeight(chain, newHeight)

	log.Printf("[REINDEX_FROM] Completed: deleted=%d, restored=%d, pendingFixed=%d, balanceCleared=%d, "+
		"pendingTeleportCleared=%d, arrivalCleared=%d, teleportPendingInCleared=%d, transferPendingInCleared=%d, newHeight=%d",
		stats["deleted"], stats["restored"], stats["pendingFixed"], stats["balanceCleared"],
		stats["pendingTeleportCleared"], stats["arrivalCleared"], stats["teleportPendingInCleared"],
		stats["transferPendingInCleared"], newHeight)

	return stats, nil
}

// ============== 验证工具 ==============

// VerifyBalance 验证余额是否正确（缓存与 UTXO 一致）
func (pd *PebbleData) VerifyBalance(chain, address, tickId string) (bool, error) {
	// 从缓存获取余额
	cachedBalance, err := pd.GetMrc20AccountBalance(chain, address, tickId)
	if err != nil {
		return false, err
	}

	// 从 UTXO 计算余额
	calculatedBalance := decimal.Zero
	prefix := fmt.Sprintf("mrc20_in_%s_%s_", address, tickId)
	iter, err := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: []byte(prefix + "~"),
	})
	if err != nil {
		return false, err
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		var utxo mrc20.Mrc20Utxo
		if err := sonic.Unmarshal(iter.Value(), &utxo); err != nil {
			continue
		}

		if utxo.ToAddress == address && utxo.Status == mrc20.UtxoStatusAvailable {
			calculatedBalance = calculatedBalance.Add(utxo.AmtChange)
		}
	}

	// 比较
	if cachedBalance == nil {
		return calculatedBalance.IsZero(), nil
	}

	return cachedBalance.Balance.Equal(calculatedBalance), nil
}

// ============== 启动时修复 Pending UTXO 状态 ==============

// FixPendingUtxoStatus 修复 TransferPending/TeleportPending 状态的 UTXO
// 在启动时调用，检查 pending 状态的 UTXO 对应的交易是否已经确认
// 如果已确认，将输入 UTXO 的状态更新为 Spent
func (pd *PebbleData) FixPendingUtxoStatus(chainName string) (int, error) {
	fixedCount := 0

	// 收集所有 pending 状态的 UTXO 和它们的 OperationTx
	pendingUtxos := make(map[string]*mrc20.Mrc20Utxo) // txPoint -> utxo
	operationTxMap := make(map[string][]string)       // operationTx -> []inputTxPoint

	// 扫描所有 UTXO
	prefix := []byte("mrc20_utxo_")
	iter, err := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return 0, err
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		var utxo mrc20.Mrc20Utxo
		if err := sonic.Unmarshal(iter.Value(), &utxo); err != nil {
			continue
		}

		// 只处理指定链的 UTXO
		if utxo.Chain != chainName {
			continue
		}

		// 只处理 TransferPending 或 TeleportPending 状态的 UTXO
		if utxo.Status != mrc20.UtxoStatusTransferPending && utxo.Status != mrc20.UtxoStatusTeleportPending {
			continue
		}

		// OperationTx 是花费这个 UTXO 的交易
		if utxo.OperationTx == "" {
			continue
		}

		pendingUtxos[utxo.TxPoint] = &utxo
		operationTxMap[utxo.OperationTx] = append(operationTxMap[utxo.OperationTx], utxo.TxPoint)
	}

	// 对于每个 OperationTx，检查其输出 UTXO 是否已确认
	for operationTx, inputTxPoints := range operationTxMap {
		// 查找这笔交易的输出 UTXO（index 0）
		outputTxPoint := fmt.Sprintf("%s:0", operationTx)
		outputUtxo, err := pd.GetMrc20UtxoByTxPoint(outputTxPoint, false)
		if err != nil || outputUtxo == nil {
			// 找不到输出 UTXO，可能交易还在 mempool
			continue
		}

		// 如果输出 UTXO 的 BlockHeight > 0，说明交易已确认
		if outputUtxo.BlockHeight > 0 {
			// 收集发送方和接收方 UTXO
			var spentUtxos []*mrc20.Mrc20Utxo
			for _, inputTxPoint := range inputTxPoints {
				inputUtxo := pendingUtxos[inputTxPoint]
				if inputUtxo == nil {
					continue
				}
				// 恢复 AmtChange 为正数
				utxoCopy := *inputUtxo
				if utxoCopy.AmtChange.LessThan(decimal.Zero) {
					utxoCopy.AmtChange = utxoCopy.AmtChange.Neg()
				}
				spentUtxos = append(spentUtxos, &utxoCopy)
			}

			if len(spentUtxos) == 0 {
				continue
			}

			log.Printf("[MRC20] FixPendingUtxoStatus: confirming transfer tx=%s, spent=%d, outputHeight=%d",
				operationTx, len(spentUtxos), outputUtxo.BlockHeight)

			// 调用 ProcessNativeTransferSuccess 更新余额
			createdUtxos := []*mrc20.Mrc20Utxo{outputUtxo}
			err := pd.ProcessNativeTransferSuccess(operationTx, chainName, outputUtxo.BlockHeight, spentUtxos, createdUtxos)
			if err != nil {
				log.Printf("[ERROR] FixPendingUtxoStatus: ProcessNativeTransferSuccess failed for %s: %v", operationTx, err)
				continue
			}

			fixedCount += len(spentUtxos)

			// 删除相关的 TransferPendingIn 记录
			for _, inputTxPoint := range inputTxPoints {
				inputUtxo := pendingUtxos[inputTxPoint]
				if inputUtxo != nil {
					pd.DeleteTransferPendingIn(inputTxPoint, inputUtxo.FromAddress)
				}
			}
		}
	}

	return fixedCount, nil
}
