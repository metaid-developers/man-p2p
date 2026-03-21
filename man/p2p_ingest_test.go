package man

import (
	"man-p2p/common"
	"man-p2p/pebblestore"
	"man-p2p/pin"
	"testing"
)

func TestIngestP2PPinStoresPinAndMetaIdInfo(t *testing.T) {
	origStore := PebbleStore
	if PebbleStore == nil {
		PebbleStore = &PebbleData{}
	}
	origDB := PebbleStore.Database
	t.Cleanup(func() {
		if PebbleStore != nil && PebbleStore.Database != nil {
			_ = PebbleStore.Database.Close()
		}
		PebbleStore = origStore
		if PebbleStore != nil {
			PebbleStore.Database = origDB
		}
	})

	db, err := pebblestore.NewDataBase(t.TempDir(), 4)
	if err != nil {
		t.Fatal(err)
	}
	PebbleStore.Database = db

	address := "1P2PTestAddress"
	metaid := common.GetMetaIdByAddress(address)
	pinNode := &pin.PinInscription{
		Id:            "p2p-pin-001",
		Address:       address,
		MetaId:        metaid,
		Path:          "/info/name",
		ChainName:     "btc",
		Timestamp:     1710000000,
		GenesisHeight: 900000,
		ContentBody:   []byte("Alice"),
		ContentLength: 5,
	}

	if err := IngestP2PPin(pinNode); err != nil {
		t.Fatalf("IngestP2PPin failed: %v", err)
	}

	stored, err := PebbleStore.GetPinById(pinNode.Id)
	if err != nil {
		t.Fatalf("GetPinById failed: %v", err)
	}
	if stored.Id != pinNode.Id {
		t.Fatalf("expected stored pin %s, got %s", pinNode.Id, stored.Id)
	}

	info, err := PebbleStore.Database.GetMetaidInfo(metaid)
	if err != nil {
		t.Fatalf("GetMetaidInfo failed: %v", err)
	}
	if info.Name != "Alice" {
		t.Fatalf("expected metaid name Alice, got %q", info.Name)
	}
}
