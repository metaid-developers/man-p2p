package man

import (
	"testing"

	"man-p2p/pin"
)

// collection_desc modify(发行方专属版本链)全矩阵单测。

const mrc721Impostor = "mvc1qimpostor"

func mrc721ModifyPin(id, collectionName, address, content string) *pin.PinInscription {
	return &pin.PinInscription{
		Id:            id,
		Path:          "/nft/mrc721/" + collectionName + "/collection_desc",
		Operation:     "modify",
		ContentBody:   []byte(content),
		Address:       address,
		MetaId:        "meta-" + id,
		ChainName:     "mvc",
		GenesisHeight: 300,
		Timestamp:     1700000500,
	}
}

func TestMrc721CollectionModifyByIssuer(t *testing.T) {
	mrc721TestSetup(t)

	mrc721Run(t,
		mrc721TestPin("mc1", "/nft/mrc721/ModAlbum/collection_desc", "create",
			[]byte(`{"name":"V1","desc":"old","cover":"metafile://old","totalSupply":10,"royaltyRate":5}`), mrc721Creator),
	)

	// 发行方 modify:全量覆盖展示字段;totalSupply/royaltyRate 原样透传
	mod1 := mrc721ModifyPin("mm1", "ModAlbum", mrc721Creator,
		`{"name":"V2","desc":"new","website":"https://New.Example","cover":"metafile://newcover","metadata":"m2","totalSupply":10,"royaltyRate":5}`)
	mrc721Run(t, mod1)

	col, _ := PebbleStore.Database.GetMrc721CollectionByName("ModAlbum")
	if col == nil || col.Name != "V2" || col.Desc != "new" || col.Website != "https://New.Example" || col.Cover != "metafile://newcover" || col.Metadata != "m2" {
		t.Fatalf("modify fields not applied: %+v", col)
	}
	if col.TotalSupply != 10 || col.RoyaltyRate != 5 {
		t.Fatalf("supply/royalty should stay: %+v", col)
	}
	if len(col.ModifyHistory) != 1 || col.ModifyHistory[0] != "mm1" {
		t.Fatalf("history wrong: %v", col.ModifyHistory)
	}

	// 两次 modify,最新生效
	mod2 := mrc721ModifyPin("mm2", "ModAlbum", mrc721Creator, `{"name":"V3"}`)
	mrc721Run(t, mod2)
	col, _ = PebbleStore.Database.GetMrc721CollectionByName("ModAlbum")
	if col.Name != "V3" || col.Desc != "new" {
		t.Fatalf("second modify should win name but keep rest: %+v", col)
	}
	if len(col.ModifyHistory) != 2 || col.ModifyHistory[1] != "mm2" {
		t.Fatalf("history order wrong: %v", col.ModifyHistory)
	}

	// supply 降门:totalnum=0 时降到 1,mint 2 个 → 第 2 个超限
	mod3 := mrc721ModifyPin("mm3", "ModAlbum", mrc721Creator, `{"name":"V4","totalSupply":1}`)
	mrc721Run(t, mod3)
	mrc721Run(t,
		mrc721TestPin("mi1", "/nft/mrc721/ModAlbum/a", "create", []byte(`"1"`), mrc721Creator),
		mrc721TestPin("mi2", "/nft/mrc721/ModAlbum/b", "create", []byte(`"2"`), mrc721Creator),
	)
	col, _ = PebbleStore.Database.GetMrc721CollectionByName("ModAlbum")
	if col.TotalNum != 1 {
		t.Fatalf("lowered totalSupply should gate future mints: %+v", col)
	}
	if item, _ := PebbleStore.Database.GetMrc721Item("mi2"); item != nil {
		t.Fatal("mi2 should be rejected beyond lowered supply")
	}
}

func TestMrc721CollectionModifyPermissions(t *testing.T) {
	mrc721TestSetup(t)

	mrc721Run(t, mrc721TestPin("mc1", "/nft/mrc721/PermAlbum/collection_desc", "create",
		[]byte(`{"name":"V1"}`), mrc721Creator))

	// 非发行方 modify 被忽略
	mrc721Run(t, mrc721ModifyPin("bad1", "PermAlbum", mrc721Impostor, `{"name":"HACKED"}`))
	col, _ := PebbleStore.Database.GetMrc721CollectionByName("PermAlbum")
	if col.Name != "V1" || len(col.ModifyHistory) != 0 {
		t.Fatalf("non-issuer modify must be ignored: %+v", col)
	}

	// 非法 JSON / 越界 totalSupply / 越界 royaltyRate 整条忽略
	mrc721Run(t, mrc721ModifyPin("bad2", "PermAlbum", mrc721Creator, `not-json`))
	mrc721Run(t, mrc721ModifyPin("bad3", "PermAlbum", mrc721Creator, `{"name":"X","totalSupply":-5}`))
	mrc721Run(t, mrc721ModifyPin("bad4", "PermAlbum", mrc721Creator, `{"name":"X","royaltyRate":25}`))
	col, _ = PebbleStore.Database.GetMrc721CollectionByName("PermAlbum")
	if col.Name != "V1" || len(col.ModifyHistory) != 0 {
		t.Fatalf("invalid modifies must be ignored entirely: %+v", col)
	}
}

func TestMrc721CollectionModifyIdempotent(t *testing.T) {
	mrc721TestSetup(t)

	mrc721Run(t, mrc721TestPin("mc1", "/nft/mrc721/IdemModAlbum/collection_desc", "create",
		[]byte(`{"name":"V1"}`), mrc721Creator))
	mod := mrc721ModifyPin("im1", "IdemModAlbum", mrc721Creator, `{"name":"V2"}`)
	// 同一 modify pin 多次投递(链上 + P2P + 重放)
	mrc721Run(t, mod, mod)
	mrc721Run(t, mod)

	col, _ := PebbleStore.Database.GetMrc721CollectionByName("IdemModAlbum")
	if col.Name != "V2" || len(col.ModifyHistory) != 1 || col.ModifyHistory[0] != "im1" {
		t.Fatalf("duplicate modify ingest must be idempotent: %+v", col)
	}
}

func TestMrc721CollectionModifyPendingReplay(t *testing.T) {
	mrc721TestSetup(t)

	// modify 先于 collection 注册:暂存,注册后回放
	early1 := mrc721ModifyPin("pm1", "PendModAlbum", mrc721Creator, `{"name":"MOD1"}`)
	early1.Timestamp = 1700000100
	early2 := mrc721ModifyPin("pm2", "PendModAlbum", mrc721Creator, `{"name":"MOD2"}`)
	early2.Timestamp = 1700000200
	mrc721Run(t, early2, early1) // 先后投乱序

	if col, _ := PebbleStore.Database.GetMrc721CollectionByName("PendModAlbum"); col != nil {
		t.Fatal("collection should not exist yet")
	}

	mrc721Run(t, mrc721TestPin("pc1", "/nft/mrc721/PendModAlbum/collection_desc", "create",
		[]byte(`{"name":"BASE"}`), mrc721Creator))

	col, _ := PebbleStore.Database.GetMrc721CollectionByName("PendModAlbum")
	if col == nil || col.Name != "MOD2" {
		t.Fatalf("pending modifies should replay chronologically, latest wins: %+v", col)
	}
	if len(col.ModifyHistory) != 2 {
		t.Fatalf("both pending modifies should be in history: %v", col.ModifyHistory)
	}
	// 回放后 pending 清空
	left, _ := PebbleStore.Database.PopMrc721Pending("PendModAlbum")
	if len(left) != 0 {
		t.Fatalf("pending should be drained: %d", len(left))
	}
}

func TestMrc721ItemPathModifyIgnored(t *testing.T) {
	mrc721TestSetup(t)

	mrc721Run(t,
		mrc721TestPin("ic1", "/nft/mrc721/ItmModAlbum/collection_desc", "create", []byte(`{"name":"I"}`), mrc721Creator),
		mrc721TestPin("ii1", "/nft/mrc721/ItmModAlbum/a", "create", []byte(`"x"`), mrc721Other),
	)
	// item 路径上的 modify 不产生任何聚合效果
	itemMod := mrc721TestPin("badmod", "/nft/mrc721/ItmModAlbum/a", "modify", []byte(`"hacked"`), mrc721Other)
	mrc721Run(t, itemMod)

	item, _ := PebbleStore.Database.GetMrc721Item("ii1")
	if item == nil || item.OwnerAddress != mrc721Other || item.TransferCount != 0 {
		t.Fatalf("item modify must not affect item: %+v", item)
	}
	// revoke/hide 在 /nft/mrc721 下不生效
	revoke := mrc721TestPin("rv1", "/nft/mrc721/ItmModAlbum/collection_desc", "revoke", []byte(`{"name":"GONE"}`), mrc721Creator)
	mrc721Run(t, revoke)
	col, _ := PebbleStore.Database.GetMrc721CollectionByName("ItmModAlbum")
	if col == nil || col.Name != "I" {
		t.Fatalf("revoke must be ignored: %+v", col)
	}
}

func TestMrc721ModifyTransferAndVerifyUntouched(t *testing.T) {
	mrc721TestSetup(t)

	mrc721Run(t,
		mrc721TestPin("tc1", "/nft/mrc721/TrModAlbum/collection_desc", "create", []byte(`{"name":"T"}`), mrc721Creator),
		mrc721TestPin("ti1", "/nft/mrc721/TrModAlbum/a", "create", []byte(`"x"`), mrc721Creator),
	)
	mrc721Run(t, mrc721ModifyPin("tm1", "TrModAlbum", mrc721Creator, `{"name":"T2"}`))

	// modify 是独立新 pin,不影响归属/转移
	transferMap, pinIdByOutput := mrc721Transfer("ti1", mrc721Other, mrc721Creator, "spend:1", 546, false)
	PebbleStore.mrc721TransferHook("mvc", 400, transferMap, pinIdByOutput, 1700000600)

	item, _ := PebbleStore.Database.GetMrc721Item("ti1")
	if item == nil || item.OwnerAddress != mrc721Other || item.TransferCount != 1 {
		t.Fatalf("transfer after modify broken: %+v", item)
	}
	col, _ := PebbleStore.Database.GetMrc721CollectionByName("TrModAlbum")
	if col == nil || col.Name != "T2" || len(col.ModifyHistory) != 1 {
		t.Fatalf("collection state after transfer should be intact: %+v", col)
	}
	// creatorMinted 事实不受 modify 影响
	if !item.CreatorMinted {
		t.Fatal("creatorMinted must survive collection modify")
	}
}

func TestMrc721ReplayPinsRerunsAggregation(t *testing.T) {
	mrc721TestSetup(t)

	// 模拟"先于功能落链"的存量 pin:直接入库,不过钩子
	storePin := mrc721ModifyPin("rp1", "ReplayAlbum", mrc721Creator, `{"name":"REPLAYED"}`)
	if err := PebbleStore.Database.BatchInsertPins([]pin.PinInscription{*storePin}); err != nil {
		t.Fatalf("seed pin store: %v", err)
	}
	mrc721Run(t, mrc721TestPin("rc1", "/nft/mrc721/ReplayAlbum/collection_desc", "create",
		[]byte(`{"name":"BASE"}`), mrc721Creator))

	results := PebbleStore.Mrc721ReplayPins([]string{"rp1", "missing-pin"})
	if len(results) != 2 {
		t.Fatalf("expected 2 results: %+v", results)
	}
	if !results[0].Applied || results[0].Chain != "mvc" {
		t.Fatalf("replay should apply: %+v", results[0])
	}
	if results[1].Applied || results[1].Reason == "" {
		t.Fatalf("missing pin should report not applied: %+v", results[1])
	}
	col, _ := PebbleStore.Database.GetMrc721CollectionByName("ReplayAlbum")
	if col == nil || col.Name != "REPLAYED" || len(col.ModifyHistory) != 1 || col.ModifyHistory[0] != "rp1" {
		t.Fatalf("replayed modify not applied: %+v", col)
	}
	// 重放幂等
	PebbleStore.Mrc721ReplayPins([]string{"rp1"})
	col, _ = PebbleStore.Database.GetMrc721CollectionByName("ReplayAlbum")
	if len(col.ModifyHistory) != 1 {
		t.Fatalf("replay must be idempotent: %v", col.ModifyHistory)
	}
}
