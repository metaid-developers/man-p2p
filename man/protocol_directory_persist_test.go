package man

// 协议目录快照盘上持久化测试：
//   - 持久化 → 重启（清缓存）→ 首请求命中盘上快照（builtAt 为原构建时刻，
//     数据为盘上那份而非当前库的，随后 SWR 照旧在后台换新）；
//   - 盘上无快照（空目录）时启动装载静默退出，首请求维持既有同步兜底；
//   - 盘上快照损坏（非 JSON / 元数据残缺）时同样回退同步兜底，不产出可疑缓存；
//   - 落盘失败（路径不可创建）不影响线上应答——盘上快照是加速器不是依赖。
//
// 重启语义用 ResetProtocolDirCacheForTest（清内存缓存 + 停探测 + 等在飞重建）
// 模拟，盘上文件保持原样——与生产「进程退出→启动」的盘面等价。

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"man-p2p/common"

	"github.com/bytedance/sonic"
)

// persistSeamRoot 每个用例独立的网缝路径目录。
func persistSeamRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "snapshots")
}

// persistSeam inject 快照路径缝，t.Cleanup 归禁用态（与 ResetProtocolDirCacheForTest 等效）。
func persistSeam(t *testing.T, dir string) {
	t.Helper()
	protocolDirSnapshotPathFn = func() string {
		return filepath.Join(dir, "protocol_dir.json")
	}
	t.Cleanup(func() { protocolDirSnapshotPathFn = nil })
}

// TestProtocolDirPersistAndBootHit 覆盖「持久化 → 重启 → 首请求命中」主路径：
// ① 旧库同步首建并落盘，盘上文件 builtAt == 首建时刻；
// ② 模拟重启：换新库（数据与旧库不同），启动时装载盘上快照；
// ③ 重启后首个请求立即命中盘上快照——数据为旧库那份、builtAt 为原构建时刻
//
//	（绝不能是当前库的同步冷建结果）；
//
// ④ SWR 照旧续动：对当前库的换新走既有后台重建路径（刷新/探测驱动与旧代码
// 一致，age 驱动路径由既有 SWR 套件覆盖），落账后重新落盘。
// 测试时间尺度上盘上快照年龄 < TTL，故用 refresh=1 确保换新必达。
func TestProtocolDirPersistAndBootHit(t *testing.T) {
	swrResetCache(t)
	dir := persistSeamRoot(t)
	persistSeam(t, dir)

	oldStore := swrNewDB(t, t.TempDir(), 4010, 1) // 旧库：1 条 pin → 1 个族
	entries0, builtAt0, err := oldStore.ProtocolDirectory(false)
	if err != nil || len(entries0) != 1 {
		t.Fatalf("cold build on old store: entries=%d err=%v", len(entries0), err)
	}
	snapPath := filepath.Join(dir, "protocol_dir.json")
	raw, err := os.ReadFile(snapPath)
	if err != nil {
		t.Fatalf("snapshot must be persisted on first build: %v", err)
	}
	var onDisk protocolDirSnapshotFile
	if err := sonic.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("decode persisted snapshot: %v", err)
	}
	if len(onDisk.Entries) != 1 || onDisk.Entries[0].PinTotal != 1 {
		t.Fatalf("persisted entries wrong: %+v", onDisk.Entries)
	}
	if !onDisk.BuiltAt.Equal(builtAt0) {
		t.Fatalf("persisted builtAt %v != in-memory builtAt %v", onDisk.BuiltAt, builtAt0)
	}

	// —— 模拟重启：清内存缓存（盘上文件原样），换上新库 ——
	ResetProtocolDirCacheForTest() // 缝归 nil
	persistSeam(t, dir)
	newStore := swrNewDB(t, t.TempDir(), 4020, 2) // 新库：2 条 pin → 1 个族但 PinTotal=2

	// 生产启动路径：InitRuntime → LoadProtocolDirSnapshotBoot
	LoadProtocolDirSnapshotBoot()

	// —— 重启后首个公网请求：必须命中盘上快照（旧库数据 + 原构建时刻）——
	first, builtAt1, err := newStore.ProtocolDirectory(false)
	if err != nil {
		t.Fatalf("first request after restart: %v", err)
	}
	if len(first) != 1 || first[0].PinTotal != 1 {
		t.Fatalf("first request must serve the on-disk snapshot(old store data, pinTotal=1), got %+v", first)
	}
	if !builtAt1.Equal(builtAt0) {
		t.Fatalf("first request builtAt %v must be the persisted original build moment %v", builtAt1, builtAt0)
	}

	// —— SWR 照旧续动：refresh=1（force）立即应答旧快照并触发一次后台重建，
	// 随后对当前库（新库）换新并重新落盘。生产里重启后的盘上快照年龄通常已逾
	// TTL，首请求即走同一条后台重建路径（无 force 时仅当年龄逾 TTL 才触发，
	// 该 AGE 驱动路径已由 TestProtocolDirStaleServesOldSnapshotWhileRebuilding
	// 经 swrAgeSnapshot 覆盖；此处 force 确保换新必达，测试时间尺度上不受 TTL 限制）。
	_, builtAtF, err := newStore.ProtocolDirectory(true)
	if err != nil || !builtAtF.Equal(builtAt1) {
		t.Fatalf("refresh=1 must serve the on-disk snapshot unchanged: builtAt=%v want=%v err=%v", builtAtF, builtAt1, err)
	}
	landed := swrWaitRebuildLanded(t, builtAt1)
	refetched, builtAt2, err := newStore.ProtocolDirectory(false)
	if err != nil || len(refetched) != 1 || refetched[0].PinTotal != 2 {
		t.Fatalf("after rebuild must serve new-store data: %+v err=%v", refetched, err)
	}
	if !builtAt2.Equal(landed) {
		t.Fatalf("refetched builtAt %v != landed %v", builtAt2, landed)
	}
	raw2, err := os.ReadFile(snapPath)
	if err != nil {
		t.Fatalf("rebuild landing must re-persist: %v", err)
	}
	var onDisk2 protocolDirSnapshotFile
	if err := sonic.Unmarshal(raw2, &onDisk2); err != nil {
		t.Fatalf("decode rebuilt snapshot: %v", err)
	}
	if !onDisk2.BuiltAt.Equal(landed) {
		t.Fatalf("re-persisted builtAt %v != rebuilt builtAt %v", onDisk2.BuiltAt, landed)
	}
}

// TestProtocolDirBootWithoutOnDiskSnapshot 覆盖「盘上无快照回退同步兜底」：
// 快照路径缝已启用但目录里没有快照文件——启动装载静默退出，缓存保持空，
// 首个请求按既有语义同步构建并应答（允许慢，不允许死）。
func TestProtocolDirBootWithoutOnDiskSnapshot(t *testing.T) {
	swrResetCache(t)
	persistSeam(t, persistSeamRoot(t)) // 目录存在但为空

	LoadProtocolDirSnapshotBoot() // 无文件：静默退出，不产缓存、不启探测

	protocolDirCache.Lock()
	hasEntries := protocolDirCache.entries != nil
	protocolDirCache.Unlock()
	if hasEntries {
		t.Fatalf("boot load without file must not populate cache")
	}

	store := swrNewDB(t, t.TempDir(), 4030, 1)
	started := time.Now()
	entries, builtAt, err := store.ProtocolDirectory(false)
	if err != nil || len(entries) != 1 || builtAt.IsZero() {
		t.Fatalf("sync fallback after empty boot: entries=%d err=%v", len(entries), err)
	}
	if time.Since(started) > 10*time.Second {
		t.Fatalf("sync fallback unexpectedly slow: %v", time.Since(started))
	}
}

// TestProtocolDirBootCorruptSnapshot 覆盖盘上快照损坏的两类形态：
// 整文件非 JSON、以及 JSON 合法但元数据残缺（builtAt 为零 / entries 为 null）。
// 启动装载必须在两类形态下都拒绝装入并回退同步兜底，绝不产出可疑缓存。
func TestProtocolDirBootCorruptSnapshot(t *testing.T) {
	corrupts := map[string][]byte{
		"not-json":     []byte("not a json snapshot"),
		"zero-builtin": []byte(`{"builtAt":"0001-01-01T00:00:00Z","entries":[]}`),
		"nil-entries":  []byte(`{"builtAt":"2026-10-27T10:00:00Z"}`),
	}
	for name, content := range corrupts {
		t.Run(name, func(t *testing.T) {
			swrResetCache(t)
			dir := persistSeamRoot(t)
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(filepath.Join(dir, "protocol_dir.json"), content, 0644); err != nil {
				t.Fatalf("write corrupt snapshot: %v", err)
			}
			persistSeam(t, dir)

			LoadProtocolDirSnapshotBoot()
			protocolDirCache.Lock()
			hasEntries := protocolDirCache.entries != nil
			protocolDirCache.Unlock()
			if hasEntries {
				t.Fatalf("corrupt snapshot (%s) must not be loaded into cache", name)
			}
			store := swrNewDB(t, t.TempDir(), 4040, 1)
			entries, _, err := store.ProtocolDirectory(false)
			if err != nil || len(entries) != 1 {
				t.Fatalf("sync fallback after corrupt snapshot (%s): entries=%d err=%v", name, len(entries), err)
			}
		})
	}
}

// TestProtocolDirPersistFailureServesAnyway 覆盖「落盘失败不影响线上应答」：
// 快照路径缝指向一个不可创建的目录（父级被文件占住），落盘失败只记日志，
// 构建与应答路径完全不受影响。
func TestProtocolDirPersistFailureServesAnyway(t *testing.T) {
	swrResetCache(t)
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("file, not a dir"), 0644); err != nil {
		t.Fatalf("setup blocked parent: %v", err)
	}
	t.Helper()
	protocolDirSnapshotPathFn = func() string {
		return filepath.Join(blocked, "snapshots", "protocol_dir.json")
	}
	t.Cleanup(func() { protocolDirSnapshotPathFn = nil })

	store := swrNewDB(t, t.TempDir(), 4050, 2)
	entries, builtAt, err := store.ProtocolDirectory(false)
	if err != nil || len(entries) != 1 || entries[0].PinTotal != 2 || builtAt.IsZero() {
		t.Fatalf("persist failure must not affect serving: entries=%+v builtAt=%v err=%v", entries, builtAt, err)
	}
	protocolDirCache.Lock()
	kept := protocolDirCache.entries != nil
	protocolDirCache.Unlock()
	if !kept {
		t.Fatalf("cache must remain populated when persist fails")
	}
}

// TestProtocolDirDefaultSnapshotWiring 验证生产默认网缝接线：快照文件确实
// 落在 GetSnapshotDir(common.Config.Pebble.Dir)（既有 mrc20 快照目录）之下，
// 且归零清缝后把缝复位为生产默认值即可重新命中此前落盘的快照——即生产
// 「保留 PebbleDir 重启」语义的完整走查（不经任何测试注入路径）。
func TestProtocolDirDefaultSnapshotWiring(t *testing.T) {
	swrResetCache(t)
	prevCfg := common.Config
	prevDir := ""
	if prevCfg != nil {
		prevDir = prevCfg.Pebble.Dir
	}
	pebbleDir := t.TempDir()
	if prevCfg == nil {
		common.Config = &common.AllConfig{} // 包测试下 InitConfig 未必已跑
	}
	common.Config.Pebble.Dir = pebbleDir
	t.Cleanup(func() {
		common.Config.Pebble.Dir = prevDir
		if prevCfg == nil {
			common.Config = nil
		}
	})

	// 生产默认缝复位（swrResetCache 已把缝归 nil）：与 var 初始化完全一致
	protocolDirSnapshotPathFn = protocolDirDefaultSnapshotPath
	t.Cleanup(func() { protocolDirSnapshotPathFn = nil })

	store := swrNewDB(t, t.TempDir(), 4060, 3)
	_, builtAt0, err := store.ProtocolDirectory(false)
	if err != nil {
		t.Fatalf("first build: %v", err)
	}
	snapPath := filepath.Join(GetSnapshotDir(pebbleDir), "protocol_dir.json")
	if _, err := os.Stat(snapPath); err != nil {
		t.Fatalf("default seam must persist under GetSnapshotDir(Pebble.Dir): %v", err)
	}

	// —— 模拟重启：清缓存后把缝复位为生产默认函数（与 var 初始化一致）——
	ResetProtocolDirCacheForTest()
	common.Config.Pebble.Dir = pebbleDir
	protocolDirSnapshotPathFn = protocolDirDefaultSnapshotPath
	t.Cleanup(func() { protocolDirSnapshotPathFn = nil })
	LoadProtocolDirSnapshotBoot()

	got, builtAt1, err := store.ProtocolDirectory(false)
	if err != nil || len(got) != 1 || got[0].PinTotal != 3 {
		t.Fatalf("boot hit via default seam: entries=%d err=%v", len(got), err)
	}
	if !builtAt1.Equal(builtAt0) {
		t.Fatalf("default seam builtAt %v != original %v", builtAt1, builtAt0)
	}
}
