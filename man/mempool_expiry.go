package man

import (
	"context"
	"log"
	"man-p2p/pebblestore"
	"time"
)

// mempool 生命周期治理（R1）：
// - 链源节点在区块确认时会走 DeleteMempoolData 清理；p2p-only 节点没有这条
//   路径，mempool 库只增不减（生产节点 2 已堆积 19GB）。
// - SetMempool 现在同步写 mpexp_ 反向时间戳索引；本 sweep 从最旧端开始删除
//   超过 TTL 的条目，碰到未过期条目即停——每轮开销 O(过期数)，与库存无关。
// - 索引孤儿（确认清理只删数据不删索引）在过期时一并移除。
// - 存量数据（无索引条目）由 tools/mempool_expiry_backfill 离线回补并即时
//   清理过期，避免在线全量扫描。

const (
	MempoolExpiryTTL              = 72 * time.Hour
	mempoolExpirySweepInterval    = 10 * time.Minute
	mempoolExpiryMaxDeletesPerRun = 100000
)

// MempoolExpirySweepRun 周期清理 mempool 过期条目；随 app 启动常驻。
func MempoolExpirySweepRun(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	ticker := time.NewTicker(mempoolExpirySweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			deleted := SweepMempoolExpiryOnce()
			if deleted > 0 {
				log.Printf("[MempoolExpiry] deleted %d expired mempool pins (ttl=%s)", deleted, MempoolExpiryTTL)
			}
		}
	}
}

// SweepMempoolExpiryOnce 执行一轮过期清理，返回删除的 pin 条数。
func SweepMempoolExpiryOnce() int {
	return sweepMempoolExpiryCutoff(time.Now().Add(-MempoolExpiryTTL).Unix())
}

// sweepMempoolExpiryCutoff 从索引最旧端（反向时间戳最大）向后删除
// sortTime < cutoff 的条目；数据与索引键一起删；未过期即停。
func sweepMempoolExpiryCutoff(cutoff int64) int {
	db := PebbleStore.Database
	if db == nil {
		return 0
	}

	it, err := db.MempoolExpiryDb.NewIter(nil)
	if err != nil {
		return 0
	}

	var pinIdsToDelete []string
	var indexKeysToDelete []string
	deleted := 0
	for it.Last(); it.Valid(); it.Prev() { // 降序 = 最旧在前
		sortTime, pinId, ok := pebblestore.ParseMempoolExpiryKey(it.Key())
		if !ok {
			continue
		}
		if sortTime >= cutoff {
			break // 之后的都是新数据
		}
		indexKeysToDelete = append(indexKeysToDelete, string(it.Key()))
		if _, closer, err := db.PinsMempoolDb.Get([]byte(pinId)); err == nil {
			closer.Close()
			pinIdsToDelete = append(pinIdsToDelete, pinId)
		}
		deleted++
		if len(indexKeysToDelete) >= mempoolExpiryMaxDeletesPerRun {
			break // 单轮有界，剩余下轮继续
		}
	}
	it.Close()

	if len(pinIdsToDelete) > 0 {
		if err := db.BatchDeleteMempool(pinIdsToDelete); err != nil {
			log.Printf("[MempoolExpiry] batch delete pins failed: %v", err)
			return 0
		}
	}
	if len(indexKeysToDelete) > 0 {
		if err := pebblestore.DeleteBatchByKeyList(db.MempoolExpiryDb, &indexKeysToDelete); err != nil {
			log.Printf("[MempoolExpiry] batch delete index failed: %v", err)
			return 0
		}
	}
	return deleted
}
