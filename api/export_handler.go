package api

import (
	"fmt"
	"io"
	"net/http"

	"man-p2p/api/respond"
	"man-p2p/export"

	"github.com/gin-gonic/gin"
)

func RegisterExportRoutes(r *gin.Engine) {
	r.POST("/api/export/user-data", exportUserData)
}

func exportUserData(ctx *gin.Context) {
	var req export.ExportRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, respond.ApiError(400, "invalid request: "+err.Error()))
		return
	}

	pr, pw := io.Pipe()

	go func() {
		err := export.ExportUserData(pw, &req)
		pw.CloseWithError(err)
	}()

	filename := fmt.Sprintf("userdata_%s_%d_%d.zip", req.Identity, req.StartHeight, req.EndHeight)
	ctx.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	ctx.Header("Content-Type", "application/zip")
	ctx.Header("X-Accel-Buffering", "no")
	ctx.Status(http.StatusOK)

	io.Copy(ctx.Writer, pr)
	pr.Close()
}
