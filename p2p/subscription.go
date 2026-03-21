package p2p

import (
	"path/filepath"
	"strings"
)

// ShouldSync returns true if the PIN announcement passes the sync filter.
// MaxContentSizeKB is NOT checked here — oversized PINs still sync (metadata only).
func ShouldSync(ann PinAnnouncement) bool {
	cfg := GetConfig()

	if isBlocked(ann, cfg) {
		return false
	}

	switch cfg.SyncMode {
	case "full":
		return true
	case "self":
		return isOwnAddress(ann.Address, cfg)
	case "selective":
		return isInSelectiveList(ann, cfg)
	default:
		return false
	}
}

func isBlocked(ann PinAnnouncement, cfg P2PSyncConfig) bool {
	for _, addr := range cfg.BlockAddresses {
		if addr == ann.Address {
			return true
		}
	}
	for _, pattern := range cfg.BlockPaths {
		if matched, _ := filepath.Match(pattern, ann.Path); matched {
			return true
		}
	}
	return false
}

func isInSelectiveList(ann PinAnnouncement, cfg P2PSyncConfig) bool {
	for _, addr := range cfg.SelectiveAddresses {
		if addr == ann.Address {
			return true
		}
	}
	for _, pattern := range cfg.SelectivePaths {
		if matched, _ := filepath.Match(pattern, ann.Path); matched {
			return true
		}
		if !strings.Contains(pattern, "*") && strings.HasPrefix(ann.Path, pattern) {
			return true
		}
	}
	return false
}

var OwnAddresses []string

func isOwnAddress(address string, cfg P2PSyncConfig) bool {
	for _, a := range cfg.OwnAddresses {
		if a == address {
			return true
		}
	}
	for _, a := range OwnAddresses {
		if a == address {
			return true
		}
	}
	return false
}
