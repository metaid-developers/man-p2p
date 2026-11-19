package api

import (
	"crypto/subtle"
	"log"
	"man-p2p/api/respond"
	"man-p2p/common"
	"net/http"

	"github.com/gin-gonic/gin"
)

// AdminTokenHeader 允许通过 header 传递 admin token（query 传 token 会进入访问日志/Referer）。
const AdminTokenHeader = "X-Admin-Token"

// VerifyAdminToken 校验 admin token：配置缺失/为空时一律拒绝（安全默认），比较使用常量时间。
func VerifyAdminToken(token string) bool {
	if common.Config == nil {
		return false
	}
	expected := common.Config.AdminToken
	if expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(expected)) == 1
}

// AdminAuth 强制鉴权中间件：X-Admin-Token header 优先，兼容历史 ?token= query。
func AdminAuth() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		token := ctx.GetHeader(AdminTokenHeader)
		if token == "" {
			token = ctx.Query("token")
		}
		if !VerifyAdminToken(token) {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, &respond.ApiResponse{
				Code: 401,
				Msg:  "Unauthorized: invalid admin token",
				Data: nil,
			})
			return
		}
		ctx.Next()
	}
}

// ValidSnapshotID 快照 ID 白名单（生成格式为 2006-01-02_15-04-05），
// 拒绝路径分隔符与 .. 以防目录遍历。
func ValidSnapshotID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

const maxRequestBodyBytes int64 = 1 << 20 // 1MB

// BodySizeLimit 全局请求体上限，防止无界 body 读取进内存。
func BodySizeLimit() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if ctx.Request.Body != nil {
			ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, maxRequestBodyBytes)
		}
		ctx.Next()
	}
}

// WarnIfAdminTokenEmpty 启动时提示：adminToken 为空时所有 admin 端点将拒绝请求。
func WarnIfAdminTokenEmpty() {
	if common.Config.AdminToken == "" {
		log.Printf("[WARN] adminToken is empty: all /admin endpoints (mrc20 admin, block file, config reload) will reject requests until a token is configured")
	}
}
