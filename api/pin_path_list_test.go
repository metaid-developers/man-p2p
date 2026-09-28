package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"man-p2p/common"
	"man-p2p/man"
	"man-p2p/pebblestore"
	"man-p2p/pin"

	"github.com/gin-gonic/gin"
)

func setupPinPathListRouter(t *testing.T) *gin.Engine {
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
	r.GET("/api/pin/path/list", getAllPinByPath)
	return r
}

func ingestPathPin(t *testing.T, id string, path string, body []byte, height int64, timestamp int64) {
	t.Helper()
	pinNode := &pin.PinInscription{
		Id:            id,
		Path:          path,
		Address:       "1PinPathListAddr",
		MetaId:        "pin-path-list-metaid",
		ChainName:     "mvc",
		Timestamp:     timestamp,
		GenesisHeight: height,
	}
	if body != nil {
		pinNode.ContentBody = body
		pinNode.ContentLength = uint64(len(body))
	} else {
		pinNode.ContentLength = 2048
	}
	if err := man.IngestP2PPin(pinNode); err != nil {
		t.Fatalf("IngestP2PPin(%s): %v", id, err)
	}
}

func getPinPathList(t *testing.T, r *gin.Engine, query string) (ids []string, summaries []string, bodies [][]byte, contents []string, lengths []uint64, nextCursor string, sizeEcho int64) {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/pin/path/list"+query, nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d with body %s", w.Code, w.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			List []struct {
				Id             string `json:"id"`
				ContentSummary string `json:"contentSummary"`
				ContentBody    []byte `json:"contentBody"`
				Content        string `json:"content"`
				ContentLength  uint64 `json:"contentLength"`
			} `json:"list"`
			NextCursor string `json:"nextCursor"`
			Size       int64  `json:"size"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body %s)", err, w.Body.String())
	}
	if resp.Code != 1 {
		t.Fatalf("expected code 1, got %d with body %s", resp.Code, w.Body.String())
	}
	for _, row := range resp.Data.List {
		ids = append(ids, row.Id)
		summaries = append(summaries, row.ContentSummary)
		bodies = append(bodies, row.ContentBody)
		contents = append(contents, row.Content)
		lengths = append(lengths, row.ContentLength)
	}
	return ids, summaries, bodies, contents, lengths, resp.Data.NextCursor, resp.Data.Size
}

func TestNormalizePinListPageSize(t *testing.T) {
	cases := []struct {
		in, want int64
	}{
		{0, 20},
		{-5, 20},
		{1, 1},
		{100, 100},
		{101, 101},
		{200, 200},
		{201, 200},
		{500, 200},
	}
	for _, c := range cases {
		if got := normalizePinListPageSize(c.in); got != c.want {
			t.Fatalf("normalizePinListPageSize(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestPinPathListHonorsRequestedPageSize(t *testing.T) {
	r := setupPinPathListRouter(t)
	path := "/protocols/metatask/claim"
	for i := 0; i < 250; i++ {
		ingestPathPin(t, fmt.Sprintf("pin-%04d", i), path, []byte(fmt.Sprintf(`{"i":%d}`, i)), 191400+int64(i), 1710000000+int64(i))
	}

	// size=200 must be honored (was silently degraded to 20)
	ids, _, _, _, _, _, sizeEcho := getPinPathList(t, r, "?path="+path+"&size=200")
	if len(ids) != 200 {
		t.Fatalf("expected 200 rows for size=200, got %d", len(ids))
	}
	if sizeEcho != 200 {
		t.Fatalf("expected echoed size 200, got %d", sizeEcho)
	}

	// size=500 clamps to documented cap 200 (not reset to 20)
	ids, _, _, _, _, _, sizeEcho = getPinPathList(t, r, "?path="+path+"&size=500")
	if len(ids) != 200 {
		t.Fatalf("expected 200 rows for size=500 (clamped), got %d", len(ids))
	}
	if sizeEcho != 200 {
		t.Fatalf("expected echoed size 200 for clamped request, got %d", sizeEcho)
	}
}

func TestPinPathListSinceDeltaCursor(t *testing.T) {
	r := setupPinPathListRouter(t)
	path := "/protocols/metatask/verify"
	var wantIds []string
	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("delta-%04d", i)
		wantIds = append(wantIds, id)
		ingestPathPin(t, id, path, []byte(fmt.Sprintf(`{"v":%d}`, i)), 191400+int64(i), 1710000000+int64(i))
	}

	// First delta page: oldest-first from the beginning of the path
	ids, _, _, _, _, nextCursor, _ := getPinPathList(t, r, "?path="+path+"&size=3&since=0")
	if len(ids) != 3 || ids[0] != wantIds[0] || ids[1] != wantIds[1] || ids[2] != wantIds[2] {
		t.Fatalf("expected first ascending page %v, got %v", wantIds[:3], ids)
	}
	if nextCursor == "" {
		t.Fatalf("expected non-empty nextCursor after first ascending page")
	}

	// Second delta page continues after the watermark, oldest-first
	// （游标含 & 分隔符，必须作为查询参数值 URL 编码后传递）
	ids, _, _, _, _, nextCursor2, _ := getPinPathList(t, r, "?path="+path+"&size=3&since="+url.QueryEscape(nextCursor))
	_ = nextCursor2
	if len(ids) != 3 || ids[0] != wantIds[3] || ids[1] != wantIds[4] || ids[2] != wantIds[5] {
		t.Fatalf("expected second ascending page %v, got %v", wantIds[3:], ids)
	}

	// Polling again with the new watermark returns nothing new
	ids, _, _, _, _, finalCursor, _ := getPinPathList(t, r, "?path="+path+"&size=3&since="+url.QueryEscape(nextCursor2))
	if len(ids) != 0 {
		t.Fatalf("expected empty delta after catching up, got %d rows", len(ids))
	}
	if finalCursor != "" {
		t.Fatalf("expected empty nextCursor when caught up, got %q", finalCursor)
	}
}

func TestPinPathListContentContract(t *testing.T) {
	r := setupPinPathListRouter(t)
	path := "/protocols/metatask/submission"

	small := []byte(`{"taskid":"t1","status":"submitted"}`)
	ingestPathPin(t, "content-small", path, small, 191500, 1710000100)
	ingestPathPin(t, "content-meta-only", path, nil, 191501, 1710000101)
	big := []byte(strings.Repeat("x", 6000))
	ingestPathPin(t, "content-big", path, big, 191502, 1710000102)

	ids, summaries, bodies, contents, lengths, _, _ := getPinPathList(t, r, "?path="+path+"&size=10")
	if len(ids) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(ids))
	}
	byID := make(map[string]int, len(ids))
	for i, id := range ids {
		byID[id] = i
	}

	// Small body: contentSummary carries the full body, contentBody empty, stable download URL
	i := byID["content-small"]
	if summaries[i] != string(small) {
		t.Fatalf("expected full contentSummary for small pin, got %q", summaries[i])
	}
	if len(bodies[i]) != 0 {
		t.Fatalf("expected empty contentBody in list rows, got %d bytes", len(bodies[i]))
	}
	if contents[i] != "/content/content-small" {
		t.Fatalf("expected content download URL /content/content-small, got %q", contents[i])
	}

	// Metadata-only pin: contentSummary empty but download URL and contentLength still present
	i = byID["content-meta-only"]
	if summaries[i] != "" {
		t.Fatalf("expected empty contentSummary for metadata-only pin, got %q", summaries[i])
	}
	if contents[i] != "/content/content-meta-only" {
		t.Fatalf("expected content download URL for metadata-only pin, got %q", contents[i])
	}
	if lengths[i] != 2048 {
		t.Fatalf("expected contentLength 2048 for metadata-only pin, got %d", lengths[i])
	}

	// Oversized body: contentSummary truncated at documented limit
	i = byID["content-big"]
	if len(summaries[i]) != 4096 {
		t.Fatalf("expected contentSummary truncated to 4096 bytes, got %d", len(summaries[i]))
	}
	if summaries[i] != string(big[:4096]) {
		t.Fatalf("expected contentSummary to carry the first 4096 bytes of the body")
	}
	if lengths[i] != 6000 {
		t.Fatalf("expected contentLength 6000, got %d", lengths[i])
	}
}
