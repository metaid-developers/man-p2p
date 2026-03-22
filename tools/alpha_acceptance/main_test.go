package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPickBootstrapAddrPrefersRequestedIPv4(t *testing.T) {
	status := p2pStatusEnvelope{
		Data: p2pStatusData{
			PeerID: "12D3KooWLocalPeer",
			ListenAddrs: []string{
				"/ip4/127.0.0.1/tcp/4001",
				"/ip4/10.0.0.5/tcp/4001",
				"/ip4/192.168.3.30/tcp/4001",
			},
		},
	}

	got, err := pickBootstrapAddr(status, "192.168.3.30")
	if err != nil {
		t.Fatal(err)
	}

	want := "/ip4/192.168.3.30/tcp/4001/p2p/12D3KooWLocalPeer"
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestPickBootstrapAddrFallsBackToGlobalIPv6(t *testing.T) {
	status := p2pStatusEnvelope{
		Data: p2pStatusData{
			PeerID: "12D3KooWLocalPeer",
			ListenAddrs: []string{
				"/ip4/127.0.0.1/tcp/4001",
				"/ip6/::1/tcp/4001",
				"/ip6/240e:3b3:f1f2:29c1:187e:1045:a71a:a083/tcp/4001",
			},
		},
	}

	got, err := pickBootstrapAddr(status, "192.168.3.30")
	if err != nil {
		t.Fatal(err)
	}

	want := "/ip6/240e:3b3:f1f2:29c1:187e:1045:a71a:a083/tcp/4001/p2p/12D3KooWLocalPeer"
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestBootstrapAddrCandidatesPreservePreferredIPv4BeforeFallbacks(t *testing.T) {
	status := p2pStatusEnvelope{
		Data: p2pStatusData{
			PeerID: "12D3KooWLocalPeer",
			ListenAddrs: []string{
				"/ip4/127.0.0.1/tcp/4001",
				"/ip4/10.211.55.2/tcp/4001",
				"/ip4/192.168.3.30/tcp/4001",
				"/ip6/::1/tcp/4001",
				"/ip6/240e:3b3:f1f2:29c1:187e:1045:a71a:a083/tcp/4001",
			},
		},
	}

	got, err := bootstrapAddrCandidates(status, "192.168.3.30")
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"/ip4/192.168.3.30/tcp/4001/p2p/12D3KooWLocalPeer",
		"/ip4/10.211.55.2/tcp/4001/p2p/12D3KooWLocalPeer",
		"/ip6/240e:3b3:f1f2:29c1:187e:1045:a71a:a083/tcp/4001/p2p/12D3KooWLocalPeer",
	}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestMergeP2PConfigAppliesAcceptanceOverrides(t *testing.T) {
	raw := []byte(`{
		"p2p_sync_mode": "self",
		"p2p_bootstrap_nodes": [],
		"p2p_selective_addresses": ["1Selective"],
		"p2p_selective_paths": ["/info/*"],
		"p2p_block_addresses": ["1Blocked"],
		"p2p_block_paths": ["/blocked/*"],
		"p2p_max_content_size_kb": 512,
		"p2p_enable_relay": true,
		"p2p_storage_limit_gb": 10,
		"p2p_enable_chain_source": false,
		"p2p_own_addresses": ["1ExistingAddr"],
		"custom_field": "keep-me"
	}`)

	updated, err := mergeP2PConfig(raw, acceptanceConfigPatch{
		SyncMode:           "full",
		BootstrapNodes:     []string{"/ip4/192.168.3.30/tcp/4001/p2p/12D3KooWLocalPeer"},
		OwnAddresses:       []string{},
		SelectiveAddresses: []string{},
		SelectivePaths:     []string{},
		BlockAddresses:     []string{},
		BlockPaths:         []string{},
		MaxContentSizeKB:   intPtr(0),
	})
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(updated, &got); err != nil {
		t.Fatal(err)
	}

	if got["p2p_sync_mode"] != "full" {
		t.Fatalf("expected sync mode full, got %#v", got["p2p_sync_mode"])
	}
	bootstrap, ok := got["p2p_bootstrap_nodes"].([]any)
	if !ok || len(bootstrap) != 1 || bootstrap[0] != "/ip4/192.168.3.30/tcp/4001/p2p/12D3KooWLocalPeer" {
		t.Fatalf("unexpected bootstrap nodes: %#v", got["p2p_bootstrap_nodes"])
	}
	ownAddresses, ok := got["p2p_own_addresses"].([]any)
	if !ok || len(ownAddresses) != 0 {
		t.Fatalf("expected own addresses to be cleared, got %#v", got["p2p_own_addresses"])
	}
	if selectiveAddresses, ok := got["p2p_selective_addresses"].([]any); !ok || len(selectiveAddresses) != 0 {
		t.Fatalf("expected selective addresses cleared, got %#v", got["p2p_selective_addresses"])
	}
	if selectivePaths, ok := got["p2p_selective_paths"].([]any); !ok || len(selectivePaths) != 0 {
		t.Fatalf("expected selective paths cleared, got %#v", got["p2p_selective_paths"])
	}
	if blockAddresses, ok := got["p2p_block_addresses"].([]any); !ok || len(blockAddresses) != 0 {
		t.Fatalf("expected block addresses cleared, got %#v", got["p2p_block_addresses"])
	}
	if blockPaths, ok := got["p2p_block_paths"].([]any); !ok || len(blockPaths) != 0 {
		t.Fatalf("expected block paths cleared, got %#v", got["p2p_block_paths"])
	}
	if got["p2p_max_content_size_kb"] != float64(0) {
		t.Fatalf("expected max content size reset to 0, got %#v", got["p2p_max_content_size_kb"])
	}
	if got["custom_field"] != "keep-me" {
		t.Fatalf("expected custom field preserved, got %#v", got["custom_field"])
	}
}

func intPtr(value int) *int {
	return &value
}

func TestSSHOptionsDisablePubkeyWhenPasswordProvided(t *testing.T) {
	args := sshOptions("secret")
	joined := strings.Join(args, " ")

	for _, want := range []string{
		"StrictHostKeyChecking=no",
		"PreferredAuthentications=password",
		"PubkeyAuthentication=no",
		"IdentitiesOnly=yes",
		"NumberOfPasswordPrompts=1",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected ssh options to contain %q, got %q", want, joined)
		}
	}
}

func TestSSHOptionsLeaveDefaultAuthWhenPasswordEmpty(t *testing.T) {
	args := sshOptions("")
	joined := strings.Join(args, " ")

	if strings.Contains(joined, "PreferredAuthentications=password") {
		t.Fatalf("did not expect password-only auth when no password is configured: %q", joined)
	}
	if strings.Contains(joined, "PubkeyAuthentication=no") {
		t.Fatalf("did not expect pubkey auth to be disabled when no password is configured: %q", joined)
	}
}

func TestRemoteProcessPatternExpandsTildeToMatchableSuffix(t *testing.T) {
	got := remoteProcessPattern("~/tmp/idbots-alpha/IDBots.app/Contents/Resources/man-p2p-darwin-arm64")
	want := "/tmp/idbots-alpha/IDBots.app/Contents/Resources/man-p2p-darwin-arm64"
	if got != want {
		t.Fatalf("expected remote process pattern %q, got %q", want, got)
	}
}

func TestManBinaryPatternTargetsBundledChildBinary(t *testing.T) {
	got := manBinaryPattern("/Applications/IDBots.app")
	want := "/Applications/IDBots.app/Contents/Resources/man-p2p-darwin-arm64"
	if got != want {
		t.Fatalf("expected man binary pattern %q, got %q", want, got)
	}
}

func TestCleanupTargetPIDsIncludesNewAppAndChildProcesses(t *testing.T) {
	got := cleanupTargetPIDs(
		[]int{10},
		[]int{10, 20},
		[]int{30},
		[]int{30, 40, 50},
	)
	want := []int{20, 40, 50}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestParseRunOptionsAcceptsRemoteLaunchModeFlag(t *testing.T) {
	opts, err := parseRunOptions([]string{
		"--local-app", "/tmp/IDBots.app",
		"--remote-user", "showpay",
		"--remote-host", "192.168.3.52",
		"--remote-launch-mode", "binary",
	})
	if err != nil {
		t.Fatalf("expected remote launch mode flag to be accepted, got error: %v", err)
	}
	if opts.RemoteLaunchMode != "binary" {
		t.Fatalf("expected remote launch mode binary, got %q", opts.RemoteLaunchMode)
	}
}

func TestConfigureRemoteRuntimeBinaryModeUsesIsolatedPaths(t *testing.T) {
	runtime := &remoteRuntime{
		AppPath:       "~/tmp/idbots-alpha/IDBots.app",
		AppBinaryPath: appBinaryPattern("~/tmp/idbots-alpha/IDBots.app"),
		BaseURL:       "http://127.0.0.1:62196",
		ConfigPath:    "~/Library/Application Support/IDBots/man-p2p-config.json",
	}
	opts := runOptions{
		RemoteApp:        runtime.AppPath,
		RemoteBaseURL:    runtime.BaseURL,
		RemoteLaunchMode: "binary",
	}

	if err := configureRemoteRuntime(runtime, opts); err != nil {
		t.Fatal(err)
	}

	if runtime.RuntimeRoot != "/tmp/idbots-alpha-remote-62196" {
		t.Fatalf("expected isolated runtime root, got %q", runtime.RuntimeRoot)
	}
	if runtime.ConfigPath != "/tmp/idbots-alpha-remote-62196/userData/man-p2p-config.json" {
		t.Fatalf("unexpected config path %q", runtime.ConfigPath)
	}
	if runtime.MetaIDRPCPort != 62197 {
		t.Fatalf("expected derived MetaID RPC port 62197, got %d", runtime.MetaIDRPCPort)
	}
}

func TestAdoptExistingRemoteRuntimeRestoresDefaultConfigPath(t *testing.T) {
	runtime := &remoteRuntime{
		AppPath:       "~/tmp/idbots-alpha/IDBots.app",
		AppBinaryPath: appBinaryPattern("~/tmp/idbots-alpha/IDBots.app"),
		BaseURL:       "http://127.0.0.1:7281",
		ConfigPath:    defaultRemoteConfigPath,
	}
	opts := runOptions{
		RemoteApp:        runtime.AppPath,
		RemoteBaseURL:    runtime.BaseURL,
		RemoteLaunchMode: "binary",
	}

	if err := configureRemoteRuntime(runtime, opts); err != nil {
		t.Fatal(err)
	}
	if runtime.ConfigPath == defaultRemoteConfigPath {
		t.Fatalf("expected binary runtime config path to move away from default before adopting existing runtime")
	}

	adoptExistingRemoteRuntime(runtime)

	if runtime.ConfigPath != defaultRemoteConfigPath {
		t.Fatalf("expected existing runtime to restore default config path %q, got %q", defaultRemoteConfigPath, runtime.ConfigPath)
	}
	if runtime.RuntimeRoot != "" {
		t.Fatalf("expected existing runtime to clear isolated runtime root, got %q", runtime.RuntimeRoot)
	}
	if runtime.AppDataPath != "" || runtime.UserDataPath != "" || runtime.LogPath != "" {
		t.Fatalf("expected existing runtime to clear isolated paths, got appData=%q userData=%q log=%q", runtime.AppDataPath, runtime.UserDataPath, runtime.LogPath)
	}
	if runtime.MetaIDRPCPort != 0 {
		t.Fatalf("expected existing runtime to clear derived rpc port, got %d", runtime.MetaIDRPCPort)
	}
}

func TestBuildRemoteStartCommandBinaryModeInjectsRuntimeOverrides(t *testing.T) {
	runtime := &remoteRuntime{
		AppPath:       "~/tmp/idbots-alpha/IDBots.app",
		AppBinaryPath: "~/tmp/idbots-alpha/IDBots.app/Contents/MacOS/IDBots",
		BaseURL:       "http://127.0.0.1:62196",
		RuntimeRoot:   "/tmp/idbots-alpha-remote-62196",
		AppDataPath:   "/tmp/idbots-alpha-remote-62196/appData",
		UserDataPath:  "/tmp/idbots-alpha-remote-62196/userData",
		LogPath:       "/tmp/idbots-alpha-remote-62196/remote-app.log",
		MetaIDRPCPort: 62197,
	}

	cmd, err := buildRemoteStartCommand(runtime, runOptions{RemoteLaunchMode: "binary"})
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"nohup env",
		"IDBOTS_APP_DATA_PATH='/tmp/idbots-alpha-remote-62196/appData'",
		"IDBOTS_USER_DATA_PATH='/tmp/idbots-alpha-remote-62196/userData'",
		"IDBOTS_MAN_P2P_LOCAL_BASE='http://127.0.0.1:62196'",
		"IDBOTS_DISABLE_SINGLE_INSTANCE_LOCK=1",
		"IDBOTS_METAID_RPC_PORT='62197'",
		"\"$HOME/tmp/idbots-alpha/IDBots.app/Contents/MacOS/IDBots\"",
		"> '/tmp/idbots-alpha-remote-62196/remote-app.log' 2>&1",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("expected command to contain %q, got %q", want, cmd)
		}
	}
}

func TestBuildRemoteStartCommandOpenModeUsesOpenCommand(t *testing.T) {
	runtime := &remoteRuntime{
		AppPath:       "~/tmp/idbots-alpha/IDBots.app",
		AppBinaryPath: "~/tmp/idbots-alpha/IDBots.app/Contents/MacOS/IDBots",
		BaseURL:       "http://127.0.0.1:62532",
		RuntimeRoot:   "/tmp/idbots-alpha-remote-62532",
		AppDataPath:   "/tmp/idbots-alpha-remote-62532/appData",
		UserDataPath:  "/tmp/idbots-alpha-remote-62532/userData",
		ConfigPath:    "/tmp/idbots-alpha-remote-62532/userData/man-p2p-config.json",
		LogPath:       "/tmp/idbots-alpha-remote-62532/remote-app.log",
		MetaIDRPCPort: 62533,
	}

	cmd, err := buildRemoteStartCommand(runtime, runOptions{RemoteLaunchMode: "open"})
	if err != nil {
		t.Fatal(err)
	}

	want := "open -n \"$HOME/tmp/idbots-alpha/IDBots.app\""
	if cmd != want {
		t.Fatalf("expected command %q, got %q", want, cmd)
	}
}

func TestParseRunOptionsDefaultsRemoteLaunchModeToOpen(t *testing.T) {
	opts, err := parseRunOptions([]string{
		"--local-app", "/tmp/IDBots.app",
		"--remote-user", "showpay",
		"--remote-host", "192.168.3.52",
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.RemoteLaunchMode != "open" {
		t.Fatalf("expected default remote launch mode open, got %q", opts.RemoteLaunchMode)
	}
}

func TestBuildRemoteBootstrapProbeCommandParsesIPv4TCPBootstrapAddr(t *testing.T) {
	cmd, err := buildRemoteBootstrapProbeCommand("/ip4/192.168.3.30/tcp/64684/p2p/12D3KooWLocalPeer")
	if err != nil {
		t.Fatal(err)
	}

	want := "nc -z -G 1 192.168.3.30 64684 >/dev/null 2>&1"
	if cmd != want {
		t.Fatalf("expected probe command %q, got %q", want, cmd)
	}
}

func TestBuildRemoteBootstrapProbeCommandParsesIPv6TCPBootstrapAddr(t *testing.T) {
	cmd, err := buildRemoteBootstrapProbeCommand("/ip6/240e:3b3:f1f2:29c1:187e:1045:a71a:a083/tcp/64684/p2p/12D3KooWLocalPeer")
	if err != nil {
		t.Fatal(err)
	}

	want := "nc -6 -z -G 1 240e:3b3:f1f2:29c1:187e:1045:a71a:a083 64684 >/dev/null 2>&1"
	if cmd != want {
		t.Fatalf("expected probe command %q, got %q", want, cmd)
	}
}

func TestBuildRemoteBootstrapProbeCommandRejectsMalformedAddr(t *testing.T) {
	if _, err := buildRemoteBootstrapProbeCommand("/ip4/192.168.3.30/udp/64684/p2p/12D3KooWLocalPeer"); err == nil {
		t.Fatal("expected malformed bootstrap addr to be rejected")
	}
}
