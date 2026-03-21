package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"man-p2p/p2p"
	"man-p2p/pin"
)

const (
	defaultRemoteAppPath = "~/tmp/idbots-alpha/IDBots.app"
	defaultRemoteBaseURL = "http://127.0.0.1:7281"
	defaultFallbackPinID = "d7947500f7668e361bd84d20a45f49bb8e692d3c5ec1dc57310a8d8171f258f8i0"
)

type p2pStatusEnvelope struct {
	Data p2pStatusData `json:"data"`
}

type p2pStatusData struct {
	PeerID      string   `json:"peerId"`
	ListenAddrs []string `json:"listenAddrs"`
	PeerCount   int      `json:"peerCount"`
	SyncMode    string   `json:"syncMode"`
	RuntimeMode string   `json:"runtimeMode"`
}

type peersEnvelope struct {
	Data []string `json:"data"`
}

type acceptanceConfigPatch struct {
	SyncMode           string
	BootstrapNodes     []string
	OwnAddresses       []string
	SelectiveAddresses []string
	SelectivePaths     []string
	BlockAddresses     []string
	BlockPaths         []string
	MaxContentSizeKB   *int
}

type runOptions struct {
	LocalApp           string
	RemoteUser         string
	RemoteHost         string
	RemoteApp          string
	RemoteBaseURL      string
	RemoteCopy         bool
	PreferredLocalIPv4 string
	RemotePassword     string
	ConnectTimeout     time.Duration
	FallbackPinID      string
	SyntheticPinID     string
	Cleanup            bool
}

type localRuntime struct {
	Root          string
	BaseURL       string
	RPCBaseURL    string
	ConfigPath    string
	BinaryPattern string
	BeforePIDs    []int
}

type remoteRuntime struct {
	Target             string
	AppPath            string
	BaseURL            string
	ConfigPath         string
	BinaryPattern      string
	ManBinaryPattern   string
	BeforePIDs         []int
	ExistingRuntime    bool
	OriginalConfig     []byte
	OriginalConfigRead bool
}

type metaidRPCResponse struct {
	Success bool `json:"success"`
	Data    struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	} `json:"data"`
}

type acceptanceSummary struct {
	LocalBootstrap  string `json:"localBootstrap"`
	LocalPeerID     string `json:"localPeerId"`
	RemoteBootstrap string `json:"remoteBootstrap"`
	RemotePeerID    string `json:"remotePeerId"`
	FallbackPinID   string `json:"fallbackPinId"`
	SyntheticPinID  string `json:"syntheticPinId"`
}

func pickBootstrapAddr(status p2pStatusEnvelope, preferredIPv4 string) (string, error) {
	if status.Data.PeerID == "" {
		return "", errors.New("missing peer id")
	}

	preferred := strings.TrimSpace(preferredIPv4)
	fallback := ""
	for _, addr := range status.Data.ListenAddrs {
		host, ok := extractIPv4Host(addr)
		if !ok {
			continue
		}
		if host == preferred {
			return fmt.Sprintf("%s/p2p/%s", addr, status.Data.PeerID), nil
		}
		if fallback == "" && !isLoopbackOrUnspecified(host) {
			fallback = addr
		}
	}
	if fallback != "" {
		return fmt.Sprintf("%s/p2p/%s", fallback, status.Data.PeerID), nil
	}
	return "", fmt.Errorf("no usable ipv4 listen addr found for peer %s", status.Data.PeerID)
}

func mergeP2PConfig(raw []byte, patch acceptanceConfigPatch) ([]byte, error) {
	var cfg map[string]any
	if len(raw) == 0 {
		cfg = map[string]any{}
	} else if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}

	if cfg == nil {
		cfg = map[string]any{}
	}
	if patch.SyncMode != "" {
		cfg["p2p_sync_mode"] = patch.SyncMode
	}
	if patch.BootstrapNodes != nil {
		cfg["p2p_bootstrap_nodes"] = patch.BootstrapNodes
	}
	if patch.OwnAddresses != nil {
		cfg["p2p_own_addresses"] = patch.OwnAddresses
	}
	if patch.SelectiveAddresses != nil {
		cfg["p2p_selective_addresses"] = patch.SelectiveAddresses
	}
	if patch.SelectivePaths != nil {
		cfg["p2p_selective_paths"] = patch.SelectivePaths
	}
	if patch.BlockAddresses != nil {
		cfg["p2p_block_addresses"] = patch.BlockAddresses
	}
	if patch.BlockPaths != nil {
		cfg["p2p_block_paths"] = patch.BlockPaths
	}
	if patch.MaxContentSizeKB != nil {
		cfg["p2p_max_content_size_kb"] = *patch.MaxContentSizeKB
	}

	updated, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(updated, '\n'), nil
}

func extractIPv4Host(addr string) (string, bool) {
	trimmed := strings.TrimSpace(addr)
	parts := strings.Split(trimmed, "/")
	if len(parts) < 5 || parts[1] != "ip4" || parts[3] != "tcp" {
		return "", false
	}
	host := strings.TrimSpace(parts[2])
	if net.ParseIP(host) == nil {
		return "", false
	}
	return host, true
}

func isLoopbackOrUnspecified(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return true
	}
	return ip.IsLoopback() || ip.IsUnspecified()
}

func main() {
	opts, err := parseRunOptions(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "alpha acceptance: %v\n", err)
		os.Exit(2)
	}

	ctx := context.Background()
	summary, err := runAcceptance(ctx, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "alpha acceptance failed: %v\n", err)
		os.Exit(1)
	}

	encoded, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "alpha acceptance summary encode failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(encoded))
}

func parseRunOptions(args []string) (runOptions, error) {
	fs := flag.NewFlagSet("alpha_acceptance", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	opts := runOptions{}
	remotePasswordEnv := "IDBOTS_REMOTE_PASSWORD"
	fs.StringVar(&opts.LocalApp, "local-app", "", "Absolute path to local packaged IDBots.app")
	fs.StringVar(&opts.RemoteUser, "remote-user", "", "Remote SSH user")
	fs.StringVar(&opts.RemoteHost, "remote-host", "", "Remote SSH host")
	fs.StringVar(&opts.RemoteApp, "remote-app", defaultRemoteAppPath, "Remote packaged IDBots.app path")
	fs.StringVar(&opts.RemoteBaseURL, "remote-base-url", defaultRemoteBaseURL, "Remote man-p2p base URL")
	fs.BoolVar(&opts.RemoteCopy, "remote-copy", false, "Copy local packaged app to the remote path before validation")
	fs.StringVar(&opts.PreferredLocalIPv4, "preferred-local-ip", "", "Preferred local LAN IPv4 to expose in the bootstrap multiaddr")
	fs.StringVar(&remotePasswordEnv, "remote-password-env", remotePasswordEnv, "Environment variable containing the remote SSH password; empty means plain ssh/scp")
	fs.DurationVar(&opts.ConnectTimeout, "connect-timeout", 30*time.Second, "How long to wait for health, peers, and propagation checks")
	fs.StringVar(&opts.FallbackPinID, "fallback-pin-id", defaultFallbackPinID, "PIN ID used for local-miss fallback validation")
	fs.StringVar(&opts.SyntheticPinID, "synthetic-pin-id", "", "Synthetic PIN ID used for realtime propagation validation")
	fs.BoolVar(&opts.Cleanup, "cleanup", true, "Restore remote config and stop test-started app instances at the end")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if opts.LocalApp == "" {
		return opts, errors.New("--local-app is required")
	}
	if opts.RemoteUser == "" || opts.RemoteHost == "" {
		return opts, errors.New("--remote-user and --remote-host are required")
	}
	if opts.ConnectTimeout <= 0 {
		return opts, errors.New("--connect-timeout must be > 0")
	}
	if opts.SyntheticPinID == "" {
		opts.SyntheticPinID = fmt.Sprintf("alpha-live-pin-%d", time.Now().Unix())
	}
	if remotePasswordEnv != "" {
		opts.RemotePassword = os.Getenv(remotePasswordEnv)
	}
	return opts, nil
}

func runAcceptance(ctx context.Context, opts runOptions) (*acceptanceSummary, error) {
	if err := ensureTooling(opts); err != nil {
		return nil, err
	}

	local, err := startLocalRuntime(ctx, opts)
	if err != nil {
		return nil, err
	}
	defer cleanupLocalRuntime(context.Background(), local, opts)

	localStatus, err := fetchLocalStatus(ctx, local.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("fetch local status: %w", err)
	}
	localBootstrap, err := pickBootstrapAddr(localStatus, opts.PreferredLocalIPv4)
	if err != nil {
		return nil, fmt.Errorf("derive local bootstrap addr: %w", err)
	}
	logf("local peer %s bootstrap %s", localStatus.Data.PeerID, localBootstrap)

	remote, err := prepareRemoteRuntime(ctx, opts)
	if err != nil {
		return nil, err
	}
	if opts.Cleanup {
		defer cleanupRemoteRuntime(context.Background(), remote, opts)
	}

	remotePatch := acceptancePatch([]string{localBootstrap})
	if err := patchRemoteConfig(ctx, remote, opts, remotePatch); err != nil {
		return nil, fmt.Errorf("patch remote config: %w", err)
	}
	if err := waitForRemotePeer(ctx, remote, opts, localStatus.Data.PeerID, opts.ConnectTimeout); err != nil {
		logf("remote peer did not appear after reload; restarting remote man-p2p and retrying")
		if restartErr := restartRemoteManP2P(ctx, remote, opts); restartErr != nil {
			return nil, fmt.Errorf("wait remote peer: %w; restart failed: %v", err, restartErr)
		}
		if err := waitForRemotePeer(ctx, remote, opts, localStatus.Data.PeerID, opts.ConnectTimeout); err != nil {
			return nil, fmt.Errorf("remote peer discovery failed after restart: %w", err)
		}
	}

	remoteStatus, err := fetchRemoteStatus(ctx, remote, opts)
	if err != nil {
		return nil, fmt.Errorf("fetch remote status: %w", err)
	}
	if err := waitForLocalPeer(ctx, local.BaseURL, remoteStatus.Data.PeerID, opts.ConnectTimeout, true); err != nil {
		return nil, fmt.Errorf("local peer discovery did not reflect remote connection: %w", err)
	}
	remoteBootstrap, err := pickBootstrapAddr(remoteStatus, opts.RemoteHost)
	if err != nil {
		return nil, fmt.Errorf("derive remote bootstrap addr: %w", err)
	}
	logf("remote peer %s bootstrap %s", remoteStatus.Data.PeerID, remoteBootstrap)

	if err := patchLocalConfig(local.ConfigPath, acceptancePatch(nil)); err != nil {
		return nil, fmt.Errorf("patch local config: %w", err)
	}
	if err := postLocalReload(ctx, local.BaseURL); err != nil {
		return nil, fmt.Errorf("reload local config: %w", err)
	}

	if err := verifyFallback(ctx, local, opts); err != nil {
		return nil, err
	}

	if err := publishSyntheticPin(ctx, []string{localBootstrap, remoteBootstrap}, opts.SyntheticPinID); err != nil {
		return nil, fmt.Errorf("publish synthetic pin: %w", err)
	}
	if err := waitForLocalPin(ctx, local.BaseURL, opts.SyntheticPinID, opts.ConnectTimeout); err != nil {
		return nil, fmt.Errorf("local synthetic pin verification failed: %w", err)
	}
	if err := waitForRemotePin(ctx, remote, opts, opts.SyntheticPinID, opts.ConnectTimeout); err != nil {
		return nil, fmt.Errorf("remote synthetic pin verification failed: %w", err)
	}

	return &acceptanceSummary{
		LocalBootstrap:  localBootstrap,
		LocalPeerID:     localStatus.Data.PeerID,
		RemoteBootstrap: remoteBootstrap,
		RemotePeerID:    remoteStatus.Data.PeerID,
		FallbackPinID:   opts.FallbackPinID,
		SyntheticPinID:  opts.SyntheticPinID,
	}, nil
}

func acceptancePatch(bootstrapNodes []string) acceptanceConfigPatch {
	zero := 0
	return acceptanceConfigPatch{
		SyncMode:           "full",
		BootstrapNodes:     bootstrapNodes,
		OwnAddresses:       []string{},
		SelectiveAddresses: []string{},
		SelectivePaths:     []string{},
		BlockAddresses:     []string{},
		BlockPaths:         []string{},
		MaxContentSizeKB:   &zero,
	}
}

func ensureTooling(opts runOptions) error {
	tools := []string{"ssh", "scp", "open", "launchctl", "curl", "pgrep", "pkill"}
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("required tool %s not found in PATH", tool)
		}
	}
	return nil
}

func startLocalRuntime(ctx context.Context, opts runOptions) (*localRuntime, error) {
	root, err := os.MkdirTemp("", "idbots-alpha-local-")
	if err != nil {
		return nil, err
	}
	appData := filepath.Join(root, "appData")
	userData := filepath.Join(appData, "IDBots")
	p2pPort, err := reservePort()
	if err != nil {
		return nil, err
	}
	rpcPort, err := reservePort()
	if err != nil {
		return nil, err
	}
	binaryPattern := appBinaryPattern(opts.LocalApp)
	beforePIDs, err := listLocalPIDs(binaryPattern)
	if err != nil {
		return nil, fmt.Errorf("list local app pids: %w", err)
	}

	envPairs := [][2]string{
		{"IDBOTS_APP_DATA_PATH", appData},
		{"IDBOTS_MAN_P2P_LOCAL_BASE", fmt.Sprintf("http://127.0.0.1:%d", p2pPort)},
		{"IDBOTS_METAID_RPC_PORT", strconv.Itoa(rpcPort)},
	}
	for _, pair := range envPairs {
		if _, err := runLocalCommand(ctx, "launchctl", "setenv", pair[0], pair[1]); err != nil {
			return nil, fmt.Errorf("set %s: %w", pair[0], err)
		}
	}
	unsetEnv := func() {
		for _, pair := range envPairs {
			_, _ = runLocalCommand(context.Background(), "launchctl", "unsetenv", pair[0])
		}
	}

	if _, err := runLocalCommand(ctx, "open", "-n", opts.LocalApp); err != nil {
		unsetEnv()
		return nil, fmt.Errorf("start local app: %w", err)
	}
	if err := waitForLocalHealth(ctx, fmt.Sprintf("http://127.0.0.1:%d/health", p2pPort), opts.ConnectTimeout); err != nil {
		unsetEnv()
		return nil, fmt.Errorf("wait local health: %w", err)
	}
	unsetEnv()

	return &localRuntime{
		Root:          root,
		BaseURL:       fmt.Sprintf("http://127.0.0.1:%d", p2pPort),
		RPCBaseURL:    fmt.Sprintf("http://127.0.0.1:%d", rpcPort),
		ConfigPath:    filepath.Join(userData, "man-p2p-config.json"),
		BinaryPattern: binaryPattern,
		BeforePIDs:    beforePIDs,
	}, nil
}

func prepareRemoteRuntime(ctx context.Context, opts runOptions) (*remoteRuntime, error) {
	runtime := &remoteRuntime{
		Target:           fmt.Sprintf("%s@%s", opts.RemoteUser, opts.RemoteHost),
		AppPath:          opts.RemoteApp,
		BaseURL:          opts.RemoteBaseURL,
		ConfigPath:       "~/Library/Application Support/IDBots/man-p2p-config.json",
		BinaryPattern:    appBinaryPattern(opts.RemoteApp),
		ManBinaryPattern: pathpkg.Join(opts.RemoteApp, "Contents", "Resources", "man-p2p-darwin-arm64"),
	}

	beforePIDs, err := listRemotePIDs(ctx, runtime, opts, runtime.BinaryPattern)
	if err != nil {
		return nil, fmt.Errorf("list remote app pids: %w", err)
	}
	runtime.BeforePIDs = beforePIDs
	if opts.RemoteCopy {
		if err := copyRemoteApp(ctx, runtime, opts); err != nil {
			return nil, err
		}
	}

	if err := ensureRemoteRuntime(ctx, runtime, opts); err != nil {
		return nil, err
	}
	originalConfig, err := readRemoteConfig(ctx, runtime, opts)
	if err != nil {
		return nil, err
	}
	runtime.OriginalConfig = originalConfig
	runtime.OriginalConfigRead = true
	return runtime, nil
}

func ensureRemoteRuntime(ctx context.Context, runtime *remoteRuntime, opts runOptions) error {
	if err := waitForRemoteHealth(ctx, runtime, opts, 2*time.Second); err == nil {
		runtime.ExistingRuntime = true
		logf("remote runtime already healthy; reusing existing 7281 service")
		return nil
	}

	if _, err := runRemoteCommand(ctx, runtime, opts, fmt.Sprintf("open -n %s", remotePathExpr(runtime.AppPath))); err != nil {
		return fmt.Errorf("start remote app: %w", err)
	}
	if err := waitForRemoteHealth(ctx, runtime, opts, opts.ConnectTimeout); err != nil {
		return fmt.Errorf("wait remote health: %w", err)
	}
	return nil
}

func patchRemoteConfig(ctx context.Context, runtime *remoteRuntime, opts runOptions, patch acceptanceConfigPatch) error {
	updated, err := mergeP2PConfig(runtime.OriginalConfig, patch)
	if err != nil {
		return err
	}
	if err := writeRemoteConfig(ctx, runtime, opts, updated); err != nil {
		return err
	}
	if _, err := runRemoteCommand(ctx, runtime, opts, fmt.Sprintf("curl -fsS -X POST %s/api/config/reload", shellQuote(runtime.BaseURL))); err != nil {
		return fmt.Errorf("remote config reload failed: %w", err)
	}
	return nil
}

func patchLocalConfig(configPath string, patch acceptanceConfigPatch) error {
	raw, err := os.ReadFile(configPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	updated, err := mergeP2PConfig(raw, patch)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(configPath, updated, 0o644)
}

func postLocalReload(ctx context.Context, baseURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/config/reload", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("reload returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func fetchLocalStatus(ctx context.Context, baseURL string) (p2pStatusEnvelope, error) {
	var status p2pStatusEnvelope
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/p2p/status", nil)
	if err != nil {
		return status, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return status, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return status, fmt.Errorf("status returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return status, err
	}
	return status, nil
}

func fetchRemoteStatus(ctx context.Context, runtime *remoteRuntime, opts runOptions) (p2pStatusEnvelope, error) {
	var status p2pStatusEnvelope
	out, err := runRemoteCommand(ctx, runtime, opts, fmt.Sprintf("curl -fsS %s/api/p2p/status", shellQuote(runtime.BaseURL)))
	if err != nil {
		return status, err
	}
	if err := json.Unmarshal(out, &status); err != nil {
		return status, fmt.Errorf("decode remote status: %w", err)
	}
	return status, nil
}

func waitForLocalPeer(ctx context.Context, baseURL string, peerID string, timeout time.Duration, shouldBePresent bool) error {
	deadline := time.Now().Add(timeout)
	for {
		peers, err := fetchLocalPeers(ctx, baseURL)
		if err == nil {
			hasPeer := containsString(peers, peerID)
			if hasPeer == shouldBePresent {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("peer %s presence=%v not observed within %s", peerID, shouldBePresent, timeout)
		}
		time.Sleep(1 * time.Second)
	}
}

func waitForRemotePeer(ctx context.Context, runtime *remoteRuntime, opts runOptions, peerID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		peers, err := fetchRemotePeers(ctx, runtime, opts)
		if err == nil && containsString(peers, peerID) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("peer %s not visible on remote after %s", peerID, timeout)
		}
		time.Sleep(1 * time.Second)
	}
}

func fetchLocalPeers(ctx context.Context, baseURL string) ([]string, error) {
	var peers peersEnvelope
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/p2p/peers", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("local peers returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(&peers); err != nil {
		return nil, err
	}
	return peers.Data, nil
}

func fetchRemotePeers(ctx context.Context, runtime *remoteRuntime, opts runOptions) ([]string, error) {
	out, err := runRemoteCommand(ctx, runtime, opts, fmt.Sprintf("curl -fsS %s/api/p2p/peers", shellQuote(runtime.BaseURL)))
	if err != nil {
		return nil, err
	}
	var peers peersEnvelope
	if err := json.Unmarshal(out, &peers); err != nil {
		return nil, fmt.Errorf("decode remote peers: %w", err)
	}
	return peers.Data, nil
}

func verifyFallback(ctx context.Context, local *localRuntime, opts runOptions) error {
	localPinURL := fmt.Sprintf("%s/api/pin/%s", local.BaseURL, opts.FallbackPinID)
	status, _, err := localHTTPStatus(ctx, localPinURL)
	if err != nil {
		return fmt.Errorf("direct local fallback check failed: %w", err)
	}
	if status/100 == 2 {
		return fmt.Errorf("expected local miss for %s, got HTTP %d", opts.FallbackPinID, status)
	}

	rpcURL := fmt.Sprintf("%s/api/metaid/pin/%s?persist=false", local.RPCBaseURL, opts.FallbackPinID)
	respStatus, body, err := localHTTPStatus(ctx, rpcURL)
	if err != nil {
		return fmt.Errorf("local RPC fallback check failed: %w", err)
	}
	if respStatus/100 != 2 {
		return fmt.Errorf("expected fallback hit for %s, got HTTP %d body=%s", opts.FallbackPinID, respStatus, strings.TrimSpace(string(body)))
	}
	var rpcResp metaidRPCResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return fmt.Errorf("decode fallback rpc response: %w", err)
	}
	if !rpcResp.Success || rpcResp.Data.ID != opts.FallbackPinID {
		return fmt.Errorf("unexpected fallback rpc payload: %s", strings.TrimSpace(string(body)))
	}
	logf("fallback check passed for %s", opts.FallbackPinID)
	return nil
}

func publishSyntheticPin(ctx context.Context, bootstrapNodes []string, pinID string) error {
	dataDir, err := os.MkdirTemp("", "alpha-publish-helper-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dataDir)

	configPath := filepath.Join(dataDir, "p2p-config.json")
	configRaw, err := mergeP2PConfig(nil, acceptanceConfigPatch{SyncMode: "full", BootstrapNodes: bootstrapNodes})
	if err != nil {
		return err
	}
	if err := os.WriteFile(configPath, configRaw, 0o600); err != nil {
		return err
	}
	if err := p2p.LoadConfig(configPath); err != nil {
		return err
	}
	if err := p2p.InitHost(ctx, dataDir); err != nil {
		return err
	}
	defer p2p.CloseHost()

	p2p.GetPinFn = func(requestPinID string) (*p2p.PinResponse, error) {
		if requestPinID != pinID {
			return nil, fmt.Errorf("not found")
		}
		body := []byte("alpha-live-content")
		return &p2p.PinResponse{Pin: &pin.PinInscription{
			Id:                pinID,
			Path:              "/protocols/metabot-skill",
			Address:           "1AlphaLiveAddress",
			MetaId:            "alpha-live-metaid",
			ChainName:         "mvc",
			Timestamp:         time.Now().Unix(),
			GenesisHeight:     999999,
			ContentType:       "text/plain",
			ContentTypeDetect: "text/plain; charset=utf-8",
			ContentBody:       body,
			ContentLength:     uint64(len(body)),
			ContentSummary:    "alpha-live-content",
		}}, nil
	}
	p2p.RegisterSyncHandler()
	if err := p2p.InitGossip(ctx); err != nil {
		return err
	}

	time.Sleep(3 * time.Second)
	if err := p2p.PublishPin(ctx, p2p.PinAnnouncement{
		PinId:         pinID,
		Path:          "/protocols/metabot-skill",
		Address:       "1AlphaLiveAddress",
		MetaId:        "alpha-live-metaid",
		ChainName:     "mvc",
		Timestamp:     time.Now().Unix(),
		GenesisHeight: 999999,
		Confirmed:     true,
		SizeBytes:     int64(len("alpha-live-content")),
	}); err != nil {
		return err
	}
	logf("published synthetic PIN %s from helper peer %s", pinID, p2p.Node.ID())
	time.Sleep(5 * time.Second)
	return nil
}

func waitForLocalPin(ctx context.Context, baseURL string, pinID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	url := fmt.Sprintf("%s/api/pin/%s", baseURL, pinID)
	for {
		status, _, err := localHTTPStatus(ctx, url)
		if err == nil && status/100 == 2 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("local pin %s not available within %s", pinID, timeout)
		}
		time.Sleep(1 * time.Second)
	}
}

func waitForRemotePin(ctx context.Context, runtime *remoteRuntime, opts runOptions, pinID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		status, _, err := remoteHTTPStatus(ctx, runtime, opts, fmt.Sprintf("%s/api/pin/%s", runtime.BaseURL, pinID))
		if err == nil && status/100 == 2 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("remote pin %s not available within %s", pinID, timeout)
		}
		time.Sleep(1 * time.Second)
	}
}

func cleanupLocalRuntime(ctx context.Context, local *localRuntime, opts runOptions) {
	if !opts.Cleanup {
		return
	}
	after, err := listLocalPIDs(local.BinaryPattern)
	if err == nil {
		killPIDs(ctx, diffPIDs(local.BeforePIDs, after))
	}
	_ = os.RemoveAll(local.Root)
}

func cleanupRemoteRuntime(ctx context.Context, runtime *remoteRuntime, opts runOptions) {
	if runtime.OriginalConfigRead {
		if err := writeRemoteConfig(ctx, runtime, opts, runtime.OriginalConfig); err == nil {
			_, _ = runRemoteCommand(ctx, runtime, opts, fmt.Sprintf("curl -fsS -X POST %s/api/config/reload >/dev/null", shellQuote(runtime.BaseURL)))
		}
	}
	if runtime.ExistingRuntime {
		return
	}
	after, err := listRemotePIDs(ctx, runtime, opts, runtime.BinaryPattern)
	if err == nil {
		killRemotePIDs(ctx, runtime, opts, diffPIDs(runtime.BeforePIDs, after))
	}
}

func copyRemoteApp(ctx context.Context, runtime *remoteRuntime, opts runOptions) error {
	remoteDirRaw := pathpkg.Dir(runtime.AppPath)
	remoteDirExpr := remotePathExpr(remoteDirRaw)
	remoteAppExpr := remotePathExpr(runtime.AppPath)
	if _, err := runRemoteCommand(ctx, runtime, opts, fmt.Sprintf("mkdir -p %s && rm -rf %s", remoteDirExpr, remoteAppExpr)); err != nil {
		return fmt.Errorf("prepare remote app dir: %w", err)
	}
	if _, err := runMaybeExpect(ctx, opts.RemotePassword, "scp", append(scpOptions(opts.RemotePassword), "-r", opts.LocalApp, fmt.Sprintf("%s:%s", runtime.Target, remoteDirRaw))...); err != nil {
		return fmt.Errorf("copy remote app: %w", err)
	}
	localBase := filepath.Base(opts.LocalApp)
	remoteBase := pathpkg.Base(runtime.AppPath)
	if localBase != remoteBase {
		localCopied := remotePathExpr(pathpkg.Join(remoteDirRaw, localBase))
		if _, err := runRemoteCommand(ctx, runtime, opts, fmt.Sprintf("mv %s %s", localCopied, remoteAppExpr)); err != nil {
			return fmt.Errorf("rename remote app: %w", err)
		}
	}
	return nil
}

func restartRemoteManP2P(ctx context.Context, runtime *remoteRuntime, opts runOptions) error {
	pids, err := listRemotePIDs(ctx, runtime, opts, runtime.ManBinaryPattern)
	if err != nil {
		return err
	}
	if len(pids) == 0 {
		logf("remote man-p2p child not found; restarting remote app bundle instead")
		return restartRemoteApp(ctx, runtime, opts)
	}
	if err := killRemotePIDs(ctx, runtime, opts, pids); err != nil {
		return err
	}
	return waitForRemoteHealth(ctx, runtime, opts, opts.ConnectTimeout)
}

func restartRemoteApp(ctx context.Context, runtime *remoteRuntime, opts runOptions) error {
	appPIDs, err := listRemotePIDs(ctx, runtime, opts, runtime.BinaryPattern)
	if err != nil {
		return err
	}
	if err := killRemotePIDs(ctx, runtime, opts, appPIDs); err != nil {
		return err
	}
	if _, err := runRemoteCommand(ctx, runtime, opts, fmt.Sprintf("open -n %s", remotePathExpr(runtime.AppPath))); err != nil {
		return err
	}
	return waitForRemoteHealth(ctx, runtime, opts, opts.ConnectTimeout)
}

func readRemoteConfig(ctx context.Context, runtime *remoteRuntime, opts runOptions) ([]byte, error) {
	out, err := runRemoteCommand(ctx, runtime, opts, fmt.Sprintf("cat %s 2>/dev/null || true", remotePathExpr(runtime.ConfigPath)))
	if err != nil {
		return nil, err
	}
	return bytes.TrimSpace(out), nil
}

func writeRemoteConfig(ctx context.Context, runtime *remoteRuntime, opts runOptions, raw []byte) error {
	localTemp, err := os.CreateTemp("", "alpha-remote-config-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(localTemp.Name())
	if _, err := localTemp.Write(raw); err != nil {
		localTemp.Close()
		return err
	}
	if err := localTemp.Close(); err != nil {
		return err
	}
	remoteTemp := fmt.Sprintf("/tmp/alpha-remote-config-%d.json", time.Now().UnixNano())
	if _, err := runMaybeExpect(ctx, opts.RemotePassword, "scp", append(scpOptions(opts.RemotePassword), localTemp.Name(), fmt.Sprintf("%s:%s", runtime.Target, remoteTemp))...); err != nil {
		return err
	}
	configDirExpr := remotePathExpr(pathpkg.Dir(runtime.ConfigPath))
	configExpr := remotePathExpr(runtime.ConfigPath)
	if _, err := runRemoteCommand(ctx, runtime, opts, fmt.Sprintf("mkdir -p %s && mv %s %s", configDirExpr, shellQuote(remoteTemp), configExpr)); err != nil {
		return err
	}
	return nil
}

func waitForLocalHealth(ctx context.Context, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err == nil {
			resp, err := http.DefaultClient.Do(req)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return nil
				}
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("health %s not ready within %s", url, timeout)
		}
		time.Sleep(1 * time.Second)
	}
}

func waitForRemoteHealth(ctx context.Context, runtime *remoteRuntime, opts runOptions, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		cmd := fmt.Sprintf("curl -fsS %s/health >/dev/null", shellQuote(runtime.BaseURL))
		if _, err := runRemoteCommand(ctx, runtime, opts, cmd); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("remote health %s not ready within %s", runtime.BaseURL, timeout)
		}
		time.Sleep(1 * time.Second)
	}
}

func localHTTPStatus(ctx context.Context, url string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

func remoteHTTPStatus(ctx context.Context, runtime *remoteRuntime, opts runOptions, url string) (int, []byte, error) {
	cmd := fmt.Sprintf("tmp=$(mktemp); code=$(curl -s -o \"$tmp\" -w '%%{http_code}' %s || true); cat \"$tmp\"; printf '\n__HTTP_STATUS__%%s' \"$code\"; rm -f \"$tmp\"", shellQuote(url))
	out, err := runRemoteCommand(ctx, runtime, opts, cmd)
	if err != nil {
		return 0, nil, err
	}
	marker := []byte("\n__HTTP_STATUS__")
	idx := bytes.LastIndex(out, marker)
	if idx < 0 {
		return 0, nil, fmt.Errorf("remote response missing status marker")
	}
	statusCode, err := strconv.Atoi(strings.TrimSpace(string(out[idx+len(marker):])))
	if err != nil {
		return 0, nil, err
	}
	body := bytes.TrimSuffix(out[:idx], []byte("\n"))
	return statusCode, body, nil
}

func reservePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("listener did not return tcp addr")
	}
	return addr.Port, nil
}

func appBinaryPattern(appPath string) string {
	base := filepath.Base(strings.TrimSuffix(appPath, ".app"))
	return pathpkg.Join(appPath, "Contents", "MacOS", base)
}

func listLocalPIDs(pattern string) ([]int, error) {
	out, err := exec.Command("pgrep", "-f", pattern).CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("pgrep %s: %w (%s)", pattern, err, strings.TrimSpace(string(out)))
	}
	return parsePIDOutput(out)
}

func listRemotePIDs(ctx context.Context, runtime *remoteRuntime, opts runOptions, pattern string) ([]int, error) {
	out, err := runRemoteCommand(ctx, runtime, opts, fmt.Sprintf("pgrep -f %s || true", shellQuote(remoteProcessPattern(pattern))))
	if err != nil {
		return nil, err
	}
	return parsePIDOutput(out)
}

func parsePIDOutput(raw []byte) ([]int, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, nil
	}
	lines := strings.Fields(trimmed)
	pids := make([]int, 0, len(lines))
	for _, line := range lines {
		pid, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil {
			return nil, fmt.Errorf("parse pid %q: %w", line, err)
		}
		pids = append(pids, pid)
	}
	sort.Ints(pids)
	return pids, nil
}

func killPIDs(ctx context.Context, pids []int) {
	for _, pid := range pids {
		_, _ = runLocalCommand(ctx, "kill", "-9", strconv.Itoa(pid))
	}
}

func killRemotePIDs(ctx context.Context, runtime *remoteRuntime, opts runOptions, pids []int) error {
	if len(pids) == 0 {
		return nil
	}
	parts := make([]string, 0, len(pids))
	for _, pid := range pids {
		parts = append(parts, strconv.Itoa(pid))
	}
	_, err := runRemoteCommand(ctx, runtime, opts, fmt.Sprintf("kill -9 %s", strings.Join(parts, " ")))
	return err
}

func diffPIDs(before, after []int) []int {
	seen := map[int]struct{}{}
	for _, pid := range before {
		seen[pid] = struct{}{}
	}
	var diff []int
	for _, pid := range after {
		if _, ok := seen[pid]; !ok {
			diff = append(diff, pid)
		}
	}
	return diff
}

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func runLocalCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w (%s)", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func runRemoteCommand(ctx context.Context, runtime *remoteRuntime, opts runOptions, remoteCmd string) ([]byte, error) {
	return runMaybeExpect(ctx, opts.RemotePassword, "ssh", append(sshOptions(opts.RemotePassword), runtime.Target, remoteCmd)...)
}

func runMaybeExpect(ctx context.Context, password string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if password != "" {
		askpassPath, err := writeAskpassScript()
		if err != nil {
			return nil, err
		}
		defer os.Remove(askpassPath)
		cmd.Env = append(os.Environ(),
			"DISPLAY=1",
			"SSH_ASKPASS="+askpassPath,
			"SSH_ASKPASS_REQUIRE=force",
			"IDBOTS_REMOTE_PASSWORD="+password,
		)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w (%s)", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func writeAskpassScript() (string, error) {
	script, err := os.CreateTemp("", "alpha-askpass-*.sh")
	if err != nil {
		return "", err
	}
	scriptBody := "#!/bin/sh\nprintf '%s\\n' \"$IDBOTS_REMOTE_PASSWORD\"\n"
	if _, err := script.WriteString(scriptBody); err != nil {
		script.Close()
		os.Remove(script.Name())
		return "", err
	}
	if err := script.Close(); err != nil {
		os.Remove(script.Name())
		return "", err
	}
	if err := os.Chmod(script.Name(), 0o700); err != nil {
		os.Remove(script.Name())
		return "", err
	}
	return script.Name(), nil
}

func sshOptions(password string) []string {
	options := []string{"-o", "StrictHostKeyChecking=no"}
	if password != "" {
		options = append(options,
			"-o", "PreferredAuthentications=password",
			"-o", "PubkeyAuthentication=no",
			"-o", "IdentitiesOnly=yes",
			"-o", "NumberOfPasswordPrompts=1",
		)
	}
	return options
}

func scpOptions(password string) []string {
	options := []string{"-o", "StrictHostKeyChecking=no"}
	if password != "" {
		options = append(options,
			"-o", "PreferredAuthentications=password",
			"-o", "PubkeyAuthentication=no",
			"-o", "IdentitiesOnly=yes",
			"-o", "NumberOfPasswordPrompts=1",
		)
	}
	return options
}

func remotePathExpr(path string) string {
	if strings.HasPrefix(path, "~/") {
		return fmt.Sprintf("\"$HOME/%s\"", escapeDoubleQuoted(strings.TrimPrefix(path, "~/")))
	}
	return shellQuote(path)
}

func remoteProcessPattern(path string) string {
	if strings.HasPrefix(path, "~/") {
		return strings.TrimPrefix(path, "~")
	}
	return path
}

func escapeDoubleQuoted(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "$", "\\$", "`", "\\`")
	return replacer.Replace(value)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[alpha-acceptance] "+format+"\n", args...)
}
