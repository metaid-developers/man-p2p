package api

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"man-p2p/common"
	"man-p2p/man"
	"man-p2p/pebblestore"
	"man-p2p/pin"
	"net/http"
	"strconv"
	"strings"
	"time"

	_ "man-p2p/docs"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/cockroachdb/pebble"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

func formatRootId(rootId string) string {
	if len(rootId) < 6 {
		return ""
	}
	//return fmt.Sprintf("%s...%s", rootId[0:3], rootId[len(rootId)-3:])
	return rootId[0:6]
}
func formatTime(t int64) string {
	tm := time.Unix(t, 0)
	return tm.Format("2006-01-02 15:04:05")
}
func formatAddress(address string) string {
	if len(address) < 6 {
		return ""
	}
	return fmt.Sprintf("%s...%s", address[0:6], address[len(address)-3:])
}
func popLevelCount(chainName, pop string) string {
	lv, _ := pin.PopLevelCount(chainName, pop)
	if lv == -1 {
		return "--"
	}
	return fmt.Sprintf("Lv%d", lv)
}
func popStrShow(chainName, pop string) string {
	_, lastStr := pin.PopLevelCount(chainName, pop)
	return lastStr[0:8] + "..."
}
func outpointToTxId(outpoint string) string {
	arr := strings.Split(outpoint, ":")
	if len(arr) == 2 {
		return arr[0]
	} else {
		return "erro"
	}
}
func CorsMiddleware() gin.HandlerFunc {
	return func(context *gin.Context) {
		method := context.Request.Method

		context.Header("Access-Control-Allow-Origin", "*")
		context.Header("Access-Control-Allow-Credentials", "true")
		context.Header("Access-Control-Allow-Headers", "*")
		context.Header("Access-Control-Allow-Methods", "GET,HEAD,POST,PUT,DELETE,OPTIONS")
		context.Header("Access-Control-Expose-Headers", "Content-Length, Access-Control-Allow-Origin, Access-Control-Allow-Headers, Content-Type")

		if method == "OPTIONS" {
			context.AbortWithStatus(http.StatusNoContent)
		}
		context.Next()
	}
}
func Start(f embed.FS) {
	gin.SetMode(gin.ReleaseMode)
	gin.DefaultWriter = io.Discard
	r := gin.Default()
	funcMap := template.FuncMap{
		"formatRootId":   formatRootId,
		"popLevelCount":  popLevelCount,
		"popStrShow":     popStrShow,
		"formatAddress":  formatAddress,
		"formatTime":     formatTime,
		"outpointToTxId": outpointToTxId,
		"add": func(a, b, c int) int {
			return a + b + c
		},
	}
	//use embed.FS
	fp, _ := fs.Sub(f, "web/static")
	r.StaticFS("/assets", http.FS(fp))
	// Go's embed.FS and ParseFS don't support ** glob patterns
	// Must specify each subdirectory explicitly
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseFS(f, "web/template/home/*.html", "web/template/public/*.html"))
	r.SetHTMLTemplate(tmpl)
	config := cors.DefaultConfig()
	config.AllowOrigins = []string{"*"}
	r.Use(cors.New(config))
	// Swagger
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	r.GET("/health", func(ctx *gin.Context) {
		ctx.JSON(200, gin.H{"status": "ok", "version": common.Version})
	})
	//r.LoadHTMLGlob("./web/template/**/*")
	//r.Static("/assets", "./web/static")
	r.GET("/", home)
	r.GET("/pin/list/:page", pinPageList)
	r.GET("/metaid/:page", metaid)
	r.GET("/blocks/:page", blocks)
	r.GET("/mempool/:page", mempool)
	r.GET("/block/:height", block)
	r.GET("/pin/:number", pinshow)
	r.GET("/search/:key", searchshow)
	r.GET("/tx/:chain/:txid", tx)
	r.GET("/node/:rootid", node)
	r.GET("/content/:number", content)
	r.GET("/stream/:number", stream)
	//debug api
	r.GET("/debug/count", debugCount)
	r.GET("/debug/sync", debugSync)
	// MRC20 routes disabled in man-p2p phase 1 — asset parsing not enabled this phase
	// r.GET("/mrc20/info/:id", mrc20Info)
	// r.GET("/mrc20/holders/:id/:page", mrc20Holders)
	// r.GET("/mrc20/history/:id/:page", mrc20History)
	// r.GET("/mrc20/address/:id/:address/:page", mrc20AddressHistory)
	// r.GET("/mrc20/:page", mrc20List)
	// MRC721 routes disabled in man-p2p phase 1 — asset parsing not enabled this phase
	// r.GET("/mrc721/:page", mrc721List)
	// r.GET("/mrc721/item/list/:name/:page", mrc721ItemList)
	//btc json api
	btcJsonApi(r)
	// mrc20JsonApi(r) // disabled in man-p2p phase 1
	// metaAccessJsonApi(r)
	// mrc721JsonApi(r)
	// if common.ModuleExist("metaso") || common.ModuleExist("metaso_pev") {
	// 	log.Println("use metaso api")
	// 	metaso.Api(r)
	// 	metaso.StatisticsApi(r)
	// }
	// if common.ModuleExist("metaname") {
	// 	log.Println("use metaname api")
	// 	metaname.Api(r)
	// }
	// go func() {
	// 	r.Run(":7777") // 第2个端口
	// }()
	log.Println("Server Start", common.Config.Web.Port)
	if common.Config.Web.KeyFile != "" && common.Config.Web.PemFile != "" {
		if err := r.RunTLS(common.Config.Web.Port, common.Config.Web.PemFile, common.Config.Web.KeyFile); err != nil {
			log.Printf("Server Start failed: %v", err)
		}
	} else {
		if err := r.Run(common.Config.Web.Port); err != nil {
			log.Printf("Server Start failed: %v", err)
		}
	}

}

// debugCount - Debug API that directly returns statistics data
func debugCount(ctx *gin.Context) {
	count := man.PebbleStore.GetAllCount()
	ctx.JSON(200, gin.H{
		"pin":    count.Pin,
		"block":  count.Block,
		"metaId": count.MetaId,
		"app":    count.App,
	})
}

// debugSync - Debug API that returns sync heights from MetaDB
func debugSync(ctx *gin.Context) {
	db := man.PebbleStore.Database
	if db == nil || db.MetaDb == nil {
		ctx.JSON(500, gin.H{"error": "db not ready"})
		return
	}

	keys := []string{
		"mvc_sync_height",
		"btc_sync_height",
		"doge_sync_height",
		"mvc_mrc20_sync_height",
		"btc_mrc20_sync_height",
		"doge_mrc20_sync_height",
	}

	result := make(map[string]interface{}, len(keys))
	for _, key := range keys {
		val, closer, err := db.MetaDb.Get([]byte(key))
		if err == nil {
			result[key] = string(val)
			closer.Close()
			continue
		}
		if err == pebble.ErrNotFound {
			result[key] = nil
			continue
		}
		result[key] = fmt.Sprintf("error: %v", err)
	}

	ctx.JSON(200, result)
}

// index page
func home(ctx *gin.Context) {
	//list, err := man.DbAdapter.GetPinPageList(1, 100)
	list, lastId, err := man.PebbleStore.PinPageList(0, 100, "")
	if err != nil {
		ctx.String(200, "fail")
	}
	var msg []*pin.PinMsg
	for _, p := range list {
		pmsg := &pin.PinMsg{Content: p.ContentSummary, Number: p.Number, Operation: p.Operation, Id: p.Id, Type: p.ContentTypeDetect, Path: p.Path, PopLv: p.PopLv, MetaId: p.MetaId, ChainName: p.ChainName}
		msg = append(msg, pmsg)
	}
	//count := man.DbAdapter.Count()
	count := man.PebbleStore.GetAllCount()
	ctx.HTML(200, "home/index.html", gin.H{"Pins": msg, "Count": &count, "Active": "index", "NextPage": 2, "PrePage": 0, "LastId": lastId})
}
func pinPageList(ctx *gin.Context) {
	page, err := strconv.Atoi(ctx.Param("page"))
	if err != nil {
		fmt.Println(err)
		ctx.String(200, "fail")
		return
	}

	//list, err := man.DbAdapter.GetPinPageList(page, 100)
	list, lastId, err := man.PebbleStore.PinPageList(page-1, 100, ctx.Query("lastId"))
	if err != nil {
		ctx.String(200, "fail")
	}
	var msg []*pin.PinMsg
	for _, p := range list {
		pmsg := &pin.PinMsg{Content: p.ContentSummary, Number: p.Number, Operation: p.Operation, Id: p.Id, Type: p.ContentTypeDetect, Path: p.Path, Pop: p.Pop, ChainName: p.ChainName}
		msg = append(msg, pmsg)
	}
	//count := man.DbAdapter.Count()
	count := man.PebbleStore.GetAllCount()
	prePage := page - 1
	nextPage := page + 1
	if len(msg) == 0 {
		nextPage = 0
	}
	if prePage <= 0 {
		prePage = 0
	}
	ctx.HTML(200, "home/index.html", gin.H{"Pins": msg, "Count": &count, "Active": "index", "NextPage": nextPage, "PrePage": prePage, "LastId": lastId})
}

func mempool(ctx *gin.Context) {
	page, err := strconv.ParseInt(ctx.Param("page"), 10, 64)
	if err != nil {
		ctx.String(200, "fail")
		return
	}
	list, err := man.PebbleStore.Database.GetMempoolPageList(page-1, 100)
	// list, err := man.DbAdapter.GetMempoolPinPageList(page, 100)
	if err != nil {
		ctx.String(200, "fail")
		return
	}
	var msg []*pin.PinMsg
	for _, p := range list {
		pmsg := &pin.PinMsg{Content: p.ContentSummary, Number: p.Number, Operation: p.Operation, Id: p.Id, Type: p.ContentTypeDetect, Path: p.Path, MetaId: p.MetaId}
		msg = append(msg, pmsg)
	}
	pinsVal, closer, err := man.PebbleStore.Database.CountDB.Get([]byte("pins"))
	var count int64
	if err == nil {
		count, _ = strconv.ParseInt(string(pinsVal), 10, 64)
		closer.Close()
	}
	//count := man.DbAdapter.Count()
	prePage := page - 1
	nextPage := page + 1
	if len(msg) == 0 {
		nextPage = 0
	}
	if prePage <= 0 {
		prePage = 0
	}
	ctx.HTML(200, "home/mempool.html", gin.H{"Pins": msg, "Count": &count, "Active": "mempool", "NextPage": nextPage, "PrePage": prePage})
}

// metaid page
func metaid(ctx *gin.Context) {
	page, err := strconv.ParseInt(ctx.Param("page"), 10, 64)
	if err != nil {
		ctx.String(200, "fail")
		return
	}
	list, err := man.PebbleStore.Database.MetaIdPageList(page-1, 100)
	if err != nil {
		ctx.String(200, "fail")
		return
	}
	prePage := page - 1
	nextPage := page + 1
	if len(list) == 0 {
		nextPage = 0
	}
	if prePage <= 0 {
		prePage = 0
	}
	ctx.HTML(200, "home/metaid.html", gin.H{"List": list, "Active": "metaid", "NextPage": nextPage, "PrePage": prePage})
}

// pinshow
func pinshow(ctx *gin.Context) {
	//pinMsg, err := man.DbAdapter.GetPinByNumberOrId(ctx.Param("number"))
	pinMsg, err := man.PebbleStore.GetPinById(ctx.Param("number"))
	if err != nil || pinMsg.Id == "" {
		ctx.String(200, "fail")
		return
	}
	pinMsg.ContentBody = []byte{}
	ctx.HTML(200, "home/pin.html", pinMsg)
}

// searchshow
func searchshow(ctx *gin.Context) {
	//TODO query
	// pinMsg, err := man.DbAdapter.GetPinByMeatIdOrId(ctx.Param("key"))
	// if err != nil || pinMsg == nil {
	// 	ctx.HTML(200, "home/search.html", pinMsg)
	// 	return
	// }
	// pinMsg.ContentBody = []byte{}
	// ctx.HTML(200, "home/search.html", gin.H{"Key": ctx.Param("key"), "Data": pinMsg})
}
func content(ctx *gin.Context) {
	// //p, err := man.DbAdapter.GetPinByNumberOrId(ctx.Param("number"))
	var p pin.PinInscription
	var err error
	p, err = man.PebbleStore.GetPinById(ctx.Param("number"))
	if err != nil || p.Id == "" {
		ctx.String(200, "fail")
		return
	}
	if p.ContentType == "application/mp4" {
		//ctx.Data(200, "application/octet-stream", p.ContentBody)
		ctx.Header("Content-Type", "text/html; charset=utf-8")
		ctx.String(200, `<video controls autoplay muted src="/stream/`+p.Id+`"></viedo>`)
	} else {
		baseStr, isImage := common.IsBase64Image(string(p.ContentBody))
		if isImage {
			ctx.String(200, baseStr+string(p.ContentBody))
		} else {
			ctx.String(200, string(p.ContentBody))
		}

	}
}

func stream(ctx *gin.Context) {
	// //TODO stream
	// p, err := man.DbAdapter.GetPinByNumberOrId(ctx.Param("number"))
	// if err != nil || p == nil {
	// 	ctx.String(200, "fail")
	// 	return
	// }
	// ctx.Data(200, "application/octet-stream", p.ContentBody)
}
func blocks(ctx *gin.Context) {
	page, err := strconv.ParseInt(ctx.Param("page"), 10, 64)
	if err != nil {
		ctx.String(200, "fail")
		return
	}
	//list, err := man.DbAdapter.GetPinPageList(page, 100)
	q := pebblestore.PageQuery{Type: "pin", Page: 0, Size: 10, LastId: ""}
	list, err := man.PebbleStore.QueryPageBlock(q)
	if err != nil {
		ctx.String(200, "fail")
		return
	}
	msgMap := make(map[int64][]*pin.PinMsg)
	var msgList []int64
	for _, x := range list {
		for _, p := range x.PinList {
			pmsg := &pin.PinMsg{Content: p.ContentSummary, Number: p.Number, Id: p.Id, Type: p.ContentTypeDetect, Height: p.GenesisHeight}
			if _, ok := msgMap[pmsg.Height]; ok {
				msgMap[pmsg.Height] = append(msgMap[pmsg.Height], pmsg)
			} else {
				msgMap[pmsg.Height] = []*pin.PinMsg{pmsg}
				msgList = append(msgList, pmsg.Height)
			}
		}
	}
	prePage := page - 1
	nextPage := page + 1
	if len(list) == 0 {
		nextPage = 0
	}
	if prePage <= 0 {
		prePage = 0
	}
	ctx.HTML(200, "home/blocks.html", gin.H{"msgMap": msgMap, "msgList": msgList, "Active": "blocks", "NextPage": nextPage, "PrePage": prePage})
}

func block(ctx *gin.Context) {
	//TODO block details page
	// height, err := strconv.ParseInt(ctx.Param("height"), 10, 64)
	// if err != nil {
	// 	ctx.String(200, "fail")
	// 	return
	// }
	// list, total, err := man.DbAdapter.GetBlockPin(height, 20)
	// if err != nil {
	// 	ctx.String(200, "fail")
	// 	return
	// }
	// var pins []*pin.PinMsg
	// for _, p := range list {
	// 	pmsg := &pin.PinMsg{Content: p.ContentSummary, Number: p.Number, Id: p.Id, Type: p.ContentTypeDetect}
	// 	pins = append(pins, pmsg)
	// }
	// block := man.ChainAdapter["btc"].GetBlockMsg(height)
	// msg := gin.H{
	// 	"Pins":   pins,
	// 	"PinNum": total,
	// 	"Height": height,
	// 	"Block":  block,
	// }
	// ctx.HTML(200, "home/block.html", &msg)
}

type txMsgOutput struct {
	Id      string
	Value   int64
	Script  string
	Address string
}
type txMsgInput struct {
	Point   string
	Witness [][]string
}

func tx(ctx *gin.Context) {
	txid := ctx.Param("txid")
	chain := ctx.Param("chain")
	if chain != "btc" && chain != "mvc" && chain != "doge" {
		ctx.String(200, "fail")
		return
	}
	trst, err := man.ChainAdapter[chain].GetTransaction(txid)
	if err != nil {
		ctx.String(200, "fail")
		return
	}
	tx := trst.(*btcutil.Tx)
	var outList []*txMsgOutput
	for i, out := range tx.MsgTx().TxOut {
		id := fmt.Sprintf("%s:%d", tx.Hash().String(), i)
		address := man.IndexerAdapter[chain].GetAddress(out.PkScript)
		outList = append(outList, &txMsgOutput{Id: id, Value: out.Value, Script: string(out.PkScript), Address: address})
	}
	var inList []*txMsgInput
	for _, in := range tx.MsgTx().TxIn {
		point := in.PreviousOutPoint
		witness := [][]string{}
		if (chain == "btc" || chain == "doge") && tx.MsgTx().HasWitness() {
			//for _, in := range tx.MsgTx().TxIn {
			if len(in.Witness) > 0 {
				w, err := common.BtcParseWitnessScript(in.Witness)
				if err == nil {
					witness = w
				}
			}
			//}
		}
		inList = append(inList, &txMsgInput{Point: point.String(), Witness: witness})
	}

	msg := gin.H{
		"TxHash":    tx.Hash().String(),
		"InputNum":  len(tx.MsgTx().TxIn),
		"OutPutNum": len(tx.MsgTx().TxOut),
		"TxIn":      inList,
		"TxOut":     outList,
		"Chain":     ctx.Param("chain"),
	}
	ctx.HTML(200, "home/tx.html", msg)
}

func node(ctx *gin.Context) {
	//TODO node details page
	// rootid := ctx.Param("rootid")
	// list, total, err := man.DbAdapter.GetMetaIdPin(rootid, 1, 200)
	// if err != nil {
	// 	ctx.String(200, "fail")
	// 	return
	// }
	// ctx.HTML(200, "home/node.html", &gin.H{"RootId": rootid, "Total": total, "Pins": list})
}
func mrc20List(ctx *gin.Context) {
	page, err := strconv.ParseInt(ctx.Param("page"), 10, 64)
	if err != nil || page < 1 {
		page = 1
	}
	pageSize := 100
	cursor := int((page - 1) * int64(pageSize))

	list, err := man.PebbleStore.GetMrc20TickList(cursor, pageSize)
	if err != nil {
		ctx.String(200, "fail: "+err.Error())
		return
	}

	prePage := page - 1
	nextPage := page + 1
	if len(list) < pageSize {
		nextPage = 0
	}
	if prePage <= 0 {
		prePage = 0
	}
	ctx.HTML(200, "home/mrc20.html", gin.H{"Ticks": list, "Active": "mrc20", "NextPage": nextPage, "PrePage": prePage})
}

func mrc20Info(ctx *gin.Context) {
	tickId := ctx.Param("id")
	if tickId == "" {
		ctx.String(200, "fail: id is required")
		return
	}

	tick, err := man.PebbleStore.GetMrc20TickInfo(tickId, "")
	if err != nil {
		ctx.String(200, "fail: "+err.Error())
		return
	}

	ctx.HTML(200, "home/mrc20info.html", gin.H{"Tick": tick, "Active": "mrc20"})
}

func mrc20Holders(ctx *gin.Context) {
	page, err := strconv.ParseInt(ctx.Param("page"), 10, 64)
	if err != nil || page < 1 {
		page = 1
	}
	pageSize := 20

	tickId := ctx.Param("id")
	if tickId == "" {
		ctx.String(200, "fail: id is required")
		return
	}

	searchAddress := ctx.Query("address")

	// 获取 tick 信息
	tick, _ := man.PebbleStore.GetMrc20TickInfo(tickId, "")

	list, err := man.PebbleStore.GetMrc20Holders(tickId, int((page-1)*int64(pageSize)), pageSize, searchAddress)
	if err != nil {
		ctx.String(200, "fail: "+err.Error())
		return
	}

	prePage := page - 1
	nextPage := page + 1
	if len(list) < pageSize {
		nextPage = 0
	}
	if prePage <= 0 {
		prePage = 0
	}

	ctx.HTML(200, "home/mrc20holders.html", gin.H{
		"List":          list,
		"TickId":        tickId,
		"TickName":      tick.Tick,
		"SearchAddress": searchAddress,
		"Offset":        int((page - 1) * int64(pageSize)),
		"Active":        "mrc20",
		"NextPage":      nextPage,
		"PrePage":       prePage,
	})
}

func mrc20History(ctx *gin.Context) {
	page, err := strconv.ParseInt(ctx.Param("page"), 10, 64)
	if err != nil || page < 1 {
		page = 1
	}
	pageSize := 20

	tickId := ctx.Param("id")
	if tickId == "" {
		ctx.String(200, "fail: id is required")
		return
	}

	// 新架构：使用 Transaction 流水表查询全局历史（跨链统一查询）
	list, _, err := man.PebbleStore.GetMrc20TransactionHistory("", tickId, pageSize, int((page-1)*int64(pageSize)))
	if err != nil {
		ctx.String(200, "fail: "+err.Error())
		return
	}

	prePage := page - 1
	nextPage := page + 1
	if len(list) < pageSize {
		nextPage = 0
	}
	if prePage <= 0 {
		prePage = 0
	}

	ctx.HTML(200, "home/mrc20history.html", gin.H{"List": list, "Tick": tickId, "Active": "", "NextPage": nextPage, "PrePage": prePage})
}

func mrc20AddressHistory(ctx *gin.Context) {
	page, err := strconv.ParseInt(ctx.Param("page"), 10, 64)
	if err != nil || page < 1 {
		page = 1
	}
	pageSize := 20

	tickId := ctx.Param("id")
	address := ctx.Param("address")
	if tickId == "" || address == "" {
		ctx.String(200, "fail: id and address are required")
		return
	}

	// 获取 tick 信息
	tick, _ := man.PebbleStore.GetMrc20TickInfo(tickId, "")

	// 使用 Transaction 流水表查询（跨链统一查询，每个 UTXO 一条流水记录）
	list, _, err := man.PebbleStore.GetMrc20TransactionHistory(address, tickId, pageSize, int((page-1)*int64(pageSize)))
	if err != nil {
		ctx.String(200, "fail: "+err.Error())
		return
	}

	prePage := page - 1
	nextPage := page + 1
	if len(list) < pageSize {
		nextPage = 0
	}
	if prePage <= 0 {
		prePage = 0
	}

	ctx.HTML(200, "home/mrc20addrhistory.html", gin.H{
		"List":     list,
		"TickId":   tickId,
		"TickName": tick.Tick,
		"Address":  address,
		"Active":   "mrc20",
		"NextPage": nextPage,
		"PrePage":  prePage,
	})
}

func mrc721List(ctx *gin.Context) {
	//TODO mrc721 pagination
	// page, err := strconv.ParseInt(ctx.Param("page"), 10, 64)
	// if err != nil {
	// 	ctx.String(200, "fail")
	// 	return
	// }
	// cousor := (page - 1) * 100
	// list, _, err := mrc721.GetMrc721CollectionList([]string{}, cousor, 100, false)
	// if err != nil {
	// 	ctx.String(200, "fail")
	// 	return
	// }
	// prePage := page - 1
	// nextPage := page + 1
	// if len(list) == 0 {
	// 	nextPage = 0
	// }
	// if prePage <= 0 {
	// 	prePage = 0
	// }
	// ctx.HTML(200, "home/mrc721.html", gin.H{"List": list, "Active": "mrc721", "NextPage": nextPage, "PrePage": prePage})
}

// mrc721ItemList
func mrc721ItemList(ctx *gin.Context) {
	//TODO mrc721 item pagination
	// page, err := strconv.ParseInt(ctx.Param("page"), 10, 64)
	// if err != nil {
	// 	ctx.String(200, "fail")
	// 	return
	// }

	// if ctx.Param("name") == "" {
	// 	ctx.String(200, "fail")
	// 	return
	// }
	// cousor := (page - 1) * 20
	// list, _, err := mrc721.GetMrc721ItemList(ctx.Param("name"), "", []string{}, cousor, 20, false)
	// if err != nil {
	// 	ctx.String(200, "fail")
	// 	return
	// }
	// prePage := page - 1
	// nextPage := page + 1
	// if len(list) == 0 {
	// 	nextPage = 0
	// }
	// if prePage <= 0 {
	// 	prePage = 0
	// }
	// ctx.HTML(200, "home/mrc721item.html", gin.H{"List": list, "Name": ctx.Param("name"), "Active": "", "NextPage": nextPage, "PrePage": prePage})
}
