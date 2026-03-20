package api

import (
	"man-p2p/api/respond"
	"man-p2p/p2p"

	"github.com/gin-gonic/gin"
)

func RegisterP2PRoutes(r *gin.Engine) {
	r.POST("/api/config/reload", configReload)
	r.GET("/api/p2p/status", p2pStatus)
	r.GET("/api/p2p/peers", p2pPeers)
}

func configReload(ctx *gin.Context) {
	if err := p2p.ReloadConfig(); err != nil {
		ctx.JSON(500, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(200, gin.H{"status": "reloaded"})
}

func p2pStatus(ctx *gin.Context) {
	ctx.JSON(200, respond.ApiSuccess(1, "ok", p2p.GetStatus()))
}

func p2pPeers(ctx *gin.Context) {
	ctx.JSON(200, respond.ApiSuccess(1, "ok", p2p.GetPeers()))
}
