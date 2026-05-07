package main

import (
	"testing"

	"man-p2p/common"
)

func TestRuntimeMRC20EnabledRequiresChainSourceAndModule(t *testing.T) {
	oldConfig := common.Config
	common.Config = &common.AllConfig{Module: []string{"mrc20"}}
	t.Cleanup(func() {
		common.Config = oldConfig
	})

	if !runtimeMRC20Enabled(true) {
		t.Fatalf("expected MRC20 runtime to be enabled when chain source and mrc20 module are enabled")
	}
	if runtimeMRC20Enabled(false) {
		t.Fatalf("did not expect MRC20 runtime when chain source is disabled")
	}
}

func TestRuntimeMRC20EnabledSupportsMrc20OnlyMode(t *testing.T) {
	oldConfig := common.Config
	common.Config = &common.AllConfig{}
	common.Config.Sync.Mrc20Only = true
	t.Cleanup(func() {
		common.Config = oldConfig
	})

	if !runtimeMRC20Enabled(true) {
		t.Fatalf("expected MRC20 runtime in mrc20Only mode when chain source is enabled")
	}
}
