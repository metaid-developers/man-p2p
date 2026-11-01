package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"man-p2p/common"
	"man-p2p/man"
	"man-p2p/pebblestore"

	"github.com/gin-gonic/gin"
)

// MRC-721 JSON API 契约测试:直接经 pebblestore 播种(man 层映射已有单测),
// 验证 8 个端点的路径/参数/响应结构(兼容旧 MetaSo 字段名)。

func setupMrc721ApiRouter(t *testing.T) (*gin.Engine, *pebblestore.Database) {
	t.Helper()

	origStore := man.PebbleStore
	origConfig := common.Config
	db, err := pebblestore.NewDataBase(t.TempDir(), 1)
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
	mrc721JsonApi(r)
	return r, db
}

func mrc721Seed(t *testing.T, db *pebblestore.Database) {
	t.Helper()

	col := &pebblestore.Mrc721Collection{
		CollectionName:   "SeedAlbum",
		Name:             "Seed Collection",
		TotalSupply:      10,
		RoyaltyRate:      5,
		PinId:            "seed-coll-pin",
		CreateAddress:    "addr-creator",
		CreateMetaId:     "meta-creator",
		CreateTime:       1700000000,
		GenesisChainName: "mvc",
		GenesisHeight:    100,
		GenesisTx:        "tx-coll",
		TotalNum:         2,
		ModifyHistory:    []string{"seed-mod-pin"},
	}
	if err := db.SaveMrc721Collection(col); err != nil {
		t.Fatalf("seed collection: %v", err)
	}
	item1 := &pebblestore.Mrc721Item{
		ItemPinId:        "seed-item-1",
		CollectionName:   "SeedAlbum",
		CollectionPinId:  "seed-coll-pin",
		Number:           1,
		Name:             "Item One",
		OwnerAddress:     "addr-creator",
		OwnerMetaId:      "meta-creator",
		CreatorMinted:    true,
		GenesisChainName: "mvc",
		GenesisHeight:    101,
		GenesisTx:        "tx-item-1",
		Output:           "seed-item-1:0",
		OutputValue:      546,
		CreateTime:       1700000100,
	}
	item2 := &pebblestore.Mrc721Item{
		ItemPinId:        "seed-item-2",
		CollectionName:   "SeedAlbum",
		CollectionPinId:  "seed-coll-pin",
		Number:           2,
		OwnerAddress:     "addr-holder",
		OwnerMetaId:      "meta-holder",
		GenesisChainName: "mvc",
		GenesisHeight:    102,
		GenesisTx:        "tx-item-2",
		Output:           "seed-item-2:0",
		OutputValue:      546,
		CreateTime:       1700000200,
	}
	for _, item := range []*pebblestore.Mrc721Item{item1, item2} {
		if err := db.SaveMrc721Item(item); err != nil {
			t.Fatalf("seed item: %v", err)
		}
		if err := db.AppendMrc721CollectionItem("SeedAlbum", item.Number, item.ItemPinId); err != nil {
			t.Fatalf("seed coll item: %v", err)
		}
	}
	if err := db.PutMrc721OwnerItem("addr-creator", "SeedAlbum", "seed-item-1"); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	if err := db.PutMrc721OwnerItem("addr-holder", "SeedAlbum", "seed-item-2"); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	if err := db.SaveMrc721ItemTransfer(&pebblestore.Mrc721ItemTransfer{
		ItemPinId: "seed-item-2", FromAddress: "addr-creator", ToAddress: "addr-holder",
		ChainName: "mvc", Height: 200, TxId: "spend-tx-1", Time: 1700000300,
	}); err != nil {
		t.Fatalf("seed transfer: %v", err)
	}
}

func mrc721GetJSON(t *testing.T, r *gin.Engine, path string) map[string]interface{} {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, path, nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%s: status %d body %s", path, w.Code, w.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s: bad json %v", path, err)
	}
	return body
}

func mrc721AssertListShape(t *testing.T, body map[string]interface{}, wantLen int, wantTotal ...int) {
	t.Helper()
	data, _ := body["data"].(map[string]interface{})
	if data == nil {
		t.Fatalf("missing data: %v", body)
	}
	list, _ := data["list"].([]interface{})
	total, _ := data["total"].(float64)
	expectTotal := wantLen
	if len(wantTotal) > 0 {
		expectTotal = wantTotal[0]
	}
	if len(list) != wantLen || int(total) != expectTotal {
		t.Fatalf("list shape wrong: len=%d total=%v want len=%d total=%d", len(list), total, wantLen, expectTotal)
	}
}

func TestMrc721CollectionPageList(t *testing.T) {
	r, db := setupMrc721ApiRouter(t)
	mrc721Seed(t, db)

	body := mrc721GetJSON(t, r, "/api/mrc721/collection/pageList?cousor=0&size=10")
	mrc721AssertListShape(t, body, 1)
	item := body["data"].(map[string]interface{})["list"].([]interface{})[0].(map[string]interface{})
	if item["collectionname"] != "SeedAlbum" || item["pinid"] != "seed-coll-pin" || item["totalnum"] != float64(2) {
		t.Fatalf("collection fields wrong: %v", item)
	}
	// camelCase 别名(IDBots 契约)
	if item["collectionName"] != "SeedAlbum" || item["totalSupply"] != float64(10) || item["royaltyRate"] != float64(5) || item["createAddress"] != "addr-creator" || item["createTime"] != float64(1700000000) || item["genesisChainName"] != "mvc" {
		t.Fatalf("collection camelCase aliases wrong: %v", item)
	}
	// modifyHistory 版本链(两种 key 别名)
	history, ok := item["modifyHistory"].([]interface{})
	if !ok || len(history) != 1 || history[0] != "seed-mod-pin" {
		t.Fatalf("modifyHistory wrong: %v", item["modifyHistory"])
	}
	if alias, ok := item["modify_history"].([]interface{}); !ok || len(alias) != 1 {
		t.Fatalf("modify_history alias wrong: %v", item["modify_history"])
	}
}

func TestMrc721CollectionInfoByNameAndPinId(t *testing.T) {
	r, db := setupMrc721ApiRouter(t)
	mrc721Seed(t, db)

	for _, path := range []string{
		"/api/mrc721/collection/info?name=SeedAlbum",
		"/api/mrc721/collection/info?pinId=seed-coll-pin",
	} {
		body := mrc721GetJSON(t, r, path)
		data, _ := body["data"].(map[string]interface{})
		if data == nil || data["collectionname"] != "SeedAlbum" {
			t.Fatalf("%s: wrong data %v", path, body)
		}
	}

	// 缺参数 → 参数错误
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/mrc721/collection/info", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("info without params should still be 200 envelope")
	}
}

func TestMrc721CollectionItemsPageList(t *testing.T) {
	r, db := setupMrc721ApiRouter(t)
	mrc721Seed(t, db)

	// 按 collectionid 查(旧 MetaSo 语义)
	body := mrc721GetJSON(t, r, "/api/mrc721/collection/items/pageList?pinid=seed-coll-pin&cousor=0&size=10")
	mrc721AssertListShape(t, body, 2)

	// 分页:第 2 页只余 1 条(total 仍为全集数)
	body = mrc721GetJSON(t, r, "/api/mrc721/collection/items/pageList?pinid=seed-coll-pin&cousor=1&size=1")
	mrc721AssertListShape(t, body, 1, 2)

	// 按专辑名查
	body = mrc721GetJSON(t, r, "/api/mrc721/collection/items/pageList?name=SeedAlbum&cousor=0&size=10")
	mrc721AssertListShape(t, body, 2)
}

func TestMrc721AddressCollectionAndItem(t *testing.T) {
	r, db := setupMrc721ApiRouter(t)
	mrc721Seed(t, db)

	body := mrc721GetJSON(t, r, "/api/mrc721/address/collection?address=addr-holder&cousor=0&size=10")
	mrc721AssertListShape(t, body, 1)
	entry := body["data"].(map[string]interface{})["list"].([]interface{})[0].(map[string]interface{})
	if entry["collectionname"] != "SeedAlbum" || entry["ownnum"] != float64(1) {
		t.Fatalf("address collection wrong: %v", entry)
	}

	body = mrc721GetJSON(t, r, "/api/mrc721/address/item?address=addr-holder")
	mrc721AssertListShape(t, body, 1)
	item := body["data"].(map[string]interface{})["list"].([]interface{})[0].(map[string]interface{})
	if item["itemPinId"] != "seed-item-2" || item["address"] != "addr-holder" {
		t.Fatalf("address item wrong: %v", item)
	}

	// 按专辑过滤
	body = mrc721GetJSON(t, r, "/api/mrc721/address/item?address=addr-holder&pinId=seed-coll-pin")
	mrc721AssertListShape(t, body, 1)
}

func TestMrc721ItemInfoAndTransfersAndVerify(t *testing.T) {
	r, db := setupMrc721ApiRouter(t)
	mrc721Seed(t, db)

	// item/info
	body := mrc721GetJSON(t, r, "/api/mrc721/item/info?pinId=seed-item-2")
	data, _ := body["data"].(map[string]interface{})
	if data == nil || data["collectionPinId"] != "seed-coll-pin" || data["txOutput"] != "seed-item-2:0" {
		t.Fatalf("item info wrong: %v", body)
	}
	// camelCase 别名(IDBots 契约)
	if data["ownerAddress"] != "addr-holder" || data["ownerMetaId"] != "meta-holder" || data["output"] != "seed-item-2:0" || data["outputValue"] != float64(546) || data["number"] != float64(2) || data["genesisChainName"] != "mvc" {
		t.Fatalf("item camelCase aliases wrong: %v", data)
	}

	// item/transfers
	body = mrc721GetJSON(t, r, "/api/mrc721/item/transfers?pinId=seed-item-2&cousor=0&size=10")
	mrc721AssertListShape(t, body, 1)
	tr := body["data"].(map[string]interface{})["list"].([]interface{})[0].(map[string]interface{})
	if tr["fromAddress"] != "addr-creator" || tr["toAddress"] != "addr-holder" {
		t.Fatalf("transfer fields wrong: %v", tr)
	}

	// item/verify(已收录)
	body = mrc721GetJSON(t, r, "/api/mrc721/item/verify?pinId=seed-item-1")
	data, _ = body["data"].(map[string]interface{})
	if data == nil || data["indexed"] != true || data["creatorMinted"] != true {
		t.Fatalf("verify wrong: %v", body)
	}
	// IDBots 契约别名
	if data["collected"] != true || data["collectionid"] != "seed-coll-pin" || data["itemPinId"] != "seed-item-1" || data["genesisChainName"] != "mvc" {
		t.Fatalf("verify aliases wrong: %v", data)
	}
	if data["collectionGenesis"] == nil || data["itemGenesis"] == nil {
		t.Fatalf("verify genesis facts missing: %v", data)
	}

	// item/verify(未收录)
	body = mrc721GetJSON(t, r, "/api/mrc721/item/verify?pinId=unknown-pin")
	data, _ = body["data"].(map[string]interface{})
	if data == nil || data["indexed"] != false {
		t.Fatalf("unindexed verify wrong: %v", body)
	}
}

func TestMrc721ItemInfoMissing(t *testing.T) {
	r, _ := setupMrc721ApiRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/mrc721/item/info?pinId=nope", nil)
	r.ServeHTTP(w, req)

	var body map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["code"] == float64(1) {
		t.Fatalf("missing item should not be success envelope: %v", body)
	}
}
