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
// （目录默认按 path 字母序）。快照走 stale-while-revalidate：有过往快照即即时
// 应答；过期或 refresh=1 只触发后台重建（请求绝不等重建——生产全量扫描
// 46–60s，同步重建曾撞 nginx 60s 代理线 504）。
//
// builtAt（Unix 秒）：当前应答快照的【构建时刻】语义不变。命中盘上持久化
// 快照（进程重启后首请求即命中）时返回该快照的原构建时刻——它早于重启点，
// 语义上如实表达"这份聚合数据算齐于彼时"，不因重启拨新；SWR 随后照旧
// 触发后台重建并在落账时换新 builtAt。响应结构与字段集零变化。
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
