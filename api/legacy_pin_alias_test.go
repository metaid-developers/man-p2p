package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"man-p2p/adapter"
	"man-p2p/adapter/microvisionchain"
	"man-p2p/man"
	"man-p2p/pin"

	"github.com/bitcoinsv/bsvd/chaincfg/chainhash"
	"github.com/bitcoinsv/bsvd/wire"
	"github.com/bitcoinsv/bsvutil"
)

type alphaLegacyAliasTestChain struct {
	tx *bsvutil.Tx
}

func (c *alphaLegacyAliasTestChain) InitChain() {}

func (c *alphaLegacyAliasTestChain) GetBlock(blockHeight int64) (interface{}, error) {
	return nil, errors.New("not implemented")
}

func (c *alphaLegacyAliasTestChain) GetBlockTime(blockHeight int64) (int64, error) {
	return 0, errors.New("not implemented")
}

func (c *alphaLegacyAliasTestChain) GetTransaction(txId string) (interface{}, error) {
	if c.tx == nil {
		return nil, errors.New("missing tx")
	}
	return c.tx, nil
}

func (c *alphaLegacyAliasTestChain) GetInitialHeight() int64 { return 0 }

func (c *alphaLegacyAliasTestChain) GetBestHeight() int64 { return 0 }

func (c *alphaLegacyAliasTestChain) GetBlockMsg(height int64) *pin.BlockMsg { return nil }

func (c *alphaLegacyAliasTestChain) GetMempoolTransactionList() ([]interface{}, error) {
	return nil, errors.New("not implemented")
}

func (c *alphaLegacyAliasTestChain) GetTxSizeAndFees(txHash string) (int64, int64, string, error) {
	return 0, 0, "", errors.New("not implemented")
}

func TestAlphaContentReturnsCanonicalContentForLegacyMVCVariantPinID(t *testing.T) {
	r := setupAlphaContractRouter(t)

	origAdapters := man.ChainAdapter
	t.Cleanup(func() {
		man.ChainAdapter = origAdapters
	})

	canonicalTxID, legacyTxID, tx := newAlphaLegacyAliasTestTx(t)
	man.ChainAdapter = map[string]adapter.Chain{
		"mvc": &alphaLegacyAliasTestChain{tx: tx},
	}

	canonicalPinID := canonicalTxID + "i0"
	legacyPinID := legacyTxID + "i0"
	if err := man.PebbleStore.Database.SetAllPins(100, []*pin.PinInscription{{
		Id:                 canonicalPinID,
		GenesisTransaction: canonicalTxID,
		Output:             fmt.Sprintf("%s:0", canonicalTxID),
		ChainName:          "mvc",
		GenesisHeight:      100,
		Timestamp:          1710000000,
		ContentBody:        []byte("alpha-legacy-alias"),
		ContentLength:      uint64(len("alpha-legacy-alias")),
	}}, 1); err != nil {
		t.Fatalf("SetAllPins: %v", err)
	}
	if _, err := man.PebbleStore.BackfillLegacyPinAliases("mvc", 0); err != nil {
		t.Fatalf("BackfillLegacyPinAliases: %v", err)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/content/"+legacyPinID, nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for legacy alias content, got %d with body %q", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != "alpha-legacy-alias" {
		t.Fatalf("expected content body %q, got %q", "alpha-legacy-alias", got)
	}
}

func newAlphaLegacyAliasTestTx(t *testing.T) (canonicalTxID string, legacyTxID string, tx *bsvutil.Tx) {
	t.Helper()

	msg := wire.NewMsgTx(10)
	prevHash, err := chainhash.NewHashFromStr("0000000000000000000000000000000000000000000000000000000000000002")
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
