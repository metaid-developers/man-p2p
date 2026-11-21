package man

import (
	"fmt"
	"testing"

	"man-p2p/mrc20"
	"man-p2p/pebblestore"

	"github.com/bytedance/sonic"
	"github.com/cockroachdb/pebble"
	"github.com/shopspring/decimal"
)

func setupReindexFixStore(t *testing.T) *PebbleData {
	t.Helper()
	origStore := PebbleStore
	db, err := pebblestore.NewDataBase(t.TempDir(), 2)
	if err != nil {
		t.Fatalf("NewDataBase: %v", err)
	}
	store := &PebbleData{Database: db}
	PebbleStore = store
	t.Cleanup(func() {
		_ = db.Close()
		PebbleStore = origStore
	})
	return store
}

func mrc20KeyExists(t *testing.T, pd *PebbleData, key string) bool {
	t.Helper()
	_, closer, err := pd.Database.MrcDb.Get([]byte(key))
	if err == nil {
		closer.Close()
		return true
	}
	if err != pebble.ErrNotFound {
		t.Fatalf("get %s: %v", key, err)
	}
	return false
}

func fixtureTx(txPoint, tickId, address, txId, chain string, height int64) *mrc20.Mrc20Transaction {
	return &mrc20.Mrc20Transaction{
		TxId:        txId,
		TxPoint:     txPoint,
		TickId:      tickId,
		Tick:        "TT",
		TxType:      "transfer",
		Direction:   "in",
		Address:     address,
		ToAddress:   address,
		Amount:      decimal.RequireFromString("10"),
		Chain:       chain,
		BlockHeight: height,
		Timestamp:   1700000000,
	}
}

func countMrc20Keys(t *testing.T, pd *PebbleData, prefix string) int {
	t.Helper()
	it, err := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: []byte(prefix + "~"),
	})
	if err != nil {
		t.Fatalf("NewIter %s: %v", prefix, err)
	}
	defer it.Close()
	n := 0
	for it.First(); it.Valid(); it.Next() {
		n++
	}
	return n
}

// TestDeleteBlockTransactionsPrefixFix 回归 R2b①：旧实现扫 mrc20_tx_
// （主键实际是 tx_{txPoint}）恒为空，区块流水从未被删；修复后主键与
// tx_tick_/tx_addr_/tx_txid_ 索引一并删除，其它区块不受影响。
func TestDeleteBlockTransactionsPrefixFix(t *testing.T) {
	pd := setupReindexFixStore(t)

	// 区块 100：两条流水（一条带地址+txId）
	if err := pd.SaveMrc20Transaction(fixtureTx("tx100:0", "tickR", "AddrR1", "hashA", "mvc", 100)); err != nil {
		t.Fatalf("SaveMrc20Transaction 1: %v", err)
	}
	if err := pd.SaveMrc20Transaction(fixtureTx("tx100:1", "tickR", "AddrR2", "hashB", "mvc", 100)); err != nil {
		t.Fatalf("SaveMrc20Transaction 2: %v", err)
	}
	// 区块 101：一条流水（不应被删）
	if err := pd.SaveMrc20Transaction(fixtureTx("tx101:0", "tickR", "AddrR1", "hashC", "mvc", 101)); err != nil {
		t.Fatalf("SaveMrc20Transaction 3: %v", err)
	}

	if err := pd.deleteBlockTransactions("mvc", 100); err != nil {
		t.Fatalf("deleteBlockTransactions: %v", err)
	}

	// 区块 100 的主键与索引全部消失
	for _, key := range []string{
		"tx_tx100:0",
		"tx_tick_tickR_000000000100_001700000000_tx100:0",
		"tx_addr_AddrR1_tickR_000000000100_001700000000_tx100:0",
		"tx_txid_hashA_tx100:0",
		"tx_tx100:1",
		"tx_txid_hashB_tx100:1",
	} {
		if mrc20KeyExists(t, pd, key) {
			t.Fatalf("key %s must be deleted for block 100", key)
		}
	}
	// 区块 101 完整保留（主键 + 三索引）
	for _, key := range []string{
		"tx_tx101:0",
		"tx_tick_tickR_000000000101_001700000000_tx101:0",
		"tx_addr_AddrR1_tickR_000000000101_001700000000_tx101:0",
		"tx_txid_hashC_tx101:0",
	} {
		if !mrc20KeyExists(t, pd, key) {
			t.Fatalf("key %s of block 101 must survive", key)
		}
	}
}

// TestDeleteMrc20TransactionsFromHeight 回归 R2b②：Step5 旧实现扫
// tx_{chain}_（主键无链名）恒为空；修复后按 tx.Chain 过滤并连带删索引，
// 且只删 >= targetHeight。
func TestDeleteMrc20TransactionsFromHeight(t *testing.T) {
	pd := setupReindexFixStore(t)

	if err := pd.SaveMrc20Transaction(fixtureTx("fh50:0", "tickF", "AddrF", "hash50", "mvc", 50)); err != nil {
		t.Fatal(err)
	}
	if err := pd.SaveMrc20Transaction(fixtureTx("fh60:0", "tickF", "AddrF", "hash60", "mvc", 60)); err != nil {
		t.Fatal(err)
	}
	// 其它链同高度（不应被删）
	if err := pd.SaveMrc20Transaction(fixtureTx("fb60:0", "tickF", "AddrF", "hashBtc", "btc", 60)); err != nil {
		t.Fatal(err)
	}

	deleted, err := pd.deleteMrc20TransactionsFromHeight("mvc", 60)
	if err != nil {
		t.Fatalf("deleteMrc20TransactionsFromHeight: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1 (only mvc@60)", deleted)
	}

	for _, key := range []string{
		"tx_fh60:0",
		"tx_tick_tickF_000000000060_001700000000_fh60:0",
		"tx_txid_hash60_fh60:0",
	} {
		if mrc20KeyExists(t, pd, key) {
			t.Fatalf("key %s (mvc@60) must be deleted", key)
		}
	}
	if !mrc20KeyExists(t, pd, "tx_fh50:0") {
		t.Fatal("mvc@50 must survive (below targetHeight)")
	}
	if !mrc20KeyExists(t, pd, "tx_fb60:0") {
		t.Fatal("btc@60 must survive (other chain)")
	}
}

// TestTeleportBlockSpentRollback 回归 R2b③：teleport 源 UTXO 硬删除后
// CleanBlock 可经 block_spent JSON 记录完整恢复（主键+索引+状态）。
func TestTeleportBlockSpentRollback(t *testing.T) {
	pd := setupReindexFixStore(t)
	const (
		tick  = "tickTp"
		from  = "TpFrom"
		to    = "TpTo"
		tp    = "tp11:0"
		chain = "mvc"
	)

	// 建源 UTXO（走真实写路径）
	mustSaveUtxo(t, pd, fixtureUtxo(tp, tick, from, to, 100, mrc20.UtxoStatusAvailable, "25"))

	// 模拟 teleport 完成路径：记 block_spent（JSON）→ 硬删源 UTXO
	spent := fixtureUtxo(tp, tick, from, to, 100, mrc20.UtxoStatusSpent, "25")
	spent.SpentAtHeight = 130
	if err := pd.SaveBlockSpentRecord(chain, 130, tp, &spent); err != nil {
		t.Fatalf("SaveBlockSpentRecord: %v", err)
	}
	if err := pd.DeleteMrc20Utxo(tp, to, tick); err != nil {
		t.Fatalf("DeleteMrc20Utxo: %v", err)
	}
	if mrc20KeyExists(t, pd, "mrc20_utxo_"+tp) {
		t.Fatal("source utxo must be hard-deleted")
	}

	// CleanBlock 回滚区块 130
	affected, err := pd.CleanBlock(chain, 130)
	if err != nil {
		t.Fatalf("CleanBlock: %v", err)
	}
	wantAffected := fmt.Sprintf("%s_%s_%s", chain, to, tick)
	if _, ok := affected[wantAffected]; !ok {
		t.Fatalf("affected addresses missing %s: %+v", wantAffected, affected)
	}

	// 源 UTXO 完整恢复：主键 + To/From 索引副本 + available_utxo
	if !mrc20KeyExists(t, pd, "mrc20_utxo_"+tp) {
		t.Fatal("source utxo main key must be restored")
	}
	val, closer, err := pd.Database.MrcDb.Get([]byte("mrc20_utxo_" + tp))
	if err != nil {
		t.Fatalf("get restored: %v", err)
	}
	var restored mrc20.Mrc20Utxo
	if err := sonic.Unmarshal(val, &restored); err != nil {
		t.Fatal(err)
	}
	closer.Close()
	if restored.Status != mrc20.UtxoStatusAvailable || restored.SpentAtHeight != 0 {
		t.Fatalf("restored status=%d spentAt=%d, want Available/0", restored.Status, restored.SpentAtHeight)
	}
	if !mrc20KeyExists(t, pd, "mrc20_in_"+to+"_"+tick+"_"+tp) {
		t.Fatal("To copy must be restored")
	}
	if !mrc20KeyExists(t, pd, "mrc20_in_"+from+"_"+tick+"_"+tp) {
		t.Fatal("From copy must be restored")
	}
	if !mrc20KeyExists(t, pd, fmt.Sprintf("available_utxo_%s_%s_%s_%s", chain, to, tick, tp)) {
		t.Fatal("available_utxo must be restored")
	}
	if mrc20KeyExists(t, pd, fmt.Sprintf("block_spent_%s_%d_%s", chain, 130, tp)) {
		t.Fatal("block_spent key must be consumed after rollback")
	}

	// 旧格式 "1" + 主键缺失：无法恢复但不报错，索引被清理
	legacySpent := fixtureUtxo("legacy:0", tick, from, to, 100, mrc20.UtxoStatusAvailable, "5")
	mustSaveUtxo(t, pd, legacySpent)
	if err := pd.Database.MrcDb.Set([]byte("block_spent_"+chain+"_140_legacy:0"), []byte("1"), nil); err != nil {
		t.Fatal(err)
	}
	if err := pd.DeleteMrc20Utxo("legacy:0", to, tick); err != nil {
		t.Fatal(err)
	}
	if _, err := pd.CleanBlock(chain, 140); err != nil {
		t.Fatalf("CleanBlock legacy: %v", err)
	}
	if mrc20KeyExists(t, pd, "block_spent_"+chain+"_140_legacy:0") {
		t.Fatal("legacy block_spent key must still be consumed")
	}
}
