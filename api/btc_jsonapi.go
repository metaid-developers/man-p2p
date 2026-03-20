package api

import (
	"fmt"
	"man-p2p/api/respond"
	"man-p2p/common"
	"man-p2p/man"
	"man-p2p/pin"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type ApiResponse struct {
	Code int         `json:"code"`
	Msg  string      `json:"message"`
	Data interface{} `json:"data"`
}

func btcJsonApi(r *gin.Engine) {
	btcGroup := r.Group("/api")
	btcGroup.Use(CorsMiddleware())
	btcGroup.GET("/metaid/list", metaidList)
	btcGroup.GET("/pin/list", pinList)
	btcGroup.GET("/block/list", blockList)
	btcGroup.GET("/mempool/list", mempoolList)
	btcGroup.GET("/notifcation/list", notifcationList)
	btcGroup.GET("/block/file", blockFileGet)
	btcGroup.GET("/block/file/partCount", blockPartCount)
	btcGroup.GET("/block/file/create", blockFileCreate)
	btcGroup.GET("/block/id/list", blockIdList)
	btcGroup.GET("/block/id/create", setPinIdList)

	btcGroup.GET("/pin/:numberOrId", getPinById)
	btcGroup.GET("/pin/ver/:pinid/:ver", pinHistory)
	btcGroup.GET("/pin/path/list", getAllPinByPath)
	btcGroup.GET("/address/pin/list/:address", getPinListByAddress)
	btcGroup.GET("/metaid/pin/list/:metaid", getAllPinByPathAndMetaId)
	btcGroup.GET("/info/address/:address", getInfoByAddress)
	btcGroup.GET("/info/metaid/:metaId", getInfoByMetaId)

	// Alias routes for IDBots compatibility (spec section 5.2)
	v1 := r.Group("/api/v1")
	v1.Use(CorsMiddleware())
	v1.GET("/users/info/metaid/:metaId", getInfoByMetaId)
	v1.GET("/users/info/address/:address", getInfoByAddress)

}

func metaidList(ctx *gin.Context) {
	page, err := strconv.ParseInt(ctx.Query("page"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	size, err := strconv.ParseInt(ctx.Query("size"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	list, err := man.PebbleStore.Database.MetaIdPageList(page-1, size)
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{"list": list, "count": 0}))
		return
	}
	sortParams := parseSortParams(ctx, "number")
	sortMetaIdInfoList(list, sortParams)
	count := man.PebbleStore.GetAllCount().MetaId
	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{"list": list, "count": count}))
}

func pinList(ctx *gin.Context) {
	page, err := strconv.Atoi(ctx.Query("page"))
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	//size, err := strconv.ParseInt(ctx.Query("size"), 10, 64)
	size, err := strconv.Atoi(ctx.Query("size"))
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	list, lastId, err := man.PebbleStore.PinPageList(page-1, size, ctx.Query("lastId"))
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{"Pins": []pin.PinMsg{}, "Count": 0, "Active": "index", "LastId": ""}))
		return
	}
	var msg []*pin.PinMsg
	for _, p := range list {
		pmsg := &pin.PinMsg{
			Content: p.ContentSummary, Number: p.Number, Operation: p.Operation,
			Id: p.Id, Type: p.ContentTypeDetect, Path: p.Path, MetaId: p.MetaId,
			Pop: p.Pop, ChainName: p.ChainName,
			InitialOwner: p.InitialOwner, Address: p.Address, CreateAddress: p.CreateAddress,
			Timestamp: p.Timestamp,
		}
		msg = append(msg, pmsg)
	}
	sortParams := parseSortParams(ctx, "timestamp")
	sortPinMsgList(msg, sortParams)
	count := man.PebbleStore.GetAllCount()
	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{"Pins": msg, "Count": &count, "Active": "index", "LastId": lastId}))
}
func mempoolList(ctx *gin.Context) {
	page, err := strconv.ParseInt(ctx.Query("page"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	size, err := strconv.ParseInt(ctx.Query("size"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	list, err := man.PebbleStore.Database.GetMempoolPageList(page-1, size)
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{"Pins": []pin.PinMsg{}, "Count": 0, "Active": "mempool"}))
		return
	}
	var msg []*pin.PinMsg
	for _, p := range list {
		pmsg := &pin.PinMsg{
			Content:   p.ContentSummary,
			Number:    p.Number,
			Operation: p.Operation,
			Id:        p.Id,
			Type:      p.ContentTypeDetect,
			Path:      p.Path,
			MetaId:    p.MetaId,
			Timestamp: p.Timestamp,
		}
		msg = append(msg, pmsg)
	}
	sortParams := parseSortParams(ctx, "timestamp")
	sortPinMsgList(msg, sortParams)
	count := man.PebbleStore.GetAllCount()
	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{"Pins": msg, "Count": &count, "Active": "mempool"}))
}

// get pin by id
func getPinById(ctx *gin.Context) {
	pinMsg, err := man.PebbleStore.GetPinById(ctx.Param("numberOrId"))
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", pinMsg))
		return
	}
	//pinMsg.ContentBody = []byte{}
	pinMsg.ContentSummary = string(pinMsg.ContentBody)
	pinMsg.PopLv, _ = pin.PopLevelCount(pinMsg.ChainName, pinMsg.Pop)
	pinMsg.Preview = common.Config.Web.Host + "/pin/" + pinMsg.Id
	pinMsg.Content = common.Config.Web.Host + "/content/" + pinMsg.Id
	// check, err := man.DbAdapter.GetMempoolTransferById(pinMsg.Id)
	// if err == nil && check != nil {
	// 	pinMsg.Status = -9
	// }
	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", pinMsg))
}

func blockList(ctx *gin.Context) {
	// TODO Paginated query for block list
	// page, err := strconv.ParseInt(ctx.Query("page"), 10, 64)
	// if err != nil {
	// 	ctx.JSON(http.StatusOK, respond.ErrParameterError)
	// 	return
	// }
	// size, err := strconv.ParseInt(ctx.Query("size"), 10, 64)
	// if err != nil {
	// 	ctx.JSON(http.StatusOK, respond.ErrParameterError)
	// 	return
	// }
	// list, err := man.DbAdapter.GetPinPageList(page, size)
	// if err != nil || list == nil {
	// 	if err == mongo.ErrNoDocuments {
	// 		ctx.JSON(http.StatusOK, respond.ErrNoDataFound)
	// 	} else {
	// 		ctx.JSON(http.StatusOK, respond.ErrServiceError)
	// 	}
	// 	return
	// }
	// msgMap := make(map[int64][]*pin.PinMsg)
	// var msgList []int64
	// for _, p := range list {
	// 	pmsg := &pin.PinMsg{Operation: p.Operation, Path: p.Path, Content: p.ContentSummary, Number: p.Number, Id: p.Id, Type: p.ContentTypeDetect, MetaId: p.MetaId, Height: p.GenesisHeight, Pop: p.Pop}
	// 	if _, ok := msgMap[pmsg.Height]; ok {
	// 		msgMap[pmsg.Height] = append(msgMap[pmsg.Height], pmsg)
	// 	} else {
	// 		msgMap[pmsg.Height] = []*pin.PinMsg{pmsg}
	// 		msgList = append(msgList, pmsg.Height)
	// 	}
	// }
	// ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{"msgMap": msgMap, "msgList": msgList, "Active": "blocks"}))
}

// get pin list by address
func getPinListByAddress(ctx *gin.Context) {
	sizeStr := ctx.Query("size")
	if sizeStr == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	path := ctx.Query("path")
	if path == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	address := ctx.Param("address")
	if address == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	metaid := common.GetMetaIdByAddress(address)
	size, _ := strconv.ParseInt(sizeStr, 10, 64)
	// Limit the maximum size
	if size <= 0 || size > 100 {
		size = 20
	}
	// Use cursor-based pagination
	cursor := ctx.Query("cursor")
	pinList, total, nextCursor, err := man.PebbleStore.GetPinByMetaIdAndPathPageList(metaid, path, cursor, size)
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrServiceError)
		return
	}
	var fixPinList []*pin.PinInscription
	for _, pinNode := range pinList {
		// ContentSummary 已在数据库层设置
		pinNode.Preview = common.Config.Web.Host + "/pin/" + pinNode.Id
		pinNode.Content = common.Config.Web.Host + "/content/" + pinNode.Id
		pinNode.PopLv, _ = pin.PopLevelCount(pinNode.ChainName, pinNode.Pop)
		fixPinList = append(fixPinList, pinNode)
	}
	sortParams := parseSortParams(ctx, "timestamp")
	sortPinInscriptionList(fixPinList, sortParams)
	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{"list": fixPinList, "total": total, "nextCursor": nextCursor}))
}

type metaInfo struct {
	*pin.MetaIdInfo
	Unconfirmed string `json:"unconfirmed"`
	Blocked     bool   `json:"blocked"`
}

func getInfoByAddress(ctx *gin.Context) {
	address := ctx.Param("address")
	if address == "" {
		ctx.JSON(http.StatusOK, respond.ErrAddressIsEmpty)
		return
	}
	metaid := common.GetMetaIdByAddress(address)
	info, err := man.PebbleStore.Database.GetMetaidInfo(metaid)
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrServiceError)
		return
	}
	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", metaInfo{info, "", false}))
}

func getInfoByMetaId(ctx *gin.Context) {
	metaid := ctx.Param("metaId")
	if metaid == "" {
		ctx.JSON(http.StatusOK, respond.ErrAddressIsEmpty)
		return
	}
	info, err := man.PebbleStore.Database.GetMetaidInfo(metaid)
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrServiceError)
		return
	}
	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", metaInfo{info, "", false}))
}

func getAllPinByPath(ctx *gin.Context) {
	sizeStr := ctx.Query("size")
	if sizeStr == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	path := ctx.Query("path")
	if path == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	size, _ := strconv.ParseInt(sizeStr, 10, 64)
	// Limit the maximum size to prevent querying too much data
	if size <= 0 || size > 100 {
		size = 20 // Default to 20 items
	}
	// 使用 cursor 作为 lastKey（可选参数，首次查询为空）
	cursor := ctx.Query("cursor")
	pinList1, total, nextCursor, err := man.PebbleStore.GetAllPinByPathPageList(ctx.Query("path"), cursor, size)
	//fmt.Println(pinList1)
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrNoPinFound)
		return
	}
	sortParams := parseSortParams(ctx, "timestamp")
	sortPinInscriptionList(pinList1, sortParams)
	// Return list, total count, and next page cursor
	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{"list": pinList1, "total": total, "nextCursor": nextCursor}))
}

// getAllPinByPathAndMetaId
type pinQuery struct {
	Page       int64    `json:"page"`
	Size       int64    `json:"size"`
	Path       string   `json:"path"`
	MetaIdList []string `json:"metaIdList"`
}

func getAllPinByPathAndMetaId(ctx *gin.Context) {
	cursorStr := ctx.Query("cursor")
	if cursorStr == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	sizeStr := ctx.Query("size")
	if sizeStr == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	path := ctx.Query("path")
	if path == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	metaid := ctx.Param("metaid")
	if metaid == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	size := int64(10)
	if sizeStr != "" {
		size, _ = strconv.ParseInt(sizeStr, 10, 64)
	}
	if size <= 0 || size > 100 {
		size = 20
	}
	// Use cursor-based pagination
	cursor := cursorStr
	pinList, total, nextCursor, err := man.PebbleStore.GetPinByMetaIdAndPathPageList(metaid, path, cursor, size)
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrServiceError)
		return
	}
	var fixPinList []*pin.PinInscription
	for _, pinNode := range pinList {
		// ContentSummary 已在数据库层设置
		pinNode.Preview = common.Config.Web.Host + "/pin/" + pinNode.Id
		pinNode.Content = common.Config.Web.Host + "/content/" + pinNode.Id
		pinNode.PopLv, _ = pin.PopLevelCount(pinNode.ChainName, pinNode.Pop)
		fixPinList = append(fixPinList, pinNode)
	}
	sortParams := parseSortParams(ctx, "timestamp")
	sortPinInscriptionList(fixPinList, sortParams)
	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{"list": fixPinList, "total": total, "nextCursor": nextCursor}))
}

// notifcationList address=xx&lastId=100&size=10
func notifcationList(ctx *gin.Context) {
	address := ctx.Query("address")
	if address == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	lastId, _ := strconv.ParseInt(ctx.Query("lastId"), 10, 64)
	size, _ := strconv.Atoi(ctx.Query("size"))
	if size <= 0 || size > 100 {
		size = 20 // Default to 20 items
	}

	// Use the new V2 query method
	list, total, err := man.PebbleStore.Database.GetNotifcationListV2(address, lastId, size)
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrServiceError)
		return
	}
	sortParams := parseSortParams(ctx, "notifcationtime")
	sortNotifcationList(list, sortParams)

	ctx.JSON(http.StatusOK, gin.H{"code": 200, "message": "ok", "data": list, "total": total})
}

func blockFileGet(ctx *gin.Context) {
	heightStr := ctx.Query("height")
	if heightStr == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	height, err := strconv.ParseInt(heightStr, 10, 64)
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	chainName := ctx.Query("chain")
	if chainName == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	partIndexStr := ctx.Query("part")
	partIndex := 0
	if partIndexStr != "" {
		partIndex, err = strconv.Atoi(partIndexStr)
		if err != nil {
			ctx.JSON(http.StatusOK, respond.ErrParameterError)
			return
		}
	}

	// 获取文件路径
	filePath := man.GetBlockFilePath(chainName, height, partIndex)
	if _, err := os.Stat(filePath); err != nil {
		ctx.JSON(http.StatusOK, respond.ApiError(404, "File does not exist"))
		return
	}

	// Set download response headers
	ctx.Header("Content-Type", "application/octet-stream")
	ctx.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filepath.Base(filePath)))
	ctx.File(filePath)
}

// Query the number of block shard files for a specific block
func blockPartCount(ctx *gin.Context) {
	heightStr := ctx.Query("height")
	if heightStr == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	height, err := strconv.ParseInt(heightStr, 10, 64)
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	chainName := ctx.Query("chain")
	if chainName == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}

	// Iterate through shard files and count them
	dirPath := filepath.Join(
		common.Config.Pebble.Dir+"/blockFiles",
		strconv.FormatInt(height/1000000, 10),
		strconv.FormatInt((height%1000000)/1000, 10),
	)
	prefix := chainName + "_" + strconv.FormatInt(height, 10) + "_"
	count := 0
	files, err := os.ReadDir(dirPath)
	if err == nil {
		for _, f := range files {
			if !f.IsDir() && strings.HasPrefix(f.Name(), prefix) && strings.HasSuffix(f.Name(), ".dat.zst") {
				count++
			}
		}
	}
	btcMin, btcMax, _ := man.GetFileMetaHeight("btc")
	mvcMin, mvcMax, _ := man.GetFileMetaHeight("mvc")

	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{"partCount": count, "btcMin": btcMin, "btcMax": btcMax, "mvcMin": mvcMin, "mvcMax": mvcMax}))
}
func blockFileCreate(ctx *gin.Context) {
	token := ctx.Query("token")
	if token != common.Config.AdminToken || token == "" {
		ctx.JSON(http.StatusOK, "error token")
		return
	}
	chainName := ctx.Query("chain")
	if chainName == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	from, _ := strconv.ParseInt(ctx.Query("from"), 10, 64)
	to, _ := strconv.ParseInt(ctx.Query("to"), 10, 64)
	for i := from; i <= to; i++ {
		man.SaveBlockFileFromChain(chainName, i)
	}
	ctx.String(http.StatusOK, "block file create finish")
}

// SetPinIdList
func setPinIdList(ctx *gin.Context) {
	token := ctx.Query("token")
	if token != common.Config.AdminToken || token == "" {
		ctx.JSON(http.StatusOK, "error token")
		return
	}
	chainName := ctx.Query("chain")
	if chainName == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	from, _ := strconv.ParseInt(ctx.Query("from"), 10, 64)
	to, _ := strconv.ParseInt(ctx.Query("to"), 10, 64)
	for i := from; i <= to; i++ {
		man.PebbleStore.SetPinIdList(chainName, i)
	}
	ctx.String(http.StatusOK, "block file pin id list create finish")
}
func blockIdList(ctx *gin.Context) {
	token := ctx.Query("token")
	if token != common.Config.AdminToken || token == "" {
		ctx.JSON(http.StatusOK, "error token")
		return
	}
	chainName := ctx.Query("chain")
	if chainName == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	height, err := strconv.ParseInt(ctx.Query("height"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	blockIds, err := man.GetBlockIdList(chainName, int(height))
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrServiceError)
		return
	}
	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{"data": blockIds}))
}

func pinHistory(ctx *gin.Context) {
	//pinMsg, err := man.DbAdapter.GetPinByNumberOrId(ctx.Param("number"))
	var pinMsg pin.PinInscription
	pinId := ctx.Param("pinid")
	verStr := ctx.Param("ver")
	ver, err := strconv.Atoi(verStr)
	if err != nil || pinId == "" || verStr == "" {
		ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", pinMsg))
		return
	}
	pinMsg, err = man.PebbleStore.GetPinById(pinId)
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", pinMsg))
		return
	}
	if ver == 0 {
		pinMsg.ContentSummary = pinMsg.OriginalContentSummary
		pinMsg.ContentBody = pinMsg.OriginalContentBody
		ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", pinMsg))
		return
	}
	if ver >= len(pinMsg.ModifyHistory) {
		ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", pinMsg))
		return
	}
	var verPinMsg pin.PinInscription
	verPinMsg, err = man.PebbleStore.GetPinById(pinMsg.ModifyHistory[ver])
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", pinMsg))
		return
	}
	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", verPinMsg))
}
