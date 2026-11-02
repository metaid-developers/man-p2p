package man

import (
	"log"

	"man-p2p/common"
	"man-p2p/pebblestore"
	"man-p2p/pin"
)

// MRC-721 离线回填入口(tools/mrc721_backfill 使用,方案 A:离线快照重放,零线上压力)。
// 与实时 ingest 走同一套聚合/转移逻辑,分三步:
//  1. Mrc721BackfillRunPins     铸造重放(创世持有人语义)
//  2. Mrc721BackfillRunTransfers 转移历史重放(TransferDb 时间线)
//  3. Mrc721BackfillReconcileOwner 按 pin 主记录校正最终归属/outpoint(历史转移缺口兜底)
// 另有 RepairOutputs/AssertOutputs 两个收尾入口:补写回填数据缺失的当前 outpoint 并断言。

// Mrc721BackfillRunPins 重放历史 /nft/mrc721 create pin。
// pins 须已按 (genesisHeight, txIndex, Id) 升序排列;Address 应为创世持有人(InitialOwner)。
func (pd *PebbleData) Mrc721BackfillRunPins(chainName string, pins []*pin.PinInscription) {
	pd.handleMrc721(chainName, 0, pins)
}

// Mrc721BackfillRunTransfers 重放历史转移(须按 (height, time) 升序;PinId 与 itemPinId 同为 txidiN 格式)。
func (pd *PebbleData) Mrc721BackfillRunTransfers(chainName string, histories []*pin.PinTransferHistory) {
	for _, h := range histories {
		if h.PinId == "" {
			continue
		}
		key := h.PinId + ":0"
		transferMap := map[string]*pin.PinTransferInfo{
			key: {
				Address:     h.ToAddress,
				FromAddress: h.FromAddress,
				Location:    h.TransferTx,
			},
		}
		pd.mrc721TransferHook(chainName, h.TransferHeight, transferMap, map[string]string{key: h.PinId}, h.TransferTime)
	}
}

// Mrc721BackfillReconcileOwner 按 pin 主记录校正条目最终归属与当前 outpoint,
// 兜底 TransferDb 缺口的历史转移;burned 由调用方按 pin.Status==PinStatusMeltdown 判定传入。
// 返回是否存在对应条目且已处理。
func (pd *PebbleData) Mrc721BackfillReconcileOwner(itemPinId, currentOwner, output string, outputValue int64, burned bool) bool {
	item, err := pd.Database.GetMrc721Item(itemPinId)
	if err != nil || item == nil {
		return false
	}
	switch {
	case burned && !item.Burned:
		item.Burned = true
		if item.OwnerAddress != "" {
			pd.Database.DeleteMrc721OwnerItem(item.OwnerAddress, item.CollectionName, item.ItemPinId)
		}
	case !burned && currentOwner != "" && item.OwnerAddress != currentOwner:
		if item.OwnerAddress != "" {
			pd.Database.DeleteMrc721OwnerItem(item.OwnerAddress, item.CollectionName, item.ItemPinId)
		}
		pd.Database.PutMrc721OwnerItem(currentOwner, item.CollectionName, item.ItemPinId)
		item.OwnerAddress = currentOwner
		item.OwnerMetaId = common.GetMetaIdByAddress(currentOwner)
	}
	if output != "" {
		item.Output = output
		item.OutputValue = outputValue
	}
	if err := pd.Database.SaveMrc721Item(item); err != nil {
		log.Printf("[MRC721] backfill reconcile %s: %v", itemPinId, err)
		return false
	}
	return true
}

// Mrc721BackfillRepairOutputs 以 pin 主记录为准,为「转移过(transferCount>0)但当前
// outpoint 为空」的条目补写 Output(-replay-transfers 模式的历史回填数据缺
// 「当前可花 UTXO 指针」,不补会导致二级转移断链)。pin 主记录缺失或同样无 outpoint
// 的条目留给 Mrc721BackfillAssertOutputs 上报。返回修补的条目数。
func (pd *PebbleData) Mrc721BackfillRepairOutputs() int {
	repaired := 0
	_ = pd.Database.IterateMrc721Items(func(item *pebblestore.Mrc721Item) bool {
		if item.TransferCount <= 0 || item.Burned || item.Output != "" {
			return true
		}
		node, err := pd.Database.GetPinInscriptionByKey(item.ItemPinId)
		if err != nil || node.Id == "" || node.Output == "" {
			return true
		}
		if pd.Mrc721BackfillReconcileOwner(item.ItemPinId, node.Address, node.Output, node.OutputValue, node.Status == pin.PinStatusMeltdown) {
			repaired++
		}
		return true
	})
	return repaired
}

// Mrc721BackfillAssertOutputs 防回归断言:transferCount>0 且未熔化的条目必须有当前
// outpoint,违例条目列表非空时回填任务应报错退出。熔化条目无可花 UTXO,不在断言范围。
func (pd *PebbleData) Mrc721BackfillAssertOutputs() []string {
	var missing []string
	_ = pd.Database.IterateMrc721Items(func(item *pebblestore.Mrc721Item) bool {
		if item.TransferCount > 0 && !item.Burned && item.Output == "" {
			missing = append(missing, item.ItemPinId)
		}
		return true
	})
	return missing
}
