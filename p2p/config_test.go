package p2p

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
)

func writeTempConfig(t *testing.T, jsonStr string) string {
	t.Helper()
	f, err := os.CreateTemp("", "p2p-config-*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(jsonStr)
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })
	return f.Name()
}

func TestLoadConfig(t *testing.T) {
	path := writeTempConfig(t, `{
        "p2p_sync_mode": "selective",
        "p2p_selective_addresses": ["1A2B3C"],
        "p2p_max_content_size_kb": 512,
        "p2p_storage_limit_gb": 10,
        "p2p_enable_relay": true,
        "p2p_listen_port": 4001,
        "p2p_announce_addrs": [
            "/ip4/8.217.14.206/tcp/4001",
            "/dns4/manapi.metaid.io/tcp/4001"
        ]
    }`)

	if err := LoadConfig(path); err != nil {
		t.Fatal(err)
	}
	got := GetConfig()
	if got.SyncMode != "selective" {
		t.Errorf("expected selective, got %s", got.SyncMode)
	}
	if got.MaxContentSizeKB != 512 {
		t.Errorf("expected 512, got %d", got.MaxContentSizeKB)
	}
	if len(got.SelectiveAddresses) != 1 || got.SelectiveAddresses[0] != "1A2B3C" {
		t.Errorf("unexpected addresses: %v", got.SelectiveAddresses)
	}
	if !got.EnableRelay {
		t.Error("expected EnableRelay=true")
	}
	if got.ListenPort != 4001 {
		t.Errorf("expected listen port 4001, got %d", got.ListenPort)
	}
	if len(got.AnnounceAddrs) != 2 {
		t.Fatalf("expected 2 announce addrs, got %v", got.AnnounceAddrs)
	}
	if got.AnnounceAddrs[0] != "/ip4/8.217.14.206/tcp/4001" {
		t.Errorf("unexpected first announce addr: %q", got.AnnounceAddrs[0])
	}
}

func TestReloadConfig(t *testing.T) {
	path := writeTempConfig(t, `{"p2p_sync_mode": "self"}`)
	if err := LoadConfig(path); err != nil {
		t.Fatal(err)
	}
	if got := GetConfig(); got.SyncMode != "self" {
		t.Errorf("expected self, got %s", got.SyncMode)
	}

	// Update file content and reload
	os.WriteFile(path, []byte(`{"p2p_sync_mode": "full"}`), 0644)
	if err := ReloadConfig(); err != nil {
		t.Fatal(err)
	}
	if got := GetConfig(); got.SyncMode != "full" {
		t.Errorf("expected full after reload, got %s", got.SyncMode)
	}
}

func TestReloadConfigConnectsNewBootstrapNode(t *testing.T) {
	path := writeTempConfig(t, `{"p2p_sync_mode":"full","p2p_bootstrap_nodes":[]}`)
	if err := LoadConfig(path); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := InitHost(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer CloseHost()

	peerB, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	if err != nil {
		t.Fatal(err)
	}
	defer peerB.Close()

	if got := len(Node.Network().Peers()); got != 0 {
		t.Fatalf("expected no peers before reload, got %d", got)
	}

	bootstrapAddr, err := peer.AddrInfoToP2pAddrs(&peer.AddrInfo{ID: peerB.ID(), Addrs: peerB.Addrs()})
	if err != nil {
		t.Fatal(err)
	}
	if len(bootstrapAddr) == 0 {
		t.Fatal("expected bootstrap multiaddr")
	}

	if err := os.WriteFile(path, []byte(`{"p2p_sync_mode":"full","p2p_bootstrap_nodes":["`+bootstrapAddr[0].String()+`"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ReloadConfig(); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, connected := range Node.Network().Peers() {
			if connected == peerB.ID() {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("expected reload to connect bootstrap peer %s, peers=%v", peerB.ID(), Node.Network().Peers())
}

func TestChainSourceDefaultsToEnabled(t *testing.T) {
	path := writeTempConfig(t, `{}`)
	if err := LoadConfig(path); err != nil {
		t.Fatal(err)
	}
	if !GetConfig().ChainSourceEnabled() {
		t.Fatal("expected chain source to default to enabled")
	}
}

func TestLoadConfigCanDisableChainSource(t *testing.T) {
	path := writeTempConfig(t, `{"p2p_enable_chain_source": false}`)
	if err := LoadConfig(path); err != nil {
		t.Fatal(err)
	}
	if GetConfig().ChainSourceEnabled() {
		t.Fatal("expected chain source to be disabled by config")
	}
}

func TestLoadConfigIncludesPresenceGlobalMetaIDs(t *testing.T) {
	path := writeTempConfig(t, `{
		"p2p_presence_global_metaids": ["idaaa", "idbbb"]
	}`)

	if err := LoadConfig(path); err != nil {
		t.Fatal(err)
	}

	got := GetConfig()
	if len(got.PresenceGlobalMetaIDs) != 2 {
		t.Fatalf("expected 2 presence global metaids, got %v", got.PresenceGlobalMetaIDs)
	}
	if got.PresenceGlobalMetaIDs[0] != "idaaa" || got.PresenceGlobalMetaIDs[1] != "idbbb" {
		t.Fatalf("unexpected presence global metaids: %v", got.PresenceGlobalMetaIDs)
	}
}

func TestReloadConfigUpdatesPresenceGlobalMetaIDs(t *testing.T) {
	path := writeTempConfig(t, `{
		"p2p_presence_global_metaids": ["idold"]
	}`)
	if err := LoadConfig(path); err != nil {
		t.Fatal(err)
	}
	if got := GetConfig().PresenceGlobalMetaIDs; len(got) != 1 || got[0] != "idold" {
		t.Fatalf("expected [idold], got %v", got)
	}

	if err := os.WriteFile(path, []byte(`{"p2p_presence_global_metaids":["idnew1","idnew2"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ReloadConfig(); err != nil {
		t.Fatal(err)
	}

	got := GetConfig().PresenceGlobalMetaIDs
	if len(got) != 2 {
		t.Fatalf("expected 2 presence global metaids after reload, got %v", got)
	}
	if got[0] != "idnew1" || got[1] != "idnew2" {
		t.Fatalf("unexpected presence global metaids after reload: %v", got)
	}
}
