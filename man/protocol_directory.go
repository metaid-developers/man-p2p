package man

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"man-p2p/common"
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

// 快照落盘持久化：每次快照生成/换新时同步写入盘上快照，进程启动时装入
// 最近一份可用快照作为初始缓存——生产重启后首个公网请求不再进入 46–60s
// 同步冷重建（2026-10-27 生产复现：重启后公网 /api/protocol/list 全 504 约
// 10 分 50 秒）。盘上快照是加速器不是依赖：读不到、写失败都只记日志，
// 应答路径永远可用（无盘上快照时维持既有同步兜底）。
//
// 存储设施复用既有 mrc20 快照目录（GetSnapshotDir，同属 common.Config.Pebble.Dir
// 之下的 snapshots/），不发明新的持久化面。

// protocolDirSnapshotPathFn 快照文件路径缝（test-only 注入），nil = 持久化
// 禁用。生产默认 protocolDirDefaultSnapshotPath（进程生命周期内恒非 nil，
// 不经本缝切换配置面）；ResetProtocolDirCacheForTest 把缝归 nil，使 Go 包
// 测试默认不落盘。
var protocolDirSnapshotPathFn func() string = protocolDirDefaultSnapshotPath

func protocolDirDefaultSnapshotPath() string {
	return filepath.Join(GetSnapshotDir(common.Config.Pebble.Dir), "protocol_dir.json")
}

// protocolDirSnapshotFile 盘上快照文件体。BuiltAt 用 time.Time 直接
// JSON 往返（RFC3339 带纳秒，无损），恢复时保持「快照原构建时刻」不变——
// 见 ProtocolDirectory 的 builtAt 语义注释。
type protocolDirSnapshotFile struct {
	BuiltAt time.Time          `json:"builtAt"`
	Entries []ProtocolDirEntry `json:"entries"`
}

// persistProtocolDirSnapshotLocked 把当前快照+构建时刻写盘（临时文件 + rename
// 原子替换，防进程中途被杀留下半截文件）。调用方持锁（写约 50KB 的文件在
// 锁内代价可忽略，且保证落盘与缓存换新的次序不乱）；任何失败只记日志。
// 生产代码勿调 protocolDirSnapshotPathFn=nil（测试经 ResetProtocolDirCacheForTest
// 归禁用态）。
func persistProtocolDirSnapshotLocked(entries []ProtocolDirEntry, builtAt time.Time) {
	if protocolDirSnapshotPathFn == nil || builtAt.IsZero() {
		return
	}
	path := protocolDirSnapshotPathFn()
	raw, err := sonic.Marshal(protocolDirSnapshotFile{BuiltAt: builtAt, Entries: entries})
	if err != nil {
		log.Printf("[ProtocolDir] snapshot marshal failed (serve unaffected): %v", err)
		return
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("[ProtocolDir] snapshot mkdir %s failed (serve unaffected): %v", dir, err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0644); err != nil {
		log.Printf("[ProtocolDir] snapshot write %s failed (serve unaffected): %v", tmp, err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		log.Printf("[ProtocolDir] snapshot rename %s->%s failed (serve unaffected): %v", tmp, path, err)
		return
	}
}

// readProtocolDirSnapshotLocked 读最近一份盘上快照；文件缺失/损坏/元数据
// 不完整（builtAt 为零或 entries 为 null）一律报错交回让路同步兜底，
// 绝不产出可疑缓存。调用方持锁。
func readProtocolDirSnapshotLocked() ([]ProtocolDirEntry, time.Time, error) {
	path := protocolDirSnapshotPathFn()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	var f protocolDirSnapshotFile
	if err := sonic.Unmarshal(raw, &f); err != nil {
		return nil, time.Time{}, fmt.Errorf("decode %s: %w", path, err)
	}
	if f.BuiltAt.IsZero() {
		return nil, time.Time{}, fmt.Errorf("snapshot %s has zero builtAt", path)
	}
	if f.Entries == nil {
		return nil, time.Time{}, fmt.Errorf("snapshot %s has nil entries", path)
	}
	return f.Entries, f.BuiltAt, nil
}

// LoadProtocolDirSnapshotBoot 进程启动时装入最近一份可用盘上快照作为初始
// 缓存并启动后台提前重建探测——重启后首个公网请求立即命中快照应答（SWR
// 语义照旧：若快照年龄已逾 TTL/快照过期，该请求只触发后台重建、不等重建）。
// 在 InitRuntime 装库之后调用（依赖 PebbleStore 就位）。缓存已有快照
// /路径缝为 nil（测试）/没有可读快照时静默退出，不改变任何既有语义。
func LoadProtocolDirSnapshotBoot() {
	protocolDirCache.Lock()
	defer protocolDirCache.Unlock()
	if protocolDirCache.entries != nil || protocolDirSnapshotPathFn == nil {
		return
	}
	entries, builtAt, err := readProtocolDirSnapshotLocked()
	if err != nil {
		log.Printf("[ProtocolDir] boot: no usable on-disk snapshot (%v) — first request falls back to sync build", err)
		return
	}
	protocolDirCache.entries = entries
	protocolDirCache.builtAt = builtAt
	log.Printf("[ProtocolDir] boot: loaded on-disk snapshot builtAt=%d entries=%d", builtAt.Unix(), len(entries))
	startProtocolDirProberLocked(PebbleStore)
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
//
// builtAt 语义（含持久化取舍）：返回快照的构建时刻。命中盘上持久化快照时
// 返回的是该快照的【原构建时刻】（写入盘上文件并回读，不做人为拨前）——
// 这样 API 侧 builtAt.Unix() 输出的时间线与"这份数据何时算齐"仍严格一致
// （取向："说老实话"：它是旧数据，时戳就该是旧的），代价是该快照年龄很可能
// 已逾 TTL，首个请求按 SWR 语义立即触发一次后台重建（不等重建，只应答旧
// 数据）；重建成功后落盘换新，次轮请求自然切回最新。缓存与盘上快照的
// builtAt 同源同值，接口契约零变化。
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
	protocolDirCache.builtAt = time.Now()
	protocolDirCache.entries = entries
	persistProtocolDirSnapshotLocked(entries, protocolDirCache.builtAt)
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
		protocolDirCache.builtAt = time.Now()
		protocolDirCache.entries = entries
		persistProtocolDirSnapshotLocked(entries, protocolDirCache.builtAt)
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
// 生产库与进程同生命周期，无需重置。快照落盘路径缝一并归禁用态（测试
// 默认不写盘；需要持久化的用例自行注入 t.TempDir 路径）。
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
	// 3) 清空快照缓存与后台状态，快照落盘路径缝归禁用态
	protocolDirCache.Lock()
	protocolDirCache.entries = nil
	protocolDirCache.builtAt = time.Time{}
	protocolDirCache.proberStarted = false
	protocolDirSnapshotPathFn = nil
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
