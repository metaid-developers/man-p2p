package pebblestore

import (
	"context"
	"testing"
	"time"

	"man-p2p/pin"
)

// TestSetMempoolWritesExpiryIndex 回归：SetMempool 必须同步写 mpexp_ 过期索引
// （独立 MempoolExpiryDb），且数据键不得混入索引。
func TestSetMempoolWritesExpiryIndex(t *testing.T) {
	db := newMempoolTestDB(t)
	seen := time.Now().Unix()
	pinNode := &pin.PinInscription{Id: "pin-idx", ChainName: "mvc", GenesisHeight: -1, SeenTime: seen, Timestamp: seen}
	if err := db.SetMempool(pinNode); err != nil {
		t.Fatalf("SetMempool: %v", err)
	}
	key := []byte(mempoolExpiryKey(seen, "pin-idx"))
	if _, closer, err := db.MempoolExpiryDb.Get(key); err != nil {
		t.Fatalf("expiry index key missing from MempoolExpiryDb: %v", err)
	} else {
		closer.Close()
	}
	sortTime, pinId, ok := ParseMempoolExpiryKey(key)
	if !ok || sortTime != seen || pinId != "pin-idx" {
		t.Fatalf("ParseMempoolExpiryKey roundtrip failed: sortTime=%d pinId=%q ok=%v", sortTime, pinId, ok)
	}
	// 数据库只应有 pin 数据键
	it, err := db.PinsMempoolDb.NewIter(nil)
	if err != nil {
		t.Fatalf("NewIter: %v", err)
	}
	n := 0
	for it.First(); it.Valid(); it.Next() {
		n++
	}
	it.Close()
	if n != 1 {
		t.Fatalf("PinsMempoolDb should hold exactly the pin key, got %d keys", n)
	}
}

// TestMempoolPageListWithoutMainPinEntry 新行为：mempool 条目即使主 pins 分片
// 没有对应记录也应出现在列表中（旧实现跨库回查、回查失败整条跳过）。
func TestMempoolPageListWithoutMainPinEntry(t *testing.T) {
	db := newMempoolTestDB(t)
	pinNode := &pin.PinInscription{Id: "mempool-only", ChainName: "mvc", GenesisHeight: -1, SeenTime: 50, Timestamp: 50, ContentBody: []byte("body")}
	if err := db.SetMempool(pinNode); err != nil {
		t.Fatalf("SetMempool: %v", err)
	}
	list, err := db.GetMempoolPageList(context.Background(), 0, 10)
	if err != nil {
		t.Fatalf("GetMempoolPageList: %v", err)
	}
	if len(list) != 1 || list[0].Id != "mempool-only" {
		t.Fatalf("expected mempool-only pin in list, got %d entries", len(list))
	}
	if len(list[0].ContentBody) != 0 {
		t.Fatalf("expected ContentBody trimmed")
	}
}

// TestExpiryIndexOrdering 索引按 sortTime 反向排列：降序迭代 = 最旧在前。
func TestExpiryIndexOrdering(t *testing.T) {
	db := newMempoolTestDB(t)
	base := time.Now().Unix()
	for i := 0; i < 5; i++ {
		pinNode := &pin.PinInscription{Id: "pin" + string(rune('0'+i)), ChainName: "mvc", GenesisHeight: -1, SeenTime: base + int64(i)*100, Timestamp: base + int64(i)*100}
		if err := db.SetMempool(pinNode); err != nil {
			t.Fatalf("SetMempool: %v", err)
		}
	}
	it, err := db.MempoolExpiryDb.NewIter(nil)
	if err != nil {
		t.Fatalf("NewIter: %v", err)
	}
	defer it.Close()
	var order []int64
	for it.Last(); it.Valid(); it.Prev() {
		sortTime, _, ok := ParseMempoolExpiryKey(it.Key())
		if !ok {
			t.Fatalf("unparseable index key: %q", it.Key())
		}
		order = append(order, sortTime)
	}
	if len(order) != 5 {
		t.Fatalf("expected 5 index entries, got %d", len(order))
	}
	// 降序迭代应从最旧（sortTime 最小）到最新
	for i := 1; i < len(order); i++ {
		if order[i] <= order[i-1] {
			t.Fatalf("expected strictly ascending sortTime from oldest, got %v", order)
		}
	}
}
