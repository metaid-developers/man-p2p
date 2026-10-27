package man

import (
	"fmt"
	"testing"

	"man-p2p/pebblestore"
	"man-p2p/pin"
)

func setupProtocolDirStore(t *testing.T) *PebbleData {
	t.Helper()
	origStore := PebbleStore
	db, err := pebblestore.NewDataBase(t.TempDir(), 2)
	if err != nil {
		t.Fatalf("NewDataBase: %v", err)
	}
	PebbleStore = &PebbleData{Database: db}
	t.Cleanup(func() {
		_ = db.Close()
		PebbleStore = origStore
	})
	return PebbleStore
}

func ingestProtocolPin(t *testing.T, id, path string, height, timestamp int64, address, globalMetaId string, status int, body []byte) {
	t.Helper()
	p := &pin.PinInscription{
		Id:            id,
		Path:          path,
		Address:       address,
		GlobalMetaId:  globalMetaId,
		ChainName:     "mvc",
		Timestamp:     timestamp,
		GenesisHeight: height,
		Status:        status,
	}
	if body != nil {
		p.ContentBody = body
		p.ContentLength = uint64(len(body))
	}
	if err := IngestP2PPin(p); err != nil {
		t.Fatalf("IngestP2PPin(%s): %v", id, err)
	}
}

func TestProtocolFamily(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"/protocols/simplequestion", "/protocols/simplequestion", true},
		{"/protocols/preference/round", "/protocols/preference", true},
		{"/protocols/a/b/c/d", "/protocols/a", true},
		{"/protocols", "", false},
		{"/protocols/", "", false},
		{"/protocols//x", "", false},
		{"/file/x", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := protocolFamily(c.in)
		if got != c.want || ok != c.ok {
			t.Fatalf("protocolFamily(%q) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestBuildProtocolDirectory(t *testing.T) {
	pd := setupProtocolDirStore(t)

	ingestProtocolPin(t, "a2e0f0000000000000000000000000000000000000000000000000000000000i0", "/protocols/simplequestion", 188592, 1788777819, "1GrqX7K9jdnUor8hAoAfDx99uFH2tT75Za", "idq14hmv23j5fnlx4ccnmvlyldjd38xjsechzwg9xz", 0, nil)
	ingestProtocolPin(t, "a1e0f0000000000000000000000000000000000000000000000000000000000i0", "/protocols/simplequestion", 188591, 1788777818, "1GrqX7K9jdnUor8hAoAfDx99uFH2tT75Za", "idq14hmv23j5fnlx4ccnmvlyldjd38xjsechzwg9xz", 0, nil)
	// 子路径归入族：/protocols/preference 本体 0 pin，round 2 pin
	ingestProtocolPin(t, "b1e0f0000000000000000000000000000000000000000000000000000000000i0", "/protocols/preference/round", 194034, 1792029683, "1GrqX7K9jdnUor8hAoAfDx99uFH2tT75Za", "idq14hmv23j5fnlx4ccnmvlyldjd38xjsechzwg9xz", 0, nil)
	ingestProtocolPin(t, "b2e0f0000000000000000000000000000000000000000000000000000000000i0", "/protocols/preference/round", 194035, 1792029684, "1GrqX7K9jdnUor8hAoAfDx99uFH2tT75Za", "idq14hmv23j5fnlx4ccnmvlyldjd38xjsechzwg9xz", 0, nil)
	// mempool 不计入；已撤销(status=-1)的 pin 仍是链上事实，照常计入
	ingestProtocolPin(t, "c1e0f0000000000000000000000000000000000000000000000000000000000i0", "/protocols/mempoolproto", -1, 1799999999, "1MFi1WM2NXnV3kjdLKaUw7Ad23LSvSD9fY", "idq1mc4fynwqdluw8nfe7ylmnd6280uxuzzj5n6q9z", 0, nil)
	ingestProtocolPin(t, "c2e0f0000000000000000000000000000000000000000000000000000000000i0", "/protocols/revokedproto", 190000, 1791111111, "1MFi1WM2NXnV3kjdLKaUw7Ad23LSvSD9fY", "idq1mc4fynwqdluw8nfe7ylmnd6280uxuzzj5n6q9z", -1, nil)
	// 非协议路径不计入
	ingestProtocolPin(t, "d1e0f0000000000000000000000000000000000000000000000000000000000i0", "/file/somefile", 188600, 1788788555, "1QBUGmScwRQFW1c4YwdWuB9f8pPZ4gohZb", "idq1lezaz60m9u62mnw0je3tjr4llft4tlgehq74rn", 0, nil)
	// 注册 pin：注册 /protocols/preference 族（深路径注册折叠到族）
	regBody := []byte(`{"title":"Preference","path":"/protocols/preference/round","version":"1.0.0","protocolName":"preference"}`)
	ingestProtocolPin(t, "e1e0f0000000000000000000000000000000000000000000000000000000000i0", "/protocols/metaprotocol", 194858, 1792637057, "1QBUGmScwRQFW1c4YwdWuB9f8pPZ4gohZb", "idq1lezaz60m9u62mnw0je3tjr4llft4tlgehq74rn", 0, regBody)

	entries, err := pd.BuildProtocolDirectory()
	if err != nil {
		t.Fatalf("BuildProtocolDirectory: %v", err)
	}
	byPath := make(map[string]ProtocolDirEntry, len(entries))
	for _, e := range entries {
		byPath[e.Path] = e
	}
	if len(entries) != 4 { // simplequestion, preference, metaprotocol, revokedproto（mempoolproto 未上链不计）
		t.Fatalf("expected 4 families, got %d: %+v", len(entries), entries)
	}
	revoked := byPath["/protocols/revokedproto"]
	if revoked.PinTotal != 1 || revoked.FirstTimestamp != 1791111111 {
		t.Fatalf("revokedproto entry = %+v, want total 1 (revoked pin still counts)", revoked)
	}

	sq := byPath["/protocols/simplequestion"]
	if sq.PinTotal != 2 {
		t.Fatalf("simplequestion pinTotal = %d, want 2", sq.PinTotal)
	}
	// 首现取 timestamp 最小的 a1 pin
	if sq.FirstPinId != "a1e0f0000000000000000000000000000000000000000000000000000000000i0" {
		t.Fatalf("simplequestion firstPinId = %s", sq.FirstPinId)
	}
	if sq.FirstTxid != "a1e0f0000000000000000000000000000000000000000000000000000000000" {
		t.Fatalf("simplequestion firstTxid = %s", sq.FirstTxid)
	}
	if sq.FirstHeight != 188591 || sq.FirstTimestamp != 1788777818 {
		t.Fatalf("simplequestion first height/ts = %d/%d", sq.FirstHeight, sq.FirstTimestamp)
	}
	if sq.FirstAddress != "1GrqX7K9jdnUor8hAoAfDx99uFH2tT75Za" || sq.FirstGlobalMetaId != "idq14hmv23j5fnlx4ccnmvlyldjd38xjsechzwg9xz" {
		t.Fatalf("simplequestion firstAuthor = %s/%s", sq.FirstAddress, sq.FirstGlobalMetaId)
	}
	if sq.Registered {
		t.Fatalf("simplequestion should not be registered")
	}

	pref := byPath["/protocols/preference"]
	if pref.PinTotal != 2 {
		t.Fatalf("preference pinTotal = %d, want 2 (family-aggregated from /round)", pref.PinTotal)
	}
	if !pref.Registered || pref.RegisteredPinId != "e1e0f0000000000000000000000000000000000000000000000000000000000i0" {
		t.Fatalf("preference registered = %v/%s, want true/e1e0...", pref.Registered, pref.RegisteredPinId)
	}

	// metaprotocol 自身是一个族，但注册表没有 pin 声明 /protocols/metaprotocol
	meta := byPath["/protocols/metaprotocol"]
	if meta.PinTotal != 1 || meta.Registered {
		t.Fatalf("metaprotocol entry = %+v", meta)
	}
}

func TestBuildProtocolDirectoryEmpty(t *testing.T) {
	pd := setupProtocolDirStore(t)
	entries, err := pd.BuildProtocolDirectory()
	if err != nil {
		t.Fatalf("BuildProtocolDirectory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries on empty store, got %d", len(entries))
	}
}

func TestProtocolDirectoryCache(t *testing.T) {
	swrResetCache(t)
	pd := setupProtocolDirStore(t)
	ingestProtocolPin(t, fmt.Sprintf("f%063di0", 1), "/protocols/cacheproto", 191000, 1793000000, "1AxUdSkVdDyDreYSYVoDRFeyS1pvQdvcJx", "idq1d5m392ahkhp79wsy9ur79e3vhak7tg729dwdr5", 0, nil)

	first, builtAt1, err := pd.ProtocolDirectory(true)
	if err != nil || len(first) != 1 {
		t.Fatalf("forced build: %v, entries=%d", err, len(first))
	}
	// 缓存命中：新增 pin 后不强制重建，返回同一快照
	ingestProtocolPin(t, fmt.Sprintf("f%063di0", 2), "/protocols/cacheproto", 191001, 1793000001, "1AxUdSkVdDyDreYSYVoDRFeyS1pvQdvcJx", "idq1d5m392ahkhp79wsy9ur79e3vhak7tg729dwdr5", 0, nil)
	cached, builtAt2, err := pd.ProtocolDirectory(false)
	if err != nil || len(cached) != 1 || !builtAt2.Equal(builtAt1) {
		t.Fatalf("cached snapshot should be unchanged: n=%d err=%v", len(cached), err)
	}
	if cached[0].PinTotal != 1 {
		t.Fatalf("cached pinTotal = %d, want 1", cached[0].PinTotal)
	}
	// refresh=1（force）不再同步重建：立即返回旧快照，后台重建随后换新
	// （旧行为把 46–60s 全量扫描钳在请求路径上，生产曾撞 nginx 60s 超时 504）
	_, builtAt3, err := pd.ProtocolDirectory(true)
	if err != nil || !builtAt3.Equal(builtAt1) {
		t.Fatalf("refresh=1 must serve the cached snapshot unchanged: builtAt=%v want=%v err=%v", builtAt3, builtAt1, err)
	}
	swrWaitRebuildLanded(t, builtAt3)
	refetched, _, err := pd.ProtocolDirectory(false)
	if err != nil || len(refetched) != 1 || refetched[0].PinTotal != 2 {
		t.Fatalf("background rebuild should see the new pin: n=%d total=%d err=%v",
			len(refetched), func() int64 {
				if len(refetched) == 0 {
					return -1
				}
				return refetched[0].PinTotal
			}(), err)
	}
}
