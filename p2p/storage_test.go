package p2p

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStorageLimitEnforcement(t *testing.T) {
	dir := t.TempDir()
	// Write 2 KiB so it exceeds a 1-KiB-equivalent limit
	os.WriteFile(filepath.Join(dir, "data.bin"), make([]byte, 2048), 0644)

	_ = LoadConfig(writeTempConfig(t, `{"p2p_storage_limit_gb": 0.000001}`))
	checkStorage(dir)
	if !storageLimitReached.Load() {
		t.Error("expected storageLimitReached=true")
	}

	_ = LoadConfig(writeTempConfig(t, `{"p2p_storage_limit_gb": 100}`))
	checkStorage(dir)
	if storageLimitReached.Load() {
		t.Error("expected storageLimitReached=false after raising limit")
	}
}
