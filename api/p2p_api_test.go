package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupP2PTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterP2PRoutes(r)
	return r
}

func TestP2PStatusEndpoint(t *testing.T) {
	r := setupP2PTestRouter()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/p2p/status", nil)
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if body := w.Body.String(); len(body) == 0 {
		t.Error("expected non-empty body")
	}
	if body := w.Body.String(); !contains(body, "peerCount") {
		t.Errorf("expected peerCount in response, got: %s", body)
	}
}

func TestP2PPeersEndpoint(t *testing.T) {
	r := setupP2PTestRouter()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/p2p/peers", nil)
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestConfigReloadEndpoint(t *testing.T) {
	r := setupP2PTestRouter()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/config/reload", nil)
	r.ServeHTTP(w, req)
	// May return 200 or 500 depending on config state; assert not 404
	if w.Code == 404 {
		t.Errorf("expected non-404, got %d", w.Code)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
