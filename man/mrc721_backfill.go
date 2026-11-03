package man

import (
	"log"
	"strings"

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
// 值语义:Output/OutputValue 转移后新输出;TransferDb 旧记录无值(OutputValue=0),
// 由 reconcile/repair 按 pin 主记录校正,断言拒绝值级仍为 0 的未熔化条目。
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
				OutputValue: h.OutputValue,
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
// outpoint 为空或值级为 0」的条目补写 Output/OutputValue(-replay-transfers 模式的
// 历史回填数据缺「当前可花 UTXO 指针+值」,不补会导致二级转移断链:下游转移工具门
// 为 outputValue>0)。pin 主记录缺失、无 outpoint 或值同样为 0 的条目留给
// Mrc721BackfillAssertOutputs 上报。返回修补的条目数。
func (pd *PebbleData) Mrc721BackfillRepairOutputs() int {
	repaired := 0
	_ = pd.Database.IterateMrc721Items(func(item *pebblestore.Mrc721Item) bool {
		if item.TransferCount <= 0 || item.Burned || (item.Output != "" && item.OutputValue > 0) {
			return true
		}
		node, err := pd.Database.GetPinInscriptionByKey(item.ItemPinId)
		if err != nil || node.Id == "" || node.Output == "" || node.OutputValue <= 0 {
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
// outpoint 且值级非 0(下游转移工具门为 outputValue>0,「指针绿、值假绿」同样断链),
// 违例条目列表非空时回填任务应报错退出。熔化条目无可花 UTXO,不在断言范围。
func (pd *PebbleData) Mrc721BackfillAssertOutputs() []string {
	var missing []string
	_ = pd.Database.IterateMrc721Items(func(item *pebblestore.Mrc721Item) bool {
		if item.TransferCount > 0 && !item.Burned && (item.Output == "" || item.OutputValue <= 0) {
			missing = append(missing, item.ItemPinId)
		}
		return true
	})
	return missing
}

// Mrc721BackfillRelocateOutpoint 把条目的当前 outpoint 整体迁移到新 txid 口径
// (MVC 实时路径曾按 sha256d 写转移指针,链上可花费引用用账本 txid,须改写):
// 条目主记录、转移历史键(保证 -replay-transfers 按 (pinId,height,账本txid) 的幂等
// 键与回填重放对齐)、pin 主记录 Output/Location、新 outpoint 别名一并迁移。
// 输出值不变(由调用方保证正确);条目已指向 newOutpoint 时幂等返回 true。
// 返回是否存在对应条目且已处理。
func (pd *PebbleData) Mrc721BackfillRelocateOutpoint(itemPinId, newOutpoint string) bool {
	item, err := pd.Database.GetMrc721Item(itemPinId)
	if err != nil || item == nil {
		return false
	}
	if item.Output == newOutpoint {
		return true
	}
	oldTxid, _, _ := strings.Cut(item.Output, ":")
	newTxid, _, _ := strings.Cut(newOutpoint, ":")

	// 1. 转移历史换键:TxId 为旧口径的记录删除重写(其余记录不动)
	var migrated []*pebblestore.Mrc721ItemTransfer
	_ = pd.Database.IterateMrc721ItemTransfers(itemPinId, func(tr *pebblestore.Mrc721ItemTransfer) bool {
		if tr.TxId == oldTxid {
			cp := *tr
			migrated = append(migrated, &cp)
		}
		return true
	})
	for _, tr := range migrated {
		if err := pd.Database.DeleteMrc721ItemTransfer(itemPinId, tr.Height, tr.TxId); err != nil {
			log.Printf("[MRC721] relocate %s: delete transfer %d/%s: %v", itemPinId, tr.Height, tr.TxId, err)
			return false
		}
		tr.TxId = newTxid
		if err := pd.Database.SaveMrc721ItemTransfer(tr); err != nil {
			log.Printf("[MRC721] relocate %s: save transfer %d/%s: %v", itemPinId, tr.Height, tr.TxId, err)
			return false
		}
	}

	// 2. 条目主记录
	item.Output = newOutpoint
	if err := pd.Database.SaveMrc721Item(item); err != nil {
		log.Printf("[MRC721] relocate %s: save item: %v", itemPinId, err)
		return false
	}

	// 3. pin 主记录 Output/Location + 新 outpoint 别名
	node, err := pd.Database.GetPinInscriptionByKey(itemPinId)
	if err == nil && node.Id != "" {
		node.Output = newOutpoint
		if node.Location == oldTxid || strings.HasPrefix(node.Location, oldTxid+":") {
			node.Location = newTxid + strings.TrimPrefix(node.Location, oldTxid)
		}
		if err := pd.Database.BatchInsertPins([]pin.PinInscription{node}); err != nil {
			log.Printf("[MRC721] relocate %s: update pin record: %v", itemPinId, err)
			return false
		}
		aliasKey := strings.Replace(newOutpoint, ":", "i", -1)
		if err := pd.Database.BatchSetPinAliases(map[string]string{aliasKey: node.Id}); err != nil {
			log.Printf("[MRC721] relocate %s: set alias: %v", itemPinId, err)
			return false
		}
	}
	return true
}
