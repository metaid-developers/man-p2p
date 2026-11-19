package api

import (
	"man-p2p/common"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func setAdminToken(t *testing.T, token string) {
	t.Helper()
	orig := common.Config
	common.Config = &common.AllConfig{AdminToken: token}
	t.Cleanup(func() { common.Config = orig })
}

func newAdminTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/admin/protected", AdminAuth(), func(ctx *gin.Context) {
		ctx.String(http.StatusOK, "ok")
	})
	return r
}

func TestVerifyAdminTokenEmptyConfigDeniesAll(t *testing.T) {
	setAdminToken(t, "")
	if VerifyAdminToken("") {
		t.Fatal("empty config token must deny empty request token")
	}
	if VerifyAdminToken("anything") {
		t.Fatal("empty config token must deny any request token")
	}
}

func TestVerifyAdminTokenMatch(t *testing.T) {
	setAdminToken(t, "secret-token")
	if !VerifyAdminToken("secret-token") {
		t.Fatal("matching token must pass")
	}
	if VerifyAdminToken("") {
		t.Fatal("empty request token must fail")
	}
	if VerifyAdminToken("wrong") {
		t.Fatal("wrong token must fail")
	}
	if VerifyAdminToken("secret-tokeN") {
		t.Fatal("case-mismatched token must fail")
	}
}

func TestAdminAuthRejectsMissingAndWrongToken(t *testing.T) {
	setAdminToken(t, "secret-token")
	r := newAdminTestRouter()

	for _, tc := range []struct {
		name  string
		token string
		via   string
	}{
		{"no token", "", ""},
		{"wrong query token", "bad", "query"},
		{"wrong header token", "bad", "header"},
	} {
		req := httptest.NewRequest(http.MethodGet, "/admin/protected", nil)
		if tc.via == "query" {
			req = httptest.NewRequest(http.MethodGet, "/admin/protected?token="+tc.token, nil)
		} else if tc.via == "header" {
			req.Header.Set(AdminTokenHeader, tc.token)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: expected 401, got %d", tc.name, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "invalid admin token") {
			t.Fatalf("%s: expected unauthorized message, got %q", tc.name, rec.Body.String())
		}
	}
}

func TestAdminAuthAcceptsHeaderAndQueryToken(t *testing.T) {
	setAdminToken(t, "secret-token")
	r := newAdminTestRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/protected", nil)
	req.Header.Set(AdminTokenHeader, "secret-token")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("header token: expected 200, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/admin/protected?token=secret-token", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("query token: expected 200, got %d", rec.Code)
	}
}

func TestAdminAuthEmptyConfigAlwaysDenied(t *testing.T) {
	setAdminToken(t, "")
	r := newAdminTestRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/protected?token=", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("empty config must deny even empty-token request, got %d", rec.Code)
	}
}

func TestValidSnapshotID(t *testing.T) {
	for _, id := range []string{"2026-11-20_15-04-05", "20260101", "snapshot_1", "A-b_9"} {
		if !ValidSnapshotID(id) {
			t.Fatalf("expected %q to be valid", id)
		}
	}
	for _, id := range []string{"", "..", "../etc", "a/b", `a\b`, "a b", "id;rm", "..\\x", strings.Repeat("a", 129), "id\n"} {
		if ValidSnapshotID(id) {
			t.Fatalf("expected %q to be rejected", id)
		}
	}
}

func TestBodySizeLimitRejectsOversizedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(BodySizeLimit())
	r.POST("/echo", func(ctx *gin.Context) {
		var req struct {
			A string `json:"a"`
		}
		if err := ctx.ShouldBindJSON(&req); err != nil {
			ctx.String(http.StatusBadRequest, "bind failed")
			return
		}
		ctx.String(http.StatusOK, "ok")
	})

	big := `{"a":"` + strings.Repeat("x", 2<<20) + `"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(big))
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected oversized body to be rejected with 400, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(`{"a":"small"}`))
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected small body to pass, got %d", rec.Code)
	}
}

func resetExportGate(t *testing.T) {
	t.Helper()
	exportRateMu.Lock()
	exportRate = map[string]*exportWindowCounter{}
	exportRateMu.Unlock()
	for {
		select {
		case <-exportSlots:
		default:
			return
		}
	}
}

func gateCtx(ip string) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodGet, "/export", nil)
	req.RemoteAddr = ip + ":1000"
	ctx.Request = req
	return ctx, recorder
}

func TestExportGateRateLimitPerIP(t *testing.T) {
	resetExportGate(t)
	gin.SetMode(gin.TestMode)

	for i := 0; i < exportMaxPerWindow; i++ {
		ctx, _ := gateCtx("192.0.2.50")
		release := exportGate(ctx)
		if release == nil {
			t.Fatalf("request %d within window limit should pass", i+1)
		}
		release()
	}

	ctx, recorder := gateCtx("192.0.2.50")
	if release := exportGate(ctx); release != nil {
		t.Fatal("request beyond window limit should be rejected")
	}
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", recorder.Code)
	}
}

func TestExportGateConcurrencyCap(t *testing.T) {
	resetExportGate(t)
	gin.SetMode(gin.TestMode)

	// 占满全部并发槽位，保持不释放
	held := make([]func(), 0, exportMaxConcurrent)
	for i := 0; i < exportMaxConcurrent; i++ {
		ctx, _ := gateCtx("198.51.100." + string(rune('a'+i)))
		release := exportGate(ctx)
		if release == nil {
			t.Fatalf("slot %d should be acquirable", i+1)
		}
		held = append(held, release)
	}

	ctx, recorder := gateCtx("198.51.100.z")
	if release := exportGate(ctx); release != nil {
		t.Fatal("export gate should reject when all slots are held")
	}
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", recorder.Code)
	}

	for _, release := range held {
		release()
	}
}

func TestExportGateRateWindowsIndependentPerIP(t *testing.T) {
	resetExportGate(t)
	gin.SetMode(gin.TestMode)

	pass := func(ip string) bool {
		ctx, _ := gateCtx(ip)
		release := exportGate(ctx)
		if release != nil {
			release()
		}
		return release != nil
	}

	for i := 0; i < exportMaxPerWindow; i++ {
		if !pass("203.0.113.1") {
			t.Fatalf("ip1 request %d should pass", i+1)
		}
	}
	if pass("203.0.113.1") {
		t.Fatal("ip1 beyond limit should be rejected")
	}
	if !pass("203.0.113.2") {
		t.Fatal("ip2 should have its own window")
	}
}
