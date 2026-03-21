package man

import (
	"testing"

	"man-p2p/common"
)

func TestInitRuntimeWithoutChainSourceSkipsAdapters(t *testing.T) {
	common.Config = &common.AllConfig{}
	common.Config.Pebble.Dir = t.TempDir()
	common.Config.Pebble.Num = 1

	InitRuntime("btc", "pebble", "0", "0", false)
	t.Cleanup(func() {
		if PebbleStore != nil && PebbleStore.Database != nil {
			_ = PebbleStore.Database.Close()
		}
	})

	if PebbleStore == nil || PebbleStore.Database == nil {
		t.Fatal("expected pebble store to be initialized")
	}
	if len(ChainAdapter) != 0 {
		t.Fatalf("expected no chain adapters in p2p-only mode, got %d", len(ChainAdapter))
	}
	if len(IndexerAdapter) != 0 {
		t.Fatalf("expected no indexer adapters in p2p-only mode, got %d", len(IndexerAdapter))
	}
	if len(ChainList) != 1 || ChainList[0] != "btc" {
		t.Fatalf("expected chain list to be preserved, got %#v", ChainList)
	}
}
