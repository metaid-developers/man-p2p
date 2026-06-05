package api

import (
	"archive/zip"
	"bytes"
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

func TestExportEndpoint(t *testing.T) {
	db, err := pebblestore.NewDataBase(t.TempDir(), 2)
	if err != nil {
		t.Fatalf("NewDataBase error: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	orig := man.PebbleStore
	man.PebbleStore = &man.PebbleData{Database: db}
	defer func() { man.PebbleStore = orig }()

	address := "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa"
	metaId := common.GetMetaIdByAddress(address)

	p := &pin.PinInscription{
		Id:            "test-pin-1",
		MetaId:        metaId,
		Path:          "/info/name",
		ChainName:     "btc",
		GenesisHeight: 800000,
		Timestamp:     1700000000,
		ContentType:   "text/plain",
		Content:       "hello world",
	}

	if err := db.BatchInsertPins([]pin.PinInscription{*p}); err != nil {
		t.Fatalf("BatchInsertPins error: %v", err)
	}
	pathMetaId := common.GetMetaIdByAddress(p.Path)
	timePart := pin.GetPublicKeyStr(p.Timestamp, p.ChainName, p.GenesisHeight)
	addrKey := p.MetaId + "&" + pathMetaId + "&" + timePart + "&" + p.Id
	if err := db.BatchSetAddressData(&[]string{addrKey}); err != nil {
		t.Fatalf("BatchSetAddressData error: %v", err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterExportRoutes(r)

	body := map[string]interface{}{
		"identity":      address,
		"identity_type": "address",
		"start_height":  799999,
		"end_height":    800001,
	}
	bodyBytes, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/export/user-data", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/zip" {
		t.Fatalf("expected Content-Type application/zip, got %q", ct)
	}

	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader error: %v", err)
	}

	var exportFile *zip.File
	for _, f := range zr.File {
		if f.Name == "export.json" {
			exportFile = f
			break
		}
	}
	if exportFile == nil {
		t.Fatal("missing export.json in zip archive")
	}

	rc, err := exportFile.Open()
	if err != nil {
		t.Fatalf("cannot open export.json: %v", err)
	}
	defer rc.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(rc).Decode(&result); err != nil {
		t.Fatalf("decode export.json error: %v", err)
	}

	totalPins, ok := result["totalPins"].(float64)
	if !ok {
		t.Fatalf("totalPins not found or not a number: %#v", result)
	}
	if int(totalPins) != 1 {
		t.Fatalf("expected TotalPins=1, got %v", totalPins)
	}
}
