package man

import (
	"testing"

	"man-p2p/adapter"
	"man-p2p/common"
	"man-p2p/pebblestore"
	"man-p2p/pin"
)

func TestMrc20RuntimeEnabledSupportsMrc20OnlyWithoutModule(t *testing.T) {
	oldConfig := common.Config
	common.Config = &common.AllConfig{}
	common.Config.Sync.Mrc20Only = true
	t.Cleanup(func() {
		common.Config = oldConfig
	})

	if !Mrc20RuntimeEnabled() {
		t.Fatalf("expected mrc20 runtime to be enabled by mrc20Only without module")
	}
}

func TestDoMrc20OnlyRunIndexesWhenModuleIsAbsent(t *testing.T) {
	oldConfig := common.Config
	common.Config = &common.AllConfig{}
	common.Config.Sync.Mrc20Only = true
	common.Config.Mvc.Mrc20Height = 1
	t.Cleanup(func() {
		common.Config = oldConfig
	})

	oldAdapters := IndexerAdapter
	oldStore := PebbleStore
	IndexerAdapter = map[string]adapter.Indexer{
		"mvc": &syncTestIndexer{},
	}
	t.Cleanup(func() {
		IndexerAdapter = oldAdapters
		PebbleStore = oldStore
	})

	db, err := pebblestore.NewDataBase(t.TempDir(), 1)
	if err != nil {
		t.Fatalf("NewDataBase error: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	PebbleStore = &PebbleData{Database: db}
	if err := PebbleStore.doMrc20OnlyRun("mvc", 1); err != nil {
		t.Fatalf("doMrc20OnlyRun error: %v", err)
	}
	if got := PebbleStore.GetMrc20IndexHeight("mvc"); got != 1 {
		t.Fatalf("expected mrc20Only run to save MRC20 index height 1, got %d", got)
	}
}

func TestMempoolMrc20EnabledSupportsMrc20OnlyWithoutModule(t *testing.T) {
	oldConfig := common.Config
	common.Config = &common.AllConfig{}
	common.Config.Sync.Mrc20Only = true
	t.Cleanup(func() {
		common.Config = oldConfig
	})

	if !mempoolMrc20Enabled(&pin.PinInscription{Path: "/ft/mrc20/transfer"}) {
		t.Fatalf("expected mrc20Only mempool MRC20 pin to be enabled without module")
	}
}
