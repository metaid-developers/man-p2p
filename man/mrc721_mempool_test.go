package man

import (
	"testing"

	"man-p2p/adapter"
	"man-p2p/common"
	"man-p2p/pebblestore"
	"man-p2p/pin"
)

// MRC-721 未确认转移可见性(IDBots 缺陷):mempool 内已花 NFT outpoint 在确认前
// 必须经 pending 通道可见(读侧据此区分「发了还没确认」和「没发/丢了」),
// 确认后由转移钩子幂等并入历史并清算 pending。

// mempoolSpendFixtureAdapter 模拟链适配器的 mempool 转移检测:对 idMap 命中的
// outpoint 返回预置转移要素(与实盘 GetOWnerAddress 对「vin 花 item outpoint +
// vout0 收款人」形状的输出同构,Location 为账本口径 "txid:idx:offset")。
type mempoolSpendFixtureAdapter struct {
	MockIndexerAdapter
	spends map[string]*pin.PinTransferInfo
}

func (a *mempoolSpendFixtureAdapter) MempoolSpends(txList []interface{}, idMap map[string]string) map[string]*pin.PinTransferInfo {
	out := make(map[string]*pin.PinTransferInfo)
	for outpoint := range idMap {
		if info, ok := a.spends[outpoint]; ok {
			out[outpoint] = info
		}
	}
	return out
}

var _ adapter.Indexer = (*mempoolSpendFixtureAdapter)(nil)

// seedMempoolScanItem 注册专辑 + item + 对应 pin 主记录(创世输出形态)。
// mrc721TestPin 的 Output 是 "<pinId>:0" 通用形态;生产中 item.Output 与
// pin.Output 同源(账本口径 "txid:idx"),此处把 item 输出改写为一致形态。
func seedMempoolScanItem(t *testing.T) (itemPinId, genesisSpent string) {
	t.Helper()
	mrc721Run(t,
		mrc721TestPin("mp-c", "/nft/mrc721/MempoolAlbum/collection_desc", "create",
			[]byte(`{"name":"Mempool","totalSupply":10}`), fixtureFrom),
		mrc721TestPin(fixtureItem05, "/nft/mrc721/MempoolAlbum/item-05", "create", []byte(`"nft"`), fixtureFrom))
	seedFixturePin(t, fixtureItem05, genesisTxidOf(fixtureItem05), fixtureFrom)
	spent := OutpointStyleFromPinId(fixtureItem05, genesisTxidOf(fixtureItem05))
	item, err := PebbleStore.Database.GetMrc721Item(fixtureItem05)
	if err != nil || item == nil {
		t.Fatalf("seeded item missing: %v", err)
	}
	item.Output = spent
	if err := PebbleStore.Database.SaveMrc721Item(item); err != nil {
		t.Fatalf("align item output: %v", err)
	}
	return fixtureItem05, spent
}

func runMempoolScan(t *testing.T, spends map[string]*pin.PinTransferInfo, txIns []string) {
	t.Helper()
	PebbleStore.mrc721MempoolSpendScan("mvc", &mempoolSpendFixtureAdapter{spends: spends},
		[]interface{}{"stub-mempool-tx"}, txIns)
}

func mempoolSpendFixture(spent, txid, to string) map[string]*pin.PinTransferInfo {
	return map[string]*pin.PinTransferInfo{
		spent: {Address: to, FromAddress: fixtureFrom, Location: txid + ":0:0", Output: txid + ":0", OutputValue: 1},
	}
}

func TestMrc721MempoolSpendScanRecordsPending(t *testing.T) {
	mrc721TestSetup(t)
	origConfig := common.Config
	defer func() { common.Config = origConfig }()
	common.Config = &common.AllConfig{}
	common.Config.Module = []string{"mrc721"}

	itemPinId, spent := seedMempoolScanItem(t)
	memTxid := "75c6d70e586866b75d828b52ea7d9fda1ef4cd73c98c5b6ac3076d755c6e50a2"

	// 非 MRC-721 条目的 pin 也被花费:不产生 pending(扫描只认已跟踪条目)
	if err := PebbleStore.Database.BatchInsertPins([]pin.PinInscription{{
		Id: "plain-pin-1", Path: "/protocols/simplebuzz/other", Operation: "create",
		Address: fixtureFrom, ChainName: "mvc", Output: "plain-pin-tx:0", OutputValue: 546,
	}}); err != nil {
		t.Fatalf("seed plain pin: %v", err)
	}

	runMempoolScan(t, mempoolSpendFixture(spent, memTxid, fixtureTo), []string{spent, "plain-pin-tx:0", "noise-outpoint:1"})

	pt, err := PebbleStore.Database.GetMrc721PendingTransfer(itemPinId)
	if err != nil || pt == nil {
		t.Fatalf("pending transfer not recorded: %v %+v", err, pt)
	}
	if pt.ChainName != "mvc" || pt.SpentOutpoint != spent || pt.TxId != memTxid {
		t.Fatalf("pending fields wrong: %+v", pt)
	}
	if pt.FromAddress != fixtureFrom || pt.ToAddress != fixtureTo {
		t.Fatalf("pending addresses wrong: %+v", pt)
	}
	if pt.FirstSeenAt <= 0 {
		t.Fatalf("firstSeenAt not set: %d", pt.FirstSeenAt)
	}

	// 非条目 pin 的花费不产生 pending
	if stray, _ := PebbleStore.Database.GetMrc721PendingTransfer("plain-pin-1"); stray != nil {
		t.Fatalf("non-item pin spend must not produce pending: %+v", stray)
	}

	// 同一笔交易重扫:保留首见时间(回拨后验证不被 now 覆盖)
	pt.FirstSeenAt = 12345
	if err := PebbleStore.Database.SaveMrc721PendingTransfer(pt); err != nil {
		t.Fatalf("backdate pending: %v", err)
	}
	runMempoolScan(t, mempoolSpendFixture(spent, memTxid, fixtureTo), []string{spent})
	pt, _ = PebbleStore.Database.GetMrc721PendingTransfer(itemPinId)
	if pt == nil || pt.FirstSeenAt != 12345 || pt.TxId != memTxid {
		t.Fatalf("rescan must keep firstSeenAt/txId: %+v", pt)
	}

	// RBF 替换为新交易:整条覆盖(latest-wins),首见时间刷新
	replaceTxid := "24af8ab20f82fbb2d966259da3744e9ddb2002e9f781cebb32d45a28ab28d007"
	runMempoolScan(t, mempoolSpendFixture(spent, replaceTxid, fixtureBuyer), []string{spent})
	pt, _ = PebbleStore.Database.GetMrc721PendingTransfer(itemPinId)
	if pt == nil || pt.TxId != replaceTxid || pt.ToAddress != fixtureBuyer || pt.FirstSeenAt == 12345 {
		t.Fatalf("replacement tx must overwrite pending: %+v", pt)
	}
}

func TestMrc721MempoolSpendScanReconcilesVanished(t *testing.T) {
	mrc721TestSetup(t)
	origConfig := common.Config
	defer func() { common.Config = origConfig }()
	common.Config = &common.AllConfig{}
	common.Config.Module = []string{"mrc721"}

	itemPinId, spent := seedMempoolScanItem(t)
	memTxid := "75c6d70e586866b75d828b52ea7d9fda1ef4cd73c98c5b6ac3076d755c6e50a2"

	runMempoolScan(t, mempoolSpendFixture(spent, memTxid, fixtureTo), []string{spent})
	if pt, _ := PebbleStore.Database.GetMrc721PendingTransfer(itemPinId); pt == nil {
		t.Fatal("precondition: pending should exist")
	}

	// 交易从 mempool 消失(丢弃/已确认前窗口):空扫描轮清除本链 pending
	runMempoolScan(t, nil, nil)
	if pt, _ := PebbleStore.Database.GetMrc721PendingTransfer(itemPinId); pt != nil {
		t.Fatalf("vanished tx pending must be reconciled away: %+v", pt)
	}

	// 他链 pending 不受本轮扫描影响
	if err := PebbleStore.Database.SaveMrc721PendingTransfer(&pebblestore.Mrc721PendingTransfer{
		ItemPinId: "other-chain-item", ChainName: "btc", SpentOutpoint: "x:0", TxId: "y",
	}); err != nil {
		t.Fatalf("seed other-chain pending: %v", err)
	}
	runMempoolScan(t, nil, nil)
	if pt, _ := PebbleStore.Database.GetMrc721PendingTransfer("other-chain-item"); pt == nil {
		t.Fatal("other-chain pending must survive this chain's reconcile")
	}
}

func TestMrc721ConfirmedTransferSettlesPending(t *testing.T) {
	mrc721TestSetup(t)
	origConfig := common.Config
	defer func() { common.Config = origConfig }()
	common.Config = &common.AllConfig{}
	common.Config.Module = []string{"mrc721"}

	itemPinId, spent := seedMempoolScanItem(t)
	memTxid := "75c6d70e586866b75d828b52ea7d9fda1ef4cd73c98c5b6ac3076d755c6e50a2"

	runMempoolScan(t, mempoolSpendFixture(spent, memTxid, fixtureTo), []string{spent})

	// 同一笔交易确认入块:归属改写 + 转移历史落库 + pending 清算(pending 并入
	// 历史的幂等改写路径)
	runTransferBlock(t, 197299, []string{spent}, map[string]*pin.PinTransferInfo{
		spent: {Address: fixtureTo, FromAddress: fixtureFrom, Location: memTxid + ":0:0", Output: memTxid + ":0", OutputValue: 1},
	})

	item, err := PebbleStore.Database.GetMrc721Item(itemPinId)
	if err != nil || item == nil || item.OwnerAddress != fixtureTo || item.TransferCount != 1 {
		t.Fatalf("confirmed transfer not applied: %v %+v", err, item)
	}
	historyCount := 0
	var recorded *pebblestore.Mrc721ItemTransfer
	if err := PebbleStore.Database.IterateMrc721ItemTransfers(itemPinId, func(tr *pebblestore.Mrc721ItemTransfer) bool {
		historyCount++
		recorded = tr
		return true
	}); err != nil || historyCount != 1 {
		t.Fatalf("history wrong: %v %d", err, historyCount)
	}
	if recorded.TxId != memTxid+":0:0" || recorded.ToAddress != fixtureTo {
		t.Fatalf("history fields wrong: %+v", recorded)
	}
	if pt, _ := PebbleStore.Database.GetMrc721PendingTransfer(itemPinId); pt != nil {
		t.Fatalf("pending must be settled on confirmation: %+v", pt)
	}

	// 转移钩子重放(幂等):不回写 pending、不重复计数
	runTransferBlock(t, 197299, []string{spent}, map[string]*pin.PinTransferInfo{
		spent: {Address: fixtureTo, FromAddress: fixtureFrom, Location: memTxid + ":0:0", Output: memTxid + ":0", OutputValue: 1},
	})
	item, _ = PebbleStore.Database.GetMrc721Item(itemPinId)
	if item.TransferCount != 1 {
		t.Fatalf("replayed confirmation must not double count: %d", item.TransferCount)
	}
	if pt, _ := PebbleStore.Database.GetMrc721PendingTransfer(itemPinId); pt != nil {
		t.Fatalf("replayed confirmation must not resurrect pending: %+v", pt)
	}
}
