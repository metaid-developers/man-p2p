package microvisionchain

import (
	"encoding/binary"
	"encoding/hex"
	"testing"

	"man-p2p/common"

	"github.com/bitcoinsv/bsvd/chaincfg/chainhash"
	"github.com/bitcoinsv/bsvd/txscript"
	"github.com/bitcoinsv/bsvd/wire"
)

const mvcFalseReturnPinID = "9baa381fe078e8bcf93b39b76c479b46dd4be7ce17a46cc6620b093d7f3333aci0"

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

func TestCatchMempoolPinsParsesFalseReturnPin(t *testing.T) {
	oldConfig := common.Config
	common.Config = &common.AllConfig{
		ProtocolID: "6d6574616964",
		SyncHost:   []string{"*"},
	}
	t.Cleanup(func() {
		common.Config = oldConfig
	})

	indexer := &Indexer{
		ChainParams: "mainnet",
		ChainName:   "mvc",
	}
	indexer.InitIndexer()

	rawTx := rawTransactionWithFalseReturnPinForTest(t)

	pins, _ := indexer.CatchMempoolPins([]interface{}{rawTx})
	if len(pins) != 1 {
		t.Fatalf("expected 1 pin, got %d", len(pins))
	}
	if pins[0].Id != mvcFalseReturnPinID {
		t.Fatalf("unexpected pin id: %s", pins[0].Id)
	}
	if pins[0].Path != "/protocols/simplebuzz" {
		t.Fatalf("unexpected path: %s", pins[0].Path)
	}
	if pins[0].GenesisHeight != -1 {
		t.Fatalf("expected mempool pin genesis height -1, got %d", pins[0].GenesisHeight)
	}
}

func rawTransactionWithFalseReturnPinForTest(t *testing.T) *RawTransaction {
	t.Helper()
	pinScript := falseReturnPinScriptForTest(t)
	ownerScript := ownerScriptForRawTransactionTest(t)
	return &RawTransaction{
		TxID: mvcFalseReturnPinID[:len(mvcFalseReturnPinID)-2],
		Vins: []TxIn{
			{
				TxID: []byte{1, 2, 3},
				Vout: uint32BytesForTest(2),
			},
		},
		Vouts: []TxOut{
			{n: 0, amount: int64BytesForTest(1), lockScript: ownerScript},
			{n: 1, amount: int64BytesForTest(0), lockScript: pinScript},
			{n: 2, amount: int64BytesForTest(9871389), lockScript: ownerScript},
		},
	}
}

func falseReturnPinScriptForTest(t *testing.T) []byte {
	t.Helper()
	protocolID, err := hex.DecodeString(common.Config.ProtocolID)
	if err != nil {
		t.Fatalf("decode protocol id: %v", err)
	}
	script, err := txscript.NewScriptBuilder().
		AddOp(txscript.OP_FALSE).
		AddOp(txscript.OP_RETURN).
		AddData(protocolID).
		AddData([]byte("create")).
		AddData([]byte("/protocols/simplebuzz")).
		AddData([]byte("0")).
		AddData([]byte("1.0")).
		AddData([]byte("application/json")).
		AddData([]byte("{}")).
		Script()
	if err != nil {
		t.Fatalf("build pin script: %v", err)
	}
	return script
}

func ownerScriptForRawTransactionTest(t *testing.T) []byte {
	t.Helper()
	script, err := txscript.NewScriptBuilder().
		AddOp(txscript.OP_DUP).
		AddOp(txscript.OP_HASH160).
		AddData([]byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}).
		AddOp(txscript.OP_EQUALVERIFY).
		AddOp(txscript.OP_CHECKSIG).
		Script()
	if err != nil {
		t.Fatalf("build owner script: %v", err)
	}
	return script
}

func int64BytesForTest(value int64) []byte {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, uint64(value))
	return buf
}

func uint32BytesForTest(value uint32) []byte {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, value)
	return buf
}
