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

func TestIngestP2PRevokeOfModifyMarksOriginalPinRevoked(t *testing.T) {
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
	originalPin := &pin.PinInscription{
		Id:            "original-metaapp-pin",
		Operation:     "create",
		Address:       address,
		MetaId:        metaid,
		Path:          "/protocols/metaapp",
		ChainName:     "mvc",
		Timestamp:     1710000000,
		GenesisHeight: 100,
		ContentBody:   []byte(`{"version":"1.0.0"}`),
	}
	modifyPin := &pin.PinInscription{
		Id:            "modify-metaapp-pin",
		Operation:     "modify",
		Address:       address,
		MetaId:        metaid,
		Path:          "@original-metaapp-pin",
		ChainName:     "mvc",
		Timestamp:     1710000010,
		GenesisHeight: 101,
		ContentBody:   []byte(`{"version":"1.0.1"}`),
	}
	revokePin := &pin.PinInscription{
		Id:            "revoke-modify-metaapp-pin",
		Operation:     "revoke",
		Address:       address,
		MetaId:        metaid,
		Path:          "@modify-metaapp-pin",
		ChainName:     "mvc",
		Timestamp:     1710000020,
		GenesisHeight: 102,
	}

	if err := IngestP2PPin(originalPin); err != nil {
		t.Fatalf("IngestP2PPin(original) failed: %v", err)
	}
	if err := IngestP2PPin(modifyPin); err != nil {
		t.Fatalf("IngestP2PPin(modify) failed: %v", err)
	}

	modifiedOriginal, err := PebbleStore.GetPinById(originalPin.Id)
	if err != nil {
		t.Fatalf("GetPinById(modified original) failed: %v", err)
	}
	if modifiedOriginal.Status != 1 {
		t.Fatalf("expected original pin status 1 after modify, got %d", modifiedOriginal.Status)
	}

	if err := IngestP2PPin(revokePin); err != nil {
		t.Fatalf("IngestP2PPin(revoke) failed: %v", err)
	}

	revokedOriginal, err := PebbleStore.GetPinById(originalPin.Id)
	if err != nil {
		t.Fatalf("GetPinById(revoked original) failed: %v", err)
	}
	if revokedOriginal.Status != -1 {
		t.Fatalf("expected original pin status -1 after revoke of modify, got %d", revokedOriginal.Status)
	}

	list, _, _, err := PebbleStore.GetPinByMetaIdAndPathPageList(metaid, originalPin.Path, "0", 10)
	if err != nil {
		t.Fatalf("GetPinByMetaIdAndPathPageList failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected original index to return 1 pin, got %d", len(list))
	}
	if list[0].Id != originalPin.Id {
		t.Fatalf("expected original index pin %q, got %q", originalPin.Id, list[0].Id)
	}
	if list[0].Status != -1 {
		t.Fatalf("expected original index status -1, got %d", list[0].Status)
	}
}
