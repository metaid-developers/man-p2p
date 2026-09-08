package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"man-p2p/man"
	"man-p2p/pin"
)

type metaModePinDetail struct {
	Code int `json:"code"`
	Data struct {
		Id                  string `json:"id"`
		Path                string `json:"path"`
		Operation           string `json:"operation"`
		ContentType         string `json:"contentType"`
		ContentTypeDetect   string `json:"contentTypeDetect"`
		Timestamp           int64  `json:"timestamp"`
		SeenTime            int64  `json:"seenTime"`
		Address             string `json:"address"`
		ContentLength       uint64 `json:"contentLength"`
		ContentBody         []byte `json:"contentBody"`
		ContentSummary      string `json:"contentSummary"`
		OriginalContentBody []byte `json:"originalContentBody"`
	} `json:"data"`
}

func seedMetaModeTestPin(t *testing.T, pinID string, content []byte) {
	t.Helper()
	if err := man.PebbleStore.Database.SetAllPins(100, []*pin.PinInscription{{
		Id:                  pinID,
		GenesisTransaction:  "meta-mode-test-tx",
		Output:              "meta-mode-test-tx:0",
		ChainName:           "mvc",
		GenesisHeight:       100,
		Timestamp:           1710000000,
		SeenTime:            1710000001,
		Operation:           "upsert",
		Path:                "/files/meta-mode-test.bin",
		ContentType:         "application/octet-stream",
		ContentTypeDetect:   "image/png",
		Address:             "1MetaModeTestAddr",
		MetaId:              "meta-mode-test-metaid",
		ContentBody:         content,
		OriginalContentBody: content,
		ContentLength:       uint64(len(content)),
	}}, 1); err != nil {
		t.Fatalf("SetAllPins: %v", err)
	}
}

func TestGetPinByIdMetaModeStripsContentPayloads(t *testing.T) {
	r := setupAlphaContractRouter(t)

	content := []byte(strings.Repeat("x", 300*1024))
	seedMetaModeTestPin(t, "meta-mode-pin", content)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/pin/meta-mode-pin?meta=1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for meta mode, got %d with body %s", w.Code, w.Body.String())
	}
	if w.Body.Len() >= 2048 {
		t.Fatalf("expected meta mode response < 2 KB, got %d bytes", w.Body.Len())
	}
	var body metaModePinDetail
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != 1 {
		t.Fatalf("expected success code 1, got %d", body.Code)
	}
	data := body.Data
	if data.Id != "meta-mode-pin" || data.Path != "/files/meta-mode-test.bin" || data.Operation != "upsert" {
		t.Fatalf("metadata mismatch: %+v", data)
	}
	if data.ContentType != "application/octet-stream" || data.ContentTypeDetect != "image/png" {
		t.Fatalf("content type metadata mismatch: %+v", data)
	}
	if data.Timestamp != 1710000000 || data.SeenTime != 1710000001 || data.Address != "1MetaModeTestAddr" {
		t.Fatalf("timestamp/owner metadata mismatch: %+v", data)
	}
	if data.ContentLength != uint64(len(content)) {
		t.Fatalf("expected contentLength %d, got %d", len(content), data.ContentLength)
	}
	if data.ContentBody != nil || data.ContentSummary != "" || data.OriginalContentBody != nil {
		t.Fatalf("expected content payloads stripped, got body=%d summary=%d original=%d",
			len(data.ContentBody), len(data.ContentSummary), len(data.OriginalContentBody))
	}
}

func TestGetPinByIdDefaultModeKeepsFullContent(t *testing.T) {
	r := setupAlphaContractRouter(t)

	content := []byte(strings.Repeat("x", 300*1024))
	seedMetaModeTestPin(t, "meta-mode-pin", content)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/pin/meta-mode-pin", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for default mode, got %d with body %s", w.Code, w.Body.String())
	}
	if w.Body.Len() <= 2*len(content) {
		t.Fatalf("expected default mode to keep full payloads, got %d bytes", w.Body.Len())
	}
	var body metaModePinDetail
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != 1 {
		t.Fatalf("expected success code 1, got %d", body.Code)
	}
	data := body.Data
	if string(data.ContentBody) != string(content) {
		t.Fatalf("expected contentBody %d bytes, got %d", len(content), len(data.ContentBody))
	}
	if data.ContentSummary != string(content) {
		t.Fatalf("expected contentSummary to mirror contentBody, got %d bytes", len(data.ContentSummary))
	}
}
