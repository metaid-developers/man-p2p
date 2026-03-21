package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"man-p2p/api/respond"
	"man-p2p/p2p"
)

func TestAlphaP2PStatusFields(t *testing.T) {
	r := setupP2PTestRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/p2p/status", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var res respond.ApiResponse
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("expected valid json response: %v", err)
	}

	data, ok := res.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object data payload, got %#v", res.Data)
	}

	requiredFields := []string{
		"peerCount",
		"storageUsedBytes",
		"storageLimitReached",
		"syncMode",
		"runtimeMode",
		"peerId",
		"listenAddrs",
	}
	for _, field := range requiredFields {
		if _, ok := data[field]; !ok {
			t.Fatalf("expected field %q in status payload, got %#v", field, data)
		}
	}
}

func TestAlphaConfigReloadUpdatesRuntimeFilterState(t *testing.T) {
	configFile, err := os.CreateTemp("", "p2p-status-reload-*.json")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(configFile.Name())

	if _, err := configFile.WriteString(`{"p2p_sync_mode":"self"}`); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if err := configFile.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := p2p.LoadConfig(configFile.Name()); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	r := setupP2PTestRouter()

	if err := os.WriteFile(configFile.Name(), []byte(`{"p2p_sync_mode":"full","p2p_enable_chain_source":false}`), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	reloadResp := httptest.NewRecorder()
	reloadReq, _ := http.NewRequest(http.MethodPost, "/api/config/reload", nil)
	r.ServeHTTP(reloadResp, reloadReq)
	if reloadResp.Code != http.StatusOK {
		t.Fatalf("expected reload 200, got %d", reloadResp.Code)
	}

	statusResp := httptest.NewRecorder()
	statusReq, _ := http.NewRequest(http.MethodGet, "/api/p2p/status", nil)
	r.ServeHTTP(statusResp, statusReq)
	if statusResp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", statusResp.Code)
	}

	var res respond.ApiResponse
	if err := json.Unmarshal(statusResp.Body.Bytes(), &res); err != nil {
		t.Fatalf("expected valid json response: %v", err)
	}

	data, ok := res.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object data payload, got %#v", res.Data)
	}
	if got := data["syncMode"]; got != "full" {
		t.Fatalf("expected syncMode full after reload, got %#v", got)
	}
	if got := data["runtimeMode"]; got != "p2p-only" {
		t.Fatalf("expected runtimeMode p2p-only after reload, got %#v", got)
	}
}
