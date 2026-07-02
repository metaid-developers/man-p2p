package man

import (
	"errors"
	"fmt"
	"testing"

	"man-p2p/adapter"
	"man-p2p/adapter/microvisionchain"
	"man-p2p/common"
	"man-p2p/mrc20"
	"man-p2p/pebblestore"
	"man-p2p/pin"

	"github.com/bitcoinsv/bsvd/chaincfg/chainhash"
	"github.com/bitcoinsv/bsvd/wire"
	"github.com/bitcoinsv/bsvutil"
)

type legacyAliasTestChain struct {
	tx *bsvutil.Tx
}

func (c *legacyAliasTestChain) InitChain() {}

func (c *legacyAliasTestChain) GetBlock(blockHeight int64) (interface{}, error) {
	return nil, errors.New("not implemented")
}

func (c *legacyAliasTestChain) GetBlockTime(blockHeight int64) (int64, error) {
	return 0, errors.New("not implemented")
}

func (c *legacyAliasTestChain) GetTransaction(txId string) (interface{}, error) {
	if c.tx == nil {
		return nil, errors.New("missing tx")
	}
	return c.tx, nil
}

func (c *legacyAliasTestChain) GetInitialHeight() int64 { return 0 }

func (c *legacyAliasTestChain) GetBestHeight() int64 { return 0 }

func (c *legacyAliasTestChain) GetBlockMsg(height int64) *pin.BlockMsg { return nil }

func (c *legacyAliasTestChain) GetMempoolTransactionList() ([]interface{}, error) {
	return nil, errors.New("not implemented")
}

func (c *legacyAliasTestChain) GetTxSizeAndFees(txHash string) (int64, int64, string, error) {
	return 0, 0, "", errors.New("not implemented")
}

func TestBackfillLegacyPinAliasesResolvesLegacyMVCPinID(t *testing.T) {
	origStore := PebbleStore
	origAdapters := ChainAdapter
	origConfig := common.Config
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
		ChainAdapter = origAdapters
		common.Config = origConfig
	})

	db, err := pebblestore.NewDataBase(t.TempDir(), 4)
	if err != nil {
		t.Fatal(err)
	}
	PebbleStore.Database = db
	common.Config = &common.AllConfig{}

	canonicalTxID, legacyTxID, tx := newLegacyAliasTestTx(t)
	ChainAdapter = map[string]adapter.Chain{
		"mvc": &legacyAliasTestChain{tx: tx},
	}

	canonicalPinID := canonicalTxID + "i0"
	legacyPinID := legacyTxID + "i0"
	if err := PebbleStore.Database.SetAllPins(100, []*pin.PinInscription{{
		Id:                 canonicalPinID,
		GenesisTransaction: canonicalTxID,
		Output:             fmt.Sprintf("%s:0", canonicalTxID),
		ChainName:          "mvc",
		GenesisHeight:      100,
		Timestamp:          1710000000,
		ContentBody:        []byte("legacy-alias-content"),
		ContentLength:      uint64(len("legacy-alias-content")),
	}}, 1); err != nil {
		t.Fatalf("SetAllPins: %v", err)
	}

	if _, err := PebbleStore.GetPinById(legacyPinID); err == nil {
		t.Fatalf("expected legacy pin id %s to miss before backfill", legacyPinID)
	}

	stats, err := PebbleStore.BackfillLegacyPinAliases("mvc", 0)
	if err != nil {
		t.Fatalf("BackfillLegacyPinAliases: %v", err)
	}
	if stats.Scanned != 1 {
		t.Fatalf("expected 1 scanned pin, got %d", stats.Scanned)
	}
	if stats.Created != 1 {
		t.Fatalf("expected 1 created alias, got %d", stats.Created)
	}

	got, err := PebbleStore.GetPinById(legacyPinID)
	if err != nil {
		t.Fatalf("GetPinById(%s): %v", legacyPinID, err)
	}
	if got.Id != canonicalPinID {
		t.Fatalf("expected canonical pin id %s, got %s", canonicalPinID, got.Id)
	}
}

func TestGetPinByIdUsesStoredLegacyPinAlias(t *testing.T) {
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

	if err := PebbleStore.Database.SetAllPins(100, []*pin.PinInscription{{
		Id:                 "canonical-pin-idi0",
		LegacyPinId:        "legacy-pin-idi0",
		GenesisTransaction: "canonical-pin-id",
		Output:             "canonical-pin-id:0",
		ChainName:          "mvc",
		GenesisHeight:      100,
		Timestamp:          1710000000,
		ContentBody:        []byte("stored-alias-content"),
		ContentLength:      uint64(len("stored-alias-content")),
	}}, 1); err != nil {
		t.Fatalf("SetAllPins: %v", err)
	}

	got, err := PebbleStore.GetPinById("legacy-pin-idi0")
	if err != nil {
		t.Fatalf("GetPinById(legacy-pin-idi0): %v", err)
	}
	if got.Id != "canonical-pin-idi0" {
		t.Fatalf("expected canonical pin id %s, got %s", "canonical-pin-idi0", got.Id)
	}
}

func TestGetMrc20ArrivalByPinIdUsesStoredLegacyPinAlias(t *testing.T) {
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

	canonicalPinID := "canonical-arrival-pin-idi0"
	legacyPinID := "legacy-arrival-pin-idi0"
	if err := PebbleStore.Database.SetAllPins(100, []*pin.PinInscription{{
		Id:                 canonicalPinID,
		LegacyPinId:        legacyPinID,
		GenesisTransaction: "canonical-arrival-pin-id",
		Output:             "canonical-arrival-pin-id:0",
		ChainName:          "mvc",
		GenesisHeight:      100,
		Timestamp:          1710000000,
	}}, 1); err != nil {
		t.Fatalf("SetAllPins: %v", err)
	}

	if err := PebbleStore.SaveMrc20Arrival(&mrc20.Mrc20Arrival{
		PinId:         canonicalPinID,
		AssetOutpoint: "asset:0",
		TickId:        "tick",
		Chain:         "mvc",
		Status:        mrc20.ArrivalStatusPending,
	}); err != nil {
		t.Fatalf("SaveMrc20Arrival: %v", err)
	}

	arrival, err := PebbleStore.GetMrc20ArrivalByPinId(legacyPinID)
	if err != nil {
		t.Fatalf("GetMrc20ArrivalByPinId(%s): %v", legacyPinID, err)
	}
	if arrival.PinId != canonicalPinID {
		t.Fatalf("expected canonical pin id %s, got %s", canonicalPinID, arrival.PinId)
	}
}

func newLegacyAliasTestTx(t *testing.T) (canonicalTxID string, legacyTxID string, tx *bsvutil.Tx) {
	t.Helper()

	msg := wire.NewMsgTx(10)
	prevHash, err := chainhash.NewHashFromStr("0000000000000000000000000000000000000000000000000000000000000001")
	if err != nil {
		t.Fatalf("NewHashFromStr: %v", err)
	}
	msg.AddTxIn(wire.NewTxIn(wire.NewOutPoint(prevHash, 0), []byte{0x51}))
	msg.AddTxOut(wire.NewTxOut(546, []byte{0x51}))

	canonicalTxID, err = microvisionchain.GetNewHash(msg)
	if err != nil {
		t.Fatalf("GetNewHash: %v", err)
	}
	legacyTxID = msg.TxHash().String()
	return canonicalTxID, legacyTxID, bsvutil.NewTx(msg)
}
