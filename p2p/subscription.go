package p2p

import (
	"path/filepath"
	"strings"
)

// PinAnnouncement represents a PIN broadcast received from a peer.
// NOTE: This struct is temporarily defined here until gossip.go is implemented.
// When gossip.go is added, this definition must be removed to avoid a redeclaration error.
type PinAnnouncement struct {
	PinId     string
	Address   string
	Path      string
	SizeBytes int64
}

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
		return isOwnAddress(ann.Address)
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

func isOwnAddress(address string) bool {
	for _, a := range OwnAddresses {
		if a == address {
			return true
		}
	}
	return false
}
