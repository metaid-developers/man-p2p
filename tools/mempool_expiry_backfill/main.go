// mempool_expiry_backfill 为存量 mempool 条目回补 mpexp_ 过期索引
//（独立库 mempool_expiry/db），并可立即删除超过 TTL 的条目。
//
// 背景：mpexp_ 反向时间戳索引自 R1 起由 SetMempool 同步写入；此前的存量
// 数据没有索引条目，在线 sweep 只能清理新写入，历史堆积（如生产节点 2
// 的 19GB）需用本工具离线回补。
//
// 用法（必须先停 man-p2p 服务——同一 Pebble 目录只允许一个进程打开）：
//
//	mempool_expiry_backfill -data-dir /mnt2/man-p2p/man_base_data_pebble \
//	  [-ttl-hours 72] [-delete-expired]
//
// 磁盘回收：批量删除后 Pebble 自动 compaction 会渐进回收空间（19GB 级
// 存量通常需数小时）；如需立即回收，可在工具跑完后用 pebble 自带工具或
// 短暂重启服务触发 compaction。
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
	expiryPrefix      = "mpexp_"
	maxInt64     int64 = 9223372036854775807
)

func expiryKey(sortTime int64, pinId string) string {
	return fmt.Sprintf("%s%019d_%s", expiryPrefix, maxInt64-sortTime, pinId)
}

func main() {
	dataDir := flag.String("data-dir", "", "pebble data dir (contains mempool/db)")
	ttlHours := flag.Int64("ttl-hours", 72, "mempool entry TTL in hours")
	deleteExpired := flag.Bool("delete-expired", false, "delete entries older than TTL during backfill")
	flag.Parse()

	if *dataDir == "" {
		log.Fatal("-data-dir required")
	}
	// 回补阶段始终写索引库，因此两库都以读写打开
	pinDB, err := pebble.Open(filepath.Join(*dataDir, "mempool", "db"), &pebble.Options{})
	if err != nil {
		log.Fatalf("open mempool db: %v", err)
	}
	if err != nil {
		log.Fatalf("open mempool db: %v", err)
	}
	idxDB, err := pebble.Open(filepath.Join(*dataDir, "mempool_expiry", "db"), &pebble.Options{})
	if err != nil {
		log.Fatalf("open mempool_expiry db: %v", err)
	}
	cutoff := time.Now().Add(-time.Duration(*ttlHours) * time.Hour).Unix()

	// Phase 1: 回补索引（跳过已有索引条目，幂等可续跑）
	pinIt, err := pinDB.NewIter(nil)
	if err != nil {
		log.Fatalf("iterate: %v", err)
	}
	var (
		scanned, indexed, skippedIndexed, noTime int
		batch                                    = idxDB.NewBatch()
	)
	for pinIt.First(); pinIt.Valid(); pinIt.Next() {
		scanned++
		id := string(pinIt.Key())
		var p pin.PinInscription
		if err := sonic.Unmarshal(pinIt.Value(), &p); err != nil {
			log.Printf("[WARN] skip unparseable entry %s: %v", id, err)
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
			log.Printf("progress: scanned=%d indexed=%d skipped=%d noTime=%d", scanned, indexed, skippedIndexed, noTime)
		}
	}
	pinIt.Close()
	if err := batch.Commit(pebble.Sync); err != nil {
		log.Fatalf("final commit: %v", err)
	}
	batch.Close()
	log.Printf("phase1 done: scanned=%d indexed=%d alreadyIndexed=%d noTime=%d", scanned, indexed, skippedIndexed, noTime)

	if !*deleteExpired {
		idxDB.Close()
		pinDB.Close()
		log.Println("mempool_expiry_backfill finished (index backfill only)")
		os.Exit(0)
	}

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

	idxDB.Close()
	pinDB.Close()
	log.Println("mempool_expiry_backfill finished")
	os.Exit(0)
}
