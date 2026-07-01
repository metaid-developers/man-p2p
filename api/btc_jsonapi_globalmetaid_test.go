package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"man-p2p/common"
	"man-p2p/idaddress"
	"man-p2p/man"
	"man-p2p/pebblestore"
	"man-p2p/pin"

	"github.com/gin-gonic/gin"
)

func setupMetaIdPinListRouter(t *testing.T) *gin.Engine {
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
	btcJsonApi(r)
	return r
}

func seedMetaIdPathPin(t *testing.T, address string, path string) *pin.PinInscription {
	t.Helper()

	pinNode := &pin.PinInscription{
		Id:            "global-metaid-compatible-pin",
		MetaId:        common.GetMetaIdByAddress(address),
		Address:       address,
		Path:          path,
		ChainName:     "btc",
		GenesisHeight: 800000,
		Timestamp:     1700000000,
		ContentBody:   []byte("global metaid compatible"),
	}
	if err := man.PebbleStore.Database.BatchInsertPins([]pin.PinInscription{*pinNode}); err != nil {
		t.Fatalf("BatchInsertPins: %v", err)
	}
	addressKey := pin.GenAddressSortKey(pinNode, pinNode.Timestamp, pinNode.ChainName, pinNode.GenesisHeight)
	if err := man.PebbleStore.Database.BatchSetAddressData(&[]string{addressKey}); err != nil {
		t.Fatalf("BatchSetAddressData: %v", err)
	}
	countKey := pinNode.MetaId + "_" + common.GetMetaIdByAddress(path) + "_count"
	if err := man.PebbleStore.Database.CountSet(countKey, 1); err != nil {
		t.Fatalf("CountSet: %v", err)
	}

	return pinNode
}

func seedRevokedOriginalPin(t *testing.T) *pin.PinInscription {
	t.Helper()

	address := "1P2PTestAddress"
	metaid := common.GetMetaIdByAddress(address)
	originalPin := &pin.PinInscription{
		Id:            "api-list-original-metaapp-pin",
		Operation:     "create",
		Address:       address,
		MetaId:        metaid,
		Path:          "/protocols/metaapp",
		ChainName:     "mvc",
		Timestamp:     1710000000,
		GenesisHeight: 100,
		ContentBody:   []byte(`{"version":"1.0.0"}`),
	}
	modifyPin := &pin.PinInscription{
		Id:            "api-list-modify-metaapp-pin",
		Operation:     "modify",
		Address:       address,
		MetaId:        metaid,
		Path:          "@api-list-original-metaapp-pin",
		ChainName:     "mvc",
		Timestamp:     1710000010,
		GenesisHeight: 101,
		ContentBody:   []byte(`{"version":"1.0.1"}`),
	}
	revokePin := &pin.PinInscription{
		Id:            "api-list-revoke-modify-metaapp-pin",
		Operation:     "revoke",
		Address:       address,
		MetaId:        metaid,
		Path:          "@api-list-modify-metaapp-pin",
		ChainName:     "mvc",
		Timestamp:     1710000020,
		GenesisHeight: 102,
	}

	for _, pinNode := range []*pin.PinInscription{originalPin, modifyPin, revokePin} {
		if err := man.IngestP2PPin(pinNode); err != nil {
			t.Fatalf("IngestP2PPin(%s): %v", pinNode.Id, err)
		}
	}

	return originalPin
}

func TestMetaIdPinListAcceptsGlobalMetaIdAsBTCAddressAlias(t *testing.T) {
	r := setupMetaIdPinListRouter(t)

	address := "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa"
	path := "/info/name"
	seededPin := seedMetaIdPathPin(t, address, path)
	globalMetaId, err := idaddress.ConvertFromBitcoin(address)
	if err != nil {
		t.Fatalf("ConvertFromBitcoin: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/metaid/pin/list/"+globalMetaId+"?cursor=0&size=20&path="+url.QueryEscape(path),
		nil,
	)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", w.Code, w.Body.String())
	}
	var body struct {
		Code int `json:"code"`
		Data struct {
			List []struct {
				Id     string `json:"id"`
				MetaId string `json:"metaid"`
			} `json:"list"`
			Total int64 `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != 1 {
		t.Fatalf("expected success code 1, got %d with body %s", body.Code, w.Body.String())
	}
	if body.Data.Total != 1 {
		t.Fatalf("expected total 1, got %d with body %s", body.Data.Total, w.Body.String())
	}
	if len(body.Data.List) != 1 {
		t.Fatalf("expected 1 pin, got %d with body %s", len(body.Data.List), w.Body.String())
	}
	if body.Data.List[0].Id != seededPin.Id {
		t.Fatalf("expected pin %q, got %q", seededPin.Id, body.Data.List[0].Id)
	}
	if body.Data.List[0].MetaId != seededPin.MetaId {
		t.Fatalf("expected legacy metaid %q, got %q", seededPin.MetaId, body.Data.List[0].MetaId)
	}
}

func TestPinListIncludesRevokedStatusForOriginalPin(t *testing.T) {
	r := setupMetaIdPinListRouter(t)
	originalPin := seedRevokedOriginalPin(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/pin/list?page=1&size=20", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", w.Code, w.Body.String())
	}
	var body struct {
		Code int `json:"code"`
		Data struct {
			Pins []map[string]interface{} `json:"Pins"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != 1 {
		t.Fatalf("expected success code 1, got %d with body %s", body.Code, w.Body.String())
	}
	for _, pinNode := range body.Data.Pins {
		if pinNode["id"] != originalPin.Id {
			continue
		}
		status, ok := pinNode["status"]
		if !ok {
			t.Fatalf("expected original pin %q to include status in response body %s", originalPin.Id, w.Body.String())
		}
		if status != float64(-1) {
			t.Fatalf("expected original pin status -1, got %v with body %s", status, w.Body.String())
		}
		return
	}
	t.Fatalf("expected response to include original pin %q, body %s", originalPin.Id, w.Body.String())
}

func TestGetPinByIdIncludesRevokedStatusForOriginalPin(t *testing.T) {
	r := setupMetaIdPinListRouter(t)
	originalPin := seedRevokedOriginalPin(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/pin/"+originalPin.Id, nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", w.Code, w.Body.String())
	}
	var body struct {
		Code int `json:"code"`
		Data struct {
			Id     string `json:"id"`
			Status int    `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != 1 {
		t.Fatalf("expected success code 1, got %d with body %s", body.Code, w.Body.String())
	}
	if body.Data.Id != originalPin.Id {
		t.Fatalf("expected pin %q, got %q", originalPin.Id, body.Data.Id)
	}
	if body.Data.Status != -1 {
		t.Fatalf("expected original pin status -1, got %d with body %s", body.Data.Status, w.Body.String())
	}
}

func TestMempoolListIncludesPinStatus(t *testing.T) {
	r := setupMetaIdPinListRouter(t)

	mempoolPin := &pin.PinInscription{
		Id:            "api-mempool-status-pin",
		Operation:     "revoke",
		Status:        -1,
		Address:       "1P2PTestAddress",
		MetaId:        common.GetMetaIdByAddress("1P2PTestAddress"),
		Path:          "@api-list-original-metaapp-pin",
		ChainName:     "mvc",
		Timestamp:     1710000030,
		GenesisHeight: -1,
	}
	if err := man.IngestP2PPin(mempoolPin); err != nil {
		t.Fatalf("IngestP2PPin(%s): %v", mempoolPin.Id, err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/mempool/list?page=1&size=20", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", w.Code, w.Body.String())
	}
	var body struct {
		Code int `json:"code"`
		Data struct {
			Pins []map[string]interface{} `json:"Pins"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != 1 {
		t.Fatalf("expected success code 1, got %d with body %s", body.Code, w.Body.String())
	}
	if len(body.Data.Pins) != 1 {
		t.Fatalf("expected 1 mempool pin, got %d with body %s", len(body.Data.Pins), w.Body.String())
	}
	status, ok := body.Data.Pins[0]["status"]
	if !ok {
		t.Fatalf("expected mempool pin %q to include status in response body %s", mempoolPin.Id, w.Body.String())
	}
	if status != float64(-1) {
		t.Fatalf("expected mempool pin status -1, got %v with body %s", status, w.Body.String())
	}
}
