package api

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"man-p2p/api/respond"
	"man-p2p/export"

	"github.com/gin-gonic/gin"
)

func RegisterExportRoutes(r *gin.Engine) {
	r.POST("/api/export/user-data", exportUserData)
	r.GET("/api/export/user-data", exportUserDataGET)
}

// 导出为重活（两轮全库扫描 + zip 流），无鉴权公开，必须限流防滥用。

const (
	exportRateWindow    = time.Minute
	exportMaxPerWindow  = 3
	exportMaxConcurrent = 2
)

type exportWindowCounter struct {
	windowStart time.Time
	count       int
}

var (
	exportRateMu sync.Mutex
	exportRate   = map[string]*exportWindowCounter{}
	exportSlots  = make(chan struct{}, exportMaxConcurrent)
)

// exportGate 每 IP 固定窗口限流 + 全局并发闸门。
// 返回非 nil release 表示放行（调用方须在导出完成后调用 release 释放并发槽位）；
// 返回 nil 表示已写入 429 响应，调用方应直接结束请求。
func exportGate(ctx *gin.Context) (release func()) {
	ip := ctx.ClientIP()
	now := time.Now()
	exportRateMu.Lock()
	w := exportRate[ip]
	if w == nil || now.Sub(w.windowStart) >= exportRateWindow {
		w = &exportWindowCounter{windowStart: now}
		exportRate[ip] = w
		// 顺手清理过期窗口，避免 map 随来源数无限增长
		if len(exportRate) > 1024 {
			for k, v := range exportRate {
				if now.Sub(v.windowStart) >= exportRateWindow {
					delete(exportRate, k)
				}
			}
		}
	}
	w.count++
	allowed := w.count <= exportMaxPerWindow
	exportRateMu.Unlock()
	if !allowed {
		ctx.JSON(http.StatusTooManyRequests, respond.ApiError(429, "export rate limit exceeded, retry later"))
		return nil
	}
	select {
	case exportSlots <- struct{}{}:
		return func() { <-exportSlots }
	default:
		ctx.JSON(http.StatusTooManyRequests, respond.ApiError(429, "export concurrency limit reached, retry later"))
		return nil
	}
}

func exportUserDataGET(ctx *gin.Context) {
	var req export.ExportRequest
	req.Identity = ctx.Query("identity")
	req.IdentityType = ctx.Query("identity_type")
	if h := ctx.Query("start_height"); h != "" {
		v, err := strconv.ParseInt(h, 10, 64)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, respond.ApiError(400, "invalid start_height: "+err.Error()))
			return
		}
		req.StartHeight = v
	}
	if h := ctx.Query("end_height"); h != "" {
		v, err := strconv.ParseInt(h, 10, 64)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, respond.ApiError(400, "invalid end_height: "+err.Error()))
			return
		}
		req.EndHeight = v
	}
	exportUserDataStream(ctx, &req)
}

func exportUserData(ctx *gin.Context) {
	var req export.ExportRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, respond.ApiError(400, "invalid request: "+err.Error()))
		return
	}
	exportUserDataStream(ctx, &req)
}

func exportUserDataStream(ctx *gin.Context, req *export.ExportRequest) {
	release := exportGate(ctx)
	if release == nil {
		return
	}
	defer release()

	pr, pw := io.Pipe()
	defer pr.Close()

	go func() {
		defer pw.Close()
		err := export.ExportUserData(pw, req)
		pw.CloseWithError(err)
	}()

	safeIdentity := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == '"' || r == '\n' || r == '\r' {
			return '_'
		}
		return r
	}, req.Identity)
	filename := fmt.Sprintf("userdata_%s_%d_%d.zip", safeIdentity, req.StartHeight, req.EndHeight)
	ctx.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	ctx.Header("Content-Type", "application/zip")
	ctx.Header("X-Accel-Buffering", "no")
	ctx.Status(http.StatusOK)

	if _, err := io.Copy(ctx.Writer, pr); err != nil {
		log.Printf("[WARN] export stream aborted: %v", err)
	}
}
