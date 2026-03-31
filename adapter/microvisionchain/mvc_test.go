package microvisionchain

import (
	"errors"
	"testing"

	"github.com/bitcoinsv/bsvd/chaincfg/chainhash"
	"github.com/bitcoinsv/bsvd/wire"
	"github.com/bitcoinsv/bsvutil"
)

func TestGetMempoolTransactionListReturnsTransactions(t *testing.T) {
	oldGetRawMempool := getRawMempool
	oldGetRawTransaction := getRawTransaction
	defer func() {
		getRawMempool = oldGetRawMempool
		getRawTransaction = oldGetRawTransaction
	}()

	hash1 := mustNewHash(t, "0000000000000000000000000000000000000000000000000000000000000001")
	hash2 := mustNewHash(t, "0000000000000000000000000000000000000000000000000000000000000002")

	getRawMempool = func() ([]*chainhash.Hash, error) {
		return []*chainhash.Hash{hash1, hash2}, nil
	}
	getRawTransaction = func(hash *chainhash.Hash) (*bsvutil.Tx, error) {
		switch hash.String() {
		case hash1.String():
			msg := wire.NewMsgTx(1)
			msg.AddTxOut(&wire.TxOut{Value: 111, PkScript: []byte{0x51}})
			return bsvutil.NewTx(msg), nil
		case hash2.String():
			return nil, errors.New("skip broken tx")
		default:
			t.Fatalf("unexpected hash requested: %s", hash.String())
			return nil, nil
		}
	}

	chain := &MicroVisionChain{}
	list, err := chain.GetMempoolTransactionList()
	if err != nil {
		t.Fatalf("GetMempoolTransactionList returned error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 transaction, got %d", len(list))
	}
	if _, ok := list[0].(*wire.MsgTx); !ok {
		t.Fatalf("expected wire.MsgTx in list, got %T", list[0])
	}
}

func TestGetMempoolTransactionListReturnsMempoolError(t *testing.T) {
	oldGetRawMempool := getRawMempool
	oldGetRawTransaction := getRawTransaction
	defer func() {
		getRawMempool = oldGetRawMempool
		getRawTransaction = oldGetRawTransaction
	}()

	getRawMempool = func() ([]*chainhash.Hash, error) {
		return nil, errors.New("boom")
	}
	getRawTransaction = func(hash *chainhash.Hash) (*bsvutil.Tx, error) {
		t.Fatal("getRawTransaction should not be called when mempool fetch fails")
		return nil, nil
	}

	chain := &MicroVisionChain{}
	if _, err := chain.GetMempoolTransactionList(); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func mustNewHash(t *testing.T, value string) *chainhash.Hash {
	t.Helper()

	hash, err := chainhash.NewHashFromStr(value)
	if err != nil {
		t.Fatalf("NewHashFromStr(%q) error: %v", value, err)
	}
	return hash
}
