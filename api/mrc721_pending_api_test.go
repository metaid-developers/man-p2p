package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"man-p2p/adapter"
	"man-p2p/common"
	"man-p2p/man"
	"man-p2p/pebblestore"

	"github.com/gin-gonic/gin"
)

// MRC-721 未确认转移通道契约:item/info 挂 pending 对象、item/transfers 单列
// pending 数组(不并入 total),读侧据此区分「发了还没确认」和「没发」。

func setupPendingApiRouter(t *testing.T) (*gin.Engine, *pebblestore.Database) {
	t.Helper()
	r, db := setupMrc721ApiRouter(t)
	item := &pebblestore.Mrc721Item{
		ItemPinId:        "pending-item-1",
		CollectionName:   "SeedAlbum",
		CollectionPinId:  "seed-coll-pin",
		Number:           9,
		Name:             "Pending Item",
		OwnerAddress:     "addr-seller",
		OwnerMetaId:      "meta-seller",
		GenesisChainName: "mvc",
		GenesisHeight:    197009,
		GenesisTx:        "genesis-tx-1",
		Output:           "genesis-tx-1:0",
		OutputValue:      1,
	}
	if err := db.SaveMrc721Item(item); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	return r, db
}

func getJSON(t *testing.T, r *gin.Engine, url string) map[string]interface{} {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d", url, w.Code)
	}
	var body struct {
		Code int         `json:"code"`
		Data interface{} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("GET %s: decode %v", url, err)
	}
	if body.Code != 1 {
		t.Fatalf("GET %s: code %d body %s", url, body.Code, w.Body.String())
	}
	raw, _ := json.Marshal(body.Data)
	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("GET %s: data decode %v", url, err)
	}
	return data
}

func TestMrc721ItemInfoExposesPendingTransfer(t *testing.T) {
	r, db := setupPendingApiRouter(t)
	if err := db.SaveMrc721PendingTransfer(&pebblestore.Mrc721PendingTransfer{
		ItemPinId:     "pending-item-1",
		ChainName:     "mvc",
		SpentOutpoint: "genesis-tx-1:0",
		FromAddress:   "addr-seller",
		ToAddress:     "addr-buyer",
		TxId:          "mem-pool-txid",
		FirstSeenAt:   1793760000,
	}); err != nil {
		t.Fatalf("seed pending: %v", err)
	}

	data := getJSON(t, r, "/api/mrc721/item/info?pinId=pending-item-1")
	pending, ok := data["pending"].(map[string]interface{})
	if !ok {
		t.Fatalf("pending object missing: %s", data["pending"])
	}
	if pending["txStatus"] != "unconfirmed" {
		t.Fatalf("txStatus wrong: %v", pending["txStatus"])
	}
	if pending["spentOutpoint"] != "genesis-tx-1:0" || pending["to"] != "addr-buyer" ||
		pending["fromAddress"] != "addr-seller" || pending["txId"] != "mem-pool-txid" ||
		pending["firstSeenAt"] != float64(1793760000) {
		t.Fatalf("pending fields wrong: %+v", pending)
	}

	// transfers 视图:历史为空但 pending 单列(「no transfers」不再假阴性)
	tr := getJSON(t, r, "/api/mrc721/item/transfers?pinId=pending-item-1")
	if tr["total"] != float64(0) {
		t.Fatalf("confirmed total must stay 0: %v", tr["total"])
	}
	pendingList, ok := tr["pending"].([]interface{})
	if !ok || len(pendingList) != 1 {
		t.Fatalf("transfers pending array wrong: %v", tr["pending"])
	}
}

func TestMrc721ItemInfoPendingAbsentWhenSettled(t *testing.T) {
	r, _ := setupPendingApiRouter(t)

	data := getJSON(t, r, "/api/mrc721/item/info?pinId=pending-item-1")
	if _, exists := data["pending"]; exists {
		t.Fatalf("pending must be absent when settled: %v", data["pending"])
	}
	tr := getJSON(t, r, "/api/mrc721/item/transfers?pinId=pending-item-1")
	if pendingList, ok := tr["pending"].([]interface{}); !ok || len(pendingList) != 0 {
		t.Fatalf("transfers pending must be empty array: %v", tr["pending"])
	}
}

// ---------- /api/tx/status 兜底 ----------

type txStatusFakeChain struct {
	alphaLegacyAliasTestChain
	confirmed     bool
	confirmations int64
	blockHash     string
	height        int64
	err           error
}

func (c *txStatusFakeChain) GetTransactionStatus(txId string) (bool, int64, string, int64, error) {
	return c.confirmed, c.confirmations, c.blockHash, c.height, c.err
}

func setupTxStatusRouter(t *testing.T, fake *txStatusFakeChain) *gin.Engine {
	t.Helper()
	origAdapters := man.ChainAdapter
	t.Cleanup(func() { man.ChainAdapter = origAdapters })
	man.ChainAdapter = map[string]adapter.Chain{"faketest": fake}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/tx/status", txStatus)
	return r
}

func TestTxStatusEndpoint(t *testing.T) {
	origConfig := common.Config
	t.Cleanup(func() { common.Config = origConfig })
	common.Config = &common.AllConfig{}

	// 未确认(mempool 内)
	r := setupTxStatusRouter(t, &txStatusFakeChain{})
	data := getJSON(t, r, "/api/tx/status?chainName=faketest&txid=abc")
	if data["found"] != true || data["txStatus"] != "unconfirmed" || data["confirmations"] != float64(0) {
		t.Fatalf("unconfirmed status wrong: %v", data)
	}
	// 已确认
	r = setupTxStatusRouter(t, &txStatusFakeChain{confirmed: true, confirmations: 3, blockHash: "bh", height: 197299})
	data = getJSON(t, r, "/api/tx/status?chainName=faketest&txid=abc")
	if data["txStatus"] != "confirmed" || data["height"] != float64(197299) || data["blockHash"] != "bh" {
		t.Fatalf("confirmed status wrong: %v", data)
	}
	// 查无此交易/RPC 异常:found=false + unknown,不回传底层错误
	r = setupTxStatusRouter(t, &txStatusFakeChain{err: errTxNotFound()})
	data = getJSON(t, r, "/api/tx/status?chainName=faketest&txid=abc")
	if data["found"] != false || data["txStatus"] != "unknown" {
		t.Fatalf("not-found status wrong: %v", data)
	}
}

func errTxNotFound() error { return &txStatusTestError{} }

type txStatusTestError struct{}

func (e *txStatusTestError) Error() string { return "no such transaction" }
