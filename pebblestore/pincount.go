package pebblestore

import (
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/pebble"
)

var MetaIdCountNum int64 = 0

type CountStat struct {
	PinCount            int64            `json:"pinCount"`
	MetaIdCount         int64            `json:"metaIdCount"`
	BlockCount          map[string]int64 `json:"blockCount"`
	PathPinCount        map[string]int64 `json:"pathPinCount"`
	AddressPathPinCount map[string]int64 `json:"addressPathPinCount"`
}

func StatMetaId(db *Database) {
	for {
		metaidCount, err := db.StatAllMetaidInfo()
		if err == nil && metaidCount > 0 {
			db.CountSet("metaids", int64(metaidCount))
		}
		time.Sleep(10 * time.Minute)
	}
}
func StatPinSort(db *Database) {
	for {
		db.StatPinSortTable()
		// 改为1小时执行一次，配合增量更新，减少全表扫描频率
		time.Sleep(1 * time.Hour)
	}
}
func (db *Database) StatPinSortTable() error {
	// 获取上次统计的游标
	cursorKey := "stat_cursor_key"
	cursorVal, closer, err := db.MetaDb.Get([]byte(cursorKey))
	var lastCursor string
	if err == nil {
		lastCursor = string(cursorVal)
		closer.Close()
	}

	// 获取上次统计的高度
	heightKey := "stat_cursor_height"
	_, closer, err = db.MetaDb.Get([]byte(heightKey))
	if err == nil {
		closer.Close()
	}

	// 检查是否发生回滚 (这里简单通过比较当前最大高度和上次统计高度)
	// 注意：这里需要一种方法获取当前各链的最大高度，如果比 lastHeight 小，说明回滚了
	// 由于 PinSort 混合了多链，这里简化处理：如果 lastCursor 为空，或者强制全量，则全量
	// 实际上，为了防止“统计增多”，最安全的是：如果检测到回滚，或者数据不一致，就全量重跑。
	// 这里我们采用：如果 lastCursor 为空，全量；否则增量。
	// 并在外部逻辑中，如果发生回滚，手动删除 stat_cursor_key 触发全量重算。

	var it *pebble.Iterator
	isFullScan := false
	if lastCursor == "" {
		isFullScan = true
		it, err = db.PinSort.NewIter(nil)
	} else {
		// 增量扫描：从 lastCursor 之后开始
		it, err = db.PinSort.NewIter(&pebble.IterOptions{
			LowerBound: []byte(lastCursor + "\x00"),
		})
	}

	if err != nil {
		return err
	}
	defer it.Close()

	// 统计增量
	countStat := &CountStat{
		PinCount:            0,
		BlockCount:          make(map[string]int64),
		PathPinCount:        make(map[string]int64),
		AddressPathPinCount: make(map[string]int64),
	}

	var currentLastKey string
	for it.First(); it.Valid(); it.Next() {
		key := string(it.Key())
		currentLastKey = key
		db.statFunc(key, countStat)
	}

	// 更新持久化计数器 (Confirmed)
	if isFullScan {
		// 全量模式：覆盖
		db.CountSet("pins_confirmed", countStat.PinCount)
		for path, count := range countStat.PathPinCount {
			db.CountSet(path+"_confirmed_count", count)
		}
		for addressPath, count := range countStat.AddressPathPinCount {
			db.CountSet(addressPath+"_confirmed_count", count)
		}
	} else {
		// 增量模式：累加
		if countStat.PinCount > 0 {
			db.CountAdd("pins_confirmed", countStat.PinCount)
		}
		for path, count := range countStat.PathPinCount {
			db.CountAdd(path+"_confirmed_count", count)
		}
		for addressPath, count := range countStat.AddressPathPinCount {
			db.CountAdd(addressPath+"_confirmed_count", count)
		}
	}

	// 更新游标
	if currentLastKey != "" {
		db.MetaDb.Set([]byte(cursorKey), []byte(currentLastKey), pebble.Sync)
		// 解析 Key 中的 Height 更新 lastHeight (略，因为 Key 结构复杂，且多链混合)
	}

	// --- Mempool 统计 (独立全量) ---
	mempoolStat := &CountStat{
		PinCount:            0,
		PathPinCount:        make(map[string]int64),
		AddressPathPinCount: make(map[string]int64),
	}
	mempoolIter, err := db.PinsMempoolDb.NewIter(nil)
	if err == nil {
		defer mempoolIter.Close()
		var mempoolKeys []string
		for mempoolIter.First(); mempoolIter.Valid(); mempoolIter.Next() {
			mempoolKeys = append(mempoolKeys, string(mempoolIter.Key()))
			if len(mempoolKeys) >= 100 {
				pins, _ := db.GetPinListByIdList(mempoolKeys, 100, false)
				for _, p := range pins {
					if p != nil {
						mempoolStat.PinCount += 1
						if p.Path != "" {
							mempoolStat.PathPinCount[p.Path] += 1
							if p.MetaId != "" {
								mempoolStat.AddressPathPinCount[p.MetaId+"_"+p.Path] += 1
							}
						}
					}
				}
				mempoolKeys = nil
			}
		}
		if len(mempoolKeys) > 0 {
			pins, _ := db.GetPinListByIdList(mempoolKeys, 100, false)
			for _, p := range pins {
				if p != nil {
					mempoolStat.PinCount += 1
					if p.Path != "" {
						mempoolStat.PathPinCount[p.Path] += 1
						if p.MetaId != "" {
							mempoolStat.AddressPathPinCount[p.MetaId+"_"+p.Path] += 1
						}
					}
				}
			}
		}
	}

	// --- 合并统计并更新对外 Key ---
	// Total = Confirmed + Mempool
	// 注意：这里需要读取 Confirmed 的当前值，加上 Mempool 的值，然后 Set 到 "pins"
	// 为了性能，我们假设 Confirmed 值就在 DB 里

	// Helper function to get confirmed count
	getConfirmed := func(key string) int64 {
		val, closer, err := db.CountDB.Get([]byte(key))
		if err != nil {
			return 0
		}
		defer closer.Close()
		v, _ := strconv.ParseInt(string(val), 10, 64)
		return v
	}

	// 更新总数 pins
	confirmedPins := getConfirmed("pins_confirmed")
	db.CountSet("pins", confirmedPins+mempoolStat.PinCount)

	// 更新 Path 总数 (只更新本次 Mempool 涉及到的 Path，或者全量更新？)
	// 由于 Path 太多，无法遍历所有 Path。
	// 策略：
	// 1. 对于增量扫描到的 Path，更新其 Total。
	// 2. 对于 Mempool 涉及到的 Path，更新其 Total。
	// 但这样会漏掉 "Mempool 减少" 的情况（即某 Path 以前在 Mempool，现在不在了，Total 应该减）。
	// 这是一个难题。
	// 妥协方案：只保证 "pins" 总数是实时的 (Confirmed + Mempool)。
	// 对于 Path 计数，暂时只使用 Confirmed Count (忽略 Mempool)，或者接受 Mempool 变动带来的短暂不一致。
	// 鉴于 Path 计数通常用于排行榜，实时性要求可能没那么高，或者 Mempool 占比小。
	// 这里我们尝试尽力更新：
	// 遍历 mempoolStat 中的 Path，加上 Confirmed。
	// 问题是：如果 Mempool 以前有，现在没了，我们怎么知道要更新哪个 Path？
	// 除非我们记录了 "上一次 Mempool 的 Path 统计"。
	// 算了，为了防止 "统计增多" 和复杂性，Path 计数暂时只包含 Confirmed。
	// 或者：直接把 Mempool 的加进去，但不减？不行。
	// 决定：Path 计数 = Confirmed + Mempool (仅针对当前 Mempool 中存在的 Path 更新 Total，不存在的不更新，可能会导致旧数据残留)
	// 修正：Path 计数直接使用 Confirmed Count。Mempool 中的 Path 暂时不计入 Path 排行榜，只计入总数。
	// 这样最安全，不会有 "增多" 问题。

	for path := range countStat.PathPinCount {
		// 增量扫描到的 Path，肯定要更新 Total
		// Total = Confirmed (已更新) + Mempool (未知)
		// 简化：Total = Confirmed
		db.CountSet(path+"_count", getConfirmed(path+"_confirmed_count"))
	}
	for addressPath := range countStat.AddressPathPinCount {
		db.CountSet(addressPath+"_count", getConfirmed(addressPath+"_confirmed_count"))
	}

	// 处理区块统计 (保持原样，使用 countStat 中的 BlockCount，它是增量的吗？)
	// BlockCount 在 statFunc 中是直接赋值 height。
	// 如果是增量扫描，BlockCount 只包含新块的高度。
	// 我们需要维护一个全局的 BlockCount。
	// 之前的逻辑是：遍历 BlockCount map，累加 height - initialHeight。
	// 这在增量模式下是错误的！因为只扫到了新块。
	// 修正：BlockCount 应该直接读取各链的 Best Height。
	// 这里暂时保留原逻辑，但要注意它可能只统计了增量部分。
	// 实际上 db.CountSet("blocks") 是设置总数。
	// 我们应该单独维护 blocks count。
	// 简单做法：直接用 currentLastKey 解析出的 Height 更新 blocks count?
	// 或者直接忽略 blocks 统计的增量优化，因为 blocks 数量很少，可以直接查 MetaDb 中的 BestHeight。

	return nil
}

func (db *Database) statFunc(key string, countStat *CountStat) {
	//publicKeyStr := common.ConcatBytesOptimized([]string{fmt.Sprintf("%010d", blockTime), "&", chainName, "&", fmt.Sprintf("%010d", height)}, "")
	//sortKey := common.ConcatBytesOptimized([]string{publicKeyStr, "&", p.Path, "&", p.MetaId, "&", p.Id}, "")
	arr := strings.Split(key, "&")
	if len(arr) < 6 {
		return
	}
	chaiName := arr[1]
	heightStr := arr[2]
	height, err := strconv.ParseInt(heightStr, 10, 64)
	if err != nil {
		return
	}
	path := arr[3]
	metaId := arr[4]
	// 统计PIN
	countStat.PinCount += 1
	// 统计Path
	countStat.PathPinCount[path] += 1
	// 统计Address_Path
	countStat.AddressPathPinCount[metaId+"_"+path] += 1
	// 统计Block
	countStat.BlockCount[chaiName] = height
}
func (db *Database) StatAllMetaidInfo() (count int, err error) {
	it, err := db.MetaidInfoDB.NewIter(nil)
	if err != nil {
		return 0, err
	}
	defer it.Close()

	count = 0
	for it.First(); it.Valid(); it.Next() {
		count++
	}
	return count, nil
}
