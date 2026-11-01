package pebblestore

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bytedance/sonic"
	"github.com/cockroachdb/pebble"
)

// MRC-721 聚合存储:单库前缀 key(对齐 MrcDb 风格),设计见 MRC721_INDEXING_DESIGN.md §3。
//
//	c:{collectionName}                            → Mrc721Collection JSON(专辑主表)
//	cp:{collectionDescPinId}                      → collectionName(collectionid 反查)
//	i:{itemPinId}                                 → Mrc721Item JSON(NFT 主表)
//	ci:{collectionName}{seq}{itemPinId}           → 空(专辑内条目,按收录序正序)
//	oi:{ownerAddress}&{collectionName}&{itemPinId} → 空(按持有人查询)
//	th:{itemPinId}&{height}{txid}                 → Mrc721ItemTransfer JSON(转移路径)
//	p:{collectionName}&{pinId}                    → Mrc721PendingEntry JSON(乱序暂存)

const (
	mrc721CollectionKeyPrefix  = "c:"
	mrc721CollByPinIdKeyPrefix = "cp:"
	mrc721ItemKeyPrefix        = "i:"
	mrc721CollItemKeyPrefix    = "ci:"
	mrc721OwnerItemKeyPrefix   = "oi:"
	mrc721ItemTransferPrefix   = "th:"
	mrc721PendingKeyPrefix     = "p:"
)

type Mrc721Collection struct {
	CollectionName   string `json:"collectionName"`
	Name             string `json:"name"`
	TotalSupply      int64  `json:"totalSupply"` // -1 = 不限
	RoyaltyRate      int    `json:"royaltyRate"`
	Desc             string `json:"desc"`
	Website          string `json:"website"`
	Cover            string `json:"cover"`
	Metadata         string `json:"metadata"`
	PinId            string `json:"pinid"` // collectionid = collection_desc pinid
	CreateAddress    string `json:"createAddress"`
	CreateMetaId     string `json:"createMetaId"`
	CreateTime       int64  `json:"createTime"`
	GenesisChainName string `json:"genesisChainName"`
	GenesisHeight    int64  `json:"genesisHeight"`
	GenesisTx        string `json:"genesisTx"`
	TotalNum         int64  `json:"totalnum"`
	// ModifyHistory 发行方 collection_desc modify 的版本链:modify pinId 按 ingest 顺序追加,
	// 最后一个即当前生效版本;同一 pinId 幂等不重复。
	ModifyHistory []string `json:"modifyHistory,omitempty"`
}

type Mrc721Item struct {
	ItemPinId        string `json:"itemPinId"`
	CollectionName   string `json:"collectionName"`
	CollectionPinId  string `json:"collectionPinId"`
	Number           int64  `json:"number"` // 专辑内收录序号,1 起
	Name             string `json:"name"`
	Desc             string `json:"desc"`
	Cover            string `json:"cover"`
	Metadata         string `json:"metadata"`
	OwnerAddress     string `json:"ownerAddress"`
	OwnerMetaId      string `json:"ownerMetaId"`
	CreatorMinted    bool   `json:"creatorMinted"`
	Burned           bool   `json:"burned"` // 熔化(meltdown)标记,持有人视图排除
	ContentType      string `json:"contentType"`
	ContentSummary   string `json:"contentSummary"`
	GenesisChainName string `json:"genesisChainName"`
	GenesisHeight    int64  `json:"genesisHeight"`
	GenesisTx        string `json:"genesisTx"`
	GenesisFee       int64  `json:"genesisFee"`
	Output           string `json:"output"`
	OutputValue      int64  `json:"outputValue"`
	CreateTime       int64  `json:"createTime"`
	TransferCount    int64  `json:"transferCount"`
}

type Mrc721ItemTransfer struct {
	ItemPinId   string `json:"itemPinId"`
	FromAddress string `json:"fromAddress"`
	ToAddress   string `json:"toAddress"`
	ChainName   string `json:"chainName"`
	Height      int64  `json:"height"`
	TxId        string `json:"txId"`
	Time        int64  `json:"time"`
}

// Mrc721ItemDescRef item_desc 中对单个条目的描述引用。
type Mrc721ItemDescRef struct {
	PinId    string `json:"pinid"`
	Name     string `json:"name"`
	Desc     string `json:"desc"`
	Cover    string `json:"cover"`
	Metadata string `json:"metadata"`
}

// Mrc721PendingEntry 乱序暂存:collection 尚未注册时的 mint/item_desc/colmod。
type Mrc721PendingEntry struct {
	Kind   string               `json:"kind"` // "mint" | "desc" | "colmod"
	PinId  string               `json:"pinId"`
	Mint   *Mrc721PendingMint   `json:"mint,omitempty"`
	Desc   *Mrc721PendingDesc   `json:"desc,omitempty"`
	ColMod *Mrc721PendingColMod `json:"colmod,omitempty"`
}

// Mrc721PendingMint mint 暂存:足以重建 item 的字段。
type Mrc721PendingMint struct {
	ItemPinId        string `json:"itemPinId"`
	OwnerAddress     string `json:"ownerAddress"`
	OwnerMetaId      string `json:"ownerMetaId"`
	ContentType      string `json:"contentType"`
	ContentSummary   string `json:"contentSummary"`
	GenesisChainName string `json:"genesisChainName"`
	GenesisHeight    int64  `json:"genesisHeight"`
	GenesisTx        string `json:"genesisTx"`
	GenesisFee       int64  `json:"genesisFee"`
	Output           string `json:"output"`
	OutputValue      int64  `json:"outputValue"`
	CreateTime       int64  `json:"createTime"`
}

// Mrc721PendingDesc item_desc 暂存。
type Mrc721PendingDesc struct {
	DescPinId string              `json:"descPinId"`
	Items     []Mrc721ItemDescRef `json:"items"`
}

// Mrc721PendingColMod collection_desc modify 暂存:专辑注册后按时间序回放。
type Mrc721PendingColMod struct {
	ModifyPinId   string `json:"modifyPinId"`
	ModifyAddress string `json:"modifyAddress"`
	Content       string `json:"content"` // 原始 modify JSON
	Timestamp     int64  `json:"timestamp"`
}

func mrc721CollItemKey(collectionName string, seq int64, itemPinId string) string {
	return fmt.Sprintf("%s%s%020d%s", mrc721CollItemKeyPrefix, collectionName, seq, itemPinId)
}

func mrc721OwnerItemKey(ownerAddress, collectionName, itemPinId string) string {
	return fmt.Sprintf("%s%s&%s&%s", mrc721OwnerItemKeyPrefix, ownerAddress, collectionName, itemPinId)
}

func mrc721ItemTransferKey(itemPinId string, height int64, txId string) string {
	return fmt.Sprintf("%s%s&%020d%s", mrc721ItemTransferPrefix, itemPinId, height, txId)
}

func mrc721PutJSON(db *pebble.DB, key string, v interface{}) error {
	data, err := sonic.Marshal(v)
	if err != nil {
		return err
	}
	return db.Set([]byte(key), data, pebble.Sync)
}

func mrc721GetJSON(db *pebble.DB, key string, v interface{}) (bool, error) {
	data, closer, err := db.Get([]byte(key))
	if err == pebble.ErrNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer closer.Close()
	if err = sonic.Unmarshal(data, v); err != nil {
		return true, err
	}
	return true, nil
}

func mrc721IteratePrefix(db *pebble.DB, prefix string, fn func(key string, value []byte) bool) error {
	it, err := db.NewIter(nil)
	if err != nil {
		return err
	}
	defer it.Close()
	for it.First(); it.Valid(); it.Next() {
		key := string(it.Key())
		if !strings.HasPrefix(key, prefix) {
			if key > prefix {
				break
			}
			continue
		}
		if !fn(key, it.Value()) {
			break
		}
	}
	return nil
}

// ---------- collection ----------

func (db *Database) SaveMrc721Collection(col *Mrc721Collection) error {
	if err := mrc721PutJSON(db.Mrc721Db, mrc721CollectionKeyPrefix+col.CollectionName, col); err != nil {
		return err
	}
	return db.Mrc721Db.Set([]byte(mrc721CollByPinIdKeyPrefix+col.PinId), []byte(col.CollectionName), pebble.Sync)
}

func (db *Database) GetMrc721CollectionByName(collectionName string) (*Mrc721Collection, error) {
	var col Mrc721Collection
	found, err := mrc721GetJSON(db.Mrc721Db, mrc721CollectionKeyPrefix+collectionName, &col)
	if err != nil || !found {
		return nil, err
	}
	return &col, nil
}

func (db *Database) GetMrc721CollectionByPinId(collectionDescPinId string) (*Mrc721Collection, error) {
	nameBytes, closer, err := db.Mrc721Db.Get([]byte(mrc721CollByPinIdKeyPrefix + collectionDescPinId))
	if err == pebble.ErrNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer closer.Close()
	return db.GetMrc721CollectionByName(string(nameBytes))
}

// ListMrc721Collections 全量遍历专辑(offset 分页;当前量级可行,排序索引见设计文档 §8.4)。
func (db *Database) ListMrc721Collections(offset, limit int64) ([]*Mrc721Collection, int64, error) {
	var all []*Mrc721Collection
	err := mrc721IteratePrefix(db.Mrc721Db, mrc721CollectionKeyPrefix, func(_ string, value []byte) bool {
		var col Mrc721Collection
		if err := sonic.Unmarshal(value, &col); err == nil {
			all = append(all, &col)
		}
		return true
	})
	if err != nil {
		return nil, 0, err
	}
	total := int64(len(all))
	if offset >= total {
		return []*Mrc721Collection{}, total, nil
	}
	end := offset + limit
	if end > total || end <= 0 {
		end = total
	}
	return all[offset:end], total, nil
}

// ---------- item ----------

func (db *Database) SaveMrc721Item(item *Mrc721Item) error {
	return mrc721PutJSON(db.Mrc721Db, mrc721ItemKeyPrefix+item.ItemPinId, item)
}

func (db *Database) GetMrc721Item(itemPinId string) (*Mrc721Item, error) {
	var item Mrc721Item
	found, err := mrc721GetJSON(db.Mrc721Db, mrc721ItemKeyPrefix+itemPinId, &item)
	if err != nil || !found {
		return nil, err
	}
	return &item, nil
}

// AppendMrc721CollectionItem 记录条目在专辑内的收录序。
func (db *Database) AppendMrc721CollectionItem(collectionName string, seq int64, itemPinId string) error {
	return db.Mrc721Db.Set([]byte(mrc721CollItemKey(collectionName, seq, itemPinId)), []byte{}, pebble.Sync)
}

// IterateMrc721CollectionItems 按收录序遍历专辑条目。
func (db *Database) IterateMrc721CollectionItems(collectionName string, fn func(seq int64, itemPinId string) bool) error {
	prefix := mrc721CollItemKeyPrefix + collectionName
	return mrc721IteratePrefix(db.Mrc721Db, prefix, func(key string, _ []byte) bool {
		rest := key[len(prefix):]
		if len(rest) < 20 {
			return true
		}
		seq, _ := strconv.ParseInt(rest[:20], 10, 64)
		return fn(seq, rest[20:])
	})
}

// ---------- owner ----------

func (db *Database) PutMrc721OwnerItem(ownerAddress, collectionName, itemPinId string) error {
	return db.Mrc721Db.Set([]byte(mrc721OwnerItemKey(ownerAddress, collectionName, itemPinId)), []byte{}, pebble.Sync)
}

func (db *Database) DeleteMrc721OwnerItem(ownerAddress, collectionName, itemPinId string) error {
	return db.Mrc721Db.Delete([]byte(mrc721OwnerItemKey(ownerAddress, collectionName, itemPinId)), pebble.Sync)
}

// IterateMrc721OwnerItems 遍历某地址持有的条目(collectionName 可为空=全部)。
func (db *Database) IterateMrc721OwnerItems(ownerAddress, collectionName string, fn func(collectionName, itemPinId string) bool) error {
	prefix := mrc721OwnerItemKeyPrefix + ownerAddress + "&"
	if collectionName != "" {
		prefix += collectionName + "&"
	}
	return mrc721IteratePrefix(db.Mrc721Db, prefix, func(key string, _ []byte) bool {
		rest := key[len(prefix):]
		if collectionName != "" {
			return fn(collectionName, rest)
		}
		sep := strings.Index(rest, "&")
		if sep < 0 {
			return true
		}
		return fn(rest[:sep], rest[sep+1:])
	})
}

// ---------- transfer(P1 使用,P0 先落存储) ----------

func (db *Database) SaveMrc721ItemTransfer(tr *Mrc721ItemTransfer) error {
	return mrc721PutJSON(db.Mrc721Db, mrc721ItemTransferKey(tr.ItemPinId, tr.Height, tr.TxId), tr)
}

// GetMrc721ItemTransfer 按 (itemPinId, height, txId) 精确取转移记录(幂等判断用)。
func (db *Database) GetMrc721ItemTransfer(itemPinId string, height int64, txId string) (*Mrc721ItemTransfer, error) {
	var tr Mrc721ItemTransfer
	found, err := mrc721GetJSON(db.Mrc721Db, mrc721ItemTransferKey(itemPinId, height, txId), &tr)
	if err != nil || !found {
		return nil, err
	}
	return &tr, nil
}

// IterateMrc721ItemTransfers 按高度正序遍历某条目的转移历史。
func (db *Database) IterateMrc721ItemTransfers(itemPinId string, fn func(tr *Mrc721ItemTransfer) bool) error {
	prefix := mrc721ItemTransferPrefix + itemPinId + "&"
	return mrc721IteratePrefix(db.Mrc721Db, prefix, func(_ string, value []byte) bool {
		var tr Mrc721ItemTransfer
		if err := sonic.Unmarshal(value, &tr); err == nil {
			return fn(&tr)
		}
		return true
	})
}

// ---------- pending(乱序自愈) ----------

func (db *Database) PutMrc721Pending(collectionName string, entry *Mrc721PendingEntry) error {
	return mrc721PutJSON(db.Mrc721Db, mrc721PendingKeyPrefix+collectionName+"&"+entry.PinId, entry)
}

// PopMrc721Pending 取出并删除某专辑的全部暂存条目,按写入序返回。
func (db *Database) PopMrc721Pending(collectionName string) ([]*Mrc721PendingEntry, error) {
	var entries []*Mrc721PendingEntry
	var keys [][]byte
	prefix := mrc721PendingKeyPrefix + collectionName + "&"
	err := mrc721IteratePrefix(db.Mrc721Db, prefix, func(key string, value []byte) bool {
		var entry Mrc721PendingEntry
		if err := sonic.Unmarshal(value, &entry); err == nil {
			entries = append(entries, &entry)
		}
		keys = append(keys, []byte(key))
		return true
	})
	if err != nil {
		return nil, err
	}
	for _, key := range keys {
		if err := db.Mrc721Db.Delete(key, pebble.Sync); err != nil {
			return entries, err
		}
	}
	return entries, nil
}
