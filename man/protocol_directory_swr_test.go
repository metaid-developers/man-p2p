package man

// stale-while-revalidate 生命周期测试：
//   - 快照过期后的请求只触发后台重建，立即返回上一份可用快照（不阻塞、不命中同步重建路径）；
//   - 后台重建成功后换新快照；
//   - 后台重建失败保留旧快照，请求仍可用；
//   - 无快照（进程刚起 / 刷新持续失败）走同步兜底，仍能应答；
//   - 后台提前重建探测在无请求的情况下防过期。
//
// 构建经由可替换缝 protocolDirBuildFn（脚手架提交 1fe00e2）注入门控/失败实现。

import (
	"fmt"
	"testing"
	"time"

	"man-p2p/pebblestore"
	"man-p2p/pin"
)

// swrTestPinId 生成形态合法的占位 pinId（63 hex + i0）。
func swrTestPinId(i int) string {
	return fmt.Sprintf("%063di0", i)
}

// swrResetCacheState 清空目录缓存与后台状态（关闭既有探测循环）。
func swrResetCacheState() {
	protocolDirCache.Lock()
	if protocolDirCache.proberStop != nil {
		close(protocolDirCache.proberStop)
		protocolDirCache.proberStop = nil
	}
	protocolDirCache.entries = nil
	protocolDirCache.builtAt = time.Time{}
	protocolDirCache.rebuilding = false
	protocolDirCache.proberStarted = false
	protocolDirCache.Unlock()
}

// swrResetCache 白盒清空；t.Cleanup 再清一次——关掉测试期间启动的
// 探测循环、回落真实构建缝，防止跨测试干扰。
func swrResetCache(t *testing.T) {
	t.Helper()
	swrResetCacheState()
	t.Cleanup(func() {
		swrResetCacheState()
	})
}

// swrRestoreBuildFn 恢复真实构建缝。
func swrRestoreBuildFn(t *testing.T) {
	t.Helper()
	orig := protocolDirBuildFn
	t.Cleanup(func() { protocolDirBuildFn = orig })
}

// swrNewDB 建一个含 pins 枚 /protocols/swrproto pin 的独立库（内容按序号区分）。
func swrNewDB(t *testing.T, tmpDir string, firstIdNum, pins int) *PebbleData {
	t.Helper()
	db, err := pebblestore.NewDataBase(tmpDir, 2)
	if err != nil {
		t.Fatalf("NewDataBase: %v", err)
	}
	store := &PebbleData{Database: db}
	prevStore := PebbleStore
	PebbleStore = store // IngestP2PPin 走 PebbleStore 全局
	t.Cleanup(func() { PebbleStore = prevStore })
	for i := 0; i < pins; i++ {
		p := &pin.PinInscription{
			Id:            swrTestPinId(firstIdNum + i),
			Path:          "/protocols/swrproto",
			Address:       "1AxUdSkVdDyDreYSYVoDRFeyS1pvQdvcJx",
			GlobalMetaId:  "idq1d5m392ahkhp79wsy9ur79e3vhak7tg729dwdr5",
			ChainName:     "mvc",
			Timestamp:     1793000000 + int64(firstIdNum) + int64(i),
			GenesisHeight: 191000 + int64(firstIdNum) + int64(i),
		}
		if err := IngestP2PPin(p); err != nil {
			t.Fatalf("IngestP2PPin(%s): %v", p.Id, err)
		}
	}
	PebbleStore = prevStore
	return store
}

// swrAgeSnapshot 把首建快照人为置为过期，返回过期时刻（后续断言基准）。
func swrAgeSnapshot(t *testing.T, freshAt time.Time) time.Time {
	t.Helper()
	staleAt := freshAt.Add(-2 * protocolDirCacheTTL)
	protocolDirCache.Lock()
	protocolDirCache.builtAt = staleAt
	protocolDirCache.Unlock()
	return staleAt
}

// swrWaitRebuildLanded 等后台重建落账（builtAt 晚于 prev），超时即失败。
func swrWaitRebuildLanded(t *testing.T, prev time.Time) time.Time {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		protocolDirCache.Lock()
		cur := protocolDirCache.builtAt
		protocolDirCache.Unlock()
		if cur.After(prev) {
			return cur
		}
		if time.Now().After(deadline) {
			t.Fatalf("background rebuild did not land within deadline (builtAt still %v, want after %v)", cur, prev)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestProtocolDirStaleServesOldSnapshotWhileRebuilding 覆盖验收要求的两条路径：
// ① 快照过期后请求返回旧快照而非阻塞（构建门控模拟生产 46–60s 冷重建，
//
//	请求在门控释放前即时应答——同步重建路径零命中）；
//
// ② 门控释放后后台重建成功换新快照。
func TestProtocolDirStaleServesOldSnapshotWhileRebuilding(t *testing.T) {
	swrRestoreBuildFn(t)
	oldStore := swrNewDB(t, t.TempDir(), 2010, 1) // 旧快照：pinTotal=1
	newStore := swrNewDB(t, t.TempDir(), 3010, 2) // 重建目标：pinTotal=2

	_, freshAt, err := oldStore.ProtocolDirectory(true)
	if err != nil {
		t.Fatalf("prime snapshot: %v", err)
	}
	staleAt := swrAgeSnapshot(t, freshAt)

	// 门控构建：进入即记号，卡在 gate 上模拟慢重建；
	// gateTimeout 秒后自动解锁——旧行为（同步重建挡在请求路径上）会把
	// 请求真实钳住整整 gateTimeout，转为快速失败而非吊死测试。
	const gateTimeout = 3 * time.Second
	gate := make(chan struct{})
	entered := make(chan struct{}, 1)
	protocolDirBuildFn = func(pd *PebbleData) ([]ProtocolDirEntry, error) {
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-gate:
			return pd.BuildProtocolDirectory()
		case <-time.After(gateTimeout):
			return nil, fmt.Errorf("simulated slow rebuild still running after %v", gateTimeout)
		}
	}

	start := time.Now()
	entries, gotAt, err := newStore.ProtocolDirectory(false)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("① stale request errored: %v", err)
	}
	if elapsed > time.Second {
		t.Fatalf("① stale request blocked %v waiting for rebuild; SWR must serve old snapshot immediately", elapsed)
	}
	if len(entries) != 1 || entries[0].PinTotal != 1 {
		t.Fatalf("① expected OLD snapshot (1 family, pinTotal=1), got %+v", entries)
	}
	if !gotAt.Equal(staleAt) {
		t.Fatalf("① gotAt=%v want stale builtAt=%v (must return the previous snapshot verbatim)", gotAt, staleAt)
	}
	protocolDirCache.Lock()
	rebuilding := protocolDirCache.rebuilding
	protocolDirCache.Unlock()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatalf("① background rebuild never started; stale request returned stale data without revalidating")
	}
	if !rebuilding {
		t.Fatalf("① rebuilding flag should be set while gated build is in flight")
	}

	// ② 门控释放 → 后台重建成功 → 换新
	close(gate)
	landed := swrWaitRebuildLanded(t, staleAt)
	again, builtAt2, err := newStore.ProtocolDirectory(false)
	if err != nil {
		t.Fatalf("② post-rebuild request errored: %v", err)
	}
	if len(again) != 1 || again[0].PinTotal != 2 {
		t.Fatalf("② expected NEW snapshot (pinTotal=2) after background rebuild, got %+v", again)
	}
	if !builtAt2.Equal(landed) || !landed.After(staleAt) {
		t.Fatalf("② builtAt should have advanced after background rebuild: %v -> %v", gotAt, builtAt2)
	}
}

// TestProtocolDirBackgroundRebuildFailureKeepsOldSnapshot 后台刷新失败是设计内情形：
// 旧快照保留、请求不被错误污染、重建触发防重入，下一次请求仍有权再触发。
func TestProtocolDirBackgroundRebuildFailureKeepsOldSnapshot(t *testing.T) {
	swrRestoreBuildFn(t)
	oldStore := swrNewDB(t, t.TempDir(), 4010, 1)
	newStore := swrNewDB(t, t.TempDir(), 5010, 2)

	_, freshAt, err := oldStore.ProtocolDirectory(true)
	if err != nil {
		t.Fatalf("prime snapshot: %v", err)
	}
	staleAt := swrAgeSnapshot(t, freshAt)

	failBuilds := 0
	protocolDirBuildFn = func(pd *PebbleData) ([]ProtocolDirEntry, error) {
		failBuilds++
		return nil, fmt.Errorf("simulated rebuild failure")
	}

	got, gotAt, err := newStore.ProtocolDirectory(false)
	if err != nil {
		t.Fatalf("SWR request must not surface background rebuild failure: err=%v", err)
	}
	if len(got) != 1 || got[0].PinTotal != 1 || !gotAt.Equal(staleAt) {
		t.Fatalf("failed rebuild must keep old snapshot: entries=%v builtAt=%v", got, gotAt)
	}
	deadline := time.Now().Add(2 * time.Second)
	for failBuilds == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if failBuilds != 1 {
		t.Fatalf("expected exactly one background rebuild attempt (trigger must be deduped), got %d", failBuilds)
	}
	protocolDirCache.Lock()
	keptAt := protocolDirCache.builtAt
	kept := protocolDirCache.entries
	protocolDirCache.Unlock()
	if len(kept) != 1 || kept[0].PinTotal != 1 || !keptAt.Equal(staleAt) {
		t.Fatalf("failed rebuild must leave cache intact: entries=%v builtAt=%v", kept, keptAt)
	}
}

// TestProtocolDirColdStartSyncFallback 覆盖验收要求的极端情形（允许慢，不允许死）：
// 无快照时同步兜底构建仍应答；构建失败如实上抛（API 侧维持 ErrServiceError 语义）。
func TestProtocolDirColdStartSyncFallback(t *testing.T) {
	swrRestoreBuildFn(t)
	store := swrNewDB(t, t.TempDir(), 6010, 1)

	entries, builtAt, err := store.ProtocolDirectory(false)
	if err != nil || len(entries) != 1 || builtAt.IsZero() {
		t.Fatalf("cold start must answer synchronously: entries=%v builtAt=%v err=%v", entries, builtAt, err)
	}
	if entries[0].PinTotal != 1 {
		t.Fatalf("cold-start snapshot wrong: %+v", entries[0])
	}

	// 无快照 + 构建失败：错误如实上抛
	swrResetCache(t)
	protocolDirBuildFn = func(pd *PebbleData) ([]ProtocolDirEntry, error) {
		return nil, fmt.Errorf("simulated build failure")
	}
	if _, _, err := store.ProtocolDirectory(false); err == nil {
		t.Fatalf("build failure must propagate when no snapshot exists")
	}
}

// TestProtocolDirProberRefreshesWithoutRequests 覆盖后台提前重建：
// 快照年龄达到提前刷新阈值后，探测循环自动触发重建并换新，不依赖任何用户请求。
func TestProtocolDirProberRefreshesWithoutRequests(t *testing.T) {
	swrRestoreBuildFn(t)
	origTick, origAhead := protocolDirProberTick, protocolDirRefreshAhead
	t.Cleanup(func() {
		protocolDirProberTick, protocolDirRefreshAhead = origTick, origAhead
	})
	protocolDirProberTick = 20 * time.Millisecond
	protocolDirRefreshAhead = 40 * time.Millisecond

	store := swrNewDB(t, t.TempDir(), 7010, 1)
	_, builtAt0, err := store.ProtocolDirectory(true) // 同步首建，顺带启动探测
	if err != nil {
		t.Fatalf("prime snapshot: %v", err)
	}
	landed := swrWaitRebuildLanded(t, builtAt0)
	if !landed.After(builtAt0) {
		t.Fatalf("prober did not rebuild ahead of expiry")
	}
	protocolDirCache.Lock()
	entries := protocolDirCache.entries
	protocolDirCache.Unlock()
	if len(entries) != 1 || entries[0].PinTotal != 1 {
		t.Fatalf("prober-rebuilt snapshot wrong: %+v", entries)
	}
}
