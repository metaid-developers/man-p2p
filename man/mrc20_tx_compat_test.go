package man

import (
	"testing"

	"github.com/bitcoinsv/bsvd/wire"
	"github.com/bitcoinsv/bsvutil"
	"github.com/btcsuite/btcd/btcutil"
	btcwire "github.com/btcsuite/btcd/wire"
)

func TestNormalizeToBtcutilTxFromBtcutil(t *testing.T) {
	msgTx := btcwire.NewMsgTx(2)
	msgTx.AddTxOut(&btcwire.TxOut{Value: 1234, PkScript: []byte{0x51}})
	original := btcutil.NewTx(msgTx)

	normalized, err := normalizeToBtcutilTx(original)
	if err != nil {
		t.Fatalf("normalizeToBtcutilTx returned error: %v", err)
	}
	if normalized != original {
		t.Fatalf("expected btcutil tx to be returned as-is")
	}
}

func TestNormalizeToBtcutilTxFromBsvutil(t *testing.T) {
	msgTx := wire.NewMsgTx(2)
	msgTx.AddTxOut(&wire.TxOut{Value: 5678, PkScript: []byte{0x51}})
	original := bsvutil.NewTx(msgTx)

	normalized, err := normalizeToBtcutilTx(original)
	if err != nil {
		t.Fatalf("normalizeToBtcutilTx returned error: %v", err)
	}
	if normalized == nil {
		t.Fatal("expected normalized tx, got nil")
	}
	if normalized.MsgTx().Version != int32(msgTx.Version) {
		t.Fatalf("expected version %d, got %d", msgTx.Version, normalized.MsgTx().Version)
	}
	if len(normalized.MsgTx().TxOut) != 1 {
		t.Fatalf("expected 1 output, got %d", len(normalized.MsgTx().TxOut))
	}
	if normalized.MsgTx().TxOut[0].Value != msgTx.TxOut[0].Value {
		t.Fatalf("expected output value %d, got %d", msgTx.TxOut[0].Value, normalized.MsgTx().TxOut[0].Value)
	}
}

func TestNormalizeToBtcutilTxRejectsUnknownType(t *testing.T) {
	_, err := normalizeToBtcutilTx("bad-type")
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
}
