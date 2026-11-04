package microvisionchain

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

// MempoolSpends 回归:mempool 内 plain UTXO spend 的转移要素提取,花费 txid 必须
// 为账本口径(GetNewHash,与确认后转移记录/链上可花费引用一致);wire 与
// RawTransaction(RPC 解析退路)两种形态都要覆盖。

func TestMempoolSpendsDetectsWireTxSpend(t *testing.T) {
	indexer := initTestIndexer(t)
	tx := buildVersion10Tx(t)
	spent := "1111111111111111111111111111111111111111111111111111111111111111:0"
	ledgerTxid, err := GetNewHash(tx)
	if err != nil {
		t.Fatalf("GetNewHash: %v", err)
	}

	out := indexer.MempoolSpends([]interface{}{tx}, map[string]string{spent: "1SpendFrom"})
	if len(out) != 1 {
		t.Fatalf("expected 1 spend, got %d", len(out))
	}
	info := out[spent]
	if info == nil {
		t.Fatal("spend info missing")
	}
	if !strings.HasPrefix(info.Location, ledgerTxid+":") {
		t.Fatalf("spend txid must be ledger form %s, got %s", ledgerTxid, info.Location)
	}
	if info.Address == "" {
		t.Fatal("to-address not derived")
	}
	if info.FromAddress != "1SpendFrom" {
		t.Fatalf("fromAddress not carried: %q", info.FromAddress)
	}
}

func TestMempoolSpendsHandlesRawTransactionForm(t *testing.T) {
	indexer := initTestIndexer(t)
	tx := buildVersion10Tx(t)
	var buf bytes.Buffer
	if err := tx.Serialize(&buf); err != nil {
		t.Fatalf("serialize: %v", err)
	}
	raw := &RawTransaction{Hex: hex.EncodeToString(buf.Bytes())}
	spent := "1111111111111111111111111111111111111111111111111111111111111111:0"
	ledgerTxid, err := GetNewHash(tx)
	if err != nil {
		t.Fatalf("GetNewHash: %v", err)
	}

	out := indexer.MempoolSpends([]interface{}{raw}, map[string]string{spent: "1SpendFrom"})
	if len(out) != 1 {
		t.Fatalf("raw form: expected 1 spend, got %d", len(out))
	}
	if info := out[spent]; info == nil || !strings.HasPrefix(info.Location, ledgerTxid+":") {
		t.Fatalf("raw form must resolve ledger txid: %+v", out[spent])
	}
}

func TestMempoolSpendsSkipsUntrackedInputs(t *testing.T) {
	indexer := initTestIndexer(t)
	tx := buildVersion10Tx(t)

	out := indexer.MempoolSpends([]interface{}{tx}, map[string]string{"otheroutpoint:1": "addr"})
	if len(out) != 0 {
		t.Fatalf("untracked input must not match, got %d", len(out))
	}
	// 空 idMap(无在途候选)快速返回
	if out := indexer.MempoolSpends([]interface{}{tx}, nil); len(out) != 0 {
		t.Fatalf("empty idMap must yield no spends, got %d", len(out))
	}
	// 非 wire/raw 形态跳过
	if out := indexer.MempoolSpends([]interface{}{"garbage"}, map[string]string{"otheroutpoint:1": "addr"}); len(out) != 0 {
		t.Fatalf("unknown tx form must be skipped, got %d", len(out))
	}
}
