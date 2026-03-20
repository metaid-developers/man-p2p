package api

import (
	"net/http"
	"strconv"

	"man-p2p/api/respond"
	"man-p2p/man"

	"github.com/gin-gonic/gin"
)

// listTeleportTransactionsV2 列出所有TeleportTransaction (V2架构)
func listTeleportTransactionsV2(ctx *gin.Context) {
	limitStr := ctx.DefaultQuery("limit", "100")
	limit, _ := strconv.Atoi(limitStr)

	txs, err := man.ListAllTeleportTransactions(limit)
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ApiError(0, err.Error()))
		return
	}
	sortParams := parseSortParams(ctx, "createdat")
	sortTeleportTransactionList(txs, sortParams)

	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{
		"list":  txs,
		"total": len(txs),
	}))
}

// getTeleportTransactionV2 获取TeleportTransaction详情 (V2架构)
func getTeleportTransactionV2(ctx *gin.Context) {
	id := ctx.Param("id")

	tx, err := man.LoadTeleportTransaction(id)
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ApiError(0, "TeleportTransaction not found"))
		return
	}

	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", tx))
}

// verifySupply 验证某个代币的总供应量
func verifySupply(ctx *gin.Context) {
	tickId := ctx.Param("tickId")

	report, err := man.VerifyMRC20TotalSupply(tickId)
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ApiError(0, err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", report))
}

// verifyAllSupply 验证所有代币的总供应量
func verifyAllSupply(ctx *gin.Context) {
	reports, err := man.VerifyAllMRC20()
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ApiError(0, err.Error()))
		return
	}

	// 统计
	passed := 0
	failed := 0
	for _, report := range reports {
		if len(report.Errors) == 0 {
			passed++
		} else {
			failed++
		}
	}

	result := gin.H{
		"total":   len(reports),
		"passed":  passed,
		"failed":  failed,
		"reports": reports,
	}

	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", result))
}
