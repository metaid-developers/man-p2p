package man

import (
	"testing"

	"man-p2p/pin"
)

// MRC721 回填 txOutput 修补/断言单测:复现 2026-11-02 回填数据缺「当前可花 UTXO 指针」
// 缺陷(item/info 的 txOutput 为空 → 二级转移断链),验证 RepairOutputs 补写与
// AssertOutputs 防回归断言。流程对齐 tools/mrc721_backfill replayTransfersFile。

// seedBackfillPin 写入创世 pin 主记录(生产形态:Id=txidi0,Output=txid:idx)。
func seedBackfillPin(t *testing.T, pinId, genesisOutpoint, owner string) {
	t.Helper()
	if err := PebbleStore.Database.BatchInsertPins([]pin.PinInscription{{
		Id:          pinId,
		Path:        "/nft/mrc721/TXAlbum/nft-" + pinId[:6],
		Operation:   "create",
		Address:     owner,
		ChainName:   "mvc",
		Output:      genesisOutpoint,
		OutputValue: 1,
	}}); err != nil {
		t.Fatalf("seed pin %s: %v", pinId, err)
	}
}

// runBackfillTransferStep 按 replayTransfersFile 的顺序执行第 1/2 步:
// UpdateTransferPin 修 pin 主记录 → Mrc721BackfillRunTransfers 重放聚合。
func runBackfillTransferStep(t *testing.T, pinId, genesisOutpoint, transferTx, from, to string) {
	t.Helper()
	err := PebbleStore.Database.UpdateTransferPin(map[string]*pin.PinTransferInfo{
		genesisOutpoint: {
			Address:     to,
			FromAddress: from,
			Location:    transferTx,
			Output:      transferTx + ":0",
			OutputValue: 1,
		},
	})
	if err != nil {
		t.Fatalf("UpdateTransferPin: %v", err)
	}
	PebbleStore.Mrc721BackfillRunTransfers("backfill", []*pin.PinTransferHistory{{
		PinId:          pinId,
		TransferHeight: 200,
		TransferTime:   100,
		TransferTx:     transferTx,
		ChainName:      "mvc",
		FromAddress:    from,
		ToAddress:      to,
	}})
}

func TestMrc721BackfillRepairOutputsFromPinRecord(t *testing.T) {
	mrc721TestSetup(t)

	PebbleStore.Mrc721BackfillRunPins("backfill", []*pin.PinInscription{
		mrc721TestPin("txc1", "/nft/mrc721/TXAlbum/collection_desc", "create", []byte(`{"name":"TX"}`), mrc721Creator),
		mrc721TestPin("backfill01i0", "/nft/mrc721/TXAlbum/nft-1", "create", []byte(`"nft"`), mrc721Creator),
	})
	seedBackfillPin(t, "backfill01i0", "backfill01:0", mrc721Creator)

	// 回填路径缺陷复现:转移重放把 item.Output 覆盖为空(实时路径写 info.Output,回填缺省)
	runBackfillTransferStep(t, "backfill01i0", "backfill01:0", "tx-backfill01", mrc721Creator, mrc721Buyer)
	broken, _ := PebbleStore.Database.GetMrc721Item("backfill01i0")
	if broken == nil || broken.TransferCount != 1 || broken.Output != "" {
		t.Fatalf("expected the txOutput-empty defect to reproduce: %+v", broken)
	}

	// 修补:以 pin 主记录(已含转移后 outpoint)为准补写
	if n := PebbleStore.Mrc721BackfillRepairOutputs(); n != 1 {
		t.Fatalf("repair should fix 1 item, got %d", n)
	}
	it, _ := PebbleStore.Database.GetMrc721Item("backfill01i0")
	if it.OwnerAddress != mrc721Buyer || it.TransferCount != 1 {
		t.Fatalf("owner/count must be untouched by repair: %+v", it)
	}
	if it.Output != "tx-backfill01:0" || it.OutputValue != 1 {
		t.Fatalf("txOutput not repaired: %s %d", it.Output, it.OutputValue)
	}

	// 断言通过,且修补幂等
	if missing := PebbleStore.Mrc721BackfillAssertOutputs(); len(missing) != 0 {
		t.Fatalf("assert should pass after repair: %v", missing)
	}
	if n := PebbleStore.Mrc721BackfillRepairOutputs(); n != 0 {
		t.Fatalf("repair must be idempotent, got %d", n)
	}
}

func TestMrc721BackfillAssertOutputsReportsMissing(t *testing.T) {
	mrc721TestSetup(t)

	// 不写 pin 主记录(对应线上 pin 记录缺失/无 outpoint 的条目):修补无从下手
	PebbleStore.Mrc721BackfillRunPins("backfill", []*pin.PinInscription{
		mrc721TestPin("txc2", "/nft/mrc721/TXAlbum/collection_desc", "create", []byte(`{"name":"TX"}`), mrc721Creator),
		mrc721TestPin("backfill02i0", "/nft/mrc721/TXAlbum/nft-2", "create", []byte(`"nft"`), mrc721Creator),
	})
	PebbleStore.Mrc721BackfillRunTransfers("backfill", []*pin.PinTransferHistory{{
		PinId:          "backfill02i0",
		TransferHeight: 210,
		TransferTx:     "tx-backfill02",
		ChainName:      "mvc",
		FromAddress:    mrc721Creator,
		ToAddress:      mrc721Holder,
	}})

	if n := PebbleStore.Mrc721BackfillRepairOutputs(); n != 0 {
		t.Fatalf("repair without pin record must fix nothing, got %d", n)
	}
	missing := PebbleStore.Mrc721BackfillAssertOutputs()
	if len(missing) != 1 || missing[0] != "backfill02i0" {
		t.Fatalf("assert must report the offender: %v", missing)
	}
}

func TestMrc721BackfillAssertOutputsExemptsBurned(t *testing.T) {
	mrc721TestSetup(t)

	PebbleStore.Mrc721BackfillRunPins("backfill", []*pin.PinInscription{
		mrc721TestPin("txc3", "/nft/mrc721/TXAlbum/collection_desc", "create", []byte(`{"name":"TX"}`), mrc721Creator),
		mrc721TestPin("backfill03i0", "/nft/mrc721/TXAlbum/nft-3", "create", []byte(`"nft"`), mrc721Creator),
	})
	PebbleStore.Mrc721BackfillRunTransfers("backfill", []*pin.PinTransferHistory{{
		PinId:          "backfill03i0",
		TransferHeight: 220,
		TransferTx:     "tx-backfill03",
		ChainName:      "mvc",
		FromAddress:    mrc721Creator,
		ToAddress:      mrc721Holder,
	}})

	// 熔化条目无可花 UTXO:不计入断言,修补也跳过
	if !PebbleStore.Mrc721BackfillReconcileOwner("backfill03i0", mrc721Creator, "", 0, true) {
		t.Fatal("reconcile should find the item")
	}
	if n := PebbleStore.Mrc721BackfillRepairOutputs(); n != 0 {
		t.Fatalf("repair must skip burned items, got %d", n)
	}
	if missing := PebbleStore.Mrc721BackfillAssertOutputs(); len(missing) != 0 {
		t.Fatalf("burned item must be exempt from assertion: %v", missing)
	}
}
