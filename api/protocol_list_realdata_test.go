package api

// 真值对比实测：用 manapi（https://manapi.metaid.io）全量翻页拉取的真实链上
// pin 数据灌入 fixture 库，验证 /api/protocol/list 的首现作者/首条 pin/计数。
//
// fixture 由 evidence/manapi_pull.py 生成（2026-10-26 拉取）：
//   MAN_PROTOCOL_DIR_FIXTURE=/path/to/protocol_fixture.ndjson \
//   go test ./api -run TestProtocolListRealManapiFixture -v -count=1
// 未设置环境变量时跳过（保持 go test ./... 无网络依赖）。
//
// 期望值来源：manapi /api/pin/path/list since=0 全量翻页的逐行事实。
// 与 api_total（CountDB 小时级统计，有滞后）的差异见交接简报：
// 计数口径 = 已上链（genesisHeight>=0）的全部 pin，含已撤销（status=-1）。
import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"man-p2p/common"
	"man-p2p/man"
	"man-p2p/pebblestore"
	"man-p2p/pin"

	"github.com/gin-gonic/gin"
)

type realFixtureRow struct {
	Id            string `json:"id"`
	Path          string `json:"path"`
	Address       string `json:"address"`
	GlobalMetaId  string `json:"globalMetaId"`
	GenesisHeight int64  `json:"genesisHeight"`
	Timestamp     int64  `json:"timestamp"`
	TxIndex       int    `json:"txIndex"`
	ChainName     string `json:"chainName"`
	Status        int    `json:"status"`
	ContentBody   string `json:"contentBody"`
}

// manapi 2026-10-26 全量翻页得出的地面真值（族计数 = 已上链行数）
var realExpectations = map[string]struct {
	total      int64
	firstPinId string
	height     int64
	timestamp  int64
	address    string
	gmid       string
}{
	"/protocols/simplequestion": {113, "398f262671e6323cd5ebe9fc21266c9f3aa14186f61678d05dfd6101d2d38f9ai0", 188591, 1788777818, "1GrqX7K9jdnUor8hAoAfDx99uFH2tT75Za", "idq14hmv23j5fnlx4ccnmvlyldjd38xjsechzwg9xz"},
	"/protocols/simpleanswer":   {494, "8adb1f2121739935706144ad40d07329751c075d9455ddfbe5ef15e82d0c637ai0", 188600, 1788788555, "1GrqX7K9jdnUor8hAoAfDx99uFH2tT75Za", "idq14hmv23j5fnlx4ccnmvlyldjd38xjsechzwg9xz"},
	"/protocols/metabot-skill":  {172, "d7947500f7668e361bd84d20a45f49bb8e692d3c5ec1dc57310a8d8171f258f8i0", 162663, 1772470043, "1MFi1WM2NXnV3kjdLKaUw7Ad23LSvSD9fY", "idq1mc4fynwqdluw8nfe7ylmnd6280uxuzzj5n6q9z"},
	// skill-service 首现 pin 本身 status=-1（已撤销）：目录按链上事实计，首现不变
	"/protocols/skill-service": {115, "7ddac98fcdd193fc05a3a01403e9f5da5247d5b5eb12adf67c47c52439b1f995i0", 164121, 1773404572, "1GrqX7K9jdnUor8hAoAfDx99uFH2tT75Za", "idq14hmv23j5fnlx4ccnmvlyldjd38xjsechzwg9xz"},
	"/protocols/trust":         {5, "e9ad697d20f396a263692024f7bc5f893cc86a8cee079b548497b56acb089e52i0", 188750, 1788980697, "17EiZH4fU7UtF8myrASBeqSzYKBZa6nWqM", "idq1g35d5yftpq3jv0ukejte7z76qdqp7sve8l2etm"},
	// preference 族 = /protocols/preference/round 的 2 pin 归入族（主路径 0 pin）
	"/protocols/preference": {2, "7fef456bba9d15bc1623624846d70ea41eb22e57e8a154961a99175ae7fb7e49i0", 194034, 1792029683, "1GrqX7K9jdnUor8hAoAfDx99uFH2tT75Za", "idq14hmv23j5fnlx4ccnmvlyldjd38xjsechzwg9xz"},
	// 最早注册 pin 无 globalMetaId（早期链上数据如此）
	"/protocols/metaprotocol": {41, "0a30799fd19d18d93fb9a30de666a728a857c6db5bd71753eeb4f8e53c624fcbi0", 145049, 1761619788, "16xN11wyQmUTS3qFwaJYbwHbjHaFkibxWo", ""},
}

func TestProtocolListRealManapiFixture(t *testing.T) {
	fixturePath := os.Getenv("MAN_PROTOCOL_DIR_FIXTURE")
	if fixturePath == "" {
		t.Skip("MAN_PROTOCOL_DIR_FIXTURE not set; skipping real-data fixture test")
	}

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

	f, err := os.Open(fixturePath)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	ingested := 0
	for scanner.Scan() {
		var row realFixtureRow
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatalf("fixture row: %v", err)
		}
		p := &pin.PinInscription{
			Id:            row.Id,
			Path:          row.Path,
			Address:       row.Address,
			GlobalMetaId:  row.GlobalMetaId,
			ChainName:     row.ChainName,
			Timestamp:     row.Timestamp,
			GenesisHeight: row.GenesisHeight,
			TxIndex:       row.TxIndex,
			Status:        row.Status,
		}
		if row.ContentBody != "" {
			p.ContentBody = []byte(row.ContentBody)
			p.ContentLength = uint64(len(p.ContentBody))
		}
		if err := man.IngestP2PPin(p); err != nil {
			t.Fatalf("IngestP2PPin(%s): %v", row.Id, err)
		}
		ingested++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan fixture: %v", err)
	}
	t.Logf("ingested %d real pins from fixture", ingested)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/protocol/list", protocolList)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/protocol/list?page=1&size=50&refresh=1&sortBy=pinTotal&order=desc", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body %s", w.Code, w.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			List  []protocolListRow `json:"list"`
			Count int64             `json:"count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body %s", err, w.Body.String())
	}
	if resp.Code != 1 {
		t.Fatalf("api code = %d", resp.Code)
	}
	if resp.Data.Count != int64(len(realExpectations)) {
		t.Fatalf("family count = %d, want %d (families: %+v)", resp.Data.Count, len(realExpectations), resp.Data.List)
	}
	got := make(map[string]protocolListRow, len(resp.Data.List))
	for _, row := range resp.Data.List {
		got[row.Path] = row
	}
	if raw, err := json.MarshalIndent(resp.Data.List, "", "  "); err == nil {
		t.Logf("directory rows:\n%s", raw)
	}
	for path, want := range realExpectations {
		row, ok := got[path]
		if !ok {
			t.Errorf("%s missing from directory", path)
			continue
		}
		if row.PinTotal != want.total {
			t.Errorf("%s pinTotal = %d, want %d", path, row.PinTotal, want.total)
		}
		if row.FirstPinId != want.firstPinId {
			t.Errorf("%s firstPinId = %s, want %s", path, row.FirstPinId, want.firstPinId)
		}
		if row.FirstHeight != want.height || row.FirstTimestamp != want.timestamp {
			t.Errorf("%s first height/ts = %d/%d, want %d/%d", path, row.FirstHeight, row.FirstTimestamp, want.height, want.timestamp)
		}
		if row.FirstAddress != want.address || row.FirstGlobalMetaId != want.gmid {
			t.Errorf("%s firstAuthor = %s/%s, want %s/%s", path, row.FirstAddress, row.FirstGlobalMetaId, want.address, want.gmid)
		}
		// metaprotocol 在 h155706 被自注册（注册表登记了 /protocols/metaprotocol 自身）
		if path == "/protocols/metaprotocol" {
			if !row.Registered || row.RegisteredPinId != "77d2e8821bc07624cbdcb6d2bda65ba244a77720eb292b4621bcad6777f316bei0" {
				t.Errorf("metaprotocol registered = %v/%s, want true/77d2e882...", row.Registered, row.RegisteredPinId)
			}
		} else if row.Registered {
			// 六条缺口路径不应在注册表内（若注册表有新注册请更新盘点）
			t.Errorf("%s registered=true, 但按盘点应为未注册缺口", path)
		}
		if row.FirstChain != "mvc" {
			t.Errorf("%s firstChain = %s, want mvc", path, row.FirstChain)
		}
	}
}
