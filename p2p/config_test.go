package p2p

import (
	"os"
	"testing"
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
        "p2p_enable_relay": true
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
