package p2p

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"man-p2p/pin"
)

const (
	helperModeSource = "source"
	helperModeSink   = "sink"
)

type helperReady struct {
	Addr string `json:"addr"`
}

type helperStoredPin struct {
	PinID   string `json:"pinId"`
	Content string `json:"content"`
}

func TestAlphaDualProcessRealtimeSync(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	baseDir := t.TempDir()
	sourceReadyFile := filepath.Join(baseDir, "source-ready.json")
	sinkReadyFile := filepath.Join(baseDir, "sink-ready.json")
	triggerFile := filepath.Join(baseDir, "publish.trigger")
	resultFile := filepath.Join(baseDir, "sink-result.json")

	sourceCmd, sourceLog := helperCommand(ctx, t, helperModeSource, map[string]string{
		"MAN_P2P_HELPER_DATA_DIR":   filepath.Join(baseDir, "source"),
		"MAN_P2P_HELPER_READY_FILE": sourceReadyFile,
		"MAN_P2P_HELPER_TRIGGER":    triggerFile,
	})
	if err := sourceCmd.Start(); err != nil {
		t.Fatalf("start source helper: %v", err)
	}
	defer stopHelper(t, sourceCmd, sourceLog)

	sourceReady := waitForJSON[helperReady](t, sourceReadyFile, 8*time.Second)

	sinkCmd, sinkLog := helperCommand(ctx, t, helperModeSink, map[string]string{
		"MAN_P2P_HELPER_DATA_DIR":    filepath.Join(baseDir, "sink"),
		"MAN_P2P_HELPER_READY_FILE":  sinkReadyFile,
		"MAN_P2P_HELPER_RESULT_FILE": resultFile,
		"MAN_P2P_HELPER_BOOTSTRAP":   sourceReady.Addr,
	})
	if err := sinkCmd.Start(); err != nil {
		t.Fatalf("start sink helper: %v", err)
	}
	defer stopHelper(t, sinkCmd, sinkLog)

	_ = waitForJSON[helperReady](t, sinkReadyFile, 8*time.Second)

	if err := os.WriteFile(triggerFile, []byte("publish"), 0600); err != nil {
		t.Fatalf("write trigger file: %v", err)
	}

	result := waitForJSON[helperStoredPin](t, resultFile, 10*time.Second)
	if result.PinID != "alpha-pin-process-001" {
		t.Fatalf("expected sink pin alpha-pin-process-001, got %q", result.PinID)
	}
	if result.Content != "Alice" {
		t.Fatalf("expected sink content Alice, got %q", result.Content)
	}

	if err := sinkCmd.Wait(); err != nil {
		t.Fatalf("sink helper failed: %v\n%s", err, sinkLog.String())
	}
}

func TestAlphaDualProcessHelper(t *testing.T) {
	mode := os.Getenv("MAN_P2P_HELPER_MODE")
	if mode == "" {
		return
	}

	switch mode {
	case helperModeSource:
		runSourceHelper(t)
	case helperModeSink:
		runSinkHelper(t)
	default:
		t.Fatalf("unknown helper mode %q", mode)
	}
}

func runSourceHelper(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Second)
	defer cancel()

	readyFile := requireEnv(t, "MAN_P2P_HELPER_READY_FILE")
	triggerFile := requireEnv(t, "MAN_P2P_HELPER_TRIGGER")
	dataDir := requireEnv(t, "MAN_P2P_HELPER_DATA_DIR")

	if err := LoadConfig(writeTempConfig(t, `{"p2p_sync_mode":"full"}`)); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if err := InitHost(ctx, dataDir); err != nil {
		t.Fatalf("InitHost: %v", err)
	}
	defer CloseHost()

	GetPinFn = func(pinID string) (*PinResponse, error) {
		if pinID != "alpha-pin-process-001" {
			return nil, fmt.Errorf("not found")
		}
		return &PinResponse{Pin: &pin.PinInscription{
			Id:            "alpha-pin-process-001",
			Path:          "/info/name",
			Address:       "1AlphaAddr",
			MetaId:        "alpha-metaid",
			ChainName:     "btc",
			Timestamp:     1710000000,
			GenesisHeight: 900000,
			ContentBody:   []byte("Alice"),
			ContentLength: 5,
		}}, nil
	}
	RegisterSyncHandler()
	if err := InitGossip(ctx); err != nil {
		t.Fatalf("InitGossip: %v", err)
	}

	if err := writeJSON(readyFile, helperReady{Addr: dialableAddr(t)}); err != nil {
		t.Fatalf("write source ready: %v", err)
	}

	if err := waitForFile(triggerFile, 12*time.Second); err != nil {
		t.Fatalf("wait trigger: %v", err)
	}

	if err := PublishPin(ctx, PinAnnouncement{
		PinId:         "alpha-pin-process-001",
		Path:          "/info/name",
		Address:       "1AlphaAddr",
		MetaId:        "alpha-metaid",
		ChainName:     "btc",
		Timestamp:     1710000000,
		GenesisHeight: 900000,
		Confirmed:     true,
		SizeBytes:     5,
	}); err != nil {
		t.Fatalf("PublishPin: %v", err)
	}

	<-ctx.Done()
}

func runSinkHelper(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Second)
	defer cancel()

	readyFile := requireEnv(t, "MAN_P2P_HELPER_READY_FILE")
	resultFile := requireEnv(t, "MAN_P2P_HELPER_RESULT_FILE")
	bootstrapAddr := requireEnv(t, "MAN_P2P_HELPER_BOOTSTRAP")
	dataDir := requireEnv(t, "MAN_P2P_HELPER_DATA_DIR")

	cfgPath := writeTempConfig(t, fmt.Sprintf(`{
        "p2p_sync_mode":"full",
        "p2p_bootstrap_nodes":["%s"]
    }`, bootstrapAddr))
	if err := LoadConfig(cfgPath); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if err := InitHost(ctx, dataDir); err != nil {
		t.Fatalf("InitHost: %v", err)
	}
	defer CloseHost()
	if err := InitGossip(ctx); err != nil {
		t.Fatalf("InitGossip: %v", err)
	}

	done := make(chan struct{}, 1)
	StorePinFn = func(resp *PinResponse) error {
		if resp == nil || resp.Pin == nil {
			return fmt.Errorf("missing pin payload")
		}
		if err := writeJSON(resultFile, helperStoredPin{
			PinID:   resp.Pin.Id,
			Content: string(resp.Pin.ContentBody),
		}); err != nil {
			return err
		}
		select {
		case done <- struct{}{}:
		default:
		}
		return nil
	}

	if err := waitForPeerCount(1, 8*time.Second); err != nil {
		t.Fatalf("wait peer: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	if err := writeJSON(readyFile, helperReady{Addr: dialableAddr(t)}); err != nil {
		t.Fatalf("write sink ready: %v", err)
	}

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for synced pin")
	}
}

func helperCommand(ctx context.Context, t *testing.T, mode string, extraEnv map[string]string) (*exec.Cmd, *bytes.Buffer) {
	t.Helper()

	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run", "^TestAlphaDualProcessHelper$")
	cmd.Env = append(os.Environ(), "MAN_P2P_HELPER_MODE="+mode)
	for key, value := range extraEnv {
		cmd.Env = append(cmd.Env, key+"="+value)
	}

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	return cmd, &output
}

func stopHelper(t *testing.T, cmd *exec.Cmd, output *bytes.Buffer) {
	t.Helper()
	if cmd == nil || cmd.Process == nil {
		return
	}
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		return
	}
	_ = cmd.Process.Kill()
	err := cmd.Wait()
	logOutput := t.Failed()
	if err != nil && !strings.Contains(err.Error(), "signal: killed") {
		logOutput = true
	}
	if output != nil && output.Len() > 0 && logOutput {
		t.Logf("helper output:\n%s", output.String())
	}
}

func waitForJSON[T any](t *testing.T, path string, timeout time.Duration) T {
	t.Helper()

	if err := waitForFile(path, timeout); err != nil {
		t.Fatalf("wait for %s: %v", path, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return out
}

func waitForFile(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s", path)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func waitForPeerCount(min int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if Node != nil && len(Node.Network().Peers()) >= min {
			return nil
		}
		if time.Now().After(deadline) {
			count := 0
			if Node != nil {
				count = len(Node.Network().Peers())
			}
			return fmt.Errorf("expected at least %d peers, got %d", min, count)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func requireEnv(t *testing.T, key string) string {
	t.Helper()

	value := os.Getenv(key)
	if value == "" {
		t.Fatalf("missing env %s", key)
	}
	return value
}

func dialableAddr(t *testing.T) string {
	t.Helper()

	if Node == nil {
		t.Fatal("host not initialized")
	}
	for _, addr := range Node.Addrs() {
		addrStr := addr.String()
		addrStr = strings.Replace(addrStr, "/ip4/0.0.0.0/", "/ip4/127.0.0.1/", 1)
		addrStr = strings.Replace(addrStr, "/ip6/::/", "/ip6/::1/", 1)
		return fmt.Sprintf("%s/p2p/%s", addrStr, Node.ID())
	}
	t.Fatal("host has no listen addresses")
	return ""
}

func writeJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
