package microvisionchain

import (
	"fmt"
	"testing"

	"github.com/bitcoinsv/bsvd/chaincfg/chainhash"
	"github.com/bitcoinsv/bsvd/wire"
)

func TestCatchMempoolPinsCollectsInputs(t *testing.T) {
	t.Parallel()

	prevHash := chainhash.Hash{1, 2, 3}
	tx := wire.NewMsgTx(2)
	tx.AddTxIn(wire.NewTxIn(wire.NewOutPoint(&prevHash, 7), nil))

	indexer := &Indexer{}
	pins, txIns := indexer.CatchMempoolPins([]interface{}{tx})

	if len(pins) != 0 {
		t.Fatalf("expected no pins, got %d", len(pins))
	}
	if len(txIns) != 1 {
		t.Fatalf("expected 1 tx input, got %d", len(txIns))
	}
	expected := fmt.Sprintf("%s:%d", prevHash.String(), 7)
	if txIns[0] != expected {
		t.Fatalf("expected tx input %s, got %s", expected, txIns[0])
	}
}
