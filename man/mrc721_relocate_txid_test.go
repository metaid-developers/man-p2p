package man

import (
	"testing"

	"man-p2p/pebblestore"
	"man-p2p/pin"
)

// Mrc721BackfillRelocateOutpoint 单测(2026-11-03 第三份回折):实时路径曾按 sha256d
// 口径写转移指针,链上可花费引用用账本 txid —— 改写须覆盖条目主记录、转移历史键
// (-replay-transfers 幂等键对齐)、pin 主记录 Output/Location、新 outpoint 别名,
// 且保持 TransferCount 与历史条数不变。

const (
	shaTxid   = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	ledgerTx2 = "e4d452d5a4b41d638d7c57dfbaf43dfdecb65bba06a6f6d825202fdd17148cb8"
)

func TestMrc721BackfillRelocateOutpoint(t *testing.T) {
	mrc721TestSetup(t)

	// 铸造 + 一笔转移(账本口径 th 记录,值 1)
	PebbleStore.Mrc721BackfillRunPins("backfill", []*pin.PinInscription{
		mrc721TestPin("rlc1", "/nft/mrc721/TXAlbum/collection_desc", "create", []byte(`{"name":"TX"}`), mrc721Creator),
		mrc721TestPin("reloc01i0", "/nft/mrc721/TXAlbum/nft-1", "create", []byte(`"nft"`), mrc721Creator),
	})
	seedBackfillPin(t, "reloc01i0", "reloc01:0", mrc721Creator)
	runBackfillTransferStep(t, "reloc01i0", "reloc01:0", "tx-first", mrc721Creator, mrc721Buyer, 1)

	// 模拟实时路径写坏(2026-11-02 生产现状形态):二代转移由实时路径处理,
	// item/pin 主记录/th 全部指向 sha256d 口径(值 1 正确)
	if err := PebbleStore.Database.UpdateTransferPin(map[string]*pin.PinTransferInfo{
		"tx-first:0": {
			Address:     mrc721Buyer,
			FromAddress: mrc721Creator,
			Location:    shaTxid + ":0:0",
			Output:      shaTxid + ":0",
			OutputValue: 1,
		},
	}); err != nil {
		t.Fatalf("simulate realtime pin write: %v", err)
	}
	it, _ := PebbleStore.Database.GetMrc721Item("reloc01i0")
	it.Output = shaTxid + ":0"
	it.OutputValue = 1
	it.TransferCount = 2
	if err := PebbleStore.Database.SaveMrc721Item(it); err != nil {
		t.Fatalf("simulate realtime item write: %v", err)
	}
	if err := PebbleStore.Database.SaveMrc721ItemTransfer(&pebblestore.Mrc721ItemTransfer{
		ItemPinId:   "reloc01i0",
		FromAddress: mrc721Creator,
		ToAddress:   mrc721Buyer,
		ChainName:   "mvc",
		Height:      197102,
		TxId:        shaTxid,
		Time:        1793700000,
	}); err != nil {
		t.Fatalf("simulate realtime transfer record: %v", err)
	}
	if it2, _ := PebbleStore.Database.GetMrc721Item("reloc01i0"); it2.Output != shaTxid+":0" {
		t.Fatalf("expected sha256d pointer to reproduce: %+v", it2)
	}

	// 改写:账本口径 outpoint
	if !PebbleStore.Mrc721BackfillRelocateOutpoint("reloc01i0", ledgerTx2+":0") {
		t.Fatal("relocate should find the item")
	}

	// 条目主记录
	it3, _ := PebbleStore.Database.GetMrc721Item("reloc01i0")
	if it3.Output != ledgerTx2+":0" || it3.OutputValue != 1 || it3.TransferCount != 2 {
		t.Fatalf("item not relocated correctly: %+v", it3)
	}
	// 转移历史:2 条,sha256d 那条已换键为账本口径
	history := map[string]pebblestore.Mrc721ItemTransfer{}
	_ = PebbleStore.Database.IterateMrc721ItemTransfers("reloc01i0", func(tr *pebblestore.Mrc721ItemTransfer) bool {
		history[tr.TxId] = *tr
		return true
	})
	if len(history) != 2 {
		t.Fatalf("history must keep 2 entries: %d", len(history))
	}
	if _, ok := history[shaTxid]; ok {
		t.Fatal("sha256d txid entry must be rekeyed")
	}
	if tr, ok := history[ledgerTx2]; !ok || tr.Height != 197102 {
		t.Fatalf("ledger txid entry missing or wrong: %+v", tr)
	}
	// pin 主记录 + 新别名
	node, _ := PebbleStore.Database.GetPinInscriptionByKey("reloc01i0")
	if node.Output != ledgerTx2+":0" || node.Location != ledgerTx2+":0:0" {
		t.Fatalf("pin record not relocated: output=%s location=%s", node.Output, node.Location)
	}
	if alias, err := PebbleStore.Database.GetPinAlias(ledgerTx2 + "i0"); err != nil || alias != "reloc01i0" {
		t.Fatalf("ledger alias missing: %v %q", err, alias)
	}

	// 幂等:重复 relocate 无副作用
	if !PebbleStore.Mrc721BackfillRelocateOutpoint("reloc01i0", ledgerTx2+":0") {
		t.Fatal("idempotent relocate should return true")
	}
	it2, _ := PebbleStore.Database.GetMrc721Item("reloc01i0")
	if it2.TransferCount != 2 || it2.Output != ledgerTx2+":0" {
		t.Fatalf("idempotent relocate mutated state: %+v", it2)
	}

	// 不存在的条目返回 false
	if PebbleStore.Mrc721BackfillRelocateOutpoint("unknown-item", ledgerTx2+":0") {
		t.Fatal("relocate of unknown item should return false")
	}
}
