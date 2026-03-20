package p2p

import (
	"context"
	"io/fs"
	"log"
	"path/filepath"
	"sync/atomic"
	"time"
)

var (
	storageLimitReached atomic.Bool
	storageUsedBytes    atomic.Int64
)

func StartStorageMonitor(ctx context.Context, dataDir string) {
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		checkStorage(dataDir)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				checkStorage(dataDir)
			}
		}
	}()
}

func checkStorage(dataDir string) {
	var total int64
	_ = filepath.WalkDir(dataDir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err == nil {
			total += info.Size()
		}
		return nil
	})
	storageUsedBytes.Store(total)

	cfg := GetConfig()
	limitBytes := int64(cfg.StorageLimitGB * 1024 * 1024 * 1024)
	if limitBytes > 0 && total >= limitBytes {
		if !storageLimitReached.Load() {
			log.Printf("storage: limit reached (%.2f GB, limit %.2f GB) — P2P sync paused",
				float64(total)/(1<<30), cfg.StorageLimitGB)
		}
		storageLimitReached.Store(true)
	} else {
		storageLimitReached.Store(false)
	}
}

func GetStatus() map[string]interface{} {
	peerCount := 0
	if Node != nil {
		peerCount = len(Node.Network().Peers())
	}
	return map[string]interface{}{
		"peerCount":           peerCount,
		"syncProgress":        0.0,
		"dataSource":          "p2p",
		"storageLimitReached": storageLimitReached.Load(),
		"storageUsedBytes":    storageUsedBytes.Load(),
	}
}

func GetPeers() []string {
	if Node == nil {
		return []string{}
	}
	peers := Node.Network().Peers()
	ids := make([]string, len(peers))
	for i, p := range peers {
		ids[i] = p.String()
	}
	return ids
}
