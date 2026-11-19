package api

import (
	"encoding/json"
	"man-p2p/common"
	"net/http"
	"net/http/httptest"
	"testing"

	"man-p2p/api/respond"

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
	var res respond.ApiResponse
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("expected valid json response: %v", err)
	}
	if res.Code != 1 {
		t.Fatalf("expected success envelope code 1, got %d", res.Code)
	}
	data, ok := res.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object data payload, got %#v", res.Data)
	}
	if _, ok := data["peerCount"]; !ok {
		t.Fatalf("expected peerCount in response data, got %#v", data)
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
	orig := common.Config
	common.Config = &common.AllConfig{AdminToken: "test-admin-token"}
	t.Cleanup(func() { common.Config = orig })

	r := setupP2PTestRouter()

	// R0 安全修复：匿名与错误 token 必须被拒
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/config/reload", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous reload must be rejected with 401, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/config/reload?token=wrong", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token must be rejected with 401, got %d", w.Code)
	}

	// 正确 token（query 兼容形式）放行
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/config/reload?token=test-admin-token", nil)
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var res respond.ApiResponse
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("expected valid json response: %v", err)
	}
	if res.Code != 1 {
		t.Fatalf("expected success envelope code 1, got %d", res.Code)
	}
}
