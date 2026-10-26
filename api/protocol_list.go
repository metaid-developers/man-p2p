package api

import (
	"net/http"
	"strconv"

	"man-p2p/api/respond"
	"man-p2p/man"

	"github.com/gin-gonic/gin"
)

// protocolList GET /api/protocol/list
//
// 协议目录：聚合所有 /protocols/* 一级族路径（含未注册路径，子路径计数归入族），
// 每条返回 path / pinTotal / 首条上链 pin（txid、height、timestamp、chain、pinId）/
// 首现作者（firstAddress + firstGlobalMetaId）/ registered + registeredPinId。
//
// 分页遵循 /api/pin/list 惯例：page 从 1 起、size 默认 20 上限 200；
// 排序遵循 parseSortParams（sortBy + order），sortBy 支持
// path/pinTotal/firstTimestamp/firstHeight/firstTxid，未显式传 order 时默认升序
// （目录默认按 path 字母序）。目录快照带 10 分钟内存缓存，refresh=1 强制重建。
func protocolList(ctx *gin.Context) {
	page, err := strconv.ParseInt(ctx.Query("page"), 10, 64)
	if err != nil || page < 1 {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	size, err := strconv.ParseInt(ctx.Query("size"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}
	size = normalizePinListPageSize(size)

	refresh := ctx.Query("refresh") == "1"
	entries, builtAt, err := man.PebbleStore.ProtocolDirectory(refresh)
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrServiceError)
		return
	}

	sortParams := parseSortParams(ctx, "path")
	if ctx.Query("order") == "" && ctx.Query("sortOrder") == "" {
		sortParams.Desc = false // 目录未显式指定排序方向时默认升序
	}
	sortProtocolDirList(entries, sortParams)

	total := int64(len(entries))
	start := (page - 1) * size
	var pageList []man.ProtocolDirEntry
	if start < total {
		end := start + size
		if end > total {
			end = total
		}
		pageList = entries[start:end]
	}
	if pageList == nil {
		pageList = []man.ProtocolDirEntry{}
	}

	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{
		"list":    pageList,
		"count":   total,
		"page":    page,
		"size":    size,
		"builtAt": builtAt.Unix(),
	}))
}
