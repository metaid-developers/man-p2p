package opcat

import (
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"man-p2p/common"
)

func TestCatchPinsByTxParsesOpcatOpReturnPin(t *testing.T) {
	common.Config = &common.AllConfig{
		ProtocolID:  "6d6574616964",
		SyncHost:    []string{"*"},
		BlockedHost: []string{},
	}
	common.Config.Opcat.PopCutNum = 21

	const pinScriptHex = "6a4c98066d657461696406637265617465152f70726f746f636f6c732f73696d706c6562757a7a013005312e302e30106170706c69636174696f6e2f6a736f6e4c597b22636f6e74656e74223a2268656c6c6f204f50434154222c22636f6e74656e7454797065223a22746578742f706c61696e3b7574662d38222c226174746163686d656e7473223a5b5d2c2271756f746550696e223a22227d"
	pinScript, err := hex.DecodeString(pinScriptHex)
	if err != nil {
		t.Fatalf("decode PIN script: %v", err)
	}

	indexer := &Indexer{ChainName: "opcat"}
	indexer.InitIndexer()
	if parsed := indexer.ParsePin(pinScript); parsed == nil {
		t.Fatal("expected first output to parse as a PIN")
	}
	tx := opcatVerboseTx{
		TxID: "752cd90927691885ed237b4936d7a1f69799fe4e40cb2df6e8773a2c1bc870eb",
		Vout: []opcatVerboseVout{
			{N: 0, Value: json.Number("0")},
			{N: 1, Value: json.Number("0.00003997")},
		},
	}
	tx.Vout[0].ScriptPubKey.Hex = pinScriptHex
	tx.Vout[1].ScriptPubKey.Hex = "76a9149449c2b36e3580d00c6b89406f9f9a2b20fcc25788ac"
	pins := indexer.catchPinsByVerboseTx(tx, 121347, 1778257307, "", "", 0)

	if len(pins) != 1 {
		t.Fatalf("expected one PIN, got %d", len(pins))
	}
	if pins[0].Id != "752cd90927691885ed237b4936d7a1f69799fe4e40cb2df6e8773a2c1bc870ebi0" {
		t.Fatalf("unexpected pin id: %s", pins[0].Id)
	}
	if pins[0].Path != "/protocols/simplebuzz" {
		t.Fatalf("unexpected path: %s", pins[0].Path)
	}
	if !strings.Contains(string(pins[0].ContentBody), "hello OPCAT") {
		t.Fatalf("unexpected content body: %s", string(pins[0].ContentBody))
	}
}
