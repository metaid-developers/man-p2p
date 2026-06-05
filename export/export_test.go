package export

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"man-p2p/common"
	"man-p2p/pebblestore"
	"man-p2p/pin"
)

func setupTestDB(t *testing.T) *pebblestore.Database {
	t.Helper()
	db, err := pebblestore.NewDataBase(t.TempDir(), 2)
	if err != nil {
		t.Fatalf("NewDataBase error: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func insertTestPin(t *testing.T, db *pebblestore.Database, p *pin.PinInscription) {
	t.Helper()
	if err := db.BatchInsertPins([]pin.PinInscription{*p}); err != nil {
		t.Fatalf("BatchInsertPins error: %v", err)
	}
	pathMetaId := common.GetMetaIdByAddress(p.Path)
	timePart := pin.GetPublicKeyStr(p.Timestamp, p.ChainName, p.GenesisHeight)
	addrKey := p.MetaId + "&" + pathMetaId + "&" + timePart + "&" + p.Id
	if err := db.BatchSetAddressData(&[]string{addrKey}); err != nil {
		t.Fatalf("BatchSetAddressData error: %v", err)
	}
}

func TestQueryUserPinsByAddress(t *testing.T) {
	db := setupTestDB(t)

	address := "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa"
	metaId := common.GetMetaIdByAddress(address)

	pins := []*pin.PinInscription{
		{
			Id:            "pin-1",
			MetaId:        metaId,
			Path:          "/info/name",
			ChainName:     "btc",
			GenesisHeight: 800000,
			Timestamp:     1700000000,
			ContentType:   "text/plain",
			Content:       "hello",
		},
		{
			Id:            "pin-2",
			MetaId:        metaId,
			Path:          "/info/name",
			ChainName:     "btc",
			GenesisHeight: 800001,
			Timestamp:     1700000001,
			ContentType:   "text/plain",
			Content:       "world",
		},
		{
			Id:            "pin-3",
			MetaId:        metaId,
			Path:          "/info/name",
			ChainName:     "btc",
			GenesisHeight: 800002,
			Timestamp:     1700000002,
			ContentType:   "text/plain",
			Content:       "foo",
		},
	}

	for _, p := range pins {
		insertTestPin(t, db, p)
	}

	result, err := QueryUserPins(db, address, "address", 800001, 800002)
	if err != nil {
		t.Fatalf("QueryUserPins error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	seen := make(map[string]bool)
	for _, r := range result {
		seen[r.Id] = true
	}
	if !seen["pin-2"] {
		t.Errorf("expected pin-2 in results")
	}
	if !seen["pin-3"] {
		t.Errorf("expected pin-3 in results")
	}
}

func TestQueryUserPinsByGlobalMetaId(t *testing.T) {
	db := setupTestDB(t)

	globalMetaId := "global-meta-123"

	addrBtc := "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa"
	addrDoge := "DBTe2a2MQ4CJh5hGjdB9ZLYcgKjyqWWT7b"
	metaIdBtc := common.GetMetaIdByAddress(addrBtc)
	metaIdDoge := common.GetMetaIdByAddress(addrDoge)

	metaIdInfoData := map[string]*pin.MetaIdInfo{
		metaIdBtc: {
			MetaId:       metaIdBtc,
			ChainName:    "btc",
			GlobalMetaId: globalMetaId,
			Address:      addrBtc,
		},
		metaIdDoge: {
			MetaId:       metaIdDoge,
			ChainName:    "doge",
			GlobalMetaId: globalMetaId,
			Address:      addrDoge,
		},
	}
	if err := db.BatchSetMetaidInfo(&metaIdInfoData); err != nil {
		t.Fatalf("BatchSetMetaidInfo error: %v", err)
	}

	pinBtc := &pin.PinInscription{
		Id:            "pin-btc-1",
		MetaId:        metaIdBtc,
		Path:          "/info/name",
		ChainName:     "btc",
		GenesisHeight: 800000,
		Timestamp:     1700000000,
		ContentType:   "text/plain",
		Content:       "hello btc",
	}
	pinDoge := &pin.PinInscription{
		Id:            "pin-doge-1",
		MetaId:        metaIdDoge,
		Path:          "/info/name",
		ChainName:     "doge",
		GenesisHeight: 800001,
		Timestamp:     1700000001,
		ContentType:   "text/plain",
		Content:       "hello doge",
	}

	insertTestPin(t, db, pinBtc)
	insertTestPin(t, db, pinDoge)

	result, err := QueryUserPins(db, globalMetaId, "global_meta_id", 0, 999999999)
	if err != nil {
		t.Fatalf("QueryUserPins error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
}

func TestArchiveStructure(t *testing.T) {
	pins := []*pin.PinInscription{
		{
			Id:            "pin-a",
			MetaId:        "meta-a",
			Path:          "/info/name",
			ChainName:     "btc",
			GenesisHeight: 800000,
			Timestamp:     1700000000,
			ContentType:   "text/plain",
			Content:       "alice",
		},
		{
			Id:            "pin-b",
			MetaId:        "meta-b",
			Path:          "/protocols/test",
			ChainName:     "btc",
			GenesisHeight: 800001,
			Timestamp:     1700000001,
			ContentType:   "text/plain",
			Content:       "bob",
		},
	}

	req := &ExportRequest{
		Identity:     "test-identity",
		IdentityType: "address",
		StartHeight:  0,
		EndHeight:    999999999,
	}

	var buf bytes.Buffer
	if err := WriteArchive(&buf, pins, req); err != nil {
		t.Fatalf("WriteArchive error: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader error: %v", err)
	}

	fileNames := make(map[string]*zip.File)
	for _, f := range zr.File {
		fileNames[f.Name] = f
	}

	required := []string{"export.json", "timeline.json"}
	for _, name := range required {
		if _, ok := fileNames[name]; !ok {
			t.Errorf("missing required file: %s", name)
		}
	}

	var monthDir string
	for name := range fileNames {
		if strings.HasSuffix(name, "/_index.json") {
			monthDir = strings.TrimSuffix(name, "/_index.json")
			break
		}
	}
	if monthDir == "" {
		t.Fatal("missing month directory with _index.json")
	}

	expectedFiles := []string{
		monthDir + "/_index.json",
		monthDir + "/" + pathToFile("/info/name"),
		monthDir + "/" + pathToFile("/protocols/test"),
	}
	for _, name := range expectedFiles {
		if _, ok := fileNames[name]; !ok {
			t.Errorf("missing file in archive: %s", name)
		}
	}

	exportFile := fileNames["export.json"]
	rc, err := exportFile.Open()
	if err != nil {
		t.Fatalf("cannot open export.json: %v", err)
	}
	defer rc.Close()

	var meta ExportMeta
	if err := json.NewDecoder(rc).Decode(&meta); err != nil {
		t.Fatalf("decode export.json error: %v", err)
	}
	if meta.TotalPins != 2 {
		t.Fatalf("expected TotalPins=2, got %d", meta.TotalPins)
	}
	if meta.ExportVersion != 1 {
		t.Fatalf("expected ExportVersion=1, got %d", meta.ExportVersion)
	}
}

func TestExportValidator(t *testing.T) {
	tests := []struct {
		name    string
		req     *ExportRequest
		wantErr string
	}{
		{
			name:    "empty identity",
			req:     &ExportRequest{Identity: "", IdentityType: "address", StartHeight: 1, EndHeight: 100},
			wantErr: "identity is required",
		},
		{
			name:    "bad identity type",
			req:     &ExportRequest{Identity: "id", IdentityType: "invalid", StartHeight: 1, EndHeight: 100},
			wantErr: "identity_type must be 'global_meta_id' or 'address'",
		},
		{
			name:    "negative start height",
			req:     &ExportRequest{Identity: "id", IdentityType: "address", StartHeight: -1, EndHeight: 100},
			wantErr: "start_height and end_height must be positive",
		},
		{
			name:    "start greater than end",
			req:     &ExportRequest{Identity: "id", IdentityType: "address", StartHeight: 200, EndHeight: 100},
			wantErr: "start_height must not exceed end_height",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRequest(tt.req)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestContentClassification(t *testing.T) {
	textPin := &pin.PinInscription{
		Id:            "text-pin",
		MetaId:        "meta-text",
		Path:          "/info/name",
		ChainName:     "btc",
		GenesisHeight: 800000,
		Timestamp:     1700000000,
		ContentType:   "text/plain",
		Content:       "hello world",
	}

	binaryPin := &pin.PinInscription{
		Id:             "binary-pin",
		MetaId:         "meta-binary",
		Path:           "/file/photo",
		ChainName:      "btc",
		GenesisHeight:  800001,
		Timestamp:      1700000001,
		ContentType:    "image/png",
		ContentSummary: "PNG image, 800x600",
	}

	textRecord := pinToRecord(textPin)
	if textRecord.Content != "hello world" {
		t.Fatalf("expected text content 'hello world', got %q", textRecord.Content)
	}
	if textRecord.ContentSummary != "" {
		t.Fatalf("expected empty ContentSummary for text, got %q", textRecord.ContentSummary)
	}

	binaryRecord := pinToRecord(binaryPin)
	if binaryRecord.Content != "" {
		t.Fatalf("expected empty Content for binary, got %q", binaryRecord.Content)
	}
	if binaryRecord.ContentSummary != "PNG image, 800x600" {
		t.Fatalf("expected ContentSummary 'PNG image, 800x600', got %q", binaryRecord.ContentSummary)
	}
}
