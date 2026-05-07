package api

import (
	"testing"

	"man-p2p/common"

	"github.com/gin-gonic/gin"
)

func hasRoute(r *gin.Engine, method string, path string) bool {
	for _, route := range r.Routes() {
		if route.Method == method && route.Path == path {
			return true
		}
	}
	return false
}

func TestRegisterRuntimeFeatureRoutesEnablesMRC20WhenChainSourceEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldConfig := common.Config
	common.Config = &common.AllConfig{Module: []string{"mrc20"}}
	t.Cleanup(func() {
		common.Config = oldConfig
	})

	r := gin.New()
	registerRuntimeFeatureRoutesForMode(r, true)

	if !hasRoute(r, "GET", "/api/mrc20/address/balance/:address") {
		t.Fatalf("expected MRC20 JSON route when chain source and mrc20 module are enabled")
	}
	if !hasRoute(r, "GET", "/mrc20/:page") {
		t.Fatalf("expected MRC20 web route when chain source and mrc20 module are enabled")
	}
}

func TestRegisterRuntimeFeatureRoutesSkipsMRC20WhenChainSourceDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldConfig := common.Config
	common.Config = &common.AllConfig{Module: []string{"mrc20"}}
	t.Cleanup(func() {
		common.Config = oldConfig
	})

	r := gin.New()
	registerRuntimeFeatureRoutesForMode(r, false)

	if hasRoute(r, "GET", "/api/mrc20/address/balance/:address") {
		t.Fatalf("did not expect MRC20 JSON route when chain source is disabled")
	}
	if hasRoute(r, "GET", "/mrc20/:page") {
		t.Fatalf("did not expect MRC20 web route when chain source is disabled")
	}
}
