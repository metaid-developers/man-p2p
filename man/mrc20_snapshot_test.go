package man

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteSnapshotRejectsInvalidIDs(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"..", "../elsewhere", "a/b", `a\b`, "", "id with space"} {
		if err := DeleteSnapshot(dir, id); err == nil {
			t.Fatalf("DeleteSnapshot(%q) must be rejected", id)
		}
	}
}

func TestDeleteSnapshotRejectsTraversalEvenInsideSnapshotDir(t *testing.T) {
	// 恶意 ID 指向 snapshotDir 内已存在的目录时也不允许（白名单一律拒绝）
	dir := t.TempDir()
	real := filepath.Join(dir, "2026-11-20_15-04-05")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := DeleteSnapshot(dir, "2026-11-20_15-04-05"); err != nil {
		t.Fatalf("legit id should delete fine: %v", err)
	}
	if _, err := os.Stat(real); !os.IsNotExist(err) {
		t.Fatal("snapshot dir should have been removed")
	}
}

func TestGetSnapshotInfoAndVerifySnapshotRejectInvalidIDs(t *testing.T) {
	dir := t.TempDir()
	if _, err := GetSnapshotInfo(dir, "../x"); err == nil {
		t.Fatal("GetSnapshotInfo must reject traversal id")
	}
	if err := VerifySnapshot(dir, "a/b"); err == nil {
		t.Fatal("VerifySnapshot must reject traversal id")
	}
}
