package man

import (
	"testing"

	"man-p2p/mrc20"
	"man-p2p/pebblestore"

	"github.com/bytedance/sonic"
	"github.com/cockroachdb/pebble"
	"github.com/shopspring/decimal"
)

// setupMrc20ReadPathStore 建独立临时库并挂全局 PebbleStore
func setupMrc20ReadPathStore(t *testing.T) *PebbleData {
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

func mustSaveUtxo(t *testing.T, pd *PebbleData, utxo mrc20.Mrc20Utxo) {
	t.Helper()
	if err := pd.SaveMrc20Pin([]mrc20.Mrc20Utxo{utxo}); err != nil {
		t.Fatalf("SaveMrc20Pin %s: %v", utxo.TxPoint, err)
	}
}

func mrc20CountKeys(t *testing.T, pd *PebbleData, prefix string) int {
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

func amt(s string) decimal.Decimal {
	return decimal.RequireFromString(s)
}

// fixtureUtxo 构造一个 From→To 的确认态 UTXO（经 SaveMrc20Pin 走真实写路径维护全部索引）
func fixtureUtxo(txPoint, tickId, from, to string, height int64, status int, amount string) mrc20.Mrc20Utxo {
	return mrc20.Mrc20Utxo{
		TxPoint:     txPoint,
		Mrc20Id:     tickId,
		Tick:        "TT",
		Chain:       "mvc",
		FromAddress: from,
		ToAddress:   to,
		Status:      status,
		BlockHeight: height,
		AmtChange:   amt(amount),
		Timestamp:   1700000000,
	}
}

// TestMrc20InIndexFromCopyMaintenance 回归 R2②：spent 时 From 副本被更新/删除，
// 不再残留陈旧 Status（旧实现 sender 副本永远停留旧状态）
func TestMrc20InIndexFromCopyMaintenance(t *testing.T) {
	pd := setupMrc20ReadPathStore(t)
	const (
		tick = "tickA"
		from = "FromAddr1"
		to   = "ToAddr1"
		tp   = "aa11:0"
	)

	mustSaveUtxo(t, pd, fixtureUtxo(tp, tick, from, to, 100, mrc20.UtxoStatusAvailable, "50"))

	// SaveMrc20Pin 阶段：To/From 两个副本 + 主键
	if got := mrc20CountKeys(t, pd, "mrc20_in_"+from+"_"); got != 1 {
		t.Fatalf("sender copy missing after save, got %d", got)
	}

	// 路径1：MarkUtxoAsSpent（transfer/native 出块主路径）→ From 副本必须刷新为 -1
	if err := pd.MarkUtxoAsSpent(tp, to, tick, "mvc", 120); err != nil {
		t.Fatalf("MarkUtxoAsSpent: %v", err)
	}
	itVal, closer, err := pd.Database.MrcDb.Get([]byte("mrc20_in_" + from + "_" + tick + "_" + tp))
	if err != nil {
		t.Fatalf("sender copy must survive MarkUtxoAsSpent: %v", err)
	}
	var fromCopy mrc20.Mrc20Utxo
	if err := sonic.Unmarshal(itVal, &fromCopy); err != nil {
		t.Fatal(err)
	}
	closer.Close()
	if fromCopy.Status != mrc20.UtxoStatusSpent {
		t.Fatalf("sender copy status = %d, want %d (stale copy bug)", fromCopy.Status, mrc20.UtxoStatusSpent)
	}

	// 路径2：UpdateMrc20Utxo Spent 分支 → From 副本必须删除
	spent := fixtureUtxo("bb22:0", tick, from, to, 100, mrc20.UtxoStatusAvailable, "10")
	mustSaveUtxo(t, pd, spent)
	spent.Status = mrc20.UtxoStatusSpent
	if err := pd.UpdateMrc20Utxo([]*mrc20.Mrc20Utxo{&spent}, false); err != nil {
		t.Fatalf("UpdateMrc20Utxo: %v", err)
	}
	if _, closer, err := pd.Database.MrcDb.Get([]byte("mrc20_in_" + from + "_" + tick + "_bb22:0")); err == nil {
		closer.Close()
		t.Fatal("sender copy must be deleted by UpdateMrc20Utxo Spent branch")
	} else if err != pebble.ErrNotFound {
		t.Fatalf("unexpected get error: %v", err)
	}
}

// TestMrc20BalanceFromInIndex 回归 R2③：余额走 mrc20_in_ 定向前缀，
// 各状态口径与旧全扫实现一致；GetAddressBalances 只收集本地址 tick
func TestMrc20BalanceFromInIndex(t *testing.T) {
	pd := setupMrc20ReadPathStore(t)
	const (
		tick  = "tickB"
		addrA = "AddrBalanceA"
		addrB = "AddrBalanceB"
	)

	mustSaveUtxo(t, pd, fixtureUtxo("c1:0", tick, addrA, addrB, 100, mrc20.UtxoStatusAvailable, "30"))
	mustSaveUtxo(t, pd, fixtureUtxo("c2:0", tick, addrA, addrB, 101, mrc20.UtxoStatusTeleportPending, "5"))
	mustSaveUtxo(t, pd, fixtureUtxo("c3:0", tick, addrA, addrB, -1, mrc20.UtxoStatusMintPending, "2"))
	// 另一个地址的 utxo（同 tick）不应影响 addrB 的余额，但会出现在 holders
	mustSaveUtxo(t, pd, fixtureUtxo("c4:0", tick, addrA, "OtherAddr", 100, mrc20.UtxoStatusAvailable, "777"))
	// 另一个 tick 的 utxo（同地址）→ GetAddressBalances 应收集出两个 tick
	mustSaveUtxo(t, pd, fixtureUtxo("c5:0", "tickC", addrA, addrB, 100, mrc20.UtxoStatusAvailable, "9"))

	bal, err := CalculateBalanceFromUTXO("mvc", addrB, tick)
	if err != nil {
		t.Fatalf("CalculateBalanceFromUTXO: %v", err)
	}
	if !bal.Balance.Equal(amt("35")) { // Available 30 + TeleportPending 5
		t.Fatalf("balance = %s, want 35", bal.Balance)
	}
	if bal.PendingOut.String() != "5" {
		t.Fatalf("pendingOut = %s, want 5", bal.PendingOut)
	}
	if bal.PendingIn.String() != "2" {
		t.Fatalf("pendingIn = %s, want 2 (MintPending)", bal.PendingIn)
	}
	if bal.AvailableUtxos != 1 || bal.PendingUtxos != 2 {
		t.Fatalf("utxo counts avail=%d pend=%d, want 1/2", bal.AvailableUtxos, bal.PendingUtxos)
	}

	balances, err := GetAddressBalances("mvc", addrB)
	if err != nil {
		t.Fatalf("GetAddressBalances: %v", err)
	}
	if len(balances) != 2 {
		t.Fatalf("expected balances for 2 ticks, got %d", len(balances))
	}

	// 空地址 → 无余额
	empty, err := GetAddressBalances("mvc", "NobodyHere")
	if err != nil {
		t.Fatalf("GetAddressBalances empty: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected no balances for empty address, got %d", len(empty))
	}
}

// TestMrc20HoldersFromInIndex 回归 R2④：holders 走 mrc20_in_，
// sender 副本不计入持有者，余额口径 = Balance 字段跨链求和
func TestMrc20HoldersFromInIndex(t *testing.T) {
	pd := setupMrc20ReadPathStore(t)
	const tick = "tickH"
	const holderA = "HolderA"
	const holderB = "HolderB"
	const sender = "SenderX"

	mustSaveUtxo(t, pd, fixtureUtxo("h1:0", tick, sender, holderA, 100, mrc20.UtxoStatusAvailable, "100"))
	mustSaveUtxo(t, pd, fixtureUtxo("h2:0", tick, sender, holderB, 100, mrc20.UtxoStatusAvailable, "40"))
	mustSaveUtxo(t, pd, fixtureUtxo("h3:0", tick, sender, holderB, 101, mrc20.UtxoStatusAvailable, "8"))
	// holderB 花掉一枚 → Status=-1（MarkUtxoAsSpent 主路径），不再计入余额
	if err := pd.MarkUtxoAsSpent("h2:0", holderB, tick, "mvc", 120); err != nil {
		t.Fatalf("MarkUtxoAsSpent: %v", err)
	}
	// 其它 tick 不应计入
	mustSaveUtxo(t, pd, fixtureUtxo("h4:0", "tickOther", sender, holderA, 100, mrc20.UtxoStatusAvailable, "5000"))

	holders, err := pd.GetMrc20Holders(tick, 0, 50, "")
	if err != nil {
		t.Fatalf("GetMrc20Holders: %v", err)
	}
	if len(holders) != 2 {
		t.Fatalf("expected 2 holders (sender copy excluded), got %d: %+v", len(holders), holders)
	}
	byAddr := map[string]string{}
	for _, h := range holders {
		byAddr[h.Address] = h.Balance.String()
	}
	if byAddr[holderA] != "100" {
		t.Fatalf("holderA balance = %s, want 100", byAddr[holderA])
	}
	if byAddr[holderB] != "8" {
		t.Fatalf("holderB balance = %s, want 8 (spent h2 excluded)", byAddr[holderB])
	}
	if _, ok := byAddr[sender]; ok {
		t.Fatal("sender must not appear as holder")
	}

	// searchAddress 子串过滤
	filtered, err := pd.GetMrc20Holders(tick, 0, 50, "HolderB")
	if err != nil {
		t.Fatalf("GetMrc20Holders search: %v", err)
	}
	if len(filtered) != 1 || filtered[0].Address != holderB {
		t.Fatalf("search filter mismatch: %+v", filtered)
	}
}

// TestMrc20HistoryFromInIndex 回归 R2⑤：流水走 mrc20_in_ 定向前缀，
// in/out 语义与旧全扫一致（out=本地址被花掉的 UTXO）
func TestMrc20HistoryFromInIndex(t *testing.T) {
	pd := setupMrc20ReadPathStore(t)
	const (
		tick  = "tickHist"
		addrA = "HistAddr"
	)

	mustSaveUtxo(t, pd, fixtureUtxo("x1:0", tick, "Other", addrA, 100, mrc20.UtxoStatusAvailable, "50"))
	mustSaveUtxo(t, pd, fixtureUtxo("x2:0", tick, "Other", addrA, 101, mrc20.UtxoStatusAvailable, "20"))
	// 花掉 x1 → out 记录
	if err := pd.MarkUtxoAsSpent("x1:0", addrA, tick, "mvc", 130); err != nil {
		t.Fatalf("MarkUtxoAsSpent: %v", err)
	}

	records, total, err := pd.GetMrc20AddressHistoryWithDirection(tick, addrA, 0, 50, nil, nil)
	if err != nil {
		t.Fatalf("GetMrc20AddressHistoryWithDirection: %v", err)
	}
	if total != 3 { // x1_in, x1_out, x2_in
		t.Fatalf("total = %d, want 3", total)
	}
	dirs := map[string]int{}
	for _, r := range records {
		dirs[r.TxPoint+"_"+r.Direction]++
	}
	if dirs["x1:0_in"] != 1 || dirs["x1:0_out"] != 1 || dirs["x2:0_in"] != 1 {
		t.Fatalf("record set mismatch: %+v", records)
	}

	// 他人地址的流水应为空
	_, totalOther, err := pd.GetMrc20AddressHistoryWithDirection(tick, "Other", 0, 50, nil, nil)
	if err != nil {
		t.Fatalf("history other: %v", err)
	}
	if totalOther != 0 {
		t.Fatalf("other address history total = %d, want 0", totalOther)
	}
}

// TestCheckOperationtxOpTxIndex 回归 R2⑥：经 SaveMrc20Pin 写入的 UTXO
// 可经 op_tx 定向索引命中（TxPoint 与 OperationTx 两个匹配方向）；未命中回退全扫
func TestCheckOperationtxOpTxIndex(t *testing.T) {
	pd := setupMrc20ReadPathStore(t)
	const tick = "tickOp"
	const addr = "OpAddr"

	mustSaveUtxo(t, pd, fixtureUtxo("op11:0", tick, "Other", addr, 100, mrc20.UtxoStatusAvailable, "10"))

	// mempool 消费行（独立 TxPoint，携带 OperationTx）
	spender := fixtureUtxo("op22:0", tick, addr, "NextAddr", -1, mrc20.UtxoStatusTransferPending, "-10")
	spender.OperationTx = "spendtx77"
	mustSaveUtxo(t, pd, spender)

	// TxPoint 方向命中（block 相位）
	u, err := pd.CheckOperationtx("op11", false)
	if err != nil {
		t.Fatalf("CheckOperationtx: %v", err)
	}
	if u == nil || u.TxPoint != "op11:0" {
		t.Fatalf("expected op11:0 via index, got %+v", u)
	}

	// OperationTx 方向命中（mempool 相位）
	u2, err := pd.CheckOperationtxAll("spendtx77", true)
	if err != nil {
		t.Fatalf("CheckOperationtxAll: %v", err)
	}
	if len(u2) != 1 || u2[0].OperationTx != "spendtx77" || u2[0].TxPoint != "op22:0" {
		t.Fatalf("expected spendtx77/op22:0 via index, got %+v", u2)
	}

	// 不存在的 tx：索引无候选 → 回退全扫 → 仍为空
	u3, err := pd.CheckOperationtx("nosuchtx", false)
	if err != nil {
		t.Fatalf("CheckOperationtx miss: %v", err)
	}
	if u3 != nil {
		t.Fatalf("expected nil for unknown tx, got %+v", u3)
	}

	// mempool 相位过滤：BlockHeight=100 的行不以 mempool 相位命中
	u4, err := pd.CheckOperationtx("op11", true)
	if err != nil {
		t.Fatalf("CheckOperationtx mempool: %v", err)
	}
	if u4 != nil {
		t.Fatalf("block-phase row must not match mempool query, got %+v", u4)
	}
}
