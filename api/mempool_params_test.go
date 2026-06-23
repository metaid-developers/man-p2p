package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupMempoolParamRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/mempool/:page", mempool)
	btcJsonApi(r)
	return r
}

func assertParameterErrorResponse(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()

	var response struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("expected JSON error response, got decode error: %v", err)
	}
	if response.Code != 404 {
		t.Fatalf("expected parameter error code 404, got %d", response.Code)
	}
	if response.Message != "Parameter error." {
		t.Fatalf("expected parameter error message %q, got %q", "Parameter error.", response.Message)
	}
}

func TestMempoolHTMLRejectsPageZero(t *testing.T) {
	r := setupMempoolParamRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/mempool/0", nil)

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 for legacy HTML handler, got %d", w.Code)
	}
	if body := w.Body.String(); body != "fail" {
		t.Fatalf("expected fail body for page 0, got %q", body)
	}
}

func TestMempoolJSONRejectsPageZero(t *testing.T) {
	r := setupMempoolParamRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/mempool/list?page=0&size=100", nil)

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 response envelope, got %d", w.Code)
	}
	assertParameterErrorResponse(t, w)
}

func TestMempoolJSONCapsOversizedPageSize(t *testing.T) {
	size := normalizeMempoolPageSize(9999)
	if size != maxMempoolPageSize {
		t.Fatalf("expected oversized page size capped to %d, got %d", maxMempoolPageSize, size)
	}
}

func TestMempoolJSONRejectsDeepPage(t *testing.T) {
	r := setupMempoolParamRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/mempool/list?page=1000000&size=100", nil)

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 response envelope, got %d", w.Code)
	}
	assertParameterErrorResponse(t, w)
}
