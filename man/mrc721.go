package man

import (
	"encoding/json"
	"log"
	"sort"
	"strconv"
	"strings"

	"github.com/bytedance/sonic"
	"man-p2p/common"
	"man-p2p/pebblestore"
	"man-p2p/pin"
)

// MRC-721 索引与聚合,设计见 MRC721_INDEXING_DESIGN.md。
// 协议语义:路径 /nft/mrc721/{collection_name}/... 下的 create pin;
// collection_desc 注册专辑(首个生效),item_desc 回填条目信息(每字段首条生效),
// 其余为铸造;collection 未注册时乱序暂存(pending),注册后回放。

const mrc721PathPrefix = "/nft/mrc721"

func Mrc721RuntimeEnabled() bool {
	if common.Config == nil {
		return false
	}
	return isModuleEnabled("mrc721")
}

// handleMrc721 在 pin 入库后按路径分派 MRC-721 协议动作(DoIndexerRun 区块路径调用)。
// operation 语义:仅 collection_desc 放开 modify(发行方专属,版本链);其余路径只认 create;
// revoke/hide 在 /nft/mrc721 下不生效。
func (pd *PebbleData) handleMrc721(chainName string, height int64, pinList []*pin.PinInscription) {
	for _, pinNode := range pinList {
		if len(pinNode.Id) == 0 {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(pinNode.Path), mrc721PathPrefix) {
			continue
		}
		// collectionName 取原始路径段,保留大小写
		segments := strings.Split(pinNode.Path, "/")
		if len(segments) < 4 || segments[3] == "" {
			continue
		}
		collectionName := segments[3]
		op := ""
		if len(segments) > 4 {
			op = strings.ToLower(segments[4])
		}
		switch op {
		case "collection_desc":
			switch pinNode.Operation {
			case "create":
				pd.mrc721CollectionHandle(collectionName, pinNode)
			case "modify":
				pd.mrc721CollectionModifyHandle(collectionName, pinNode)
			}
		case "item_desc":
			if pinNode.Operation == "create" {
				pd.mrc721ItemDescHandle(collectionName, pinNode)
			}
		default:
			if pinNode.Operation == "create" {
				pd.mrc721MintHandle(collectionName, pinNode)
			}
		}
	}
}

// ---------- collection ----------

func (pd *PebbleData) mrc721CollectionHandle(collectionName string, pinNode *pin.PinInscription) {
	existing, err := pd.Database.GetMrc721CollectionByName(collectionName)
	if err != nil {
		log.Printf("[MRC721] get collection %s: %v", collectionName, err)
		return
	}
	if existing != nil {
		return // 首个 collection_desc 生效
	}
	content, err := mrc721ParseContent(pinNode.ContentBody)
	if err != nil {
		log.Printf("[MRC721] collection_desc %s invalid content: %v", pinNode.Id, err)
		return
	}
	col := &pebblestore.Mrc721Collection{
		CollectionName:   collectionName,
		PinId:            pinNode.Id,
		CreateAddress:    pinNode.Address,
		CreateMetaId:     pinNode.MetaId,
		CreateTime:       pinNode.Timestamp,
		GenesisChainName: mrc721ChainName(pinNode),
		GenesisHeight:    pinNode.GenesisHeight,
		GenesisTx:        pinNode.GenesisTransaction,
	}
	col.Name = mrc721StringField(content, "name")
	col.Desc = mrc721StringField(content, "desc")
	col.Website = mrc721StringField(content, "website")
	col.Cover = mrc721StringField(content, "cover")
	col.Metadata = mrc721StringField(content, "metadata")
	// totalSupply 缺省 -1;royaltyRate 缺省 0(对齐官方文档)
	col.TotalSupply = -1
	if raw, ok := mrc721LookupCI(content, "totalsupply"); ok {
		v, err := mrc721ParseInt64(raw)
		if err != nil {
			log.Printf("[MRC721] collection_desc %s invalid totalSupply: %v", pinNode.Id, err)
			return
		}
		col.TotalSupply = v
	}
	if raw, ok := mrc721LookupCI(content, "royaltyrate"); ok {
		v, err := mrc721ParseInt64(raw)
		if err != nil {
			log.Printf("[MRC721] collection_desc %s invalid royaltyRate: %v", pinNode.Id, err)
			return
		}
		col.RoyaltyRate = int(v)
	}
	if col.TotalSupply < -1 || col.TotalSupply > 1000000000000 {
		log.Printf("[MRC721] collection_desc %s totalSupply out of range: %d", pinNode.Id, col.TotalSupply)
		return
	}
	if col.RoyaltyRate < 0 || col.RoyaltyRate > 20 {
		log.Printf("[MRC721] collection_desc %s royaltyRate out of range: %d", pinNode.Id, col.RoyaltyRate)
		return
	}
	if err := pd.Database.SaveMrc721Collection(col); err != nil {
		log.Printf("[MRC721] save collection %s: %v", collectionName, err)
		return
	}
	// 注册成功,回放乱序暂存(mint 先入账,desc 后回填,colmod 最后按时间序生效)
	pd.mrc721DrainPending(collectionName)
}

// ---------- collection_desc modify(发行方专属版本链) ----------

func (pd *PebbleData) mrc721CollectionModifyHandle(collectionName string, pinNode *pin.PinInscription) {
	col, err := pd.Database.GetMrc721CollectionByName(collectionName)
	if err != nil {
		log.Printf("[MRC721] get collection %s: %v", collectionName, err)
		return
	}
	if col == nil {
		// 专辑未注册:暂存,注册后按时间序回放
		pd.Database.PutMrc721Pending(collectionName, &pebblestore.Mrc721PendingEntry{
			Kind: "colmod", PinId: pinNode.Id,
			ColMod: &pebblestore.Mrc721PendingColMod{
				ModifyPinId:   pinNode.Id,
				ModifyAddress: pinNode.Address,
				Content:       string(pinNode.ContentBody),
				Timestamp:     pinNode.Timestamp,
			},
		})
		return
	}
	pd.mrc721CollectionModifyCommit(collectionName, col, pinNode.Id, pinNode.Address, pinNode.ContentBody)
}

// mrc721CollectionModifyCommit 应用一笔 collection_desc modify:
// 发行方专属;同一 modify pinId 幂等;展示字段全量覆盖,totalSupply/royaltyRate
// 若出现则按同范围校验(非法整条忽略);降 totalSupply 不回撤已收录条目,只收后续 mint 闸门。
func (pd *PebbleData) mrc721CollectionModifyCommit(collectionName string, col *pebblestore.Mrc721Collection, modifyPinId, modifyAddress string, contentBody []byte) bool {
	if modifyAddress != col.CreateAddress {
		log.Printf("[MRC721] collection modify %s rejected: %s is not issuer of %s (%s)", modifyPinId, modifyAddress, collectionName, col.CreateAddress)
		return false
	}
	for _, id := range col.ModifyHistory {
		if id == modifyPinId {
			return true // 幂等:双路径重复投递/回放不重复入链
		}
	}
	content, err := mrc721ParseContent(contentBody)
	if err != nil {
		log.Printf("[MRC721] collection modify %s invalid content: %v", modifyPinId, err)
		return false
	}
	newTotalSupply := col.TotalSupply
	if raw, ok := mrc721LookupCI(content, "totalsupply"); ok {
		v, err := mrc721ParseInt64(raw)
		if err != nil {
			log.Printf("[MRC721] collection modify %s invalid totalSupply: %v", modifyPinId, err)
			return false
		}
		newTotalSupply = v
	}
	newRoyaltyRate := col.RoyaltyRate
	if raw, ok := mrc721LookupCI(content, "royaltyrate"); ok {
		v, err := mrc721ParseInt64(raw)
		if err != nil {
			log.Printf("[MRC721] collection modify %s invalid royaltyRate: %v", modifyPinId, err)
			return false
		}
		newRoyaltyRate = int(v)
	}
	if newTotalSupply < -1 || newTotalSupply > 1000000000000 {
		log.Printf("[MRC721] collection modify %s totalSupply out of range: %d", modifyPinId, newTotalSupply)
		return false
	}
	if newRoyaltyRate < 0 || newRoyaltyRate > 20 {
		log.Printf("[MRC721] collection modify %s royaltyRate out of range: %d", modifyPinId, newRoyaltyRate)
		return false
	}
	if _, ok := mrc721LookupCI(content, "name"); ok {
		col.Name = mrc721StringField(content, "name")
	}
	if _, ok := mrc721LookupCI(content, "desc"); ok {
		col.Desc = mrc721StringField(content, "desc")
	}
	if _, ok := mrc721LookupCI(content, "website"); ok {
		col.Website = mrc721StringField(content, "website")
	}
	if _, ok := mrc721LookupCI(content, "cover"); ok {
		col.Cover = mrc721StringField(content, "cover")
	}
	if _, ok := mrc721LookupCI(content, "metadata"); ok {
		col.Metadata = mrc721StringField(content, "metadata")
	}
	col.TotalSupply = newTotalSupply
	col.RoyaltyRate = newRoyaltyRate
	col.ModifyHistory = append(col.ModifyHistory, modifyPinId)
	if err := pd.Database.SaveMrc721Collection(col); err != nil {
		log.Printf("[MRC721] save collection %s modify: %v", collectionName, err)
		return false
	}
	return true
}

// Mrc721ReplayResult 定向重放的单 pin 结果。
type Mrc721ReplayResult struct {
	PinId   string `json:"pinId"`
	Chain   string `json:"chain,omitempty"`
	Applied bool   `json:"applied"`
	Reason  string `json:"reason,omitempty"`
}

// Mrc721ReplayPins 定向重放:按 pinId 从本地 pin 主记录取存量 pin 重过聚合钩子
// (O(1) 查找,不做扫描;幂等由 mintCommit/modify history/transfer 去重保证)。
// 供 api admin 端点对"先于功能部署落链"的存量协议 pin 补聚合。
func (pd *PebbleData) Mrc721ReplayPins(pinIds []string) []Mrc721ReplayResult {
	results := make([]Mrc721ReplayResult, 0, len(pinIds))
	for _, id := range pinIds {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		pinNode, err := pd.GetPinById(id)
		if err != nil || pinNode.Id == "" {
			results = append(results, Mrc721ReplayResult{PinId: id, Applied: false, Reason: "pin not found"})
			continue
		}
		pd.handleMrc721(pinNode.ChainName, pinNode.GenesisHeight, []*pin.PinInscription{&pinNode})
		results = append(results, Mrc721ReplayResult{PinId: id, Chain: pinNode.ChainName, Applied: true})
	}
	return results
}

// ---------- item_desc ----------

func (pd *PebbleData) mrc721ItemDescHandle(collectionName string, pinNode *pin.PinInscription) {
	refs, ok := mrc721ParseItemDescRefs(pinNode.ContentBody)
	if !ok {
		return
	}
	col, err := pd.Database.GetMrc721CollectionByName(collectionName)
	if err != nil {
		log.Printf("[MRC721] get collection %s: %v", collectionName, err)
		return
	}
	if col == nil {
		// 专辑未注册:解析后的引用暂存,注册后回放
		pd.Database.PutMrc721Pending(collectionName, &pebblestore.Mrc721PendingEntry{
			Kind: "desc", PinId: pinNode.Id,
			Desc: &pebblestore.Mrc721PendingDesc{DescPinId: pinNode.Id, Items: refs},
		})
		return
	}
	if !pd.mrc721ApplyDescRefs(refs) {
		// 引用含未收录条目:暂存,待对应条目入账后回放
		pd.Database.PutMrc721Pending(collectionName, &pebblestore.Mrc721PendingEntry{
			Kind: "desc", PinId: pinNode.Id,
			Desc: &pebblestore.Mrc721PendingDesc{DescPinId: pinNode.Id, Items: refs},
		})
	}
}

// mrc721ApplyDescRefs 将 item_desc 引用回填到已收录条目(每字段首条生效)。
// 返回是否全部引用条目已收录。
func (pd *PebbleData) mrc721ApplyDescRefs(refs []pebblestore.Mrc721ItemDescRef) bool {
	allApplied := true
	for _, ref := range refs {
		if ref.PinId == "" {
			continue
		}
		item, err := pd.Database.GetMrc721Item(ref.PinId)
		if err != nil {
			log.Printf("[MRC721] get item %s: %v", ref.PinId, err)
			continue
		}
		if item == nil {
			allApplied = false
			continue
		}
		changed := false
		if item.Name == "" && ref.Name != "" {
			item.Name, changed = ref.Name, true
		}
		if item.Desc == "" && ref.Desc != "" {
			item.Desc, changed = ref.Desc, true
		}
		if item.Cover == "" && ref.Cover != "" {
			item.Cover, changed = ref.Cover, true
		}
		if item.Metadata == "" && ref.Metadata != "" {
			item.Metadata, changed = ref.Metadata, true
		}
		if changed {
			if err := pd.Database.SaveMrc721Item(item); err != nil {
				log.Printf("[MRC721] update item %s desc: %v", ref.PinId, err)
			}
		}
	}
	return allApplied
}

func mrc721ParseItemDescRefs(contentBody []byte) ([]pebblestore.Mrc721ItemDescRef, bool) {
	content, err := mrc721ParseContent(contentBody)
	if err != nil {
		log.Printf("[MRC721] item_desc invalid content: %v", err)
		return nil, false
	}
	raw, ok := mrc721LookupCI(content, "items")
	if !ok {
		return nil, true
	}
	var refs []pebblestore.Mrc721ItemDescRef
	if err := sonic.Unmarshal(raw, &refs); err != nil {
		log.Printf("[MRC721] item_desc invalid items: %v", err)
		return nil, false
	}
	return refs, true
}

// ---------- mint ----------

func (pd *PebbleData) mrc721MintHandle(collectionName string, pinNode *pin.PinInscription) {
	col, err := pd.Database.GetMrc721CollectionByName(collectionName)
	if err != nil {
		log.Printf("[MRC721] get collection %s: %v", collectionName, err)
		return
	}
	mint := &pebblestore.Mrc721PendingMint{
		ItemPinId:        pinNode.Id,
		OwnerAddress:     pinNode.Address,
		OwnerMetaId:      pinNode.MetaId,
		ContentType:      pinNode.ContentType,
		ContentSummary:   pinListSummary(pinNode.ContentBody),
		GenesisChainName: mrc721ChainName(pinNode),
		GenesisHeight:    pinNode.GenesisHeight,
		GenesisTx:        pinNode.GenesisTransaction,
		GenesisFee:       pinNode.GenesisFee,
		Output:           pinNode.Output,
		OutputValue:      pinNode.OutputValue,
		CreateTime:       pinNode.Timestamp,
	}
	if col == nil {
		// 乱序:专辑未注册,暂存待注册后回放
		pd.Database.PutMrc721Pending(collectionName, &pebblestore.Mrc721PendingEntry{
			Kind: "mint", PinId: pinNode.Id, Mint: mint,
		})
		return
	}
	pd.mrc721MintCommit(collectionName, col, mint)
}

// mrc721MintCommit 收录条目:supply 校验、序号分配、主表/列表/持有人索引。
// 按 itemPinId 幂等:同一 pin 经链上与 P2P 双路径到达、或回填重跑时不重复计数。
func (pd *PebbleData) mrc721MintCommit(collectionName string, col *pebblestore.Mrc721Collection, mint *pebblestore.Mrc721PendingMint) bool {
	if existing, _ := pd.Database.GetMrc721Item(mint.ItemPinId); existing != nil {
		return true
	}
	if col.TotalSupply != -1 && col.TotalNum >= col.TotalSupply {
		log.Printf("[MRC721] mint %s rejected: exceeds totalSupply %d", collectionName, col.TotalSupply)
		return false
	}
	col.TotalNum++
	item := &pebblestore.Mrc721Item{
		ItemPinId:        mint.ItemPinId,
		CollectionName:   collectionName,
		CollectionPinId:  col.PinId,
		Number:           col.TotalNum,
		OwnerAddress:     mint.OwnerAddress,
		OwnerMetaId:      mint.OwnerMetaId,
		CreatorMinted:    mint.OwnerAddress == col.CreateAddress,
		ContentType:      mint.ContentType,
		ContentSummary:   mint.ContentSummary,
		GenesisChainName: mint.GenesisChainName,
		GenesisHeight:    mint.GenesisHeight,
		GenesisTx:        mint.GenesisTx,
		GenesisFee:       mint.GenesisFee,
		Output:           mint.Output,
		OutputValue:      mint.OutputValue,
		CreateTime:       mint.CreateTime,
	}
	if err := pd.Database.SaveMrc721Collection(col); err != nil {
		log.Printf("[MRC721] update collection %s totalnum: %v", collectionName, err)
		return false
	}
	if err := pd.Database.SaveMrc721Item(item); err != nil {
		log.Printf("[MRC721] save item: %v", err)
		return false
	}
	if err := pd.Database.AppendMrc721CollectionItem(collectionName, item.Number, item.ItemPinId); err != nil {
		log.Printf("[MRC721] append coll item: %v", err)
	}
	if err := pd.Database.PutMrc721OwnerItem(mint.OwnerAddress, collectionName, item.ItemPinId); err != nil {
		log.Printf("[MRC721] put owner item: %v", err)
	}
	// 新条目可能正是 pending desc 引用的对象,尝试回放
	pd.mrc721DrainPendingDescs(collectionName)
	return true
}

// ---------- transfer ----------

// mrc721TransferHook 消费 pin 转移检测结果(handleTransfer 调用),更新条目归属、
// 持有人索引换 key、追加转移历史。transferMap 的 key 为 "txid:index" 输出串,
// pinIdByOutput 由 handleTransfer 从待检 pin 记录构建(key → pin.Id)。
func (pd *PebbleData) mrc721TransferHook(chainName string, height int64, transferMap map[string]*pin.PinTransferInfo, pinIdByOutput map[string]string, transferTime int64) {
	for output, info := range transferMap {
		pinId := pinIdByOutput[output]
		if pinId == "" {
			continue
		}
		// 按 (itemPinId, height, txid) 幂等:同一转移事件重放(链上 re-index/P2P 重投递/回填重跑)不重复计数
		if existing, _ := pd.Database.GetMrc721ItemTransfer(pinId, height, info.Location); existing != nil {
			continue
		}
		item, err := pd.Database.GetMrc721Item(pinId)
		if err != nil {
			log.Printf("[MRC721] get item %s: %v", pinId, err)
			continue
		}
		if item == nil {
			continue // 非 MRC-721 条目
		}
		if info.IsMeltdown || info.Address == "" {
			// 熔化:标记 burned,移出持有人视图,记录保留可溯源
			item.Burned = true
			if item.OwnerAddress != "" {
				pd.Database.DeleteMrc721OwnerItem(item.OwnerAddress, item.CollectionName, item.ItemPinId)
			}
		} else if item.OwnerAddress != info.Address {
			if item.OwnerAddress != "" {
				pd.Database.DeleteMrc721OwnerItem(item.OwnerAddress, item.CollectionName, item.ItemPinId)
			}
			pd.Database.PutMrc721OwnerItem(info.Address, item.CollectionName, item.ItemPinId)
			item.OwnerAddress = info.Address
			item.OwnerMetaId = common.GetMetaIdByAddress(info.Address)
		}
		if !info.IsMeltdown {
			item.Output = info.Output
			item.OutputValue = info.OutputValue
		}
		item.TransferCount++
		if err := pd.Database.SaveMrc721Item(item); err != nil {
			log.Printf("[MRC721] save item %s on transfer: %v", pinId, err)
			continue
		}
		if err := pd.Database.SaveMrc721ItemTransfer(&pebblestore.Mrc721ItemTransfer{
			ItemPinId:   pinId,
			FromAddress: info.FromAddress,
			ToAddress:   info.Address,
			ChainName:   chainName,
			Height:      height,
			TxId:        info.Location,
			Time:        transferTime,
		}); err != nil {
			log.Printf("[MRC721] save item transfer %s: %v", pinId, err)
		}
	}
}

// ---------- pending 回放 ----------

// mrc721DrainPending 专辑注册后回放全部暂存:mint 先入账,desc 后回填,colmod 最后按时间序生效。
func (pd *PebbleData) mrc721DrainPending(collectionName string) {
	col, err := pd.Database.GetMrc721CollectionByName(collectionName)
	if err != nil || col == nil {
		return
	}
	entries, err := pd.Database.PopMrc721Pending(collectionName)
	if err != nil {
		log.Printf("[MRC721] pop pending %s: %v", collectionName, err)
		return
	}
	if len(entries) == 0 {
		return
	}
	var descs []*pebblestore.Mrc721PendingEntry
	var colMods []*pebblestore.Mrc721PendingEntry
	for _, entry := range entries {
		switch {
		case entry.Kind == "mint" && entry.Mint != nil:
			pd.mrc721MintCommit(collectionName, col, entry.Mint)
		case entry.Kind == "desc":
			descs = append(descs, entry)
		case entry.Kind == "colmod" && entry.ColMod != nil:
			colMods = append(colMods, entry)
		}
	}
	// desc 回填可能引用回放 mint 的新条目,故在 mint 之后处理
	for _, entry := range descs {
		pd.mrc721DrainOneDesc(collectionName, entry)
	}
	// colmod 按 (timestamp, pinId) 时间序回放,最后一条生效
	sort.Slice(colMods, func(i, j int) bool {
		a, b := colMods[i].ColMod, colMods[j].ColMod
		if a.Timestamp != b.Timestamp {
			return a.Timestamp < b.Timestamp
		}
		return a.ModifyPinId < b.ModifyPinId
	})
	for _, entry := range colMods {
		m := entry.ColMod
		col, err := pd.Database.GetMrc721CollectionByName(collectionName)
		if err != nil || col == nil {
			return
		}
		pd.mrc721CollectionModifyCommit(collectionName, col, m.ModifyPinId, m.ModifyAddress, []byte(m.Content))
	}
}

// mrc721DrainPendingDescs 条目入账后回放引用它的暂存 desc。
func (pd *PebbleData) mrc721DrainPendingDescs(collectionName string) {
	entries, err := pd.Database.PopMrc721Pending(collectionName)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.Kind == "desc" {
			pd.mrc721DrainOneDesc(collectionName, entry)
		} else {
			// 非 desc 暂存原样放回
			pd.Database.PutMrc721Pending(collectionName, entry)
		}
	}
}

// mrc721DrainOneDesc 应用一条暂存 desc;仍有未收录引用时放回暂存。
func (pd *PebbleData) mrc721DrainOneDesc(collectionName string, entry *pebblestore.Mrc721PendingEntry) {
	if entry.Desc == nil {
		return
	}
	if !pd.mrc721ApplyDescRefs(entry.Desc.Items) {
		pd.Database.PutMrc721Pending(collectionName, entry)
	}
}

// ---------- 内容解析(JSON key 大小写不敏感,值保留原样) ----------

type mrc721Content map[string]json.RawMessage

func mrc721ParseContent(body []byte) (mrc721Content, error) {
	var content mrc721Content
	if err := sonic.Unmarshal(body, &content); err != nil {
		return nil, err
	}
	return content, nil
}

func mrc721LookupCI(content mrc721Content, key string) (json.RawMessage, bool) {
	if v, ok := content[key]; ok {
		return v, true
	}
	for k, v := range content {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	return nil, false
}

func mrc721StringField(content mrc721Content, key string) string {
	raw, ok := mrc721LookupCI(content, key)
	if !ok {
		return ""
	}
	var s string
	if err := sonic.Unmarshal(raw, &s); err == nil {
		return s
	}
	// metadata 允许任意 JSON(对象/数组),原文保存
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return trimmed
	}
	return ""
}

func mrc721ParseInt64(raw json.RawMessage) (int64, error) {
	var i int64
	if err := sonic.Unmarshal(raw, &i); err == nil {
		return i, nil
	}
	var f float64
	if err := sonic.Unmarshal(raw, &f); err == nil {
		return int64(f), nil
	}
	var s string
	if err := sonic.Unmarshal(raw, &s); err == nil {
		return strconv.ParseInt(s, 10, 64)
	}
	return 0, strconv.ErrSyntax
}

func mrc721ChainName(pinNode *pin.PinInscription) string {
	if pinNode.ChainName != "" {
		return pinNode.ChainName
	}
	return common.Chain
}
