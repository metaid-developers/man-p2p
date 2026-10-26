package man

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"man-p2p/pin"

	"github.com/bytedance/sonic"
)

// 协议目录：聚合所有 /protocols/* 一级路径（族路径），用于
// GET /api/protocol/list。目录条目含 pin 总数、首条上链 pin 与首现作者、
// 以及是否已通过 /protocols/metaprotocol 注册。
//
// 背景约束：PathPinDB / PinSort / CountDB 的 key 均以 sha256(path) 开头，
// 无法按 "/protocols/" 原文前缀扫描，原始 path 只存在于 pins 分片的
// pin JSON 本体中，因此聚合只能全量扫描 pins 分片（带 TTL 内存缓存）。

const (
	protocolRoot        = "/protocols/"
	metaprotocolPinPath = "/protocols/metaprotocol"
)

// ProtocolDirEntry 一个 /protocols/<name> 族路径的目录条目。
// firstAuthor 由 FirstAddress + FirstGlobalMetaId 两个字段组成；
// GlobalMetaId 在早期 pin 上可能为空。
type ProtocolDirEntry struct {
	Path              string `json:"path"`
	PinTotal          int64  `json:"pinTotal"`
	FirstTxid         string `json:"firstTxid"`
	FirstPinId        string `json:"firstPinId"`
	FirstChain        string `json:"firstChain"`
	FirstHeight       int64  `json:"firstHeight"`
	FirstTimestamp    int64  `json:"firstTimestamp"`
	FirstAddress      string `json:"firstAddress"`
	FirstGlobalMetaId string `json:"firstGlobalMetaId"`
	Registered        bool   `json:"registered"`
	RegisteredPinId   string `json:"registeredPinId"`
}

// protocolFamily 把任意 /protocols/... 路径折叠到一级族路径。
// "/protocols" 本身、空名字（"/protocols//x"）不算协议族。
func protocolFamily(path string) (string, bool) {
	if !strings.HasPrefix(path, protocolRoot) {
		return "", false
	}
	rest := path[len(protocolRoot):]
	if rest == "" {
		return "", false
	}
	name := rest
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		name = rest[:i]
	}
	if name == "" {
		return "", false
	}
	return protocolRoot + name, true
}

// protocolFirstLess 首现判定：timestamp → chainName → height → txIndex → pinId 全序比较。
// 同一区块内按 txIndex（块内交易位置）排序，比索引器 seenTime 到达序更贴近链上事实。
func protocolFirstLess(a, b *protocolPinProbe) bool {
	if a.Timestamp != b.Timestamp {
		return a.Timestamp < b.Timestamp
	}
	if a.ChainName != b.ChainName {
		return a.ChainName < b.ChainName
	}
	if a.GenesisHeight != b.GenesisHeight {
		return a.GenesisHeight < b.GenesisHeight
	}
	if a.TxIndex != b.TxIndex {
		return a.TxIndex < b.TxIndex
	}
	return a.Id < b.Id
}

// protocolPinProbe 聚合扫描只需要的小字段集，避免解码 contentBody。
type protocolPinProbe struct {
	Id            string `json:"id"`
	ChainName     string `json:"chainName"`
	GenesisHeight int64  `json:"genesisHeight"`
	Timestamp     int64  `json:"timestamp"`
	TxIndex       int    `json:"txIndex"`
	Address       string `json:"address"`
	GlobalMetaId  string `json:"globalMetaId"`
	Path          string `json:"path"`
}

var protocolDirCache struct {
	sync.Mutex
	entries []ProtocolDirEntry
	builtAt time.Time
}

// protocolDirCacheTTL 目录快照缓存时长；全量扫描代价高，避免逐请求重建。
const protocolDirCacheTTL = 10 * time.Minute

// ProtocolDirectory 返回协议目录快照（默认按 path 升序）。
// force=true 时跳过缓存强制重建（对应 API 的 refresh=1）。
func (pd *PebbleData) ProtocolDirectory(force bool) ([]ProtocolDirEntry, time.Time, error) {
	protocolDirCache.Lock()
	defer protocolDirCache.Unlock()
	if !force && protocolDirCache.entries != nil && time.Since(protocolDirCache.builtAt) < protocolDirCacheTTL {
		return protocolDirCache.entries, protocolDirCache.builtAt, nil
	}
	entries, err := pd.BuildProtocolDirectory()
	if err != nil {
		return nil, protocolDirCache.builtAt, err
	}
	protocolDirCache.entries = entries
	protocolDirCache.builtAt = time.Now()
	return entries, protocolDirCache.builtAt, nil
}

// BuildProtocolDirectory 全量扫描 pins 分片，聚合协议目录：
//  1. 只统计已上链（GenesisHeight >= 0）的 pin；mempool 未上链不计入。
//     已撤销（status=-1）/熔化（status=-2）的 pin 仍是链上历史事实，照常计入
//     （首现判定同理——skill-service 的首现 pin 本身就是 revoked，排除会改写首现作者）；
//  2. 子路径计数归入一级族路径（如 /protocols/preference/round → /protocols/preference）；
//  3. 首现 pin 按 (timestamp, chainName, height, pinId) 取最小；
//  4. 注册状态来自 /protocols/metaprotocol 注册 pin 的 contentBody.path
//     （本地无内容体的 metadata-only 节点解析不到，按未注册返回）。
func (pd *PebbleData) BuildProtocolDirectory() ([]ProtocolDirEntry, error) {
	if pd == nil || pd.Database == nil {
		return nil, fmt.Errorf("pebble database not initialized")
	}
	type familyAgg struct {
		total int64
		first *protocolPinProbe
	}
	families := make(map[string]*familyAgg)
	var registryIds []string
	for _, shard := range pd.Database.PinsDBs {
		it, err := shard.NewIter(nil)
		if err != nil {
			return nil, err
		}
		for it.First(); it.Valid(); it.Next() {
			var probe protocolPinProbe
			if err := sonic.Unmarshal(it.Value(), &probe); err != nil {
				continue
			}
			fam, ok := protocolFamily(probe.Path)
			if !ok {
				continue
			}
			if probe.GenesisHeight < 0 {
				continue // mempool 未上链
			}
			p := probe
			agg := families[fam]
			if agg == nil {
				agg = &familyAgg{}
				families[fam] = agg
			}
			agg.total++
			if agg.first == nil || protocolFirstLess(&p, agg.first) {
				agg.first = &p
			}
			if probe.Path == metaprotocolPinPath {
				registryIds = append(registryIds, probe.Id)
			}
		}
		it.Close()
	}

	// 注册表：族路径 → 最新注册 pin（timestamp/height/id 取最大者）
	registered := make(map[string]string)
	if len(registryIds) > 0 {
		latest := make(map[string]protocolPinProbe)
		for _, raw := range pd.Database.BatchGetPinByKeys(registryIds, false) {
			var full pin.PinInscription
			if err := sonic.Unmarshal(raw, &full); err != nil {
				continue
			}
			var body struct {
				Path string `json:"path"`
			}
			if err := sonic.Unmarshal(full.ContentBody, &body); err != nil {
				continue
			}
			fam, ok := protocolFamily(body.Path)
			if !ok {
				continue
			}
			cur := protocolPinProbe{Id: full.Id, ChainName: full.ChainName, GenesisHeight: full.GenesisHeight, Timestamp: full.Timestamp}
			if old, seen := latest[fam]; !seen || protocolFirstLess(&old, &cur) {
				latest[fam] = cur
			}
		}
		for fam, p := range latest {
			registered[fam] = p.Id
		}
	}

	entries := make([]ProtocolDirEntry, 0, len(families))
	for fam, agg := range families {
		entry := ProtocolDirEntry{Path: fam, PinTotal: agg.total}
		if agg.first != nil {
			entry.FirstTxid = strings.TrimSuffix(agg.first.Id, "i0")
			entry.FirstPinId = agg.first.Id
			entry.FirstChain = agg.first.ChainName
			entry.FirstHeight = agg.first.GenesisHeight
			entry.FirstTimestamp = agg.first.Timestamp
			entry.FirstAddress = agg.first.Address
			entry.FirstGlobalMetaId = agg.first.GlobalMetaId
		}
		if regId, ok := registered[fam]; ok {
			entry.Registered = true
			entry.RegisteredPinId = regId
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}
