package man

import (
	"log"
	"strings"
	"time"

	"man-p2p/adapter"
	"man-p2p/pebblestore"
)

// MRC-721 未确认转移可见性(IDBots 缺陷:mempool 内已花 NFT outpoint 读侧不可见,
// 无法区分「发了还没确认」和「根本没发」)。数据源是周期性全量 mempool 同步
// (syncExistingMempool,RPC 路径,CGO_ENABLED=0 可用;MVC ZMQ 通道只投递含
// pin 的交易,plain UTXO spend 只能靠这里)。
//
// pending 记录是 advisory 通道:确认后 mrc721TransferHook 幂等改写归属并落转移
// 历史,本记录随即清算;交易被丢弃/RBF 替换时由对账步骤删除,同 item 永远只有
// 一条 latest-wins 记录。熔化判定不在 mempool 阶段做(确认后钩子权威判定),
// pending 一律按普通花费展示。

// mrc721MempoolSpendScan 扫描一轮 mempool 交易,对已跟踪 MRC-721 outpoint 的
// 花费落/刷新 pending 记录,并对账清除已消失交易残留的 pending。
// txIns 为本轮全部交易的花费 outpoint 并集(CatchMempoolPins 已按账本口径收集),
// txList 为原始交易列表(MempoolSpends 需要按链类型解析)。
func (pd *PebbleData) mrc721MempoolSpendScan(chainName string, indexer adapter.Indexer, txList []interface{}, txIns []string) {
	if !Mrc721RuntimeEnabled() || indexer == nil {
		return
	}
	seen := make(map[string]string) // itemPinId → 本轮观测到的花费 txid
	if len(txIns) > 0 {
		// 哪些被花费 outpoint 是已跟踪条目的当前 outpoint(块路径 handleTransfer 同构:
		// outpoint→pin 记录,再过滤出 MRC-721 条目)
		pins, err := pd.Database.GetPinListByIdList(txIns, 1000, true)
		if err != nil {
			log.Printf("[MRC721] mempool spend scan get pins: %v", err)
		}
		pinIdByOutput := make(map[string]string)
		idMap := make(map[string]string)
		for _, p := range pins {
			item, err := pd.Database.GetMrc721Item(p.Id)
			if err != nil || item == nil || item.Burned || item.Output == "" || item.Output != p.Output {
				continue
			}
			pinIdByOutput[p.Output] = p.Id
			idMap[p.Output] = item.OwnerAddress
		}
		if len(idMap) > 0 {
			transferMap := indexer.MempoolSpends(txList, idMap)
			now := time.Now().Unix()
			for outpoint, info := range transferMap {
				pinId := pinIdByOutput[outpoint]
				if pinId == "" || info == nil {
					continue
				}
				// 花费 txid 取账本口径首段(与确认后转移历史的 txId 同源)
				txId := info.Location
				if idx := strings.Index(txId, ":"); idx >= 0 {
					txId = txId[:idx]
				}
				if txId == "" {
					continue
				}
				pt := &pebblestore.Mrc721PendingTransfer{
					ItemPinId:     pinId,
					ChainName:     chainName,
					SpentOutpoint: outpoint,
					FromAddress:   idMap[outpoint],
					ToAddress:     info.Address,
					TxId:          txId,
					FirstSeenAt:   now,
				}
				// 同一笔交易的重扫(RBF 替换为新交易时走覆盖,首见时间重置)
				if existing, _ := pd.Database.GetMrc721PendingTransfer(pinId); existing != nil && existing.TxId == txId {
					pt.FirstSeenAt = existing.FirstSeenAt
				}
				if err := pd.Database.SaveMrc721PendingTransfer(pt); err != nil {
					log.Printf("[MRC721] save pending transfer %s: %v", pinId, err)
					continue
				}
				seen[pinId] = txId
			}
		}
	}
	pd.mrc721MempoolSpendReconcile(chainName, seen)
}

// mrc721MempoolSpendReconcile 清除本轮 mempool 扫描未再观测到的 pending 记录:
// 归因要么是交易已确认(转移钩子会落历史并清算,这里兜底收敛),要么是交易已
// 从 mempool 消失(丢弃/RBF 替换)。仅处理本链记录;整轮 mempool 拉取失败时不
// 会进入本函数,不会误删。
func (pd *PebbleData) mrc721MempoolSpendReconcile(chainName string, seen map[string]string) {
	var stale []*pebblestore.Mrc721PendingTransfer
	err := pd.Database.IterateMrc721PendingTransfers(func(pt *pebblestore.Mrc721PendingTransfer) bool {
		if pt.ChainName == chainName && seen[pt.ItemPinId] != pt.TxId {
			stale = append(stale, pt)
		}
		return true
	})
	if err != nil {
		log.Printf("[MRC721] pending transfer reconcile iterate: %v", err)
		return
	}
	for _, pt := range stale {
		if err := pd.Database.DeleteMrc721PendingTransfer(pt.ItemPinId); err != nil {
			log.Printf("[MRC721] pending transfer reconcile delete %s: %v", pt.ItemPinId, err)
		}
	}
}
