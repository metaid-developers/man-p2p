package p2p

import (
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	minPresenceTTLSeconds = 1
	maxPresenceTTLSeconds = 120
)

type PresenceAnnouncement struct {
	SchemaVersion int      `json:"schemaVersion"`
	PeerID        string   `json:"peerId"`
	SentAt        int64    `json:"sentAt"`
	TTLSec        int      `json:"ttlSec"`
	RuntimeMode   string   `json:"runtimeMode"`
	GlobalMetaIDs []string `json:"globalMetaIds"`
}

type PresenceCache struct {
	mu      sync.RWMutex
	entries map[string]map[string]time.Time // globalMetaID -> peerID -> expiry
}

func NewPresenceCache() *PresenceCache {
	return &PresenceCache{
		entries: make(map[string]map[string]time.Time),
	}
}

func (c *PresenceCache) Observe(receivedFrom string, ann PresenceAnnouncement, receivedAt time.Time) {
	peerID := strings.TrimSpace(receivedFrom)
	if peerID == "" {
		return
	}
	expiresAt := receivedAt.Add(time.Duration(clampPresenceTTLSeconds(ann.TTLSec)) * time.Second)

	seen := make(map[string]struct{})

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, raw := range ann.GlobalMetaIDs {
		globalMetaID, ok := canonicalPresenceGlobalMetaID(raw)
		if !ok {
			continue
		}
		if _, exists := seen[globalMetaID]; exists {
			continue
		}
		seen[globalMetaID] = struct{}{}

		peers, ok := c.entries[globalMetaID]
		if !ok {
			peers = make(map[string]time.Time)
			c.entries[globalMetaID] = peers
		}
		peers[peerID] = expiresAt
	}
}

func (c *PresenceCache) Snapshot(now time.Time) map[string][]string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make(map[string][]string)
	for globalMetaID, peers := range c.entries {
		var active []string
		for peerID, expiresAt := range peers {
			if now.Before(expiresAt) {
				active = append(active, peerID)
			}
		}
		if len(active) == 0 {
			continue
		}
		sort.Strings(active)
		out[globalMetaID] = active
	}
	return out
}

type presenceMembership struct {
	mu         sync.RWMutex
	globalIDs map[string]struct{}
}

var localPresenceMembership = &presenceMembership{
	globalIDs: make(map[string]struct{}),
}

func reloadPresenceLocalMembershipFromConfig() {
	cfg := GetConfig()
	localPresenceMembership.reload(cfg.PresenceGlobalMetaIDs)
}

func (m *presenceMembership) reload(rawGlobalMetaIDs []string) {
	next := make(map[string]struct{})
	for _, raw := range rawGlobalMetaIDs {
		globalMetaID, ok := canonicalPresenceGlobalMetaID(raw)
		if !ok {
			continue
		}
		next[globalMetaID] = struct{}{}
	}

	m.mu.Lock()
	m.globalIDs = next
	m.mu.Unlock()
}

func LocalPresenceGlobalMetaIDs() []string {
	localPresenceMembership.mu.RLock()
	defer localPresenceMembership.mu.RUnlock()

	out := make([]string, 0, len(localPresenceMembership.globalIDs))
	for id := range localPresenceMembership.globalIDs {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func IsLocalPresenceGlobalMetaID(globalMetaID string) bool {
	canonical, ok := canonicalPresenceGlobalMetaID(globalMetaID)
	if !ok {
		return false
	}

	localPresenceMembership.mu.RLock()
	defer localPresenceMembership.mu.RUnlock()
	_, exists := localPresenceMembership.globalIDs[canonical]
	return exists
}

func canonicalPresenceGlobalMetaID(raw string) (string, bool) {
	canonical := strings.ToLower(strings.TrimSpace(raw))
	if canonical == "" {
		return "", false
	}
	if strings.HasPrefix(canonical, "metaid:") {
		return "", false
	}
	if !strings.HasPrefix(canonical, "id") {
		return "", false
	}
	return canonical, true
}

func clampPresenceTTLSeconds(ttlSec int) int {
	if ttlSec < minPresenceTTLSeconds {
		return minPresenceTTLSeconds
	}
	if ttlSec > maxPresenceTTLSeconds {
		return maxPresenceTTLSeconds
	}
	return ttlSec
}
