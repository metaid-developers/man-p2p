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

func setupUserInfoCompatRouter(t *testing.T) *gin.Engine {
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
	r.GET("/api/info/address/:address", getInfoByAddress)
	r.GET("/api/info/metaid/:metaId", getInfoByMetaId)
	r.GET("/api/v1/users/info/address/:address", getInfoByAddress)
	r.GET("/api/v1/users/info/metaid/:metaId", getInfoByMetaId)
	return r
}

func seedLegacyStyleMetaInfo(t *testing.T, address string) string {
	t.Helper()

	metaid := common.GetMetaIdByAddress(address)
	namePin := &pin.PinInscription{
		Id:            "name-pin-compat",
		Address:       address,
		MetaId:        metaid,
		Path:          "/info/name",
		ChainName:     "mvc",
		Timestamp:     1710000001,
		GenesisHeight: 163789,
		ContentBody:   []byte("Compat User"),
		ContentLength: uint64(len("Compat User")),
	}
	chatPin := &pin.PinInscription{
		Id:            "chat-pin-compat",
		Address:       address,
		MetaId:        metaid,
		Path:          "/info/chatpubkey",
		ChainName:     "mvc",
		Timestamp:     1710000002,
		GenesisHeight: 163789,
		ContentBody:   []byte("02abc123"),
		ContentLength: uint64(len("02abc123")),
	}
	if err := man.IngestP2PPin(namePin); err != nil {
		t.Fatalf("IngestP2PPin(name): %v", err)
	}
	if err := man.IngestP2PPin(chatPin); err != nil {
		t.Fatalf("IngestP2PPin(chat): %v", err)
	}

	legacy := &pin.MetaIdInfo{
		ChainName:   "mvc",
		MetaId:      metaid,
		Name:        "Compat User",
		NameId:      namePin.Id,
		ChatPubKey:  "02abc123",
		Address:     "",
		Avatar:      "",
		Bio:         "",
		Background:  "",
		FollowCount: 0,
	}
	data := map[string]*pin.MetaIdInfo{metaid: legacy}
	if err := man.PebbleStore.Database.BatchSetMetaidInfo(&data); err != nil {
		t.Fatalf("BatchSetMetaidInfo: %v", err)
	}
	return metaid
}

func TestInfoAddressBackfillsCompatibilityFields(t *testing.T) {
	r := setupUserInfoCompatRouter(t)
	address := "1GrqX7K9jdnUor8hAoAfDx99uFH2tT75Za"
	_ = seedLegacyStyleMetaInfo(t, address)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/info/address/"+address, nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d with body %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got := resp.Data["address"]; got != address {
		t.Fatalf("expected address %q, got %#v", address, got)
	}
	if got := resp.Data["globalMetaId"]; got != common.ConvertToGlobalMetaId(address) {
		t.Fatalf("expected globalMetaId %q, got %#v", common.ConvertToGlobalMetaId(address), got)
	}
	if got := resp.Data["chatpubkeyId"]; got != "chat-pin-compat" {
		t.Fatalf("expected chatpubkeyId %q, got %#v", "chat-pin-compat", got)
	}
}

func TestInfoMetaIdBackfillsCompatibilityFields(t *testing.T) {
	r := setupUserInfoCompatRouter(t)
	address := "1GrqX7K9jdnUor8hAoAfDx99uFH2tT75Za"
	metaid := seedLegacyStyleMetaInfo(t, address)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/info/metaid/"+metaid, nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d with body %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got := resp.Data["address"]; got != address {
		t.Fatalf("expected address %q, got %#v", address, got)
	}
	if got := resp.Data["globalMetaId"]; got != common.ConvertToGlobalMetaId(address) {
		t.Fatalf("expected globalMetaId %q, got %#v", common.ConvertToGlobalMetaId(address), got)
	}
	if got := resp.Data["chatpubkeyId"]; got != "chat-pin-compat" {
		t.Fatalf("expected chatpubkeyId %q, got %#v", "chat-pin-compat", got)
	}
}
