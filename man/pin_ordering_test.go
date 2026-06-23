package man

import (
	"context"
	"testing"

	"man-p2p/common"
	"man-p2p/pebblestore"
	"man-p2p/pin"
)

func newTestPebbleData(t *testing.T) *PebbleData {
	t.Helper()

	db, err := pebblestore.NewDataBase(t.TempDir(), 1)
	if err != nil {
		t.Fatalf("NewDataBase error: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	return &PebbleData{Database: db}
}

func TestGetPinByMetaIdAndPathPageListPrefersSeenTime(t *testing.T) {
	t.Parallel()

	pd := newTestPebbleData(t)
	path := "/protocols/metabot-heartbeat"
	address := "1GrqX7K9jdnUor8hAoAfDx99uFH2tT75Za"
	metaID := common.GetMetaIdByAddress(address)

	oldPin := &pin.PinInscription{
		Id:            "z-old",
		MetaId:        metaID,
		Path:          path,
		ChainName:     "mvc",
		GenesisHeight: -1,
		Timestamp:     100,
		SeenTime:      100,
	}
	newPin := &pin.PinInscription{
		Id:            "a-new",
		MetaId:        metaID,
		Path:          path,
		ChainName:     "mvc",
		GenesisHeight: -1,
		Timestamp:     50,
		SeenTime:      200,
	}

	if err := pd.Database.SetAllPins(-1, []*pin.PinInscription{oldPin}, 20000); err != nil {
		t.Fatalf("SetAllPins old pin error: %v", err)
	}
	if err := pd.Database.SetAllPins(-1, []*pin.PinInscription{newPin}, 20000); err != nil {
		t.Fatalf("SetAllPins new pin error: %v", err)
	}

	list, _, _, err := pd.GetPinByMetaIdAndPathPageList(metaID, path, "", 1)
	if err != nil {
		t.Fatalf("GetPinByMetaIdAndPathPageList error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 result, got %d", len(list))
	}
	if list[0].Id != "a-new" {
		t.Fatalf("expected newest seen pin first, got %s", list[0].Id)
	}
}

func TestGetMempoolPageListFallsBackToTimestampWhenSeenTimeMissing(t *testing.T) {
	t.Parallel()

	pd := newTestPebbleData(t)

	oldPin := &pin.PinInscription{
		Id:            "z-old",
		MetaId:        "meta-old",
		Path:          "/protocols/metabot-heartbeat",
		ChainName:     "mvc",
		GenesisHeight: -1,
		Timestamp:     100,
	}
	newPin := &pin.PinInscription{
		Id:            "a-new",
		MetaId:        "meta-new",
		Path:          "/protocols/metabot-heartbeat",
		ChainName:     "mvc",
		GenesisHeight: -1,
		Timestamp:     200,
	}

	for _, pinNode := range []*pin.PinInscription{oldPin, newPin} {
		if err := pd.Database.SetAllPins(-1, []*pin.PinInscription{pinNode}, 20000); err != nil {
			t.Fatalf("SetAllPins %s error: %v", pinNode.Id, err)
		}
		if err := pd.Database.SetMempool(pinNode); err != nil {
			t.Fatalf("SetMempool %s error: %v", pinNode.Id, err)
		}
	}

	list, err := pd.Database.GetMempoolPageList(context.Background(), 0, 1)
	if err != nil {
		t.Fatalf("GetMempoolPageList error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 result, got %d", len(list))
	}
	if list[0].Id != "a-new" {
		t.Fatalf("expected timestamp fallback to pick latest pin, got %s", list[0].Id)
	}
}
