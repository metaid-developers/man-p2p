// mempool_expiry_backfill 为存量 mempool 条目回补 mpexp_ 过期索引
// （独立库 mempool_expiry/db），并可立即删除超过 TTL 的条目。
//
// 背景：mpexp_ 反向时间戳索引自 R1 起由 SetMempool 同步写入；此前的存量
// 数据没有索引条目，在线 sweep 只能清理新写入，历史堆积（如生产节点 2
// 的 19GB）需用本工具离线回补。
//
// 用法（必须先停 man-p2p 服务——同一 Pebble 目录只允许一个进程打开）：
//
//	mempool_expiry_backfill -data-dir /mnt2/man-p2p/man_base_data_pebble \
//	  [-ttl-hours 72] [-delete-expired] [-compact]
//
// -compact：对 mempool 与 mempool_expiry 两库做全键空间手动压实。批量删除
// 后 LSM 中留有海量 tombstone，读迭代需逐个跳过（读路径依然慢）直至压实
// 回收；-compact 在同一次停机窗口内直接完成回收。
// 幂等：已有索引条目的 pin 跳过，可中断续跑。
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"man-p2p/pin"

	"github.com/bytedance/sonic"
	"github.com/cockroachdb/pebble"
)

const (
	expiryPrefix       = "mpexp_"
	maxInt64     int64 = 9223372036854775807
)

func expiryKey(sortTime int64, pinId string) string {
	return fmt.Sprintf("%s%019d_%s", expiryPrefix, maxInt64-sortTime, pinId)
}

func main() {
	dataDir := flag.String("data-dir", "", "pebble data dir (contains mempool/db)")
	ttlHours := flag.Int64("ttl-hours", 72, "mempool entry TTL in hours")
	deleteExpired := flag.Bool("delete-expired", false, "delete entries older than TTL during backfill")
	compact := flag.Bool("compact", false, "fully compact both mempool dbs after phases (reclaims tombstones)")
	flag.Parse()

	if *dataDir == "" {
		log.Fatal("-data-dir required")
	}
	// 回补阶段始终写索引库，因此两库都以读写打开
	pinDB, err := pebble.Open(filepath.Join(*dataDir, "mempool", "db"), &pebble.Options{})
	if err != nil {
		log.Fatalf("open mempool db: %v", err)
	}
	idxDB, err := pebble.Open(filepath.Join(*dataDir, "mempool_expiry", "db"), &pebble.Options{})
	if err != nil {
		log.Fatalf("open mempool_expiry db: %v", err)
	}
	cutoff := time.Now().Add(-time.Duration(*ttlHours) * time.Hour).Unix()

	// Phase 1: 回补索引（跳过已有索引条目，幂等可续跑）；
	// 空 value 条目（旧版 SetMempool 只写 key 不写数据，存量主体）无数据
	// 无读者，直接删除。
	pinIt, err := pinDB.NewIter(nil)
	if err != nil {
		log.Fatalf("iterate: %v", err)
	}
	var (
		scanned, indexed, skippedIndexed, noTime int
		emptyDeleted, badData                    int
		batch                                    = idxDB.NewBatch()
		emptyBatch                               = pinDB.NewBatch()
	)
	for pinIt.First(); pinIt.Valid(); pinIt.Next() {
		scanned++
		id := string(pinIt.Key())
		value := pinIt.Value()
		var p pin.PinInscription
		if err := sonic.Unmarshal(value, &p); err != nil {
			if len(value) == 0 {
				if err := emptyBatch.Delete(pinIt.Key(), nil); err != nil {
					log.Fatalf("empty delete: %v", err)
				}
				emptyDeleted++
				if emptyDeleted%100000 == 0 {
					if err := emptyBatch.Commit(pebble.Sync); err != nil {
						log.Fatalf("empty commit: %v", err)
					}
					emptyBatch.Reset()
					log.Printf("progress: scanned=%d indexed=%d emptyDeleted=%d badData=%d", scanned, indexed, emptyDeleted, badData)
				}
				continue
			}
			badData++
			if badData%100000 == 0 {
				log.Printf("progress: scanned=%d indexed=%d emptyDeleted=%d badData=%d", scanned, indexed, emptyDeleted, badData)
			}
			continue
		}
		sortTime := pin.EffectiveSortTime(&p)
		if sortTime <= 0 {
			noTime++
			continue // 无法计时，不建索引（留在数据侧，由人工处理）
		}
		key := expiryKey(sortTime, id)
		if _, closer, err := idxDB.Get([]byte(key)); err == nil {
			closer.Close()
			skippedIndexed++
			continue
		}
		if err := batch.Set([]byte(key), nil, nil); err != nil {
			log.Fatalf("batch set: %v", err)
		}
		indexed++
		if indexed%10000 == 0 {
			if err := batch.Commit(pebble.Sync); err != nil {
				log.Fatalf("commit: %v", err)
			}
			batch.Reset()
			log.Printf("progress: scanned=%d indexed=%d emptyDeleted=%d badData=%d", scanned, indexed, emptyDeleted, badData)
		}
	}
	pinIt.Close()
	if err := batch.Commit(pebble.Sync); err != nil {
		log.Fatalf("final commit: %v", err)
	}
	batch.Close()
	if err := emptyBatch.Commit(pebble.Sync); err != nil {
		log.Fatalf("final empty commit: %v", err)
	}
	emptyBatch.Close()
	log.Printf("phase1 done: scanned=%d indexed=%d alreadyIndexed=%d noTime=%d emptyDeleted=%d badData=%d", scanned, indexed, skippedIndexed, noTime, emptyDeleted, badData)

	if *deleteExpired {
		// Phase 2: 从索引最旧端删除过期条目（数据 + 索引一起删）
		idxIt, err := idxDB.NewIter(nil)
		if err != nil {
			log.Fatalf("iterate index: %v", err)
		}
		var deleted, kept int
		delIdx := idxDB.NewBatch()
		delPin := pinDB.NewBatch()
		for idxIt.Last(); idxIt.Valid(); idxIt.Prev() { // 降序 = 最旧在前
			parts := strings.SplitN(string(idxIt.Key())[len(expiryPrefix):], "_", 2)
			if len(parts) != 2 {
				continue
			}
			rev, err := strconv.ParseInt(parts[0], 10, 64)
			if err != nil {
				continue
			}
			sortTime := maxInt64 - rev
			if sortTime >= cutoff {
				kept++
				continue // 之后都是新数据
			}
			pinId := parts[1]
			delPin.Delete([]byte(pinId), nil)
			delIdx.Delete(idxIt.Key(), nil)
			deleted++
			if deleted%10000 == 0 {
				if err := delPin.Commit(pebble.Sync); err != nil {
					log.Fatalf("delete commit (pins): %v", err)
				}
				if err := delIdx.Commit(pebble.Sync); err != nil {
					log.Fatalf("delete commit (index): %v", err)
				}
				delPin.Reset()
				delIdx.Reset()
				log.Printf("deleted %d expired entries so far...", deleted)
			}
		}
		idxIt.Close()
		if err := delPin.Commit(pebble.Sync); err != nil {
			log.Fatalf("final delete commit (pins): %v", err)
		}
		if err := delIdx.Commit(pebble.Sync); err != nil {
			log.Fatalf("final delete commit (index): %v", err)
		}
		delPin.Close()
		delIdx.Close()
		log.Printf("phase2 done: deleted=%d keptNewer=%d (ttl=%dh, cutoff=%d)", deleted, kept, *ttlHours, cutoff)
	}

	idxDB.Close()
	pinDB.Close()

	if *compact {
		log.Printf("compacting mempool dbs (reclaiming tombstones)...")
		compactStart := time.Now()
		cdb, err := pebble.Open(filepath.Join(*dataDir, "mempool", "db"), &pebble.Options{})
		if err != nil {
			log.Fatalf("reopen mempool db: %v", err)
		}
		if err := cdb.Compact(nil, nil, true); err != nil {
			log.Printf("[WARN] compact mempool: %v", err)
		}
		cdb.Close()
		cdb, err = pebble.Open(filepath.Join(*dataDir, "mempool_expiry", "db"), &pebble.Options{})
		if err != nil {
			log.Fatalf("reopen mempool_expiry db: %v", err)
		}
		if err := cdb.Compact(nil, nil, true); err != nil {
			log.Printf("[WARN] compact mempool_expiry: %v", err)
		}
		cdb.Close()
		log.Printf("compact done in %s", time.Since(compactStart).Round(time.Second))
	}

	log.Println("mempool_expiry_backfill finished")
	os.Exit(0)
}
