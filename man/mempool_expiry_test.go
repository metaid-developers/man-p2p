package man

import (
	"testing"
	"time"

	"man-p2p/pebblestore"
	"man-p2p/pin"

	"github.com/cockroachdb/pebble"
)

func setupMempoolStore(t *testing.T) *PebbleData {
	t.Helper()
	origStore := PebbleStore
	db, err := pebblestore.NewDataBase(t.TempDir(), 2)
	if err != nil {
		t.Fatalf("NewDataBase: %v", err)
	}
	PebbleStore = &PebbleData{Database: db}
	t.Cleanup(func() {
		_ = db.Close()
		PebbleStore = origStore
	})
	return PebbleStore
}

func mempoolSweepPin(id string, seenTime int64) *pin.PinInscription {
	return &pin.PinInscription{
		Id:            id,
		ChainName:     "mvc",
		GenesisHeight: -1,
		SeenTime:      seenTime,
		Timestamp:     seenTime,
		ContentBody:   []byte("body-" + id),
	}
}

func countKeys(t *testing.T, db *pebble.DB) int {
	t.Helper()
	it, err := db.NewIter(nil)
	if err != nil {
		t.Fatalf("NewIter: %v", err)
	}
	defer it.Close()
	n := 0
	for it.First(); it.Valid(); it.Next() {
		n++
	}
	return n
}

// TestSweepMempoolExpiryCutoff 回归：过期条目（数据+索引）被删、新数据保留、
// 索引孤儿（pin 已被确认清理路径删掉）一并移除。
func TestSweepMempoolExpiryCutoff(t *testing.T) {
	store := setupMempoolStore(t)
	now := time.Now().Unix()

	// 两条过期（72h 前）、一条新鲜
	old1 := mempoolSweepPin("old-1", now-100*3600)
	old2 := mempoolSweepPin("old-2", now-80*3600)
	fresh := mempoolSweepPin("fresh-1", now-3600)
	for _, p := range []*pin.PinInscription{old1, old2, fresh} {
		if err := store.Database.SetMempool(p); err != nil {
			t.Fatalf("SetMempool %s: %v", p.Id, err)
		}
	}

	// 孤儿索引：数据被确认清理路径删掉（模拟 DeleteMempoolData 只删数据）
	orphanSort := now - 90*3600
	orphanPin := "orphan-1"
	if err := store.Database.SetMempool(mempoolSweepPin(orphanPin, orphanSort)); err != nil {
		t.Fatalf("SetMempool orphan: %v", err)
	}
	if err := store.Database.PinsMempoolDb.Delete([]byte(orphanPin), nil); err != nil {
		t.Fatalf("simulate confirmed cleanup: %v", err)
	}

	if got := countKeys(t, store.Database.PinsMempoolDb); got != 3 {
		t.Fatalf("pre-sweep pin entries = %d, want 3 (old-1, old-2, fresh)", got)
	}
	if got := countKeys(t, store.Database.MempoolExpiryDb); got != 4 {
		t.Fatalf("pre-sweep index entries = %d, want 4", got)
	}

	deleted := sweepMempoolExpiryCutoff(now - int64(MempoolExpiryTTL/time.Second))
	if deleted != 3 {
		t.Fatalf("expected 3 expired deletions (old-1, old-2, orphan index), got %d", deleted)
	}

	// 过期数据与孤儿索引都应消失，新鲜数据保留
	if _, closer, err := store.Database.PinsMempoolDb.Get([]byte("old-1")); err == nil {
		closer.Close()
		t.Fatal("old-1 should have been deleted")
	}
	if _, closer, err := store.Database.PinsMempoolDb.Get([]byte("old-2")); err == nil {
		closer.Close()
		t.Fatal("old-2 should have been deleted")
	}
	if _, closer, err := store.Database.PinsMempoolDb.Get([]byte("fresh-1")); err != nil {
		t.Fatalf("fresh-1 must survive: %v", err)
	} else {
		closer.Close()
	}
	if got := countKeys(t, store.Database.PinsMempoolDb); got != 1 {
		t.Fatalf("post-sweep pin entries = %d, want 1 (fresh)", got)
	}
	if got := countKeys(t, store.Database.MempoolExpiryDb); got != 1 {
		t.Fatalf("post-sweep index entries = %d, want 1 (fresh)", got)
	}
}
