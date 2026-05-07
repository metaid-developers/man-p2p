package microvisionchain

import (
	"encoding/hex"
	"testing"

	"man-p2p/common"

	"github.com/bitcoinsv/bsvd/chaincfg/chainhash"
	"github.com/bitcoinsv/bsvd/txscript"
	"github.com/bitcoinsv/bsvd/wire"
)

func TestCatchMempoolPinsSkipsPopForMempoolPins(t *testing.T) {
	oldConfig := common.Config
	common.Config = &common.AllConfig{
		ProtocolID: "6d6574616964",
		SyncHost:   []string{"metaid"},
	}
	t.Cleanup(func() {
		common.Config = oldConfig
	})

	indexer := &Indexer{
		ChainParams: "mainnet",
		ChainName:   "mvc",
	}
	indexer.InitIndexer()

	prevHash := chainhash.Hash{1, 2, 3}
	tx := wire.NewMsgTx(2)
	tx.AddTxIn(wire.NewTxIn(wire.NewOutPoint(&prevHash, 7), nil))

	ownerScript, err := txscript.NewScriptBuilder().
		AddOp(txscript.OP_DUP).
		AddOp(txscript.OP_HASH160).
		AddData([]byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}).
		AddOp(txscript.OP_EQUALVERIFY).
		AddOp(txscript.OP_CHECKSIG).
		Script()
	if err != nil {
		t.Fatalf("build owner script: %v", err)
	}

	protocolID, err := hex.DecodeString(common.Config.ProtocolID)
	if err != nil {
		t.Fatalf("decode protocol id: %v", err)
	}
	pinScript, err := txscript.NewScriptBuilder().
		AddOp(txscript.OP_RETURN).
		AddData(protocolID).
		AddData([]byte("create")).
		AddData([]byte("metaid:/protocols/metabot-heartbeat")).
		AddData([]byte("0")).
		AddData([]byte("0")).
		AddData([]byte("text/plain")).
		AddData([]byte("ping")).
		Script()
	if err != nil {
		t.Fatalf("build pin script: %v", err)
	}

	tx.AddTxOut(wire.NewTxOut(546, ownerScript))
	tx.AddTxOut(wire.NewTxOut(0, pinScript))

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("CatchMempoolPins panicked for mempool pin: %v", r)
		}
	}()

	pins, txIns := indexer.CatchMempoolPins([]interface{}{tx})
	if len(txIns) != 1 {
		t.Fatalf("expected 1 tx input, got %d", len(txIns))
	}
	if len(pins) != 1 {
		t.Fatalf("expected 1 pin, got %d", len(pins))
	}
	if pins[0].Pop != "" {
		t.Fatalf("expected mempool pin to have empty pop, got %q", pins[0].Pop)
	}
	if pins[0].GenesisHeight != -1 {
		t.Fatalf("expected mempool pin genesis height -1, got %d", pins[0].GenesisHeight)
	}
}
