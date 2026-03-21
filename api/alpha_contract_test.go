package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"man-p2p/man"
	"man-p2p/pebblestore"
	"man-p2p/pin"

	"github.com/gin-gonic/gin"
)

func setupAlphaContractRouter(t *testing.T) *gin.Engine {
	t.Helper()

	origStore := man.PebbleStore
	db, err := pebblestore.NewDataBase(t.TempDir(), 4)
	if err != nil {
		t.Fatalf("NewDataBase: %v", err)
	}
	man.PebbleStore = &man.PebbleData{Database: db}
	t.Cleanup(func() {
		_ = db.Close()
		man.PebbleStore = origStore
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
