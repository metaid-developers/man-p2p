package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"man-p2p/common"
	"man-p2p/man"
	"man-p2p/pebblestore"
	"man-p2p/pin"

	"github.com/gin-gonic/gin"
)

func setupAlphaContractRouter(t *testing.T) *gin.Engine {
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
	r.GET("/api/pin/:numberOrId", getPinById)
	r.GET("/content/:number", content)
	return r
}

func TestAlphaPinMissReturnsNon2xx(t *testing.T) {
	r := setupAlphaContractRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/pin/missing-pin-id", nil)
	r.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Fatalf("expected non-2xx for missing local pin, got %d with body %s", w.Code, w.Body.String())
	}
}

func TestAlphaPinDetailReturnsMempoolPin(t *testing.T) {
	r := setupAlphaContractRouter(t)

	pendingPin := &pin.PinInscription{
		Id:            "pending-mempool-pin",
		Path:          "/protocols/simplebuzz",
		Address:       "1AlphaMempoolAddr",
		MetaId:        "alpha-mempool-metaid",
		ChainName:     "mvc",
		Timestamp:     1710000100,
		GenesisHeight: -1,
		ContentBody:   []byte(`{"content":"pending"}`),
		ContentLength: uint64(len(`{"content":"pending"}`)),
	}
	if err := man.PebbleStore.Database.SetMempool(pendingPin); err != nil {
		t.Fatalf("SetMempool: %v", err)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/pin/"+pendingPin.Id, nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected mempool pin detail to return 200, got %d with body %s", w.Code, w.Body.String())
	}
	var body struct {
		Code int `json:"code"`
		Data struct {
			Id             string `json:"id"`
			GenesisHeight  int64  `json:"genesisHeight"`
			ContentSummary string `json:"contentSummary"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != 1 {
		t.Fatalf("expected success code 1, got %d with body %s", body.Code, w.Body.String())
	}
	if body.Data.Id != pendingPin.Id {
		t.Fatalf("expected pin id %s, got %s", pendingPin.Id, body.Data.Id)
	}
	if body.Data.GenesisHeight != -1 {
		t.Fatalf("expected mempool GenesisHeight -1, got %d", body.Data.GenesisHeight)
	}
	if body.Data.ContentSummary != string(pendingPin.ContentBody) {
		t.Fatalf("expected ContentSummary %q, got %q", string(pendingPin.ContentBody), body.Data.ContentSummary)
	}
}

func TestAlphaContentMissReturnsNon2xx(t *testing.T) {
	r := setupAlphaContractRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/content/missing-pin-id", nil)
	r.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Fatalf("expected non-2xx for missing local content, got %d with body %q", w.Code, w.Body.String())
	}
}

func TestAlphaMetadataOnlyContentContract(t *testing.T) {
	r := setupAlphaContractRouter(t)

	if err := man.IngestP2PPin(&pin.PinInscription{
		Id:            "metadata-only-pin",
		Path:          "/files/test.txt",
		Address:       "1AlphaMetadataAddr",
		MetaId:        "alpha-metaid",
		ChainName:     "btc",
		Timestamp:     1710000000,
		GenesisHeight: 900000,
		ContentLength: 128,
	}); err != nil {
		t.Fatalf("IngestP2PPin: %v", err)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/content/metadata-only-pin", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected metadata-only content to return 200, got %d with body %q", w.Code, w.Body.String())
	}
	if w.Body.Len() != 0 {
		t.Fatalf("expected metadata-only content body to be empty, got %q", w.Body.String())
	}
	if got := w.Header().Get("X-Man-Content-Status"); got != "metadata-only" {
		t.Fatalf("expected metadata-only header, got %q", got)
	}
}
