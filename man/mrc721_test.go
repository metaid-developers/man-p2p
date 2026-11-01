package man

import (
	"testing"

	"man-p2p/common"
	"man-p2p/pebblestore"
	"man-p2p/pin"
)

// MRC721 P0 单测:注册/首条生效/supply 上限/乱序自愈/大小写保留。
// 直接驱动 handleMrc721(区块 ingest 路径),不依赖链适配器。

func mrc721TestSetup(t *testing.T) {
	t.Helper()
	db, err := pebblestore.NewDataBase(t.TempDir(), 1)
	if err != nil {
		t.Fatalf("init pebble: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	PebbleStore = &PebbleData{Database: db}
}

func mrc721TestPin(id, path, op string, content []byte, address string) *pin.PinInscription {
	return &pin.PinInscription{
		Id:                 id,
		Path:               path,
		Operation:          op,
		ContentBody:        content,
		Address:            address,
		MetaId:             "meta-" + id,
		ChainName:          "mvc",
		GenesisHeight:      100,
		GenesisTransaction: "tx-" + id,
		GenesisFee:         300,
		Timestamp:          1700000000,
		Output:             id + ":0",
		OutputValue:        1000,
		ContentType:        "application/json",
	}
}

func mrc721Run(t *testing.T, pins ...*pin.PinInscription) {
	t.Helper()
	PebbleStore.handleMrc721("mvc", 100, pins)
}

const mrc721Creator = "mvc1qcreator"
const mrc721Other = "mvc1qother"
const mrc721Holder = "mvc1qholder"
const mrc721Buyer = "mvc1qbuyer"

func TestMrc721CollectionRegisterFirstWins(t *testing.T) {
	mrc721TestSetup(t)

	descA := `{"name":"My Cool Album","totalSupply":100,"royaltyRate":5,"desc":"D","website":"https://Example.COM","cover":"metafile://coverpin","metadata":{"x":1}}`
	descB := `{"name":"Imposter Album","totalSupply":1}`
	pinA := mrc721TestPin("collA", "/nft/mrc721/TestAlbum/collection_desc", "create", []byte(descA), mrc721Creator)
	pinB := mrc721TestPin("collB", "/nft/mrc721/TestAlbum/collection_desc", "create", []byte(descB), mrc721Other)

	mrc721Run(t, pinA, pinB)

	col, err := PebbleStore.Database.GetMrc721CollectionByName("TestAlbum")
	if err != nil || col == nil {
		t.Fatalf("collection not registered: %v", err)
	}
	if col.PinId != "collA" {
		t.Fatalf("first collection_desc should win, got pinid=%s", col.PinId)
	}
	if col.Name != "My Cool Album" || col.Website != "https://Example.COM" {
		t.Fatalf("value case not preserved: name=%q website=%q", col.Name, col.Website)
	}
	if col.TotalSupply != 100 || col.RoyaltyRate != 5 {
		t.Fatalf("fields wrong: totalSupply=%d royaltyRate=%d", col.TotalSupply, col.RoyaltyRate)
	}
	if col.CreateAddress != mrc721Creator || col.TotalNum != 0 {
		t.Fatalf("creator/totalnum wrong: %s %d", col.CreateAddress, col.TotalNum)
	}
	byPinId, err := PebbleStore.Database.GetMrc721CollectionByPinId("collA")
	if err != nil || byPinId == nil || byPinId.CollectionName != "TestAlbum" {
		t.Fatalf("collection by pinid lookup failed: %v", err)
	}
}

func TestMrc721DefaultsAndCIReturn(t *testing.T) {
	mrc721TestSetup(t)

	// key 大小写不敏感(TotalSupply/RoyaltyRate 驼峰),缺省 royaltyRate=0
	desc := `{"Name":"Camel Keys","TotalSupply":10}`
	mrc721Run(t, mrc721TestPin("collC", "/nft/mrc721/CIAlbum/collection_desc", "create", []byte(desc), mrc721Creator))

	col, _ := PebbleStore.Database.GetMrc721CollectionByName("CIAlbum")
	if col == nil {
		t.Fatal("collection not registered")
	}
	if col.Name != "Camel Keys" || col.TotalSupply != 10 {
		t.Fatalf("case-insensitive keys failed: %+v", col)
	}
	if col.RoyaltyRate != 0 {
		t.Fatalf("royaltyRate default should be 0, got %d", col.RoyaltyRate)
	}
}

func TestMrc721MintSeqAndCreatorMinted(t *testing.T) {
	mrc721TestSetup(t)

	mrc721Run(t,
		mrc721TestPin("c1", "/nft/mrc721/SeqAlbum/collection_desc", "create", []byte(`{"name":"S","totalSupply":-1}`), mrc721Creator),
		mrc721TestPin("i1", "/nft/mrc721/SeqAlbum/item-one", "create", []byte(`"content 1"`), mrc721Creator),
		mrc721TestPin("i2", "/nft/mrc721/SeqAlbum/item-two", "create", []byte(`"content 2"`), mrc721Other),
	)

	col, _ := PebbleStore.Database.GetMrc721CollectionByName("SeqAlbum")
	if col == nil || col.TotalNum != 2 {
		t.Fatalf("totalnum wrong: %+v err=%v", col, nil)
	}
	item1, _ := PebbleStore.Database.GetMrc721Item("i1")
	item2, _ := PebbleStore.Database.GetMrc721Item("i2")
	if item1 == nil || item1.Number != 1 || !item1.CreatorMinted {
		t.Fatalf("item1 wrong: %+v", item1)
	}
	if item2 == nil || item2.Number != 2 || item2.CreatorMinted {
		t.Fatalf("item2 wrong: %+v", item2)
	}
	if item2.OwnerAddress != mrc721Other || item2.OwnerMetaId != "meta-i2" {
		t.Fatalf("owner wrong: %+v", item2)
	}
	if item1.CollectionPinId != "c1" {
		t.Fatalf("collectionPinId wrong: %s", item1.CollectionPinId)
	}
	// 专辑内条目按收录序遍历
	var seqs []int64
	PebbleStore.Database.IterateMrc721CollectionItems("SeqAlbum", func(seq int64, itemPinId string) bool {
		seqs = append(seqs, seq)
		return true
	})
	if len(seqs) != 2 || seqs[0] != 1 || seqs[1] != 2 {
		t.Fatalf("coll item order wrong: %v", seqs)
	}
	// 持有人索引
	ownerCount := 0
	PebbleStore.Database.IterateMrc721OwnerItems(mrc721Other, "SeqAlbum", func(cn, itemPinId string) bool {
		ownerCount++
		return true
	})
	if ownerCount != 1 {
		t.Fatalf("owner index count wrong: %d", ownerCount)
	}
}

func TestMrc721SupplyCap(t *testing.T) {
	mrc721TestSetup(t)

	mrc721Run(t,
		mrc721TestPin("c", "/nft/mrc721/CapAlbum/collection_desc", "create", []byte(`{"name":"C","totalSupply":2}`), mrc721Creator),
		mrc721TestPin("m1", "/nft/mrc721/CapAlbum/a", "create", []byte(`"1"`), mrc721Creator),
		mrc721TestPin("m2", "/nft/mrc721/CapAlbum/b", "create", []byte(`"2"`), mrc721Creator),
		mrc721TestPin("m3", "/nft/mrc721/CapAlbum/c", "create", []byte(`"3"`), mrc721Creator),
	)

	col, _ := PebbleStore.Database.GetMrc721CollectionByName("CapAlbum")
	if col == nil || col.TotalNum != 2 {
		t.Fatalf("totalnum should cap at 2: %+v", col)
	}
	if item, _ := PebbleStore.Database.GetMrc721Item("m3"); item != nil {
		t.Fatalf("m3 should be rejected beyond totalSupply")
	}
}

func TestMrc721OutOfOrderPending(t *testing.T) {
	mrc721TestSetup(t)

	// mint 与 item_desc 先于 collection_desc 到达
	mrc721Run(t,
		mrc721TestPin("m1", "/nft/mrc721/OooAlbum/nft-1", "create", []byte(`"raw nft"`), mrc721Other),
		mrc721TestPin("d1", "/nft/mrc721/OooAlbum/item_desc", "create",
			[]byte(`{"items":[{"pinid":"m1","name":"Named Later","cover":"metafile://x"}]}`), mrc721Creator),
	)
	if item, _ := PebbleStore.Database.GetMrc721Item("m1"); item != nil {
		t.Fatalf("m1 should stay pending before collection register")
	}

	// 注册后回放:mint 入账 + desc 回填
	mrc721Run(t, mrc721TestPin("c1", "/nft/mrc721/OooAlbum/collection_desc", "create",
		[]byte(`{"name":"OOO","totalSupply":10}`), mrc721Creator))

	item, _ := PebbleStore.Database.GetMrc721Item("m1")
	if item == nil || item.Number != 1 {
		t.Fatalf("pending mint not replayed: %+v", item)
	}
	if item.Name != "Named Later" || item.Cover != "metafile://x" {
		t.Fatalf("pending desc not applied: %+v", item)
	}
	col, _ := PebbleStore.Database.GetMrc721CollectionByName("OooAlbum")
	if col == nil || col.TotalNum != 1 {
		t.Fatalf("totalnum after replay wrong: %+v", col)
	}
	// 回放后 pending 清空
	left, _ := PebbleStore.Database.PopMrc721Pending("OooAlbum")
	if len(left) != 0 {
		t.Fatalf("pending should be drained, left %d", len(left))
	}
}

func TestMrc721DescBeforeMint(t *testing.T) {
	mrc721TestSetup(t)

	mrc721Run(t,
		mrc721TestPin("c1", "/nft/mrc721/DescFirstAlbum/collection_desc", "create", []byte(`{"name":"DF"}`), mrc721Creator),
		mrc721TestPin("d1", "/nft/mrc721/DescFirstAlbum/item_desc", "create",
			[]byte(`{"items":[{"pinid":"late-1","name":"Late Name","desc":"Late Desc"}]}`), mrc721Creator),
	)
	if item, _ := PebbleStore.Database.GetMrc721Item("late-1"); item != nil {
		t.Fatalf("item should not exist before mint")
	}

	// 条目入账时回放 desc
	mrc721Run(t, mrc721TestPin("late-1", "/nft/mrc721/DescFirstAlbum/x", "create", []byte(`"late"`), mrc721Other))

	item, _ := PebbleStore.Database.GetMrc721Item("late-1")
	if item == nil || item.Name != "Late Name" || item.Desc != "Late Desc" {
		t.Fatalf("desc not replayed after mint: %+v", item)
	}
}

func TestMrc721DescFirstWinsPerField(t *testing.T) {
	mrc721TestSetup(t)

	mrc721Run(t,
		mrc721TestPin("c1", "/nft/mrc721/FWAlbum/collection_desc", "create", []byte(`{"name":"FW"}`), mrc721Creator),
		mrc721TestPin("i1", "/nft/mrc721/FWAlbum/a", "create", []byte(`"x"`), mrc721Creator),
		mrc721TestPin("d1", "/nft/mrc721/FWAlbum/item_desc", "create",
			[]byte(`{"items":[{"pinid":"i1","name":"First","desc":"D1"}]}`), mrc721Creator),
		mrc721TestPin("d2", "/nft/mrc721/FWAlbum/item_desc", "create",
			[]byte(`{"items":[{"pinid":"i1","name":"Second","cover":"metafile://c"}]}`), mrc721Creator),
	)

	item, _ := PebbleStore.Database.GetMrc721Item("i1")
	if item.Name != "First" {
		t.Fatalf("first desc should win per field: %q", item.Name)
	}
	if item.Desc != "D1" {
		t.Fatalf("desc field wrong: %q", item.Desc)
	}
	if item.Cover != "metafile://c" {
		t.Fatalf("second desc should fill empty field: %q", item.Cover)
	}
}

func TestMrc721InvalidIgnored(t *testing.T) {
	mrc721TestSetup(t)

	mrc721Run(t,
		// royaltyRate 超范围
		mrc721TestPin("bad1", "/nft/mrc721/BadAlbum/collection_desc", "create", []byte(`{"name":"B","royaltyRate":25}`), mrc721Creator),
		// totalSupply 超范围
		mrc721TestPin("bad2", "/nft/mrc721/BadAlbum2/collection_desc", "create", []byte(`{"name":"B","totalSupply":-5}`), mrc721Creator),
		// 非法 JSON
		mrc721TestPin("bad3", "/nft/mrc721/BadAlbum3/collection_desc", "create", []byte(`not-json`), mrc721Creator),
		// 非 create 操作
		mrc721TestPin("bad4", "/nft/mrc721/BadAlbum/collection_desc", "modify", []byte(`{"name":"B"}`), mrc721Creator),
		// 非 /nft/mrc721 路径
		mrc721TestPin("bad5", "/info/file/x", "create", []byte(`{"name":"B"}`), mrc721Creator),
		// 路径段不足
		mrc721TestPin("bad6", "/nft/mrc721", "create", []byte(`{"name":"B"}`), mrc721Creator),
	)

	for _, name := range []string{"BadAlbum", "BadAlbum2", "BadAlbum3"} {
		if col, _ := PebbleStore.Database.GetMrc721CollectionByName(name); col != nil {
			t.Fatalf("collection %s should not be registered", name)
		}
	}
}

// ---------- P1 转移钩子 ----------

func mrc721Transfer(itemPinId, to, from, output string, value int64, meltdown bool) (map[string]*pin.PinTransferInfo, map[string]string) {
	transferMap := map[string]*pin.PinTransferInfo{
		itemPinId + ":0": {
			Address:     to,
			FromAddress: from,
			Output:      output,
			OutputValue: value,
			Location:    "spendtx-" + itemPinId,
			IsMeltdown:  meltdown,
		},
	}
	pinIdByOutput := map[string]string{itemPinId + ":0": itemPinId}
	return transferMap, pinIdByOutput
}

func TestMrc721TransferUpdatesOwner(t *testing.T) {
	mrc721TestSetup(t)

	mrc721Run(t,
		mrc721TestPin("c1", "/nft/mrc721/TrAlbum/collection_desc", "create", []byte(`{"name":"T"}`), mrc721Creator),
		mrc721TestPin("n1", "/nft/mrc721/TrAlbum/a", "create", []byte(`"nft"`), mrc721Other),
	)

	transferMap, pinIdByOutput := mrc721Transfer("n1", mrc721Creator, mrc721Other, "spendtx-n1:1", 546, false)
	PebbleStore.mrc721TransferHook("mvc", 200, transferMap, pinIdByOutput, 1700000100)

	item, _ := PebbleStore.Database.GetMrc721Item("n1")
	if item == nil || item.OwnerAddress != mrc721Creator || item.TransferCount != 1 {
		t.Fatalf("owner not updated: %+v", item)
	}
	if item.OwnerMetaId == "" {
		t.Fatalf("owner metaid missing")
	}
	if item.Output != "spendtx-n1:1" || item.OutputValue != 546 {
		t.Fatalf("current outpoint not updated: %s %d", item.Output, item.OutputValue)
	}
	// 持有人索引换 key
	newCount, oldCount := 0, 0
	PebbleStore.Database.IterateMrc721OwnerItems(mrc721Creator, "TrAlbum", func(cn, ip string) bool { newCount++; return true })
	PebbleStore.Database.IterateMrc721OwnerItems(mrc721Other, "TrAlbum", func(cn, ip string) bool { oldCount++; return true })
	if newCount != 1 || oldCount != 0 {
		t.Fatalf("owner index not swapped: new=%d old=%d", newCount, oldCount)
	}
	// 转移历史
	histories := 0
	var first *pebblestore.Mrc721ItemTransfer
	err := PebbleStore.Database.IterateMrc721ItemTransfers("n1", func(tr *pebblestore.Mrc721ItemTransfer) bool {
		histories++
		if first == nil {
			first = tr
		}
		return true
	})
	if err != nil || histories != 1 {
		t.Fatalf("transfer history not recorded: %v %d", err, histories)
	}
	if first.FromAddress != mrc721Other || first.ToAddress != mrc721Creator || first.Height != 200 {
		t.Fatalf("transfer history fields wrong: %+v", first)
	}
}

func TestMrc721MeltdownBurns(t *testing.T) {
	mrc721TestSetup(t)

	mrc721Run(t,
		mrc721TestPin("c1", "/nft/mrc721/BurnAlbum/collection_desc", "create", []byte(`{"name":"B"}`), mrc721Creator),
		mrc721TestPin("n1", "/nft/mrc721/BurnAlbum/a", "create", []byte(`"nft"`), mrc721Creator),
	)

	transferMap, pinIdByOutput := mrc721Transfer("n1", mrc721Creator, mrc721Creator, "melttx:0", 546, true)
	PebbleStore.mrc721TransferHook("mvc", 201, transferMap, pinIdByOutput, 1700000200)

	item, _ := PebbleStore.Database.GetMrc721Item("n1")
	if item == nil || !item.Burned {
		t.Fatalf("meltdown should mark burned: %+v", item)
	}
	// 持有人视图移除
	ownerCount := 0
	PebbleStore.Database.IterateMrc721OwnerItems(mrc721Creator, "BurnAlbum", func(cn, ip string) bool { ownerCount++; return true })
	if ownerCount != 0 {
		t.Fatalf("burned item should leave owner view: %d", ownerCount)
	}
	// genesis outpoint 保持不变(熔化不改写 Output)
	if item.Output != "n1:0" {
		t.Fatalf("burned item output should stay genesis: %s", item.Output)
	}
}

func TestMrc721TransferNonItemNoop(t *testing.T) {
	mrc721TestSetup(t)

	// 非 MRC-721 pin 的转移:钩子应无副作用
	transferMap, pinIdByOutput := mrc721Transfer("plain-pin", mrc721Creator, mrc721Other, "x:1", 1000, false)
	PebbleStore.mrc721TransferHook("mvc", 202, transferMap, pinIdByOutput, 1700000300)

	if item, _ := PebbleStore.Database.GetMrc721Item("plain-pin"); item != nil {
		t.Fatalf("non-item pin should not be indexed")
	}
}

// ---------- 幂等:同一 pin/转移事件经链上与 P2P 双路径到达、或回填重跑 ----------

func TestMrc721DoubleMintIdempotent(t *testing.T) {
	mrc721TestSetup(t)

	pin1 := mrc721TestPin("c1", "/nft/mrc721/IdemAlbum/collection_desc", "create", []byte(`{"name":"I"}`), mrc721Creator)
	item1 := mrc721TestPin("i1", "/nft/mrc721/IdemAlbum/a", "create", []byte(`"x"`), mrc721Creator)
	// 同一批次重复投递 + 跨批次重复投递
	mrc721Run(t, pin1, item1, item1)
	mrc721Run(t, item1)

	col, _ := PebbleStore.Database.GetMrc721CollectionByName("IdemAlbum")
	if col == nil || col.TotalNum != 1 {
		t.Fatalf("double mint should not double count: %+v", col)
	}
	item, _ := PebbleStore.Database.GetMrc721Item("i1")
	if item == nil || item.Number != 1 {
		t.Fatalf("item number should stay 1: %+v", item)
	}
	listed := 0
	PebbleStore.Database.IterateMrc721CollectionItems("IdemAlbum", func(seq int64, id string) bool { listed++; return true })
	if listed != 1 {
		t.Fatalf("coll item list should have 1 entry: %d", listed)
	}
}

func TestMrc721DoubleTransferIdempotent(t *testing.T) {
	mrc721TestSetup(t)

	mrc721Run(t,
		mrc721TestPin("c1", "/nft/mrc721/IdemTrAlbum/collection_desc", "create", []byte(`{"name":"IT"}`), mrc721Creator),
		mrc721TestPin("i1", "/nft/mrc721/IdemTrAlbum/a", "create", []byte(`"x"`), mrc721Creator),
	)
	transferMap, pinIdByOutput := mrc721Transfer("i1", mrc721Other, mrc721Creator, "spend:1", 546, false)
	PebbleStore.mrc721TransferHook("mvc", 210, transferMap, pinIdByOutput, 1700000400)
	PebbleStore.mrc721TransferHook("mvc", 210, transferMap, pinIdByOutput, 1700000400)

	item, _ := PebbleStore.Database.GetMrc721Item("i1")
	if item == nil || item.TransferCount != 1 || item.OwnerAddress != mrc721Other {
		t.Fatalf("double transfer should be idempotent: %+v", item)
	}
	historyCount := 0
	PebbleStore.Database.IterateMrc721ItemTransfers("i1", func(tr *pebblestore.Mrc721ItemTransfer) bool { historyCount++; return true })
	if historyCount != 1 {
		t.Fatalf("transfer history should have 1 entry: %d", historyCount)
	}
}

func TestMrc721P2PIngestHook(t *testing.T) {
	mrc721TestSetup(t)

	origConfig := common.Config
	defer func() { common.Config = origConfig }()
	common.Config = &common.AllConfig{}
	common.Config.Module = []string{"mrc721"}

	// 确认 pin(GenesisHeight>0)经 P2P ingest 应聚合
	confirmed := mrc721TestPin("p2p-i1", "/nft/mrc721/P2PAlbum/a", "create", []byte(`"x"`), mrc721Creator)
	if err := IngestP2PPin(confirmed); err != nil {
		t.Fatalf("ingest confirmed: %v", err)
	}
	// 专辑未注册 → 暂存 pending(不是丢失)
	pending, _ := PebbleStore.Database.PopMrc721Pending("P2PAlbum")
	if len(pending) != 1 {
		t.Fatalf("confirmed p2p pin should be aggregated (pending), got %d", len(pending))
	}

	// mempool pin(GenesisHeight<0)不聚合
	mempoolPin := mrc721TestPin("p2p-i2", "/nft/mrc721/P2PAlbum/b", "create", []byte(`"y"`), mrc721Creator)
	mempoolPin.GenesisHeight = -1
	if err := IngestP2PPin(mempoolPin); err != nil {
		t.Fatalf("ingest mempool: %v", err)
	}
	pending2, _ := PebbleStore.Database.PopMrc721Pending("P2PAlbum")
	if len(pending2) != 0 {
		t.Fatalf("mempool p2p pin should not be aggregated, got %d", len(pending2))
	}
}
