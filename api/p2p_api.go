package api

import (
	"man-p2p/api/respond"
	"man-p2p/p2p"
	"net/http"

	"github.com/gin-gonic/gin"
)

func RegisterP2PRoutes(r *gin.Engine) {
	r.POST("/api/config/reload", configReload)
	r.GET("/api/p2p/status", p2pStatus)
	r.GET("/api/p2p/peers", p2pPeers)
	r.GET("/api/p2p/presence", p2pPresence)
}

func configReload(ctx *gin.Context) {
	if err := p2p.ReloadConfig(); err != nil {
		ctx.JSON(http.StatusInternalServerError, respond.ApiError(500, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{"status": "reloaded"}))
}

func p2pStatus(ctx *gin.Context) {
	ctx.JSON(200, respond.ApiSuccess(1, "ok", p2p.GetStatus()))
}

func p2pPeers(ctx *gin.Context) {
	ctx.JSON(200, respond.ApiSuccess(1, "ok", p2p.GetPeers()))
}

func p2pPresence(ctx *gin.Context) {
	status := p2p.GetPresenceStatus()
	ctx.JSON(http.StatusOK, respond.ApiSuccess(1, "ok", gin.H{
		"healthy":               status.Healthy,
		"peerCount":             status.PeerCount,
		"unhealthyReason":       status.UnhealthyReason,
		"lastConfigReloadError": status.LastConfigReloadError,
		"nowSec":                status.NowSec,
		"onlineBots":            status.OnlineBots,
	}))
}
