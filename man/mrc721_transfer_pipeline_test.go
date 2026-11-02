package man

import (
	"strings"
	"testing"

	"man-p2p/adapter"
	"man-p2p/common"
	"man-p2p/pebblestore"
	"man-p2p/pin"
)

// MRC721 转移检测管线回归:以 2026-11-02 三笔已确认 MVC 转移为离线 fixture
// (需求文档 MRC721需求-转移索引钩子修复.md §2),覆盖
// 「vin 花 item outpoint + 1-sat vout0 + 找零 vout1」实盘形状的端到端识别,
// 以及同一条 pin 的二次转移(别名路径)。

const (
	fixtureFrom  = "1AxUdSkVdDyDreYSYVoDRFeyS1pvQdvcJx"
	fixtureTo    = "1Ljxf25BxB2uGH7qdM4vtfG2eRCNg5SYm2"
	fixtureBuyer = "1Mrc721FixtureBuyerAddress00000000000"

	fixtureItem05 = "96ad85b33226dd735aea18a4dbbbfb697bc00c48f3a6141efbf1d21f9fc9c083i0"
	fixtureTx1    = "720712463674aea23748eafe8d02342af2ff5a1054d65acadfbaab51b1c2b4c4"
	fixtureH1     = int64(197008)

	fixtureItem06 = "aa06aa06aa06aa06aa06aa06aa06aa06aa06aa06aa06aa06aa06aa06aa06aa06i0"
	fixtureTx2    = "24af8ab20f82fbb2d966259da3744e9ddb2002e9f781cebb32d45a28ab28d007"
	fixtureH2     = int64(197016)

	fixtureItem07 = "bb07bb07bb07bb07bb07bb07bb07bb07bb07bb07bb07bb07bb07bb07bb07bb07i0"
	fixtureTx3    = "05220b5727f000cf1ded42344d92b4b01356eba91d97712f1e89be6bd036f986"
	fixtureH3     = int64(197022)
)

// transferFixtureAdapter 模拟链适配器的转移检测:对 idMap 中命中的 outpoint
// 返回预先构造的转移信息(即实盘 CatchTransfer 对「vin0 花 pin + vout0 收款人」
// 形状的输出:新归属 = vout0 地址,outpoint = 转移 tx 的 vout0)。
type transferFixtureAdapter struct {
	MockIndexerAdapter
	transferMap map[string]*pin.PinTransferInfo
}

func (a *transferFixtureAdapter) CatchTransfer(idMap map[string]string) map[string]*pin.PinTransferInfo {
	out := make(map[string]*pin.PinTransferInfo)
	for outpoint := range idMap {
		if info, ok := a.transferMap[outpoint]; ok {
			out[outpoint] = info
		}
	}
	return out
}

var _ adapter.Indexer = (*transferFixtureAdapter)(nil)

// seedFixturePin 写入创世 pin 主记录(owner=铸造方,outpoint=创世输出,冒号形态)。
func seedFixturePin(t *testing.T, pinId, genesisTxid, owner string) {
	t.Helper()
	genesisOutpoint := OutpointStyleFromPinId(pinId, genesisTxid)
	if err := PebbleStore.Database.BatchInsertPins([]pin.PinInscription{{
		Id:          pinId,
		Path:        "/nft/mrc721/test-sunny-2917/fixture-" + pinId[:6],
		Operation:   "create",
		Address:     owner,
		MetaId:      common.GetMetaIdByAddress(owner),
		ChainName:   "mvc",
		Output:      genesisOutpoint,
		OutputValue: 1,
	}}); err != nil {
		t.Fatalf("seed pin %s: %v", pinId, err)
	}
}

// OutpointStyleFromPinId 用与生产一致的 "txid:idx" 形态构造 outpoint
// (测试内固定 idx=0,与实盘 pin 创世输出一致)。
func OutpointStyleFromPinId(pinId, txid string) string {
	return txid + ":0"
}

func transferTxOutpoint(txid string) string {
	return txid + ":0"
}

// runTransferBlock 模拟一个区块的 handleTransfer。
func runTransferBlock(t *testing.T, chainHeight int64, spentOutpoints []string, fixture map[string]*pin.PinTransferInfo) {
	t.Helper()
	orig := IndexerAdapter
	IndexerAdapter = map[string]adapter.Indexer{"mvc": &transferFixtureAdapter{transferMap: fixture}}
	t.Cleanup(func() { IndexerAdapter = orig })

	PebbleStore.handleTransfer("mvc", spentOutpoints, chainHeight)
}

func TestMrc721TransferPipelineThreeFixtures(t *testing.T) {
	mrc721TestSetup(t)
	origConfig := common.Config
	defer func() { common.Config = origConfig }()
	common.Config = &common.AllConfig{}
	common.Config.Module = []string{"mrc721"}

	// 铸造态:专辑 + 3 枚 item(owner=铸造方)+ 对应 pin 主记录
	mrc721Run(t,
		mrc721TestPin("fx-c", "/nft/mrc721/test-sunny-2917/collection_desc", "create",
			[]byte(`{"name":"Sunny","totalSupply":10,"royaltyRate":5}`), fixtureFrom))
	for _, itemPinId := range []string{fixtureItem05, fixtureItem06, fixtureItem07} {
		mrc721Run(t, mrc721TestPin(itemPinId, "/nft/mrc721/test-sunny-2917/item-"+itemPinId[:6], "create", []byte(`"nft"`), fixtureFrom))
		seedFixturePin(t, itemPinId, genesisTxidOf(itemPinId), fixtureFrom)
	}

	// 三笔实盘转移:每块一笔,vin 花创世 outpoint
	type fx struct {
		itemPinId, txid string
		height          int64
	}
	for _, f := range []fx{
		{fixtureItem05, fixtureTx1, fixtureH1},
		{fixtureItem06, fixtureTx2, fixtureH2},
		{fixtureItem07, fixtureTx3, fixtureH3},
	} {
		spent := OutpointStyleFromPinId(f.itemPinId, genesisTxidOf(f.itemPinId))
		runTransferBlock(t, f.height,
			[]string{spent, "unrelated-noise-outpoint:1"},
			map[string]*pin.PinTransferInfo{
				spent: {
					Address:     fixtureTo,
					FromAddress: fixtureFrom,
					Location:    f.txid,
					Output:      transferTxOutpoint(f.txid),
					OutputValue: 1,
				},
			})

		// pin 主记录(legacy 路径修复)
		pinNode, err := PebbleStore.Database.GetPinInscriptionByKey(f.itemPinId)
		if err != nil || pinNode.Id == "" {
			t.Fatalf("pin %s missing: %v", f.itemPinId, err)
		}
		if pinNode.Address != fixtureTo || !pinNode.IsTransfered {
			t.Fatalf("pin %s legacy record not updated: addr=%s isTransfered=%v", f.itemPinId, pinNode.Address, pinNode.IsTransfered)
		}
		if pinNode.Output != transferTxOutpoint(f.txid) {
			t.Fatalf("pin %s output not updated: %s", f.itemPinId, pinNode.Output)
		}

		// mrc721 聚合(验收 1)
		item, err := PebbleStore.Database.GetMrc721Item(f.itemPinId)
		if err != nil || item == nil {
			t.Fatalf("item %s missing: %v", f.itemPinId, err)
		}
		if item.OwnerAddress != fixtureTo || item.TransferCount < 1 {
			t.Fatalf("item %s owner/count wrong: %s %d", f.itemPinId, item.OwnerAddress, item.TransferCount)
		}
		if item.Output != transferTxOutpoint(f.txid) {
			t.Fatalf("item %s txOutput wrong: %s", f.itemPinId, item.Output)
		}

		// 转移历史(验收 2)
		historyCount := 0
		var first *pebblestore.Mrc721ItemTransfer
		err = PebbleStore.Database.IterateMrc721ItemTransfers(f.itemPinId, func(tr *pebblestore.Mrc721ItemTransfer) bool {
			historyCount++
			if first == nil {
				first = tr
			}
			return true
		})
		if err != nil || historyCount != 1 {
			t.Fatalf("item %s history wrong: %v %d", f.itemPinId, err, historyCount)
		}
		if first.TxId != f.txid || first.Height != f.height || first.FromAddress != fixtureFrom || first.ToAddress != fixtureTo {
			t.Fatalf("item %s history fields wrong: %+v", f.itemPinId, first)
		}

		// 转移别名(支撑同一条 pin 的后续转移识别)
		aliasKey := strings.Replace(transferTxOutpoint(f.txid), ":", "i", -1)
		canonical, err := PebbleStore.Database.GetPinAlias(aliasKey)
		if err != nil || canonical != f.itemPinId {
			t.Fatalf("transfer alias missing for %s: %v %q", aliasKey, err, canonical)
		}
	}

	// 未命中的噪声 outpoint 不产生副作用
	if item, _ := PebbleStore.Database.GetMrc721Item("unrelated-noise-outpoint"); item != nil {
		t.Fatal("noise outpoint must not create items")
	}
}

func TestMrc721TransferPipelineSecondGeneration(t *testing.T) {
	mrc721TestSetup(t)
	origConfig := common.Config
	defer func() { common.Config = origConfig }()
	common.Config = &common.AllConfig{}
	common.Config.Module = []string{"mrc721"}

	// 铸造 + 第一代转移(与上一测试相同路径,建立别名)
	mrc721Run(t,
		mrc721TestPin("fx-c", "/nft/mrc721/test-sunny-2917/collection_desc", "create",
			[]byte(`{"name":"Sunny","totalSupply":10}`), fixtureFrom),
		mrc721TestPin(fixtureItem05, "/nft/mrc721/test-sunny-2917/item-05", "create", []byte(`"nft"`), fixtureFrom))
	seedFixturePin(t, fixtureItem05, genesisTxidOf(fixtureItem05), fixtureFrom)

	genesisSpent := OutpointStyleFromPinId(fixtureItem05, genesisTxidOf(fixtureItem05))
	runTransferBlock(t, fixtureH1, []string{genesisSpent},
		map[string]*pin.PinTransferInfo{
			genesisSpent: {Address: fixtureTo, FromAddress: fixtureFrom, Location: fixtureTx1, Output: transferTxOutpoint(fixtureTx1), OutputValue: 1},
		})

	// 第二代转移:花费的是转移后的 outpoint("tx1:0"),pin 主键仍是创世 id ——
	// 必须经转移别名才能识别(f1ed15a 时代的结构性缺口,本次修复点之一)
	secondTxid := "c0ffee00c0ffee00c0ffee00c0ffee00c0ffee00c0ffee00c0ffee00c0ffee00"
	secondSpent := transferTxOutpoint(fixtureTx1)
	runTransferBlock(t, 197030, []string{secondSpent},
		map[string]*pin.PinTransferInfo{
			secondSpent: {Address: fixtureBuyer, FromAddress: fixtureTo, Location: secondTxid, Output: transferTxOutpoint(secondTxid), OutputValue: 1},
		})

	item, _ := PebbleStore.Database.GetMrc721Item(fixtureItem05)
	if item == nil || item.OwnerAddress != fixtureBuyer || item.TransferCount != 2 {
		t.Fatalf("second-generation transfer not recognized: %+v", item)
	}
	historyCount := 0
	PebbleStore.Database.IterateMrc721ItemTransfers(fixtureItem05, func(tr *pebblestore.Mrc721ItemTransfer) bool {
		historyCount++
		return true
	})
	if historyCount != 2 {
		t.Fatalf("history should have 2 entries: %d", historyCount)
	}
	pinNode, _ := PebbleStore.Database.GetPinInscriptionByKey(fixtureItem05)
	if pinNode.Id != fixtureItem05 || pinNode.Address != fixtureBuyer || pinNode.Output != transferTxOutpoint(secondTxid) {
		t.Fatalf("second-generation pin record wrong: %+v", pinNode)
	}
}

func TestMrc721TransferIdempotentReplayFixtures(t *testing.T) {
	mrc721TestSetup(t)
	origConfig := common.Config
	defer func() { common.Config = origConfig }()
	common.Config = &common.AllConfig{}
	common.Config.Module = []string{"mrc721"}

	mrc721Run(t,
		mrc721TestPin("fx-c", "/nft/mrc721/test-sunny-2917/collection_desc", "create",
			[]byte(`{"name":"Sunny","totalSupply":10}`), fixtureFrom),
		mrc721TestPin(fixtureItem05, "/nft/mrc721/test-sunny-2917/item-05", "create", []byte(`"nft"`), fixtureFrom))
	seedFixturePin(t, fixtureItem05, genesisTxidOf(fixtureItem05), fixtureFrom)

	spent := OutpointStyleFromPinId(fixtureItem05, genesisTxidOf(fixtureItem05))
	fixture := map[string]*pin.PinTransferInfo{
		spent: {Address: fixtureTo, FromAddress: fixtureFrom, Location: fixtureTx1, Output: transferTxOutpoint(fixtureTx1), OutputValue: 1},
	}
	// 同一块被重放(索引器 re-index 场景):计数与历史必须不变
	runTransferBlock(t, fixtureH1, []string{spent}, fixture)
	runTransferBlock(t, fixtureH1, []string{spent}, fixture)

	item, _ := PebbleStore.Database.GetMrc721Item(fixtureItem05)
	if item == nil || item.TransferCount != 1 {
		t.Fatalf("replayed block must not double count: %+v", item)
	}
	historyCount := 0
	PebbleStore.Database.IterateMrc721ItemTransfers(fixtureItem05, func(tr *pebblestore.Mrc721ItemTransfer) bool {
		historyCount++
		return true
	})
	if historyCount != 1 {
		t.Fatalf("replayed history must stay 1: %d", historyCount)
	}
}

// genesisTxidOf 由 fixture pinId 还原创世 txid(测试数据内部自洽)。
func genesisTxidOf(pinId string) string {
	idx := strings.LastIndex(pinId, "i")
	return pinId[:idx]
}
