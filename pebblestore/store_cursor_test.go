package pebblestore

import (
	"fmt"
	"man-p2p/pin"
	"testing"

	"github.com/bytedance/sonic"
)

// TestQueryPinPageListLastIdCursor 回归：lastId 游标必须等价于 offset 深页，
// 但只做 O(size) 定位（R1 修复：旧实现 page*size 次 Prev 回退扫描）。
func TestQueryPinPageListLastIdCursor(t *testing.T) {
	db, err := NewDataBase(t.TempDir(), 2)
	if err != nil {
		t.Fatalf("NewDataBase: %v", err)
	}
	defer db.Close()

	const n = 10
	ids := make([]string, n)
	var sortKeys []string
	for i := 0; i < n; i++ {
		ids[i] = fmt.Sprintf("cursor%02d00000000000000000000000000000000000000000000000000000000i0", i)
		p := pin.PinInscription{
			Id:            ids[i],
			Path:          "/p",
			MetaId:        "meta",
			ChainName:     "mvc",
			SeenTime:      1000 + int64(i), // SeenTime 递增 → PinSort 倒序 = ids[9] 最新
			Timestamp:     0,
			GenesisHeight: 100,
		}
		val, err := sonic.Marshal(p)
		if err != nil {
			t.Fatalf("marshal pin: %v", err)
		}
		if err := db.getShard(p.Id).Set([]byte(p.Id), val, nil); err != nil {
			t.Fatalf("set pin: %v", err)
		}
		sortKeys = append(sortKeys, pin.GenPinSortKey(&p, pin.EffectiveSortTime(&p), p.ChainName, p.GenesisHeight))
	}
	if err := db.InsertPinSort(db.PinSort, sortKeys); err != nil {
		t.Fatalf("InsertPinSort: %v", err)
	}

	// offset 首页：最新 3 条 = ids[9], ids[8], ids[7]
	res1, err := db.QueryPinPageList(db.PinSort, PageQuery{Page: 0, Size: 3})
	if err != nil {
		t.Fatalf("offset page1: %v", err)
	}
	want1 := []string{ids[9], ids[8], ids[7]}
	if !equalStrings(res1.List, want1) {
		t.Fatalf("offset page1 = %v, want %v", res1.List, want1)
	}
	if res1.NextId != ids[7] {
		t.Fatalf("offset page1 NextId = %q, want %q", res1.NextId, ids[7])
	}

	// 游标模式：从 lastId 续页，应等于 offset 第 2 页
	resCursor, err := db.QueryPinPageList(db.PinSort, PageQuery{Page: 0, Size: 3, LastId: res1.NextId})
	if err != nil {
		t.Fatalf("cursor page: %v", err)
	}
	resOff2, err := db.QueryPinPageList(db.PinSort, PageQuery{Page: 1, Size: 3})
	if err != nil {
		t.Fatalf("offset page2: %v", err)
	}
	if !equalStrings(resCursor.List, resOff2.List) {
		t.Fatalf("cursor page %v != offset page2 %v", resCursor.List, resOff2.List)
	}
	if len(resCursor.List) != 3 {
		t.Fatalf("cursor page expected 3 keys, got %d", len(resCursor.List))
	}

	// 游标续到尽头
	resTail, err := db.QueryPinPageList(db.PinSort, PageQuery{Page: 0, Size: 3, LastId: resOff2.NextId})
	if err != nil {
		t.Fatalf("cursor tail: %v", err)
	}
	if !equalStrings(resTail.List, []string{ids[3], ids[2], ids[1]}) {
		t.Fatalf("cursor tail = %v, want [ids3,ids2,ids1]", resTail.List)
	}
	resEnd, err := db.QueryPinPageList(db.PinSort, PageQuery{Page: 0, Size: 3, LastId: resTail.NextId})
	if err != nil {
		t.Fatalf("cursor end: %v", err)
	}
	if !equalStrings(resEnd.List, []string{ids[0]}) {
		t.Fatalf("cursor end = %v, want [ids0]", resEnd.List)
	}

	// 不存在的 lastId：回退 offset 模式，结果与首页一致
	resFallback, err := db.QueryPinPageList(db.PinSort, PageQuery{Page: 0, Size: 3, LastId: "no-such-pin"})
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if !equalStrings(resFallback.List, want1) {
		t.Fatalf("fallback should equal offset page1: %v", resFallback.List)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
