package microvisionchain

import (
	"strings"
	"testing"

	"github.com/bitcoinsv/bsvd/chaincfg"
	"github.com/bitcoinsv/bsvd/chaincfg/chainhash"
	"github.com/bitcoinsv/bsvd/txscript"
	"github.com/bitcoinsv/bsvd/wire"
	btcdChaincfg "github.com/btcsuite/btcd/chaincfg"
)

// GetOWnerAddress 账本口径回归(2026-11-03 回折):转移后的当前 UTXO 指针必须用
// 账本 txid(GetNewHash,MVC 对 version≥10 新格式交易的重排口径,与链上可花费引用、
// /tx/ 查询一致),不得用 wire 的 sha256d TxHash —— 后者不可花,下游按指针构建输入
// 会报 [-25] Missing inputs。

func initTestIndexer(t *testing.T) *Indexer {
	t.Helper()
	netParams = &chaincfg.MainNetParams
	btcNetParams = &btcdChaincfg.MainNetParams
	return &Indexer{ChainName: "mvc"}
}

// buildVersion10Tx 构造 version≥10(MVC 新格式)的单输入单输出 P2PKH 交易。
func buildVersion10Tx(t *testing.T) *wire.MsgTx {
	t.Helper()
	tx := wire.NewMsgTx(10)
	prevHash, err := chainhash.NewHashFromStr("1111111111111111111111111111111111111111111111111111111111111111")
	if err != nil {
		t.Fatalf("prev hash: %v", err)
	}
	prevOut := wire.NewOutPoint(prevHash, 0)
	tx.AddTxIn(wire.NewTxIn(prevOut, []byte{}))
	// 标准 P2PKH:OP_DUP OP_HASH160 <20B> OP_EQUALVERIFY OP_CHECKSIG
	pkScript := append([]byte{txscript.OP_DUP, txscript.OP_HASH160, 20},
		append([]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14},
			txscript.OP_EQUALVERIFY, txscript.OP_CHECKSIG)...)
	tx.AddTxOut(wire.NewTxOut(1, pkScript))
	return tx
}

func TestGetOWnerAddressUsesLedgerTxid(t *testing.T) {
	indexer := initTestIndexer(t)
	tx := buildVersion10Tx(t)

	ledgerTxid, err := GetNewHash(tx)
	if err != nil {
		t.Fatalf("GetNewHash: %v", err)
	}
	if ledgerTxid == tx.TxHash().String() {
		t.Skip("ledger txid equals sha256d for this tx shape, nothing to assert")
	}

	info, err := indexer.GetOWnerAddress("1111111111111111111111111111111111111111111111111111111111111111:0", tx)
	if err != nil {
		t.Fatalf("GetOWnerAddress: %v", err)
	}
	if !strings.HasPrefix(info.Output, ledgerTxid+":") {
		t.Fatalf("Output must use ledger txid %s, got %s", ledgerTxid, info.Output)
	}
	if strings.HasPrefix(info.Output, tx.TxHash().String()+":") {
		t.Fatalf("Output must not use sha256d txhash: %s", info.Output)
	}
	if !strings.HasPrefix(info.Location, ledgerTxid+":") {
		t.Fatalf("Location must use ledger txid, got %s", info.Location)
	}
	if info.OutputValue != 1 {
		t.Fatalf("OutputValue wrong: %d", info.OutputValue)
	}
	if info.Address == "" {
		t.Fatal("Output address must be resolved")
	}
}
