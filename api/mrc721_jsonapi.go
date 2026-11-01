package api

import (
	"net/http"
	"strconv"

	"man-p2p/api/respond"
	"man-p2p/man"
	"man-p2p/pebblestore"

	"github.com/gin-gonic/gin"
)

// MRC-721 JSON API,设计见 MRC721_INDEXING_DESIGN.md §5。
// 端点路径/参数/响应结构兼容旧 MetaSo /api/mrc721/*,便于调用方平滑切换;
// 另含 item/transfers、item/verify 两个新端点。分页 cursor 同时接受旧拼写 cousor。

func mrc721JsonApi(r *gin.Engine) {
	group := r.Group("/api/mrc721")
	group.Use(CorsMiddleware())
	group.GET("/collection/pageList", mrc721CollectionPageList)
	group.GET("/collection/info", mrc721CollectionInfo)
	group.GET("/collection/items/pageList", mrc721CollectionItemsPageList)
	group.GET("/address/collection", mrc721AddressCollection)
	group.GET("/address/item", mrc721AddressItem)
	group.GET("/item/info", mrc721ItemInfo)
	group.GET("/item/transfers", mrc721ItemTransfers)
	group.GET("/item/verify", mrc721ItemVerify)
}

func mrc721DB() *pebblestore.Database {
	if man.PebbleStore == nil || man.PebbleStore.Database == nil {
		return nil
	}
	return man.PebbleStore.Database
}

func mrc721QueryCursor(ctx *gin.Context) int64 {
	v := ctx.Query("cousor")
	if v == "" {
		v = ctx.Query("cursor")
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func mrc721QuerySize(ctx *gin.Context) int64 {
	n, err := strconv.ParseInt(ctx.Query("size"), 10, 64)
	if err != nil || n <= 0 {
		return 20
	}
	if n > 200 {
		return 200
	}
	return n
}

// mrc721CollectionDTO 字段名兼容旧 MetaSo(collectionname/pinid/createtime/totalnum 等),
// 并同时提供 camelCase 别名(IDBots 契约)与链来源字段。
func mrc721CollectionDTO(col *pebblestore.Mrc721Collection) gin.H {
	return gin.H{
		"collectionname":   col.CollectionName,
		"collectionName":   col.CollectionName,
		"name":             col.Name,
		"totalsupply":      col.TotalSupply,
		"totalSupply":      col.TotalSupply,
		"royaltyrate":      col.RoyaltyRate,
		"royaltyRate":      col.RoyaltyRate,
		"desc":             col.Desc,
		"website":          col.Website,
		"cover":            col.Cover,
		"metadata":         col.Metadata,
		"pinid":            col.PinId,
		"address":          col.CreateAddress,
		"metaid":           col.CreateMetaId,
		"createAddress":    col.CreateAddress,
		"createMetaId":     col.CreateMetaId,
		"createtime":       col.CreateTime,
		"createTime":       col.CreateTime,
		"totalnum":         col.TotalNum,
		"chainName":        col.GenesisChainName,
		"genesisChainName": col.GenesisChainName,
		"genesisHeight":    col.GenesisHeight,
		"genesisTx":        col.GenesisTx,
	}
}

// mrc721ItemDTO 字段名兼容旧 MetaSo(collectionPinId/itemPinId/txOutput 等),
// 归属字段 address/metaId 为当前持有人;扩展 burned/transferCount/链来源。
func mrc721ItemDTO(item *pebblestore.Mrc721Item) gin.H {
	return gin.H{
		"collectionPinId":  item.CollectionPinId,
		"collectionName":   item.CollectionName,
		"itemPinId":        item.ItemPinId,
		"itemPinNumber":    item.Number,
		"number":           item.Number,
		"name":             item.Name,
		"desc":             item.Desc,
		"cover":            item.Cover,
		"metaData":         item.Metadata,
		"metadata":         item.Metadata,
		"createTime":       item.CreateTime,
		"address":          item.OwnerAddress,
		"metaId":           item.OwnerMetaId,
		"ownerAddress":     item.OwnerAddress,
		"ownerMetaId":      item.OwnerMetaId,
		"creatorMinted":    item.CreatorMinted,
		"contentType":      item.ContentType,
		"contentString":    item.ContentSummary,
		"contentSummary":   item.ContentSummary,
		"txOutput":         item.Output,
		"txValue":          item.OutputValue,
		"output":           item.Output,
		"outputValue":      item.OutputValue,
		"burned":           item.Burned,
		"transferCount":    item.TransferCount,
		"chainName":        item.GenesisChainName,
		"genesisChainName": item.GenesisChainName,
		"genesisHeight":    item.GenesisHeight,
		"genesisTx":        item.GenesisTx,
	}
}

func mrc721CollectionPageList(ctx *gin.Context) {
	db := mrc721DB()
	if db == nil {
		ctx.JSON(http.StatusOK, respond.ErrServiceError)
		return
	}
	list, total, err := db.ListMrc721Collections(mrc721QueryCursor(ctx), mrc721QuerySize(ctx))
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrServiceError)
		return
	}
	dtos := make([]gin.H, 0, len(list))
	for _, col := range list {
		dtos = append(dtos, mrc721CollectionDTO(col))
	}
	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{"list": dtos, "total": total}))
}

func mrc721CollectionInfo(ctx *gin.Context) {
	db := mrc721DB()
	if db == nil {
		ctx.JSON(http.StatusOK, respond.ErrServiceError)
		return
	}
	name := ctx.Query("name")
	pinId := ctx.Query("pinId")
	if name == "" && pinId == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	var col *pebblestore.Mrc721Collection
	var err error
	if name != "" {
		col, err = db.GetMrc721CollectionByName(name)
	} else {
		col, err = db.GetMrc721CollectionByPinId(pinId)
	}
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrServiceError)
		return
	}
	if col == nil {
		ctx.JSON(http.StatusOK, respond.ErrNoDataFound)
		return
	}
	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", mrc721CollectionDTO(col)))
}

func mrc721CollectionItemsPageList(ctx *gin.Context) {
	db := mrc721DB()
	if db == nil {
		ctx.JSON(http.StatusOK, respond.ErrServiceError)
		return
	}
	// 兼容:pinid=collectionid(旧 MetaSo 语义);name=专辑名
	pinId := ctx.Query("pinid")
	collectionName := ctx.Query("name")
	if pinId == "" && collectionName == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	if collectionName == "" {
		col, err := db.GetMrc721CollectionByPinId(pinId)
		if err != nil {
			ctx.JSON(http.StatusOK, respond.ErrServiceError)
			return
		}
		if col == nil {
			ctx.JSON(http.StatusOK, respond.ErrNoDataFound)
			return
		}
		collectionName = col.CollectionName
	}
	cursor, size := mrc721QueryCursor(ctx), mrc721QuerySize(ctx)
	var pinIds []string
	var pos int64
	err := db.IterateMrc721CollectionItems(collectionName, func(seq int64, itemPinId string) bool {
		if pos >= cursor+size {
			return false
		}
		if pos >= cursor {
			pinIds = append(pinIds, itemPinId)
		}
		pos++
		return true
	})
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrServiceError)
		return
	}
	col, _ := db.GetMrc721CollectionByName(collectionName)
	var total int64
	if col != nil {
		total = col.TotalNum
	}
	dtos := make([]gin.H, 0, len(pinIds))
	for _, itemPinId := range pinIds {
		item, err := db.GetMrc721Item(itemPinId)
		if err != nil || item == nil {
			continue
		}
		dtos = append(dtos, mrc721ItemDTO(item))
	}
	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{"list": dtos, "total": total}))
}

func mrc721AddressCollection(ctx *gin.Context) {
	db := mrc721DB()
	if db == nil {
		ctx.JSON(http.StatusOK, respond.ErrServiceError)
		return
	}
	address := ctx.Query("address")
	if address == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	// 按持有人条目去重专辑,同时统计持有个数(ownnum)
	holdCounts := make(map[string]int64)
	var order []string
	err := db.IterateMrc721OwnerItems(address, "", func(collectionName, itemPinId string) bool {
		if _, seen := holdCounts[collectionName]; !seen {
			order = append(order, collectionName)
		}
		holdCounts[collectionName]++
		return true
	})
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrServiceError)
		return
	}
	cursor, size := mrc721QueryCursor(ctx), mrc721QuerySize(ctx)
	total := int64(len(order))
	dtos := make([]gin.H, 0)
	if cursor < total {
		end := cursor + size
		if end > total {
			end = total
		}
		for _, name := range order[cursor:end] {
			col, err := db.GetMrc721CollectionByName(name)
			if err != nil || col == nil {
				continue
			}
			dto := mrc721CollectionDTO(col)
			dto["ownnum"] = holdCounts[name]
			dtos = append(dtos, dto)
		}
	}
	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{"list": dtos, "total": total}))
}

func mrc721AddressItem(ctx *gin.Context) {
	db := mrc721DB()
	if db == nil {
		ctx.JSON(http.StatusOK, respond.ErrServiceError)
		return
	}
	address := ctx.Query("address")
	if address == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	// 可选按专辑过滤:pinId=collectionid 或 name=专辑名
	collectionFilter := ctx.Query("pinId")
	if collectionFilter != "" {
		col, err := db.GetMrc721CollectionByPinId(collectionFilter)
		if err != nil {
			ctx.JSON(http.StatusOK, respond.ErrServiceError)
			return
		}
		if col == nil {
			ctx.JSON(http.StatusOK, respond.ErrNoDataFound)
			return
		}
		collectionFilter = col.CollectionName
	} else {
		collectionFilter = ctx.Query("name")
	}
	cursor, size := mrc721QueryCursor(ctx), mrc721QuerySize(ctx)
	dtos := make([]gin.H, 0)
	var total int64
	err := db.IterateMrc721OwnerItems(address, collectionFilter, func(collectionName, itemPinId string) bool {
		if total < cursor || total >= cursor+size {
			total++
			return true
		}
		item, err := db.GetMrc721Item(itemPinId)
		if err != nil || item == nil {
			total++
			return true
		}
		dtos = append(dtos, mrc721ItemDTO(item))
		total++
		return true
	})
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrServiceError)
		return
	}
	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{"list": dtos, "total": total}))
}

func mrc721ItemInfo(ctx *gin.Context) {
	db := mrc721DB()
	if db == nil {
		ctx.JSON(http.StatusOK, respond.ErrServiceError)
		return
	}
	pinId := ctx.Query("pinId")
	if pinId == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	item, err := db.GetMrc721Item(pinId)
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrServiceError)
		return
	}
	if item == nil {
		ctx.JSON(http.StatusOK, respond.ErrNoDataFound)
		return
	}
	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", mrc721ItemDTO(item)))
}

func mrc721ItemTransfers(ctx *gin.Context) {
	db := mrc721DB()
	if db == nil {
		ctx.JSON(http.StatusOK, respond.ErrServiceError)
		return
	}
	pinId := ctx.Query("pinId")
	if pinId == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	cursor, size := mrc721QueryCursor(ctx), mrc721QuerySize(ctx)
	list := make([]gin.H, 0)
	var total int64
	err := db.IterateMrc721ItemTransfers(pinId, func(tr *pebblestore.Mrc721ItemTransfer) bool {
		if total >= cursor && total < cursor+size {
			list = append(list, gin.H{
				"itemPinId":   tr.ItemPinId,
				"fromAddress": tr.FromAddress,
				"toAddress":   tr.ToAddress,
				"chainName":   tr.ChainName,
				"height":      tr.Height,
				"txId":        tr.TxId,
				"time":        tr.Time,
			})
		}
		total++
		return true
	})
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrServiceError)
		return
	}
	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{"list": list, "total": total}))
}

func mrc721ItemVerify(ctx *gin.Context) {
	db := mrc721DB()
	if db == nil {
		ctx.JSON(http.StatusOK, respond.ErrServiceError)
		return
	}
	pinId := ctx.Query("pinId")
	if pinId == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	item, err := db.GetMrc721Item(pinId)
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrServiceError)
		return
	}
	if item == nil {
		// 未收录:返回事实集,由展示层判定(collected 为 IDBots 契约别名)
		ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{"indexed": false, "collected": false, "pinId": pinId, "itemPinId": pinId}))
		return
	}
	genesisChainName, genesisHeight, genesisTx := "", int64(0), ""
	if col, _ := db.GetMrc721CollectionByName(item.CollectionName); col != nil {
		genesisChainName, genesisHeight, genesisTx = col.GenesisChainName, col.GenesisHeight, col.GenesisTx
	}
	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{
		"indexed":          true,
		"collected":        true,
		"pinId":            pinId,
		"itemPinId":        pinId,
		"collectionName":   item.CollectionName,
		"collectionid":     item.CollectionPinId,
		"collectionPinId":  item.CollectionPinId,
		"creatorMinted":    item.CreatorMinted,
		"burned":           item.Burned,
		"transferCount":    item.TransferCount,
		"ownerAddress":     item.OwnerAddress,
		"ownerMetaId":      item.OwnerMetaId,
		"genesisChainName": item.GenesisChainName,
		"genesisHeight":    item.GenesisHeight,
		"genesisTx":        item.GenesisTx,
		"itemGenesis": gin.H{
			"chainName": item.GenesisChainName,
			"height":    item.GenesisHeight,
			"tx":        item.GenesisTx,
		},
		"collectionGenesis": gin.H{
			"chainName": genesisChainName,
			"height":    genesisHeight,
			"tx":        genesisTx,
		},
	}))
}
