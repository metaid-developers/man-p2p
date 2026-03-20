package man

import (
	"fmt"
	"log"
	"man-p2p/mrc20"
	"man-p2p/pin"
	"time"

	"github.com/bytedance/sonic"
	"github.com/cockroachdb/pebble"
)

// SaveTeleportTransaction 保存 TeleportTransaction
func SaveTeleportTransaction(tx *mrc20.TeleportTransaction) error {
	if tx == nil {
		return fmt.Errorf("tx is nil")
	}

	data, err := sonic.Marshal(tx)
	if err != nil {
		return fmt.Errorf("marshal failed: %w", err)
	}

	key := fmt.Sprintf("teleport_tx_v2_%s", tx.ID)
	err = PebbleStore.Database.MrcDb.Set([]byte(key), data, pebble.Sync)
	if err != nil {
		return fmt.Errorf("save failed: %w", err)
	}

	return nil
}

// LoadTeleportTransaction 加载 TeleportTransaction
func LoadTeleportTransaction(teleportID string) (*mrc20.TeleportTransaction, error) {
	key := fmt.Sprintf("teleport_tx_v2_%s", teleportID)
	value, closer, err := PebbleStore.Database.MrcDb.Get([]byte(key))
	if err != nil {
		return nil, err
	}
	defer closer.Close()

	var tx mrc20.TeleportTransaction
	if err := sonic.Unmarshal(value, &tx); err != nil {
		return nil, fmt.Errorf("unmarshal failed: %w", err)
	}

	return &tx, nil
}

// DeleteTeleportTransaction 删除 TeleportTransaction
func DeleteTeleportTransaction(teleportID string) error {
	key := fmt.Sprintf("teleport_tx_v2_%s", teleportID)
	return PebbleStore.Database.MrcDb.Delete([]byte(key), pebble.Sync)
}

// ListPendingTeleportTransactions 列出所有待处理的 TeleportTransaction
func ListPendingTeleportTransactions() ([]*mrc20.TeleportTransaction, error) {
	prefix := []byte("teleport_tx_v2_")
	iter, err := PebbleStore.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var result []*mrc20.TeleportTransaction
	for iter.First(); iter.Valid(); iter.Next() {
		var tx mrc20.TeleportTransaction
		if err := sonic.Unmarshal(iter.Value(), &tx); err != nil {
			continue
		}

		// 只返回未完成的
		if !mrc20.IsTerminalState(tx.State) {
			result = append(result, &tx)
		}
	}

	return result, nil
}

// ListAllTeleportTransactions 列出所有 TeleportTransaction（包括已完成）
func ListAllTeleportTransactions(limit int) ([]*mrc20.TeleportTransaction, error) {
	prefix := []byte("teleport_tx_v2_")
	iter, err := PebbleStore.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var result []*mrc20.TeleportTransaction
	count := 0
	for iter.First(); iter.Valid(); iter.Next() {
		if limit > 0 && count >= limit {
			break
		}

		var tx mrc20.TeleportTransaction
		if err := sonic.Unmarshal(iter.Value(), &tx); err != nil {
			continue
		}

		result = append(result, &tx)
		count++
	}

	return result, nil
}

// GetTeleportTransactionByCoord 通过 Coord 查找 TeleportTransaction
func GetTeleportTransactionByCoord(coord string) (*mrc20.TeleportTransaction, error) {
	// 遍历所有 TeleportTransaction，查找匹配的 Coord
	// 注意：这个查询效率较低，如果需要频繁使用，应该建立索引
	prefix := []byte("teleport_tx_v2_")
	iter, err := PebbleStore.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		var tx mrc20.TeleportTransaction
		if err := sonic.Unmarshal(iter.Value(), &tx); err != nil {
			continue
		}

		if tx.Coord == coord {
			return &tx, nil
		}
	}

	return nil, fmt.Errorf("teleport transaction not found for coord: %s", coord)
}

// RetryStuckTeleports 重试所有卡住的 Teleport
// 这个函数在每个区块处理后调用
func RetryStuckTeleports() error {
	pendingTxs, err := ListPendingTeleportTransactions()
	if err != nil {
		return err
	}

	for _, tx := range pendingTxs {
		// 检查是否应该重试
		if !tx.ShouldRetry() {
			continue
		}

		// 检查锁
		if tx.IsLocked() {
			continue
		}

		// 尝试获取锁
		if !tx.AcquireLock(ProcessID, 5*time.Minute) {
			continue
		}

		// 增加重试计数
		tx.RetryCount++
		tx.LastRetryAt = time.Now().Unix()

		// 重新执行状态机
		// 注意：这里需要重新加载 PIN 数据，暂时跳过
		// TODO: 实现完整的重试逻辑

		tx.ReleaseLock(ProcessID)
		SaveTeleportTransaction(tx)
	}

	return nil
}

// ============ PendingTeleport Storage ============

// SavePendingTeleport 保存等待配对的 Teleport
func SavePendingTeleport(pending *mrc20.PendingTeleport) error {
	if pending == nil {
		return fmt.Errorf("pending is nil")
	}

	data, err := sonic.Marshal(pending)
	if err != nil {
		return fmt.Errorf("marshal failed: %w", err)
	}

	key := fmt.Sprintf("pending_teleport_%s", pending.Coord)
	err = PebbleStore.Database.MrcDb.Set([]byte(key), data, pebble.Sync)
	if err != nil {
		return fmt.Errorf("save pending teleport failed: %w", err)
	}

	return nil
}

// GetPendingTeleport 获取等待配对的 Teleport
func GetPendingTeleport(coord string) (*mrc20.PendingTeleport, error) {
	key := fmt.Sprintf("pending_teleport_%s", coord)
	value, closer, err := PebbleStore.Database.MrcDb.Get([]byte(key))
	if err != nil {
		return nil, err
	}
	defer closer.Close()

	var pending mrc20.PendingTeleport
	if err := sonic.Unmarshal(value, &pending); err != nil {
		return nil, fmt.Errorf("unmarshal failed: %w", err)
	}

	return &pending, nil
}

// DeletePendingTeleport 删除等待配对的 Teleport
func DeletePendingTeleport(coord string) error {
	key := fmt.Sprintf("pending_teleport_%s", coord)
	return PebbleStore.Database.MrcDb.Delete([]byte(key), pebble.Sync)
}

// ListAllPendingTeleports 列出所有等待配对的 Teleport
func ListAllPendingTeleports() ([]*mrc20.PendingTeleport, error) {
	prefix := []byte("pending_teleport_")
	iter, err := PebbleStore.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var result []*mrc20.PendingTeleport
	for iter.First(); iter.Valid(); iter.Next() {
		var pending mrc20.PendingTeleport
		if err := sonic.Unmarshal(iter.Value(), &pending); err != nil {
			continue
		}
		result = append(result, &pending)
	}

	return result, nil
}

// CleanExpiredPendingTeleports 清理过期的 Pending Teleport
func CleanExpiredPendingTeleports() error {
	allPending, err := ListAllPendingTeleports()
	if err != nil {
		return err
	}

	now := time.Now().Unix()
	for _, pending := range allPending {
		if pending.ExpireAt > 0 && now > pending.ExpireAt {
			DeletePendingTeleport(pending.Coord)
		}
	}

	return nil
}

// RetryPendingTeleports 定期扫描并重试 Pending Teleport（兜底机制）
// 检查是否有等待的transfer可以与已到达的arrival配对
func RetryPendingTeleports() error {
	allPending, err := ListAllPendingTeleports()
	if err != nil {
		return err
	}

	for _, pending := range allPending {
		// 检查重试间隔（避免频繁重试）
		now := time.Now().Unix()
		if pending.LastCheckAt > 0 && now-pending.LastCheckAt < 60 {
			// 60秒内已检查过，跳过
			continue
		}

		// 检查是否已过期
		if pending.ExpireAt > 0 && now > pending.ExpireAt {
			log.Printf("[RetryPending] ⏰ Expired: coord=%s, deleting...", pending.Coord)
			DeletePendingTeleport(pending.Coord)
			continue
		}

		// 只处理transfer类型（arrival类型暂不需要重试）
		if pending.Type != "transfer" {
			continue
		}

		// 检查对应的arrival是否已到达
		arrival, err := PebbleStore.GetMrc20ArrivalByPinId(pending.Coord)
		if err != nil {
			// Arrival还未到达，继续等待
			continue
		}
		_ = arrival // Suppress unused variable warning

		log.Printf("[RetryPending] ✅ Found arrival for pending transfer: coord=%s, retrying...", pending.Coord)

		// 反序列化pinNode
		var pinNode pin.PinInscription
		err = sonic.Unmarshal([]byte(pending.PinNodeJson), &pinNode)
		if err != nil {
			log.Printf("[RetryPending] ❌ Failed to unmarshal pinNode: %v", err)
			DeletePendingTeleport(pending.Coord)
			continue
		}

		// 重新触发处理
		isMempool := pinNode.GenesisHeight <= 0
		err = ProcessTeleportV2(&pinNode, pending.Data, isMempool)
		if err != nil {
			log.Printf("[RetryPending] ⚠️  Retry failed: %v, will retry later", err)
			// 更新重试计数
			pending.RetryCount++
			pending.LastCheckAt = now
			SavePendingTeleport(pending)
			continue
		}

		// 成功，删除pending记录
		DeletePendingTeleport(pending.Coord)
		log.Printf("[RetryPending] ✅ Successfully processed: coord=%s", pending.Coord)
	}

	return nil
}
