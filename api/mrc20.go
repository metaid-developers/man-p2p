package api

import (
	"fmt"
	"log"
	"man-p2p/api/respond"
	"man-p2p/common"
	"man-p2p/man"
	"man-p2p/mrc20"
	"net/http"
	"strconv"
	"strings"

	"github.com/bytedance/sonic"
	"github.com/cockroachdb/pebble"
	"github.com/gin-gonic/gin"
)

func mrc20JsonApi(r *gin.Engine) {
	mrc20Group := r.Group("/api/mrc20")
	mrc20Group.Use(CorsMiddleware())
	mrc20Group.GET("/tick/all", allTick)
	mrc20Group.GET("/tick/info/:id", getTickInfoById)
	mrc20Group.GET("/tick/info", getTickInfo)
	mrc20Group.GET("/tick/address", getHistoryByAddress)
	mrc20Group.GET("/tick/history", getHistoryById)
	mrc20Group.GET("/address/balance/:address", getBalanceByAddress)
	mrc20Group.GET("/address/history/:tickId/:address", getAddressHistoryByTickAndAddress)
	mrc20Group.GET("/tx/history", getHistoryByTx)
	mrc20Group.GET("/tick/AddressBalance", getAddressBalance)

	// 管理接口
	adminGroup := mrc20Group.Group("/admin")
	adminGroup.GET("/index-height/:chain", getIndexHeight)
	adminGroup.GET("/index-height/:chain/set", setIndexHeight)
	adminGroup.GET("/reindex-block/:chain/:height", reindexBlock)
	adminGroup.GET("/reindex-range/:chain/:start/:end", reindexBlockRange)
	adminGroup.GET("/reindex-from/:chain/:height", reindexFromHeight)
	adminGroup.GET("/recalculate-balance/:chain/:address/:tickId", recalculateBalance)
	adminGroup.GET("/verify-balance/:chain/:address/:tickId", verifyBalance)
	adminGroup.GET("/fix-pending/:chain", fixPendingUtxos)

	// Teleport 诊断和修复接口
	adminGroup.GET("/teleport/list-pending", listPendingTeleports)
	adminGroup.GET("/teleport/diagnose/:coord", diagnoseTeleport)
	adminGroup.GET("/teleport/check-arrival/:pinId", checkArrivalByPinId)
	adminGroup.GET("/teleport/check-asset-index/:assetOutpoint", checkArrivalIndex)
	adminGroup.POST("/teleport/fix/:coord", fixTeleport)

	// Teleport V2 接口
	adminGroup.GET("/teleport/v2/list", listTeleportTransactionsV2)
	adminGroup.GET("/teleport/v2/detail/:id", getTeleportTransactionV2)

	// 资产验证接口
	adminGroup.GET("/verify/supply/:tickId", verifySupply)
	adminGroup.GET("/verify/all", verifyAllSupply)

	// 快照管理接口
	adminGroup.POST("/snapshot/create", createSnapshot)
	adminGroup.GET("/snapshot/list", listSnapshots)
	adminGroup.GET("/snapshot/info/:id", getSnapshotInfo)
	adminGroup.POST("/snapshot/restore/:id", restoreSnapshot)
	adminGroup.DELETE("/snapshot/:id", deleteSnapshot)

	// 调试接口
	debugGroup := mrc20Group.Group("/debug")
	debugGroup.GET("/pending-in/:address", debugPendingIn)
	debugGroup.GET("/utxo-status/:address/:tickId", debugUtxoStatus)
}

func allTick(ctx *gin.Context) {
	cursor, err := strconv.ParseInt(ctx.Query("cursor"), 10, 64)
	if err != nil {
		cursor = 0
	}
	size, err := strconv.ParseInt(ctx.Query("size"), 10, 64)
	if err != nil {
		size = 20
	}
	// PebbleStore 方法不支持 order/completed/orderType 参数，返回全量数据
	list, err := man.PebbleStore.GetMrc20TickList(int(cursor), int(size))
	if err != nil || list == nil || len(list) == 0 {
		ctx.JSON(http.StatusOK, respond.ErrNoDataFound)
		return
	}
	sortParams := parseSortParams(ctx, "deploytime")
	sortMrc20DeployInfoList(list, sortParams)
	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{"list": list, "total": len(list)}))
}
func getTickInfoById(ctx *gin.Context) {
	info, err := man.PebbleStore.GetMrc20TickInfo(ctx.Param("id"), "")
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrNoResultFound)
		return
	}
	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", info))
}
func getTickInfo(ctx *gin.Context) {
	info, err := man.PebbleStore.GetMrc20TickInfo(ctx.Query("id"), ctx.Query("tick"))
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrNoResultFound)
		return
	}
	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", info))
}
func getHistoryByAddress(ctx *gin.Context) {
	cursor, err := strconv.ParseInt(ctx.Query("cursor"), 10, 64)
	if err != nil {
		cursor = 0
	}
	size, err := strconv.ParseInt(ctx.Query("size"), 10, 64)
	if err != nil {
		size = 20
	}
	tickId := ctx.Query("tickId")
	address := ctx.Query("address")

	// 状态参数，默认为空返回所有状态
	statusStr := ctx.Query("status")
	var statusFilter *int
	if statusStr != "" {
		status, err := strconv.Atoi(statusStr)
		if err == nil {
			statusFilter = &status
		}
	}

	// 验证参数，默认为空返回所有验证状态
	verifyStr := ctx.Query("verify")
	var verifyFilter *bool
	if verifyStr != "" {
		if verifyStr == "true" || verifyStr == "1" {
			verify := true
			verifyFilter = &verify
		} else if verifyStr == "false" || verifyStr == "0" {
			verify := false
			verifyFilter = &verify
		}
	}

	list, total, err := man.PebbleStore.GetMrc20AddressHistory(tickId, address, int(cursor), int(size), statusFilter, verifyFilter)
	if err != nil || list == nil || len(list) == 0 {
		ctx.JSON(http.StatusOK, respond.ErrNoDataFound)
		return
	}
	sortParams := parseSortParams(ctx, "timestamp")
	sortMrc20UtxoList(list, sortParams)
	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{"list": list, "total": total}))
}
func getHistoryById(ctx *gin.Context) {
	cursor, err := strconv.ParseInt(ctx.Query("cursor"), 10, 64)
	if err != nil {
		cursor = 0
	}
	size, err := strconv.ParseInt(ctx.Query("size"), 10, 64)
	if err != nil {
		size = 20
	}
	tickId := ctx.Query("tickId")

	// 新架构：使用 Transaction 流水表（跨链统一查询）
	// 查询该 tick 的所有交易（不限地址）
	list, total, err := man.PebbleStore.GetMrc20TransactionHistory("", tickId, int(size), int(cursor))
	if err != nil || list == nil || len(list) == 0 {
		ctx.JSON(http.StatusOK, respond.ErrNoDataFound)
		return
	}
	sortParams := parseSortParams(ctx, "timestamp")
	sortMrc20TransactionList(list, sortParams)
	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{"list": list, "total": total}))
}

func getBalanceByAddress(ctx *gin.Context) {
	address := ctx.Param("address")
	cursor, err := strconv.ParseInt(ctx.Query("cursor"), 10, 64)
	if err != nil {
		cursor = 0
	}
	size, err := strconv.ParseInt(ctx.Query("size"), 10, 64)
	if err != nil {
		size = 20
	}

	// 可选的 chain 参数，如果不传则查询所有链
	chainFilter := ctx.Query("chain")

	// 💡 新架构：基于 UTXO 实时计算余额，不使用 AccountBalance 表
	//log.Printf("[API] 💡 使用 UTXO 实时计算余额: address=%s, chain=%s", address, chainFilter)

	var balanceList []*man.MRC20Balance

	if chainFilter != "" {
		// 查询单条链
		balances, err := man.GetAddressBalances(chainFilter, address)
		if err != nil {
			log.Printf("[API] 计算余额失败: chain=%s, address=%s, error=%v", chainFilter, address, err)
		} else {
			balanceList = append(balanceList, balances...)
		}
	} else {
		// 查询所有链
		allBalances, err := man.GetAllChainsBalances(address)
		if err != nil {
			ctx.JSON(http.StatusOK, respond.ErrServiceError)
			return
		}

		// 合并所有链的余额
		for _, balances := range allBalances {
			balanceList = append(balanceList, balances...)
		}
	}

	if len(balanceList) == 0 {
		ctx.JSON(http.StatusOK, respond.ErrNoDataFound)
		return
	}

	// 转换为 API 响应格式（兼容旧接口）
	apiBalances := make([]*mrc20.Mrc20Balance, 0, len(balanceList))
	for _, b := range balanceList {
		apiBalances = append(apiBalances, &mrc20.Mrc20Balance{
			Id:                b.TickId,
			Name:              b.Tick,
			Chain:             b.Chain,
			Balance:           b.Balance,
			PendingOutBalance: b.PendingOut,
			PendingInBalance:  b.PendingIn,
		})
	}

	sortParams := parseSortParams(ctx, "id")
	sortMrc20BalanceList(apiBalances, sortParams)

	// 分页
	total := int64(len(apiBalances))
	start := int(cursor)
	end := int(cursor + size)
	if start >= len(apiBalances) {
		ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{"list": []*mrc20.Mrc20Balance{}, "total": total}))
		return
	}
	if end > len(apiBalances) {
		end = len(apiBalances)
	}
	apiBalances = apiBalances[start:end]

	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{"list": apiBalances, "total": total}))
}
func getHistoryByTx(ctx *gin.Context) {
	txId := ctx.Query("txId")
	if txId == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}

	// 获取 index 参数（可选）
	indexStr := ctx.Query("index")
	var targetIndex *int
	if indexStr != "" {
		if idx, err := strconv.Atoi(indexStr); err == nil {
			targetIndex = &idx
		}
	}

	// 通过 txId 查找 UTXO
	if targetIndex != nil {
		// 如果指定了 index，查找特定的 txPoint (txId:index)
		txPoint := fmt.Sprintf("%s:%d", txId, *targetIndex)
		utxo, err := man.PebbleStore.CheckOperationtxByTxPoint(txPoint, false)
		if err != nil || utxo == nil {
			ctx.JSON(http.StatusOK, respond.ErrNoDataFound)
			return
		}
		sortParams := parseSortParams(ctx, "timestamp")
		sortMrc20UtxoList([]*mrc20.Mrc20Utxo{utxo}, sortParams)
		ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{"list": []*mrc20.Mrc20Utxo{utxo}, "total": 1}))
	} else {
		// 如果没有指定 index，返回该交易的所有 UTXO
		utxos, err := man.PebbleStore.CheckOperationtxAll(txId, false)
		if err != nil || len(utxos) == 0 {
			ctx.JSON(http.StatusOK, respond.ErrNoDataFound)
			return
		}
		sortParams := parseSortParams(ctx, "timestamp")
		sortMrc20UtxoList(utxos, sortParams)
		ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{"list": utxos, "total": len(utxos)}))
	}
}
func getAddressHistoryByTickAndAddress(ctx *gin.Context) {
	tickId := ctx.Param("tickId")
	address := ctx.Param("address")
	if tickId == "" || address == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}

	cursor, err := strconv.ParseInt(ctx.Query("cursor"), 10, 64)
	if err != nil {
		cursor = 0
	}
	size, err := strconv.ParseInt(ctx.Query("size"), 10, 64)
	if err != nil {
		size = 20
	}

	// 新架构：使用 Transaction 流水表（跨链统一查询）
	list, total, err := man.PebbleStore.GetMrc20TransactionHistory(address, tickId, int(size), int(cursor))
	if err != nil || list == nil || len(list) == 0 {
		ctx.JSON(http.StatusOK, respond.ErrNoDataFound)
		return
	}
	sortParams := parseSortParams(ctx, "timestamp")
	sortMrc20TransactionList(list, sortParams)
	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{"list": list, "total": total}))
}

func getAddressBalance(ctx *gin.Context) {
	address := ctx.Query("address")
	tickId := ctx.Query("tickId")
	if address == "" || tickId == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}

	chain := ctx.Query("chain")
	if chain == "" {
		chain = "btc" // 默认 BTC
	}

	// 💡 新架构：基于 UTXO 实时计算余额，不使用 AccountBalance 表
	//log.Printf("[API] 💡 使用 UTXO 实时计算余额: chain=%s, address=%s, tickId=%s", chain, address, tickId)

	balance, err := man.CalculateBalanceFromUTXO(chain, address, tickId)
	if err != nil {
		log.Printf("[API] 计算余额失败: %v", err)
		ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{
			"balance":    "0",
			"pendingIn":  "0",
			"pendingOut": "0",
		}))
		return
	}

	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{
		"balance":    balance.Balance.String(),
		"pendingIn":  balance.PendingIn.String(),
		"pendingOut": balance.PendingOut.String(),
	}))
}

// getIndexHeight 获取指定链的 MRC20 索引高度
func getIndexHeight(ctx *gin.Context) {
	chainName := strings.ToLower(ctx.Param("chain"))

	// 验证链名称
	if !isValidChain(chainName) {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}

	// 获取当前索引高度
	currentHeight := man.PebbleStore.GetMrc20IndexHeight(chainName)

	// 获取配置中的 mrc20Height
	var configHeight int64
	switch chainName {
	case "btc", "bitcoin":
		configHeight = common.Config.Btc.Mrc20Height
	case "doge", "dogecoin":
		configHeight = common.Config.Doge.Mrc20Height
	case "mvc":
		configHeight = common.Config.Mvc.Mrc20Height
	default:
		configHeight = 0
	}

	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{
		"chain":         chainName,
		"currentHeight": currentHeight,
		"configHeight":  configHeight,
	}))
}

// setIndexHeight 设置指定链的 MRC20 索引高度
func setIndexHeight(ctx *gin.Context) {
	chainName := strings.ToLower(ctx.Param("chain"))

	// 验证链名称
	if !isValidChain(chainName) {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}

	// 从查询参数获取数据
	heightStr := ctx.Query("height")
	token := ctx.Query("token")
	reason := ctx.Query("reason")

	// 验证 height 参数
	if heightStr == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}

	height, err := strconv.ParseInt(heightStr, 10, 64)
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}

	// 简单的 token 验证（可以根据需要加强）
	if common.Config.AdminToken != "" && token != common.Config.AdminToken {
		ctx.JSON(http.StatusUnauthorized, &respond.ApiResponse{
			Code: 401,
			Msg:  "Unauthorized: invalid admin token",
			Data: nil,
		})
		return
	}

	// 获取当前高度
	currentHeight := man.PebbleStore.GetMrc20IndexHeight(chainName)

	// 验证新高度是否合理
	if height < 0 {
		ctx.JSON(http.StatusOK, &respond.ApiResponse{
			Code: -1,
			Msg:  "Height cannot be negative",
			Data: nil,
		})
		return
	}

	// 记录日志
	log.Printf("[ADMIN] MRC20 index height change for %s: %d -> %d, reason: %s",
		chainName, currentHeight, height, reason)

	// 保存新的索引高度
	err = man.PebbleStore.SaveMrc20IndexHeight(chainName, height)
	if err != nil {
		log.Printf("Failed to save MRC20 index height for %s: %v", chainName, err)
		ctx.JSON(http.StatusOK, &respond.ApiResponse{
			Code: -1,
			Msg:  fmt.Sprintf("Failed to save index height: %v", err),
			Data: nil,
		})
		return
	}

	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{
		"chain":     chainName,
		"oldHeight": currentHeight,
		"newHeight": height,
		"reason":    reason,
		"message":   "MRC20 index height updated successfully",
	}))
}

// isValidChain 验证链名称是否有效
func isValidChain(chainName string) bool {
	validChains := []string{"btc", "bitcoin", "doge", "dogecoin", "mvc"}
	for _, valid := range validChains {
		if chainName == valid {
			return true
		}
	}
	return false
}

// ============== 调试接口 ==============

// debugPendingIn 查看指定地址的所有 TransferPendingIn 记录
func debugPendingIn(ctx *gin.Context) {
	address := ctx.Param("address")
	if address == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}

	// 查询 transfer pending in
	transferPendingIns, err := man.PebbleStore.GetTransferPendingInByAddress(address)
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{
			"error":   err.Error(),
			"records": []interface{}{},
			"count":   0,
		}))
		return
	}

	// 也查询 teleport pending in
	teleportPendingIns, _ := man.PebbleStore.GetTeleportPendingInByAddress(address)

	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{
		"transferPendingIn": transferPendingIns,
		"teleportPendingIn": teleportPendingIns,
		"transferCount":     len(transferPendingIns),
		"teleportCount":     len(teleportPendingIns),
	}))
}

// debugUtxoStatus 查看指定地址和 tick 的 UTXO 状态
func debugUtxoStatus(ctx *gin.Context) {
	address := ctx.Param("address")
	tickId := ctx.Param("tickId")
	if address == "" || tickId == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}

	type UtxoInfo struct {
		TxPoint     string `json:"txPoint"`
		Amount      string `json:"amount"`
		Status      int    `json:"status"`
		StatusName  string `json:"statusName"`
		FromAddress string `json:"fromAddress"`
		ToAddress   string `json:"toAddress"`
		BlockHeight int64  `json:"blockHeight"`
	}

	var utxos []UtxoInfo

	prefix := []byte(fmt.Sprintf("mrc20_in_%s_%s_", address, tickId))
	iter, err := man.PebbleStore.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{
			"error": err.Error(),
			"utxos": []interface{}{},
		}))
		return
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		var utxo mrc20.Mrc20Utxo
		if err := sonic.Unmarshal(iter.Value(), &utxo); err != nil {
			continue
		}

		statusName := "Unknown"
		switch utxo.Status {
		case 0:
			statusName = "Available"
		case 1:
			statusName = "TeleportPending"
		case 2:
			statusName = "TransferPending"
		case -1:
			statusName = "Spent"
		}

		utxos = append(utxos, UtxoInfo{
			TxPoint:     utxo.TxPoint,
			Amount:      utxo.AmtChange.String(),
			Status:      utxo.Status,
			StatusName:  statusName,
			FromAddress: utxo.FromAddress,
			ToAddress:   utxo.ToAddress,
			BlockHeight: utxo.BlockHeight,
		})
	}

	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{
		"address": address,
		"tickId":  tickId,
		"utxos":   utxos,
		"count":   len(utxos),
	}))
}

// ============== 区块重跑管理接口 ==============

// reindexBlock 重跑单个区块
// GET /api/mrc20/admin/reindex-block/:chain/:height?token=xxx
func reindexBlock(ctx *gin.Context) {
	chainName := strings.ToLower(ctx.Param("chain"))
	heightStr := ctx.Param("height")
	token := ctx.Query("token")

	// 验证链名
	if !isValidChain(chainName) {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}

	// 验证 token
	if common.Config.AdminToken != "" && token != common.Config.AdminToken {
		ctx.JSON(http.StatusUnauthorized, &respond.ApiResponse{
			Code: 401,
			Msg:  "Unauthorized: invalid admin token",
			Data: nil,
		})
		return
	}

	height, err := strconv.ParseInt(heightStr, 10, 64)
	if err != nil || height <= 0 {
		ctx.JSON(http.StatusOK, &respond.ApiResponse{
			Code: -1,
			Msg:  "Invalid height",
			Data: nil,
		})
		return
	}

	log.Printf("[ADMIN] ReindexBlock request: chain=%s, height=%d", chainName, height)

	// 执行重跑
	err = man.PebbleStore.ReindexBlock(chainName, height)
	if err != nil {
		log.Printf("[ADMIN] ReindexBlock failed: %v", err)
		ctx.JSON(http.StatusOK, &respond.ApiResponse{
			Code: -1,
			Msg:  fmt.Sprintf("ReindexBlock failed: %v", err),
			Data: nil,
		})
		return
	}

	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{
		"chain":   chainName,
		"height":  height,
		"message": "Block reindexed successfully",
	}))
}

// reindexBlockRange 重跑区块范围
// GET /api/mrc20/admin/reindex-range/:chain/:start/:end?token=xxx
func reindexBlockRange(ctx *gin.Context) {
	chainName := strings.ToLower(ctx.Param("chain"))
	startStr := ctx.Param("start")
	endStr := ctx.Param("end")
	token := ctx.Query("token")

	// 验证链名
	if !isValidChain(chainName) {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}

	// 验证 token
	if common.Config.AdminToken != "" && token != common.Config.AdminToken {
		ctx.JSON(http.StatusUnauthorized, &respond.ApiResponse{
			Code: 401,
			Msg:  "Unauthorized: invalid admin token",
			Data: nil,
		})
		return
	}

	startHeight, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil || startHeight <= 0 {
		ctx.JSON(http.StatusOK, &respond.ApiResponse{
			Code: -1,
			Msg:  "Invalid start height",
			Data: nil,
		})
		return
	}

	endHeight, err := strconv.ParseInt(endStr, 10, 64)
	if err != nil || endHeight <= 0 || endHeight < startHeight {
		ctx.JSON(http.StatusOK, &respond.ApiResponse{
			Code: -1,
			Msg:  "Invalid end height (must be >= start height)",
			Data: nil,
		})
		return
	}

	// 限制最大范围，避免长时间阻塞
	maxRange := int64(100)
	if endHeight-startHeight > maxRange {
		ctx.JSON(http.StatusOK, &respond.ApiResponse{
			Code: -1,
			Msg:  fmt.Sprintf("Range too large, max allowed: %d blocks", maxRange),
			Data: nil,
		})
		return
	}

	log.Printf("[ADMIN] ReindexBlockRange request: chain=%s, start=%d, end=%d", chainName, startHeight, endHeight)

	// 执行重跑
	err = man.PebbleStore.ReindexBlockRange(chainName, startHeight, endHeight)
	if err != nil {
		log.Printf("[ADMIN] ReindexBlockRange failed: %v", err)
		ctx.JSON(http.StatusOK, &respond.ApiResponse{
			Code: -1,
			Msg:  fmt.Sprintf("ReindexBlockRange failed: %v", err),
			Data: nil,
		})
		return
	}

	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{
		"chain":       chainName,
		"startHeight": startHeight,
		"endHeight":   endHeight,
		"blockCount":  endHeight - startHeight + 1,
		"message":     "Block range reindexed successfully",
	}))
}

// reindexFromHeight 从指定高度重跑（真正幂等）
// GET /api/mrc20/admin/reindex-from/:chain/:height?token=xxx
// 该接口会：
// 1. 删除所有 BlockHeight >= height 的 UTXO
// 2. 恢复所有 SpentAtHeight >= height 的 UTXO
// 3. 设置索引高度为 height - 1
// 调用后需要重启服务让主循环重新索引
func reindexFromHeight(ctx *gin.Context) {
	chainName := strings.ToLower(ctx.Param("chain"))
	heightStr := ctx.Param("height")
	token := ctx.Query("token")

	// 验证链名
	if !isValidChain(chainName) {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}

	// 验证 token
	if common.Config.AdminToken != "" && token != common.Config.AdminToken {
		ctx.JSON(http.StatusUnauthorized, &respond.ApiResponse{
			Code: 401,
			Msg:  "Unauthorized: invalid admin token",
			Data: nil,
		})
		return
	}

	height, err := strconv.ParseInt(heightStr, 10, 64)
	if err != nil || height <= 0 {
		ctx.JSON(http.StatusOK, &respond.ApiResponse{
			Code: -1,
			Msg:  "Invalid height",
			Data: nil,
		})
		return
	}

	log.Printf("[ADMIN] ReindexFromHeight request: chain=%s, height=%d", chainName, height)

	// 执行重跑
	stats, err := man.PebbleStore.ReindexFromHeight(chainName, height)
	if err != nil {
		log.Printf("[ADMIN] ReindexFromHeight failed: %v", err)
		ctx.JSON(http.StatusOK, &respond.ApiResponse{
			Code: -1,
			Msg:  fmt.Sprintf("ReindexFromHeight failed: %v", err),
			Data: nil,
		})
		return
	}

	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{
		"chain":                    chainName,
		"fromHeight":               height,
		"newIndexHeight":           height - 1,
		"deleted":                  stats["deleted"],
		"restored":                 stats["restored"],
		"pendingFixed":             stats["pendingFixed"],
		"balanceCleared":           stats["balanceCleared"],
		"pendingTeleportCleared":   stats["pendingTeleportCleared"],
		"arrivalCleared":           stats["arrivalCleared"],
		"teleportPendingInCleared": stats["teleportPendingInCleared"],
		"transferPendingInCleared": stats["transferPendingInCleared"],
		"message":                  "Reindex prepared. Restart service to reindex from the specified height.",
	}))
}

// recalculateBalance 重算指定地址余额
// GET /api/mrc20/admin/recalculate-balance/:chain/:address/:tickId?token=xxx
func recalculateBalance(ctx *gin.Context) {
	chainName := strings.ToLower(ctx.Param("chain"))
	address := ctx.Param("address")
	tickId := ctx.Param("tickId")
	token := ctx.Query("token")

	// 验证链名
	if !isValidChain(chainName) {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}

	// 验证 token
	if common.Config.AdminToken != "" && token != common.Config.AdminToken {
		ctx.JSON(http.StatusUnauthorized, &respond.ApiResponse{
			Code: 401,
			Msg:  "Unauthorized: invalid admin token",
			Data: nil,
		})
		return
	}

	if address == "" || tickId == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}

	log.Printf("[ADMIN] RecalculateBalance request: chain=%s, address=%s, tickId=%s", chainName, address, tickId)

	// 获取重算前的余额
	oldBalance, _ := man.PebbleStore.GetMrc20AccountBalance(chainName, address, tickId)
	var oldBalanceStr string
	if oldBalance != nil {
		oldBalanceStr = oldBalance.Balance.String()
	} else {
		oldBalanceStr = "0"
	}

	// 执行重算
	err := man.PebbleStore.RecalculateBalance(chainName, address, tickId)
	if err != nil {
		log.Printf("[ADMIN] RecalculateBalance failed: %v", err)
		ctx.JSON(http.StatusOK, &respond.ApiResponse{
			Code: -1,
			Msg:  fmt.Sprintf("RecalculateBalance failed: %v", err),
			Data: nil,
		})
		return
	}

	// 获取重算后的余额
	newBalance, _ := man.PebbleStore.GetMrc20AccountBalance(chainName, address, tickId)
	var newBalanceStr string
	if newBalance != nil {
		newBalanceStr = newBalance.Balance.String()
	} else {
		newBalanceStr = "0"
	}

	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{
		"chain":      chainName,
		"address":    address,
		"tickId":     tickId,
		"oldBalance": oldBalanceStr,
		"newBalance": newBalanceStr,
		"message":    "Balance recalculated successfully",
	}))
}

// verifyBalance 验证余额是否正确（缓存与 UTXO 一致）
// GET /api/mrc20/admin/verify-balance/:chain/:address/:tickId?token=xxx
func verifyBalance(ctx *gin.Context) {
	chainName := strings.ToLower(ctx.Param("chain"))
	address := ctx.Param("address")
	tickId := ctx.Param("tickId")
	token := ctx.Query("token")

	// 验证链名
	if !isValidChain(chainName) {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}

	// 验证 token（验证操作不需要严格认证，可以放宽）
	if common.Config.AdminToken != "" && token != common.Config.AdminToken {
		// 验证不需要严格认证，但记录日志
		log.Printf("[ADMIN] VerifyBalance without token: chain=%s, address=%s, tickId=%s", chainName, address, tickId)
	}

	if address == "" || tickId == "" {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}

	// 执行验证
	isValid, err := man.PebbleStore.VerifyBalance(chainName, address, tickId)
	if err != nil {
		ctx.JSON(http.StatusOK, &respond.ApiResponse{
			Code: -1,
			Msg:  fmt.Sprintf("VerifyBalance failed: %v", err),
			Data: nil,
		})
		return
	}

	// 获取当前缓存余额
	cachedBalance, _ := man.PebbleStore.GetMrc20AccountBalance(chainName, address, tickId)
	var cachedBalanceStr string
	if cachedBalance != nil {
		cachedBalanceStr = cachedBalance.Balance.String()
	} else {
		cachedBalanceStr = "0"
	}

	status := "VALID"
	if !isValid {
		status = "MISMATCH"
	}

	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{
		"chain":         chainName,
		"address":       address,
		"tickId":        tickId,
		"cachedBalance": cachedBalanceStr,
		"status":        status,
		"isValid":       isValid,
	}))
}

// fixPendingUtxos 修复 pending 状态的 UTXO
// GET /api/mrc20/admin/fix-pending/:chain?token=xxx
func fixPendingUtxos(ctx *gin.Context) {
	chainName := strings.ToLower(ctx.Param("chain"))
	token := ctx.Query("token")

	// 验证链名
	if !isValidChain(chainName) {
		ctx.JSON(http.StatusOK, respond.ErrParameterError)
		return
	}

	// 验证 token
	if common.Config.AdminToken != "" && token != common.Config.AdminToken {
		ctx.JSON(http.StatusUnauthorized, &respond.ApiResponse{
			Code: 401,
			Msg:  "Unauthorized: invalid admin token",
			Data: nil,
		})
		return
	}

	log.Printf("[ADMIN] FixPendingUtxos request: chain=%s", chainName)

	// 执行修复
	fixedCount, err := man.PebbleStore.FixPendingUtxoStatus(chainName)
	if err != nil {
		log.Printf("[ADMIN] FixPendingUtxos failed: %v", err)
		ctx.JSON(http.StatusOK, &respond.ApiResponse{
			Code: -1,
			Msg:  fmt.Sprintf("FixPendingUtxos failed: %v", err),
			Data: nil,
		})
		return
	}

	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{
		"chain":      chainName,
		"fixedCount": fixedCount,
		"message":    fmt.Sprintf("Fixed %d pending UTXOs", fixedCount),
	}))
}

// ============== 快照管理接口 ==============

// createSnapshot 创建快照
// POST /api/mrc20/admin/snapshot/create?token=xxx
// Body: {"description": "snapshot description"}
func createSnapshot(ctx *gin.Context) {
	token := ctx.Query("token")

	// 验证 token
	if common.Config.AdminToken != "" && token != common.Config.AdminToken {
		ctx.JSON(http.StatusUnauthorized, &respond.ApiResponse{
			Code: 401,
			Msg:  "Unauthorized: invalid admin token",
			Data: nil,
		})
		return
	}

	// 获取描述
	var req struct {
		Description string `json:"description"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		req.Description = ctx.Query("description")
	}
	if req.Description == "" {
		req.Description = "Manual snapshot"
	}

	snapshotDir := man.GetSnapshotDir(common.Config.Pebble.Dir)
	log.Printf("[ADMIN] CreateSnapshot request: dir=%s, description=%s", snapshotDir, req.Description)

	// 执行快照创建
	metadata, err := man.PebbleStore.CreateSnapshot(snapshotDir, req.Description)
	if err != nil {
		log.Printf("[ADMIN] CreateSnapshot failed: %v", err)
		ctx.JSON(http.StatusOK, &respond.ApiResponse{
			Code: -1,
			Msg:  fmt.Sprintf("CreateSnapshot failed: %v", err),
			Data: nil,
		})
		return
	}

	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{
		"snapshotId":   metadata.ID,
		"createdAt":    metadata.CreatedAt,
		"description":  metadata.Description,
		"chainHeights": metadata.ChainHeights,
		"recordCounts": metadata.RecordCounts,
		"fileSize":     metadata.FileSize,
		"message":      "Snapshot created successfully",
	}))
}

// listSnapshots 列出所有快照
// GET /api/mrc20/admin/snapshot/list?token=xxx
func listSnapshots(ctx *gin.Context) {
	token := ctx.Query("token")

	// 验证 token
	if common.Config.AdminToken != "" && token != common.Config.AdminToken {
		ctx.JSON(http.StatusUnauthorized, &respond.ApiResponse{
			Code: 401,
			Msg:  "Unauthorized: invalid admin token",
			Data: nil,
		})
		return
	}

	snapshotDir := man.GetSnapshotDir(common.Config.Pebble.Dir)
	snapshots, err := man.ListSnapshots(snapshotDir)
	if err != nil {
		log.Printf("[ADMIN] ListSnapshots failed: %v", err)
		ctx.JSON(http.StatusOK, &respond.ApiResponse{
			Code: -1,
			Msg:  fmt.Sprintf("ListSnapshots failed: %v", err),
			Data: nil,
		})
		return
	}

	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{
		"snapshots": snapshots,
		"count":     len(snapshots),
	}))
}

// getSnapshotInfo 获取快照信息
// GET /api/mrc20/admin/snapshot/info/:id?token=xxx
func getSnapshotInfo(ctx *gin.Context) {
	snapshotID := ctx.Param("id")
	token := ctx.Query("token")

	// 验证 token
	if common.Config.AdminToken != "" && token != common.Config.AdminToken {
		ctx.JSON(http.StatusUnauthorized, &respond.ApiResponse{
			Code: 401,
			Msg:  "Unauthorized: invalid admin token",
			Data: nil,
		})
		return
	}

	snapshotDir := man.GetSnapshotDir(common.Config.Pebble.Dir)
	metadata, err := man.GetSnapshotInfo(snapshotDir, snapshotID)
	if err != nil {
		ctx.JSON(http.StatusOK, &respond.ApiResponse{
			Code: -1,
			Msg:  fmt.Sprintf("Snapshot not found: %v", err),
			Data: nil,
		})
		return
	}

	// 验证快照完整性
	verifyErr := man.VerifySnapshot(snapshotDir, snapshotID)

	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{
		"snapshotId":   metadata.ID,
		"createdAt":    metadata.CreatedAt,
		"description":  metadata.Description,
		"chainHeights": metadata.ChainHeights,
		"recordCounts": metadata.RecordCounts,
		"fileSize":     metadata.FileSize,
		"valid":        verifyErr == nil,
		"verifyError": func() string {
			if verifyErr != nil {
				return verifyErr.Error()
			} else {
				return ""
			}
		}(),
	}))
}

// restoreSnapshot 从快照恢复
// POST /api/mrc20/admin/snapshot/restore/:id?token=xxx
// 警告：此操作会清空现有 MRC20 数据！
func restoreSnapshot(ctx *gin.Context) {
	snapshotID := ctx.Param("id")
	token := ctx.Query("token")

	// 验证 token
	if common.Config.AdminToken != "" && token != common.Config.AdminToken {
		ctx.JSON(http.StatusUnauthorized, &respond.ApiResponse{
			Code: 401,
			Msg:  "Unauthorized: invalid admin token",
			Data: nil,
		})
		return
	}

	snapshotDir := man.GetSnapshotDir(common.Config.Pebble.Dir)
	snapshotPath := snapshotDir + "/" + snapshotID

	log.Printf("[ADMIN] RestoreSnapshot request: id=%s", snapshotID)

	// 先验证快照
	if err := man.VerifySnapshot(snapshotDir, snapshotID); err != nil {
		ctx.JSON(http.StatusOK, &respond.ApiResponse{
			Code: -1,
			Msg:  fmt.Sprintf("Snapshot verification failed: %v", err),
			Data: nil,
		})
		return
	}

	// 执行恢复
	metadata, err := man.PebbleStore.RestoreSnapshot(snapshotPath)
	if err != nil {
		log.Printf("[ADMIN] RestoreSnapshot failed: %v", err)
		ctx.JSON(http.StatusOK, &respond.ApiResponse{
			Code: -1,
			Msg:  fmt.Sprintf("RestoreSnapshot failed: %v", err),
			Data: nil,
		})
		return
	}

	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{
		"snapshotId":   metadata.ID,
		"createdAt":    metadata.CreatedAt,
		"chainHeights": metadata.ChainHeights,
		"message":      "Snapshot restored successfully. Please restart the indexer to continue from the snapshot height.",
	}))
}

// deleteSnapshot 删除快照
// DELETE /api/mrc20/admin/snapshot/:id?token=xxx
func deleteSnapshot(ctx *gin.Context) {
	snapshotID := ctx.Param("id")
	token := ctx.Query("token")

	// 验证 token
	if common.Config.AdminToken != "" && token != common.Config.AdminToken {
		ctx.JSON(http.StatusUnauthorized, &respond.ApiResponse{
			Code: 401,
			Msg:  "Unauthorized: invalid admin token",
			Data: nil,
		})
		return
	}

	snapshotDir := man.GetSnapshotDir(common.Config.Pebble.Dir)

	log.Printf("[ADMIN] DeleteSnapshot request: id=%s", snapshotID)

	if err := man.DeleteSnapshot(snapshotDir, snapshotID); err != nil {
		ctx.JSON(http.StatusOK, &respond.ApiResponse{
			Code: -1,
			Msg:  fmt.Sprintf("DeleteSnapshot failed: %v", err),
			Data: nil,
		})
		return
	}

	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{
		"snapshotId": snapshotID,
		"message":    "Snapshot deleted successfully",
	}))
}

// listPendingTeleports 列出所有pending状态的teleport
// GET /api/mrc20/admin/teleport/list-pending
func listPendingTeleports(ctx *gin.Context) {
	// V1 API - deprecated in V2
	ctx.JSON(http.StatusOK, respond.ApiError(0, "listPendingTeleports (V1) is deprecated, use V2 teleport APIs"))
}

// diagnoseTeleport 诊断指定的teleport
// GET /api/mrc20/admin/teleport/diagnose/:coord
func diagnoseTeleport(ctx *gin.Context) {
	// V1 API - deprecated in V2
	ctx.JSON(http.StatusOK, respond.ApiError(0, "diagnoseTeleport (V1) is deprecated, use V2 teleport APIs"))
}

// checkArrivalIndex 检查 arrival 在索引中的状态
func checkArrivalIndex(ctx *gin.Context) {
	assetOutpoint := ctx.Query("assetOutpoint")
	if assetOutpoint == "" {
		ctx.JSON(http.StatusBadRequest, respond.ApiError(0, "assetOutpoint parameter is required"))
		return
	}

	result := gin.H{
		"assetOutpoint": assetOutpoint,
	}

	// 1. 查询索引
	assetKey := fmt.Sprintf("arrival_asset_%s", assetOutpoint)
	pinIdBytes, closer, err := man.PebbleStore.Database.MrcDb.Get([]byte(assetKey))

	if err != nil {
		result["indexExists"] = false
		result["indexError"] = err.Error()
	} else {
		closer.Close()
		indexedPinId := string(pinIdBytes)
		result["indexExists"] = true
		result["indexedPinId"] = indexedPinId

		// 2. 查询这个PinId对应的arrival是否存在
		arrival, err2 := man.PebbleStore.GetMrc20ArrivalByPinId(indexedPinId)
		if err2 != nil {
			result["arrivalExists"] = false
			result["arrivalError"] = err2.Error()
		} else if arrival == nil {
			result["arrivalExists"] = false
			result["arrivalError"] = "arrival is nil"
		} else {
			result["arrivalExists"] = true
			result["arrival"] = gin.H{
				"pinId":         arrival.PinId,
				"txId":          arrival.TxId,
				"assetOutpoint": arrival.AssetOutpoint,
				"amount":        arrival.Amount.String(),
				"toAddress":     arrival.ToAddress,
				"chain":         arrival.Chain,
				"status":        arrival.Status,
				"blockHeight":   arrival.BlockHeight,
			}
		}
	}

	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", result))
}

// checkArrivalByPinId 检查指定PinId的arrival是否存在（用于调试）
// GET /api/mrc20/admin/teleport/check-arrival/:pinId
func checkArrivalByPinId(ctx *gin.Context) {
	pinId := ctx.Param("pinId")
	if pinId == "" {
		ctx.JSON(http.StatusBadRequest, respond.ApiError(0, "pinId parameter is required"))
		return
	}

	// 尝试直接从数据库读取
	arrival, err := man.PebbleStore.GetMrc20ArrivalByPinId(pinId)

	result := gin.H{
		"pinId":       pinId,
		"queryMethod": "GetMrc20ArrivalByPinId",
	}

	if err != nil {
		result["exists"] = false
		result["error"] = err.Error()
	} else if arrival == nil {
		result["exists"] = false
		result["error"] = "arrival is nil"
	} else {
		result["exists"] = true
		result["arrival"] = gin.H{
			"pinId":         arrival.PinId,
			"txId":          arrival.TxId,
			"assetOutpoint": arrival.AssetOutpoint,
			"amount":        arrival.Amount.String(),
			"tickId":        arrival.TickId,
			"tick":          arrival.Tick,
			"locationIndex": arrival.LocationIndex,
			"toAddress":     arrival.ToAddress,
			"chain":         arrival.Chain,
			"sourceChain":   arrival.SourceChain,
			"status":        arrival.Status,
			"msg":           arrival.Msg,
			"blockHeight":   arrival.BlockHeight,
			"timestamp":     arrival.Timestamp,
		}
	}

	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", result))
}

// fixTeleport 修复指定的teleport
// POST /api/mrc20/admin/teleport/fix/:coord
func fixTeleport(ctx *gin.Context) {
	coord := ctx.Param("coord")
	if coord == "" {
		ctx.JSON(http.StatusBadRequest, respond.ApiError(0, "coord parameter is required"))
		return
	}

	// TODO: 重新实现 DiagnosePendingTeleport 函数
	// success, message := man.DiagnosePendingTeleport(coord)
	success := false
	message := "DiagnosePendingTeleport function temporarily disabled"

	if success {
		ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "Teleport fixed successfully", gin.H{
			"coord":   coord,
			"message": message,
		}))
	} else {
		ctx.JSON(http.StatusBadRequest, respond.ApiError(0, message))
	}
}
