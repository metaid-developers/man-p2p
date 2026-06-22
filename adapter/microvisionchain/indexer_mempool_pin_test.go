package microvisionchain

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"

	"man-p2p/common"
	"man-p2p/pin"

	"github.com/bitcoinsv/bsvd/chaincfg/chainhash"
	"github.com/bitcoinsv/bsvd/txscript"
	"github.com/bitcoinsv/bsvd/wire"
)

const mvcFalseReturnPinID = "9baa381fe078e8bcf93b39b76c479b46dd4be7ce17a46cc6620b093d7f3333aci0"
const mvcLargeFalseReturnPinID = "3f1bc813551bcfbcd2a306782e827e5ce2d5f72636bb98baaf9da8d966f3f601i0"
const mvcLargeFalseReturnRawTxHex = "0a00000001ccce7fb344b6b1f2b9f120ea5a13bd40b24ff0b6ddbd676b7f95031461f0c0a5020000006a47304402203da29fa6c1cd887d4210e379ffff7d79790da20eedfdfde04db875e8fc7dcfee0220478040f847e3d45e538c99af775d38b5e212861cf4395aa7d140a7cfdfbf2ad7412103256c20c4a8aa011b01ea526f748b477104809fcafbb762a61d2ac66107ea3162ffffffff0301000000000000001976a9149449c2b36e3580d00c6b89406f9f9a2b20fcc25788ac0000000000000000fd1a04006a066d657461696406637265617465152f70726f746f636f6c732f73696d706c6562757a7a013003312e30106170706c69636174696f6e2f6a736f6e4dda037b22636f6e74656e74223a225461736b203620646576656c6f706d656e74206a6f75726e616c20666f72204944426f74732041424320486f73742e5c6e436f6d6d69743a203633313064363020666561743a2061646420426f742042726f77736572207368656c6c207377697463685c6e496d706c656d656e7465642061206c69676874776569676874206f7574657220426f7420486f6d65202f20426f742042726f777365722073776974636820737472697020616e64206b65707420696e6c696e652057696e646f7773207469746c6562617220636f6e74726f6c732076697369626c65206f6e6c7920696e2042726f77736572206d6f64652e5c6e416464656420757365426f7442726f777365725368656c6c20746f206f776e20737572666163654d6f64652c206861734d6f756e74656442726f777365722c2062726f777365725265662c2070656e64696e672062726f77736572206f70656e732c206c6f63616c2d626f74206775617264207669612077696e646f772e656c656374726f6e2e6d657461626f742e6c69737428292c20616e64204d6574614170702066616c6c6261636b2068616e646c696e672e5c6e55706461746564204170702e74737820746f207072657365727665206578697374696e6720696e697469616c697a6174696f6e20616e6420486f6d652076696577206c6f6769632c206578747261637420686f6d65436f6e74656e742c206869646520536964656261722f486f6d6520626f647920696e2042726f77736572206d6f64652c206b65657020426f7442726f7773657253757266616365206d6f756e74656420616674657220666972737420656e7472792c20616e642075736520612074656d706f7261727920746f617374207374756220666f7220636f6e766572736174696f6e206f70656e696e67732e5c6e4164646564207374617469632054444420636f76657261676520696e2074657374732f626f7442726f777365725368656c6c5374617469632e746573742e6d6a7320616e642076657269666965642077697468202f55736572732f7475736d2f2e6e766d2f76657273696f6e732f6e6f64652f7632342e31332e312f62696e2f6e6f6465202d2d746573742074657374732f626f7442726f777365725368656c6c5374617469632e746573742e6d6a7320706c7573202f55736572732f7475736d2f2e6e766d2f76657273696f6e732f6e6f64652f7632342e31332e312f62696e2f6e706d2072756e206275696c642e222c22636f6e74656e7454797065223a22746578742f706c61696e3b7574662d38222c226174746163686d656e7473223a5b5d2c2271756f746550696e223a22227df9c1ca04000000001976a9149449c2b36e3580d00c6b89406f9f9a2b20fcc25788ac00000000"

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

func TestCatchMempoolPinsParsesLargeFalseReturnRawPin(t *testing.T) {
	indexer := newMempoolPinTestIndexer(t)
	rawTx := largeFalseReturnRawTransactionForTest(t)

	pins, _ := indexer.CatchMempoolPins([]interface{}{rawTx})
	assertLargeFalseReturnPin(t, pins)
}

func TestCatchMempoolPinsParsesLargeFalseReturnWirePin(t *testing.T) {
	indexer := newMempoolPinTestIndexer(t)
	raw := largeFalseReturnRawBytesForTest(t)

	var tx wire.MsgTx
	if err := tx.Deserialize(bytes.NewReader(raw)); err != nil {
		t.Fatalf("Deserialize raw tx: %v", err)
	}
	pins, _ := indexer.CatchMempoolPins([]interface{}{&tx})
	assertLargeFalseReturnPin(t, pins)
}

func newMempoolPinTestIndexer(t *testing.T) *Indexer {
	t.Helper()
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
	return indexer
}

func largeFalseReturnRawTransactionForTest(t *testing.T) *RawTransaction {
	t.Helper()
	rawTx, err := DecodeRawTransaction(largeFalseReturnRawBytesForTest(t))
	if err != nil {
		t.Fatalf("DecodeRawTransaction: %v", err)
	}
	return rawTx
}

func largeFalseReturnRawBytesForTest(t *testing.T) []byte {
	t.Helper()
	raw, err := hex.DecodeString(mvcLargeFalseReturnRawTxHex)
	if err != nil {
		t.Fatalf("decode raw tx: %v", err)
	}
	return raw
}

func assertLargeFalseReturnPin(t *testing.T, pins []*pin.PinInscription) {
	t.Helper()
	if len(pins) != 1 {
		t.Fatalf("expected 1 pin, got %d", len(pins))
	}
	if pins[0].Id != mvcLargeFalseReturnPinID {
		t.Fatalf("unexpected pin id: %s", pins[0].Id)
	}
	if pins[0].Path != "/protocols/simplebuzz" {
		t.Fatalf("unexpected path: %s", pins[0].Path)
	}
	if pins[0].GenesisHeight != -1 {
		t.Fatalf("expected mempool pin genesis height -1, got %d", pins[0].GenesisHeight)
	}
	if pins[0].ContentLength == 0 {
		t.Fatal("expected non-empty content body")
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
