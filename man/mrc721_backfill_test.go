package man

import (
	"testing"

	"man-p2p/pebblestore"
	"man-p2p/pin"
)

// MRC721 回填入口单测:覆盖乱序重放、转移时间线重放、归属/outpoint 校正、熔化标记。

func TestMrc721BackfillReplayAndReconcile(t *testing.T) {
	mrc721TestSetup(t)

	// 模拟快照记录:item 已由 creator 转移给 holder(Address=当前持有人,InitialOwner=创世)
	coll := mrc721TestPin("bc1", "/nft/mrc721/BFAlbum/collection_desc", "create", []byte(`{"name":"BF"}`), mrc721Creator)
	item := mrc721TestPin("bi1", "/nft/mrc721/BFAlbum/nft-1", "create", []byte(`"nft"`), mrc721Holder)
	item.InitialOwner = mrc721Creator
	item.Output = "spendtx-bi1:1"
	item.OutputValue = 546

	// 乱序:item 先于 collection(经 pending 自愈);重放用创世持有人(Address=InitialOwner,MetaId 重算)
	replay := []*pin.PinInscription{item, coll}
	for _, p := range replay {
		if p.InitialOwner != "" {
			p.Address = p.InitialOwner
		}
	}
	PebbleStore.Mrc721BackfillRunPins("backfill", replay)

	col, _ := PebbleStore.Database.GetMrc721CollectionByName("BFAlbum")
	if col == nil || col.TotalNum != 1 {
		t.Fatalf("collection not replayed: %+v", col)
	}
	it, _ := PebbleStore.Database.GetMrc721Item("bi1")
	if it == nil || it.OwnerAddress != mrc721Creator || !it.CreatorMinted {
		t.Fatalf("mint replay with genesis owner failed: %+v", it)
	}

	// 归属校正:按快照主记录(当前持有人 holder + 最新 outpoint)
	if !PebbleStore.Mrc721BackfillReconcileOwner("bi1", mrc721Holder, "spendtx-bi1:1", 546, false) {
		t.Fatal("reconcile should find the item")
	}
	it, _ = PebbleStore.Database.GetMrc721Item("bi1")
	if it.OwnerAddress != mrc721Holder || it.OwnerMetaId == "" {
		t.Fatalf("owner not reconciled: %+v", it)
	}
	if it.Output != "spendtx-bi1:1" || it.OutputValue != 546 {
		t.Fatalf("outpoint not reconciled: %s %d", it.Output, it.OutputValue)
	}
	if !it.CreatorMinted {
		t.Fatal("creatorMinted fact must survive transfer")
	}
	holderCount, creatorCount := 0, 0
	PebbleStore.Database.IterateMrc721OwnerItems(mrc721Holder, "BFAlbum", func(cn, ip string) bool { holderCount++; return true })
	PebbleStore.Database.IterateMrc721OwnerItems(mrc721Creator, "BFAlbum", func(cn, ip string) bool { creatorCount++; return true })
	if holderCount != 1 || creatorCount != 0 {
		t.Fatalf("owner index wrong after reconcile: holder=%d creator=%d", holderCount, creatorCount)
	}
}

func TestMrc721BackfillTransferHistoryReplay(t *testing.T) {
	mrc721TestSetup(t)

	coll := mrc721TestPin("bc1", "/nft/mrc721/THAlbum/collection_desc", "create", []byte(`{"name":"TH"}`), mrc721Creator)
	item := mrc721TestPin("bi1", "/nft/mrc721/THAlbum/nft-1", "create", []byte(`"nft"`), mrc721Creator)
	PebbleStore.Mrc721BackfillRunPins("backfill", []*pin.PinInscription{coll, item})

	// 历史转移时间线:creator→holder→buyer(按高度升序重放)
	histories := []*pin.PinTransferHistory{
		{PinId: "bi1", TransferHeight: 200, TransferTime: 100, TransferTx: "tx-a", ChainName: "btc", FromAddress: mrc721Creator, ToAddress: mrc721Holder},
		{PinId: "bi1", TransferHeight: 300, TransferTime: 200, TransferTx: "tx-b", ChainName: "btc", FromAddress: mrc721Holder, ToAddress: mrc721Buyer},
	}
	PebbleStore.Mrc721BackfillRunTransfers("backfill", histories)

	it, _ := PebbleStore.Database.GetMrc721Item("bi1")
	if it == nil || it.OwnerAddress != mrc721Buyer || it.TransferCount != 2 {
		t.Fatalf("history replay failed: %+v", it)
	}
	historyCount := 0
	err := PebbleStore.Database.IterateMrc721ItemTransfers("bi1", func(tr *pebblestore.Mrc721ItemTransfer) bool {
		historyCount++
		return true
	})
	if err != nil || historyCount != 2 {
		t.Fatalf("history entries wrong: %v %d", err, historyCount)
	}
	buyerCount := 0
	PebbleStore.Database.IterateMrc721OwnerItems(mrc721Buyer, "THAlbum", func(cn, ip string) bool { buyerCount++; return true })
	if buyerCount != 1 {
		t.Fatalf("buyer index wrong: %d", buyerCount)
	}

	// reconcile 与最终归属一致时应为幂等
	if !PebbleStore.Mrc721BackfillReconcileOwner("bi1", mrc721Buyer, "final:0", 546, false) {
		t.Fatal("reconcile should find item")
	}
	it, _ = PebbleStore.Database.GetMrc721Item("bi1")
	if it.OwnerAddress != mrc721Buyer || it.TransferCount != 2 || it.Output != "final:0" {
		t.Fatalf("reconcile not idempotent: %+v", it)
	}
}

func TestMrc721BackfillMeltdownReconcile(t *testing.T) {
	mrc721TestSetup(t)

	coll := mrc721TestPin("bc1", "/nft/mrc721/MBAlbum/collection_desc", "create", []byte(`{"name":"MB"}`), mrc721Creator)
	item := mrc721TestPin("bi1", "/nft/mrc721/MBAlbum/nft-1", "create", []byte(`"nft"`), mrc721Creator)
	PebbleStore.Mrc721BackfillRunPins("backfill", []*pin.PinInscription{coll, item})

	// 快照记录 Status=PinStatusMeltdown → burned
	if !PebbleStore.Mrc721BackfillReconcileOwner("bi1", mrc721Creator, "", 0, true) {
		t.Fatal("reconcile should find item")
	}
	it, _ := PebbleStore.Database.GetMrc721Item("bi1")
	if it == nil || !it.Burned {
		t.Fatalf("meltdown should mark burned: %+v", it)
	}
	ownerCount := 0
	PebbleStore.Database.IterateMrc721OwnerItems(mrc721Creator, "MBAlbum", func(cn, ip string) bool { ownerCount++; return true })
	if ownerCount != 0 {
		t.Fatalf("burned item should leave owner view: %d", ownerCount)
	}

	// 未收录条目的 reconcile 应安全返回 false
	if PebbleStore.Mrc721BackfillReconcileOwner("unknown", mrc721Creator, "", 0, false) {
		t.Fatal("reconcile of unknown item should return false")
	}
}
