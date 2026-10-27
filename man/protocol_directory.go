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
	// 以下为 stale-while-revalidate 生命周期状态：
	// rebuilding 一次后台重建在跑（触发防重入）；
	// proberStarted/proberStop 后台提前重建探测（进程生命周期一次，test-only 重置）。
	rebuilding    bool
	proberStarted bool
	proberStop    chan struct{}
}

// protocolDirBuildFn 快照构建的替换缝：同步兜底与后台重建都经由它，
// 测试可注入慢/失败构建来验证不阻塞与降级语义。
var protocolDirBuildFn = (*PebbleData).BuildProtocolDirectory

// 后台提前重建节奏：protocolDirProberTick 是探测节拍；
// 快照年龄达到 protocolDirRefreshAhead（< TTL）即触发一次后台重建，
// 保证一次后台重建至多间隔 protocolDirRefreshAhead + 一次节拍发生，快照常新不过期。
var (
	protocolDirProberTick   = time.Minute
	protocolDirRefreshAhead = 5 * time.Minute
)

// protocolDirCacheTTL 目录快照缓存时长；全量扫描代价高，避免逐请求重建。
const protocolDirCacheTTL = 10 * time.Minute

// ProtocolDirectory 返回协议目录快照（默认按 path 升序）。
// stale-while-revalidate：只要有过往快照就立即返回上一份可用快照——
// 快照过期或 force=true（API 的 refresh=1）只触发一次后台重建，请求
// 绝不等重建（生产全量扫描 46–60s，同步重建曾撞 nginx 60s 代理线）；
// 仅无快照（进程刚起、后台刷新持续失败）时同步构建兜底（允许慢，不允许死）。
// builtAt 语义保持原样：返回快照的构建时刻（过期快照即其为旧时刻）。
func (pd *PebbleData) ProtocolDirectory(force bool) ([]ProtocolDirEntry, time.Time, error) {
	protocolDirCache.Lock()
	defer protocolDirCache.Unlock()
	if protocolDirCache.entries != nil {
		if force || time.Since(protocolDirCache.builtAt) >= protocolDirCacheTTL {
			startProtocolDirRebuildLocked(pd)
		}
		return protocolDirCache.entries, protocolDirCache.builtAt, nil
	}
	entries, err := protocolDirBuildFn(pd)
	if err != nil {
		return nil, protocolDirCache.builtAt, err
	}
	protocolDirCache.entries = entries
	protocolDirCache.builtAt = time.Now()
	startProtocolDirProberLocked(pd)
	return protocolDirCache.entries, protocolDirCache.builtAt, nil
}

// startProtocolDirRebuildLocked 触发一次后台重建（重建中防重入）。
// 调用方持锁；重建本身不持锁，读取方不被阻塞。重建失败保留旧快照，
// 交由下一次过期请求或探测重试。
func startProtocolDirRebuildLocked(pd *PebbleData) {
	if pd == nil || protocolDirCache.rebuilding {
		return
	}
	protocolDirCache.rebuilding = true
	go func() {
		entries, err := protocolDirBuildFn(pd)
		protocolDirCache.Lock()
		defer protocolDirCache.Unlock()
		protocolDirCache.rebuilding = false
		if err != nil || entries == nil {
			return
		}
		protocolDirCache.entries = entries
		protocolDirCache.builtAt = time.Now()
	}()
}

// startProtocolDirProberLocked 启动后台提前重建探测（进程级一次）：
// 每 protocolDirProberTick 检查一次，快照年龄达到 protocolDirRefreshAhead
// 即后台重建——快照在 TTL 到期前总被换新，防过期不依赖任何用户请求。
func startProtocolDirProberLocked(pd *PebbleData) {
	if protocolDirCache.proberStarted {
		return
	}
	protocolDirCache.proberStarted = true
	stop := make(chan struct{})
	protocolDirCache.proberStop = stop
	interval, ahead := protocolDirProberTick, protocolDirRefreshAhead
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				protocolDirCache.Lock()
				running := protocolDirCache.proberStarted && protocolDirCache.proberStop == stop
				if running && protocolDirCache.entries != nil && time.Since(protocolDirCache.builtAt) >= ahead {
					startProtocolDirRebuildLocked(pd)
				}
				protocolDirCache.Unlock()
				if !running {
					return // 已被重置（test-only）：安静退出，不在已关闭的库上重建
				}
			}
		}
	}()
}

// ResetProtocolDirCacheForTest 测试专用：清空目录快照缓存并停掉后台探测，
// 再等在飞后台重建结束——防止跨测试用例的库存活污染，以及测试收尾
// db.Close() 与在飞全量扫描相撞（panic: pebble: closed）。生产代码勿调用，
// 生产库与进程同生命周期，无需重置。
func ResetProtocolDirCacheForTest() {
	// 1) 停后台探测（探测只会再触发新重建）
	protocolDirCache.Lock()
	if protocolDirCache.proberStop != nil {
		close(protocolDirCache.proberStop)
		protocolDirCache.proberStop = nil
	}
	protocolDirCache.Unlock()
	// 2) 等在飞的后台重建退出（完稿落账或失败保留），避免其继续使用
	//    即将被 Close 的库，也避免它落在清空之后把旧库数据带回来
	for {
		protocolDirCache.Lock()
		inflight := protocolDirCache.rebuilding
		protocolDirCache.Unlock()
		if !inflight {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	// 3) 清空快照缓存与后台状态
	protocolDirCache.Lock()
	protocolDirCache.entries = nil
	protocolDirCache.builtAt = time.Time{}
	protocolDirCache.proberStarted = false
	protocolDirCache.Unlock()
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
