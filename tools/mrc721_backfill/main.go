// mrc721_backfill 离线重放 Pebble 快照中的历史 /nft/mrc721 pin(设计文档 §6 方案 A)。
//
// 流程(零线上压力):
//  1. 按新节点 SOP 的种子复制方式从生产节点取得数据目录快照(在线预复制 + 短停机 final delta)
//  2. 对快照执行本工具:扫描全部 pins 分片 → 过滤 /nft/mrc721 create pin
//     → 铸造重放(创世持有人语义)→ TransferDb 转移时间线重放 → pin 主记录归属/outpoint 校正
//  3. 产物为快照内的 mrc721/db 聚合库,可整体拷回线上节点,或由该离线节点直接作为 p2p follower 服务
//
// 用法:
//
//	mrc721_backfill -data-dir /path/to/man_base_data_pebble [-chain btc] [-dry-run]
package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/bytedance/sonic"
	"github.com/cockroachdb/pebble"

	"man-p2p/common"
	"man-p2p/man"
	"man-p2p/pebblestore"
	"man-p2p/pin"
)

const mrc721PathPrefix = "/nft/mrc721"

func main() {
	dataDir := flag.String("data-dir", "", "Pebble 数据目录(快照)")
	chain := flag.String("chain", "", "只回填指定链(默认全部)")
	dryRun := flag.Bool("dry-run", false, "只扫描统计,不写入")
	replayTransfers := flag.String("replay-transfers", "", "定向重放转移记录 JSON 文件(数组:pinId/transferHeight/transferTime/transferTx/fromAddress/toAddress),修复后回填历史转移用")
	flag.Parse()
	if *dataDir == "" {
		flag.Usage()
		log.Fatal("-data-dir required")
	}

	shardNum, err := detectShardNum(*dataDir)
	if err != nil {
		log.Fatalf("detect shards: %v", err)
	}
	log.Printf("data-dir=%s shards=%d dry-run=%v chain=%q replay-transfers=%q", *dataDir, shardNum, *dryRun, *chain, *replayTransfers)

	db, err := pebblestore.NewDataBase(*dataDir, shardNum)
	if err != nil {
		log.Fatalf("open pebble: %v", err)
	}
	defer db.Close()

	// 定向转移重放模式:不扫描,只按给定记录回填(服务须已停止,pebble 单写者)
	if *replayTransfers != "" {
		replayTransfersFile(db, *replayTransfers)
		return
	}

	// 1. 扫描 pins 分片,收集 /nft/mrc721 create pin(主表每 pinId 单条,即最新记录)
	matched := scanPins(db, shardNum, *chain)
	log.Printf("matched create pins under %s: %d", mrc721PathPrefix, len(matched))
	if len(matched) == 0 {
		fmt.Println("nothing to backfill")
		return
	}

	// 概览:按链统计
	byChain := map[string]int{}
	for _, p := range matched {
		byChain[p.ChainName]++
	}
	for c, n := range byChain {
		fmt.Printf("  chain=%s pins=%d\n", c, n)
	}
	if *dryRun {
		for id, p := range matched {
			fmt.Printf("  pin=%s path=%s height=%d owner(current)=%s initial=%s\n", id, p.Path, p.GenesisHeight, p.Address, p.InitialOwner)
		}
		return
	}

	pd := &man.PebbleData{Database: db}

	// 2. 铸造重放:按区块序,创世持有人语义(Address=InitialOwner,MetaId 同步重算)
	pins := make([]*pin.PinInscription, 0, len(matched))
	for _, p := range matched {
		clone := *p
		if clone.InitialOwner != "" {
			clone.Address = clone.InitialOwner
		}
		clone.MetaId = genesisMetaId(clone.Address, clone.MetaId)
		pins = append(pins, &clone)
	}
	sort.Slice(pins, func(i, j int) bool {
		a, b := pins[i], pins[j]
		if a.GenesisHeight != b.GenesisHeight {
			return a.GenesisHeight < b.GenesisHeight
		}
		if a.TxIndex != b.TxIndex {
			return a.TxIndex < b.TxIndex
		}
		return a.Id < b.Id
	})
	const batch = 500
	for start := 0; start < len(pins); start += batch {
		end := start + batch
		if end > len(pins) {
			end = len(pins)
		}
		pd.Mrc721BackfillRunPins("backfill", pins[start:end])
	}
	log.Printf("mint replay done: %d pins", len(pins))

	// 3. 转移历史重放(TransferDb 时间线,仅限条目 pin)
	itemSet := make(map[string]struct{}, len(matched))
	for id := range matched {
		itemSet[id] = struct{}{}
	}
	histories := scanTransfers(db, itemSet)
	log.Printf("transfer history replay: %d entries", len(histories))
	pd.Mrc721BackfillRunTransfers("backfill", histories)

	// 4. 归属/outpoint 校正(以 pin 主记录为准,兜底历史转移缺口)
	reconciled := 0
	for _, p := range matched {
		burned := p.Status == pin.PinStatusMeltdown
		if pd.Mrc721BackfillReconcileOwner(p.Id, p.Address, p.Output, p.OutputValue, burned) {
			reconciled++
		}
	}
	log.Printf("ownership reconcile done: %d items", reconciled)

	// 5. 防回归断言:transferCount>0 的条目必须有当前 outpoint(txOutput),
	//    否则二级转移断链,回填任务报错退出
	if missing := pd.Mrc721BackfillAssertOutputs(); len(missing) > 0 {
		for _, id := range missing {
			log.Printf("assert fail: item %s transferCount>0 but txOutput empty", id)
		}
		log.Fatalf("txOutput assertion failed: %d items", len(missing))
	}

	cols, total, err := db.ListMrc721Collections(0, 1<<30)
	if err != nil {
		log.Printf("count collections: %v", err)
	} else {
		fmt.Printf("collections=%d\n", total)
		for _, c := range cols {
			fmt.Printf("  %s totalnum=%d supply=%d royalty=%d chain=%s pinid=%s\n",
				c.CollectionName, c.TotalNum, c.TotalSupply, c.RoyaltyRate, c.GenesisChainName, c.PinId)
		}
	}
	fmt.Printf("items=%d\n", countPrefix(db.Mrc721Db, "i:"))
	fmt.Printf("transfers=%d\n", countPrefix(db.Mrc721Db, "th:"))
	fmt.Println("backfill complete")
}

// detectShardNum 数 pins_N 目录得分片数。
func detectShardNum(dataDir string) (int, error) {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return 0, err
	}
	max := -1
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(e.Name(), "pins_%d", &n); err == nil && n > max {
			max = n
		}
	}
	if max < 0 {
		return 0, fmt.Errorf("no pins_N dirs under %s", dataDir)
	}
	return max + 1, nil
}

// maybeMrc721 粗筛:JSON 里含 mrc721 的任意大小写变体(rc721 已覆盖全部 4 种变体)。
func maybeMrc721(v []byte) bool {
	for _, s := range []string{"rc721", "RC721", "Rc721", "rC721"} {
		if bytes.Contains(v, []byte(s)) {
			return true
		}
	}
	return false
}

func scanPins(db *pebblestore.Database, shardNum int, chainFilter string) map[string]*pin.PinInscription {
	matched := map[string]*pin.PinInscription{}
	scanned := 0
	for i := 0; i < shardNum; i++ {
		shard := db.PinsDBs[i]
		it, err := shard.NewIter(nil)
		if err != nil {
			log.Fatalf("iter shard %d: %v", i, err)
		}
		for it.First(); it.Valid(); it.Next() {
			scanned++
			v := it.Value()
			if !maybeMrc721(v) {
				continue
			}
			var p pin.PinInscription
			if err := sonic.Unmarshal(v, &p); err != nil {
				continue
			}
			if p.Operation != "create" || !strings.HasPrefix(strings.ToLower(p.Path), mrc721PathPrefix) {
				continue
			}
			if chainFilter != "" && p.ChainName != chainFilter {
				continue
			}
			if old, ok := matched[p.Id]; !ok || p.GenesisHeight > old.GenesisHeight {
				matched[p.Id] = &p
			}
		}
		it.Close()
		log.Printf("shard %d scanned, cumulative matched=%d", i, len(matched))
	}
	log.Printf("total pins scanned: %d", scanned)
	return matched
}

func scanTransfers(db *pebblestore.Database, itemSet map[string]struct{}) []*pin.PinTransferHistory {
	var histories []*pin.PinTransferHistory
	it, err := db.TransferDb.NewIter(nil)
	if err != nil {
		log.Fatalf("iter transfer db: %v", err)
	}
	defer it.Close()
	for it.First(); it.Valid(); it.Next() {
		var h pin.PinTransferHistory
		if err := sonic.Unmarshal(it.Value(), &h); err != nil {
			continue
		}
		if _, ok := itemSet[h.PinId]; ok {
			cp := h
			histories = append(histories, &cp)
		}
	}
	sort.Slice(histories, func(i, j int) bool {
		a, b := histories[i], histories[j]
		if a.TransferHeight != b.TransferHeight {
			return a.TransferHeight < b.TransferHeight
		}
		if a.TransferTime != b.TransferTime {
			return a.TransferTime < b.TransferTime
		}
		return a.TransferTx < b.TransferTx
	})
	return histories
}

func genesisMetaId(address, fallback string) string {
	if address == "" {
		return fallback
	}
	return common.GetMetaIdByAddress(address)
}

// replayTransfersFile 定向重放历史转移(JSON 数组),修复 2026-11-02 三笔 MRC721
// 转移未被识别一类的历史数据。流程:
//  1. UpdateTransferPin 修复 pin 主记录(owner/output/isTransfered + 新 outpoint 别名)
//  2. Mrc721BackfillRunTransfers 更新 mrc721 聚合(owner/transferCount/转移历史)
//  3. Mrc721BackfillRepairOutputs 以 pin 主记录补写聚合缺失的当前 outpoint(txOutput),
//     并断言 transferCount>0 的条目 txOutput 非空,否则报错退出
//     幂等:重复执行不产生重复读数(mrc721 转移历史按 pinId+height+txid 去重)。
func replayTransfersFile(db *pebblestore.Database, path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("read %s: %v", path, err)
	}
	var records []pin.PinTransferHistory
	if err := sonic.Unmarshal(raw, &records); err != nil {
		log.Fatalf("parse %s: %v", path, err)
	}
	if len(records) == 0 {
		fmt.Println("no transfer records")
		return
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].TransferHeight != records[j].TransferHeight {
			return records[i].TransferHeight < records[j].TransferHeight
		}
		return records[i].TransferTx < records[j].TransferTx
	})

	pd := &man.PebbleData{Database: db}

	// 1. pin 主记录修复:transferMap 以 pin 当前 outpoint 为键
	//    (创世 pin 从 pinId 尾部 "i<idx>" 还原;转移过的 pin 用其记录的 Output)
	transferMap := make(map[string]*pin.PinTransferInfo, len(records))
	pinIdByOutput := make(map[string]string, len(records))
	for _, r := range records {
		outpoint := currentOutpoint(db, r.PinId)
		if outpoint == "" {
			log.Printf("skip: pin %s not found", r.PinId)
			continue
		}
		transferMap[outpoint] = &pin.PinTransferInfo{
			Address:     r.ToAddress,
			FromAddress: r.FromAddress,
			Location:    r.TransferTx,
			Output:      outpointToTxOutput(r.TransferTx),
		}
		pinIdByOutput[outpoint] = r.PinId
	}
	if err := db.UpdateTransferPin(transferMap); err != nil {
		log.Printf("UpdateTransferPin: %v", err)
	}

	// 2. mrc721 聚合回填(内部按 (pinId,height,txid) 幂等)
	historyPtrs := make([]*pin.PinTransferHistory, len(records))
	for i := range records {
		historyPtrs[i] = &records[i]
	}
	pd.Mrc721BackfillRunTransfers("backfill", historyPtrs)

	// 3. txOutput 补写 + 防回归断言:历史回填数据缺「当前可花 UTXO 指针」,
	//    以 pin 主记录(第 1 步已修)为准补正;仍缺失则报错退出
	if repaired := pd.Mrc721BackfillRepairOutputs(); repaired > 0 {
		log.Printf("txOutput repair: %d items", repaired)
	}
	if missing := pd.Mrc721BackfillAssertOutputs(); len(missing) > 0 {
		for _, id := range missing {
			log.Printf("assert fail: item %s transferCount>0 but txOutput empty", id)
		}
		log.Fatalf("txOutput assertion failed: %d items", len(missing))
	}

	// 4. 验证读数
	for _, r := range records {
		item, err := db.GetMrc721Item(r.PinId)
		if err != nil || item == nil {
			fmt.Printf("  pin=%s: no mrc721 item\n", r.PinId)
			continue
		}
		historyCount := 0
		db.IterateMrc721ItemTransfers(r.PinId, func(tr *pebblestore.Mrc721ItemTransfer) bool {
			historyCount++
			return true
		})
		pinNode, _ := db.GetPinInscriptionByKey(db.ResolvePinLookupKey(outpointToTxOutput(r.TransferTx)))
		fmt.Printf("  pin=%s owner=%s transferCount=%d history=%d txOutput=%s pinRecord(addr=%s isTransfered=%v)\n",
			r.PinId, item.OwnerAddress, item.TransferCount, historyCount, item.Output,
			shortAddr(pinOwner(&pinNode)), pinTransferred(&pinNode))
	}
	fmt.Println("replay complete")
}

// currentOutpoint 返回 pin 当前记录的 outpoint;查不到返回空。
func currentOutpoint(db *pebblestore.Database, pinId string) string {
	node, err := db.GetPinInscriptionByKey(pinId)
	if err != nil || node.Id == "" {
		// 主键直查失败再试 outpoint 形态(创世 outpoint = pinId 的 "i<idx>" 换回 ":")
		node, err = db.GetPinInscriptionByKey(db.ResolvePinLookupKey(OutpointFromPinId(pinId)))
		if err != nil || node.Id == "" {
			return ""
		}
	}
	if node.Output != "" {
		return node.Output
	}
	return OutpointFromPinId(pinId)
}

// OutpointFromPinId 把 "txidi<idx>" 形态的 pinId 还原为 "txid:<idx>"(txid 为 hex,不含 'i')。
func OutpointFromPinId(pinId string) string {
	idx := strings.LastIndex(pinId, "i")
	if idx < 0 {
		return pinId
	}
	return pinId[:idx] + ":" + pinId[idx+1:]
}

// outpointToTxOutput 从转移 tx 构造新 outpoint(实盘转移把 pin 放在 vout[0])。
func outpointToTxOutput(transferTx string) string {
	txPart := transferTx
	if i := strings.Index(transferTx, ":"); i > 0 {
		txPart = transferTx[:i]
	}
	return txPart + ":0"
}

func pinOwner(n *pin.PinInscription) string {
	if n == nil {
		return ""
	}
	return n.Address
}

func pinTransferred(n *pin.PinInscription) bool {
	return n != nil && n.IsTransfered
}

func shortAddr(a string) string {
	if len(a) > 14 {
		return a[:14] + "..."
	}
	return a
}

func countPrefix(db *pebble.DB, prefix string) int {
	count := 0
	it, err := db.NewIter(nil)
	if err != nil {
		return 0
	}
	defer it.Close()
	for it.First(); it.Valid(); it.Next() {
		if strings.HasPrefix(string(it.Key()), prefix) {
			count++
		}
	}
	return count
}
