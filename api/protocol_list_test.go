package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"man-p2p/common"
	"man-p2p/man"
	"man-p2p/pebblestore"
	"man-p2p/pin"

	"github.com/gin-gonic/gin"
)

func setupProtocolListRouter(t *testing.T) *gin.Engine {
	t.Helper()

	origStore := man.PebbleStore
	origConfig := common.Config
	db, err := pebblestore.NewDataBase(t.TempDir(), 4)
	if err != nil {
		t.Fatalf("NewDataBase: %v", err)
	}
	man.PebbleStore = &man.PebbleData{Database: db}
	common.Config = &common.AllConfig{}
	t.Cleanup(func() {
		_ = db.Close()
		man.PebbleStore = origStore
		common.Config = origConfig
	})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/protocol/list", protocolList)
	return r
}

func ingestProtocolListPin(t *testing.T, id, path string, height, timestamp int64, address, globalMetaId string) {
	t.Helper()
	p := &pin.PinInscription{
		Id:            id,
		Path:          path,
		Address:       address,
		GlobalMetaId:  globalMetaId,
		ChainName:     "mvc",
		Timestamp:     timestamp,
		GenesisHeight: height,
	}
	if err := man.IngestP2PPin(p); err != nil {
		t.Fatalf("IngestP2PPin(%s): %v", id, err)
	}
}

type protocolListRow struct {
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

func getProtocolList(t *testing.T, r *gin.Engine, query string) (rows []protocolListRow, count int64, code int) {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/protocol/list"+query, nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d with body %s", w.Code, w.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			List  []protocolListRow `json:"list"`
			Count int64             `json:"count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body %s)", err, w.Body.String())
	}
	return resp.Data.List, resp.Data.Count, resp.Code
}

func TestProtocolListDirectory(t *testing.T) {
	r := setupProtocolListRouter(t)

	ingestProtocolListPin(t, "a1e0f0000000000000000000000000000000000000000000000000000000000i0", "/protocols/simplequestion", 188591, 1788777818, "1GrqX7K9jdnUor8hAoAfDx99uFH2tT75Za", "idq14hmv23j5fnlx4ccnmvlyldjd38xjsechzwg9xz")
	ingestProtocolListPin(t, "a2e0f0000000000000000000000000000000000000000000000000000000000i0", "/protocols/simplequestion", 188592, 1788777819, "1GrqX7K9jdnUor8hAoAfDx99uFH2tT75Za", "idq14hmv23j5fnlx4ccnmvlyldjd38xjsechzwg9xz")
	ingestProtocolListPin(t, "b1e0f0000000000000000000000000000000000000000000000000000000000i0", "/protocols/preference/round", 194034, 1792029683, "1GrqX7K9jdnUor8hAoAfDx99uFH2tT75Za", "idq14hmv23j5fnlx4ccnmvlyldjd38xjsechzwg9xz")

	rows, count, code := getProtocolList(t, r, "?page=1&size=20&refresh=1")
	if code != 1 {
		t.Fatalf("expected code 1, got %d", code)
	}
	if count != 2 {
		t.Fatalf("expected count 2 families, got %d (%+v)", count, rows)
	}
	byPath := map[string]protocolListRow{}
	for _, row := range rows {
		byPath[row.Path] = row
	}
	sq := byPath["/protocols/simplequestion"]
	if sq.PinTotal != 2 || sq.FirstHeight != 188591 || sq.FirstTimestamp != 1788777818 {
		t.Fatalf("simplequestion row = %+v", sq)
	}
	if sq.FirstTxid != "a1e0f0000000000000000000000000000000000000000000000000000000000" {
		t.Fatalf("simplequestion firstTxid = %s", sq.FirstTxid)
	}
	if sq.FirstAddress != "1GrqX7K9jdnUor8hAoAfDx99uFH2tT75Za" || sq.FirstGlobalMetaId != "idq14hmv23j5fnlx4ccnmvlyldjd38xjsechzwg9xz" {
		t.Fatalf("simplequestion firstAuthor = %s/%s", sq.FirstAddress, sq.FirstGlobalMetaId)
	}
	if sq.Registered || sq.RegisteredPinId != "" {
		t.Fatalf("simplequestion should be unregistered: %+v", sq)
	}
	pref := byPath["/protocols/preference"]
	if pref.PinTotal != 1 {
		t.Fatalf("preference pinTotal = %d, want 1 (aggregated from /round)", pref.PinTotal)
	}
	if pref.FirstChain != "mvc" {
		t.Fatalf("preference firstChain = %s", pref.FirstChain)
	}
}

func TestProtocolListPagination(t *testing.T) {
	r := setupProtocolListRouter(t)
	for i := 0; i < 5; i++ {
		ingestProtocolListPin(t, fmt.Sprintf("%064xi0", i), fmt.Sprintf("/protocols/page%d", i), 190000+int64(i), 1793000000+int64(i), "1AxUdSkVdDyDreYSYVoDRFeyS1pvQdvcJx", "idq1d5m392ahkhp79wsy9ur79e3vhak7tg729dwdr5")
	}

	// page=1 size=2：默认按 path 升序
	rows, count, _ := getProtocolList(t, r, "?page=1&size=2&refresh=1")
	if count != 5 || len(rows) != 2 || rows[0].Path != "/protocols/page0" || rows[1].Path != "/protocols/page1" {
		t.Fatalf("page1: count=%d rows=%+v", count, rows)
	}
	// 末页
	rows, count, _ = getProtocolList(t, r, "?page=3&size=2&refresh=1")
	if count != 5 || len(rows) != 1 || rows[0].Path != "/protocols/page4" {
		t.Fatalf("page3: count=%d rows=%+v", count, rows)
	}
	// 超出范围的页：空列表但 count 不变
	rows, count, _ = getProtocolList(t, r, "?page=4&size=2&refresh=1")
	if count != 5 || len(rows) != 0 {
		t.Fatalf("page4 (out of range): count=%d rows=%d", count, len(rows))
	}
}

func TestProtocolListEmptyDatabase(t *testing.T) {
	r := setupProtocolListRouter(t)
	rows, count, code := getProtocolList(t, r, "?page=1&size=20&refresh=1")
	if code != 1 || count != 0 || len(rows) != 0 {
		t.Fatalf("empty db: code=%d count=%d rows=%d", code, count, len(rows))
	}
}

func TestProtocolListRouteRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	btcJsonApi(r)
	found := false
	for _, ri := range r.Routes() {
		if ri.Method == http.MethodGet && ri.Path == "/api/protocol/list" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("GET /api/protocol/list not registered by btcJsonApi")
	}
}

func TestProtocolListParameterErrors(t *testing.T) {
	r := setupProtocolListRouter(t)

	// 缺 page / 非法 page：参数错误（遵循 /api/pin/list 惯例）
	for _, q := range []string{"?size=20", "?page=0&size=20", "?page=-1&size=20", "?page=abc&size=20"} {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/protocol/list"+q, nil)
		r.ServeHTTP(w, req)
		var resp struct {
			Code int `json:"code"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Code != 404 {
			t.Fatalf("query %q: expected error code 404, got %d (body %s)", q, resp.Code, w.Body.String())
		}
	}

	ingestProtocolListPin(t, "a1e0f0000000000000000000000000000000000000000000000000000000000i0", "/protocols/sortbyproto", 188591, 1788777818, "1GrqX7K9jdnUor8hAoAfDx99uFH2tT75Za", "idq14hmv23j5fnlx4ccnmvlyldjd38xjsechzwg9xz")

	// sortBy=pinTotal&order=asc 与未知 sortBy 回退 path 均可响应
	rows, _, _ := getProtocolList(t, r, "?page=1&size=20&refresh=1&sortBy=pinTotal&order=asc")
	if len(rows) != 1 {
		t.Fatalf("sortBy=pinTotal rows=%d", len(rows))
	}
	rows, _, _ = getProtocolList(t, r, "?page=1&size=20&refresh=1&sortBy=unknown")
	if len(rows) != 1 {
		t.Fatalf("sortBy=unknown rows=%d", len(rows))
	}
}
