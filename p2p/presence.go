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

type PresenceBotState struct {
	LastSeenSec  int64    `json:"lastSeenSec,omitempty"`
	ExpiresAtSec int64    `json:"expiresAtSec,omitempty"`
	PeerIDs      []string `json:"peerIds"`
}

type PresenceStatus struct {
	Healthy               bool                        `json:"healthy"`
	PeerCount             int                         `json:"peerCount"`
	UnhealthyReason       string                      `json:"unhealthyReason,omitempty"`
	LastConfigReloadError string                      `json:"lastConfigReloadError,omitempty"`
	NowSec                int64                       `json:"nowSec"`
	OnlineBots            map[string]PresenceBotState `json:"onlineBots"`
}

var (
	presenceSubsystemStateMu      sync.RWMutex
	presenceSubsystemReady        bool
	presenceLastConfigReloadError string

	presenceStatusTestMu       sync.RWMutex
	presenceStatusTestOverride *PresenceStatus
)

func GetPresenceStatus() PresenceStatus {
	presenceStatusTestMu.RLock()
	override := clonePresenceStatusPtr(presenceStatusTestOverride)
	presenceStatusTestMu.RUnlock()
	if override != nil {
		if override.NowSec == 0 {
			override.NowSec = time.Now().Unix()
		}
		return *override
	}

	status := PresenceStatus{
		Healthy:         false,
		PeerCount:       0,
		UnhealthyReason: "presence_not_initialized",
		NowSec:          time.Now().Unix(),
		OnlineBots:      map[string]PresenceBotState{},
	}
	if Node != nil {
		status.PeerCount = len(Node.Network().Peers())
	}

	presenceSubsystemStateMu.RLock()
	status.LastConfigReloadError = presenceLastConfigReloadError
	ready := presenceSubsystemReady
	presenceSubsystemStateMu.RUnlock()

	if ready {
		status.UnhealthyReason = "no_active_peers"
		if status.PeerCount >= 1 {
			status.Healthy = true
			status.UnhealthyReason = ""
		}
	}
	return status
}

func SetPresenceSubsystemReady(ready bool) {
	presenceSubsystemStateMu.Lock()
	presenceSubsystemReady = ready
	presenceSubsystemStateMu.Unlock()
}

func SetPresenceLastConfigReloadError(message string) {
	presenceSubsystemStateMu.Lock()
	presenceLastConfigReloadError = strings.TrimSpace(message)
	presenceSubsystemStateMu.Unlock()
}

func SetPresenceStatusForTests(status PresenceStatus) {
	cloned := clonePresenceStatus(status)

	presenceStatusTestMu.Lock()
	presenceStatusTestOverride = &cloned
	presenceStatusTestMu.Unlock()
}

func ResetPresenceStatusForTests() {
	presenceStatusTestMu.Lock()
	presenceStatusTestOverride = nil
	presenceStatusTestMu.Unlock()
}

func clonePresenceStatusPtr(status *PresenceStatus) *PresenceStatus {
	if status == nil {
		return nil
	}
	cloned := clonePresenceStatus(*status)
	return &cloned
}

func clonePresenceStatus(status PresenceStatus) PresenceStatus {
	status.OnlineBots = canonicalPresenceBotStates(status.OnlineBots)
	if status.OnlineBots == nil {
		status.OnlineBots = map[string]PresenceBotState{}
	}
	return status
}

func canonicalPresenceBotStates(raw map[string]PresenceBotState) map[string]PresenceBotState {
	if len(raw) == 0 {
		return map[string]PresenceBotState{}
	}

	out := make(map[string]PresenceBotState, len(raw))
	for rawGlobalMetaID, rawState := range raw {
		globalMetaID, ok := canonicalPresenceGlobalMetaID(rawGlobalMetaID)
		if !ok {
			continue
		}
		out[globalMetaID] = mergePresenceBotState(out[globalMetaID], rawState)
	}
	return out
}

func mergePresenceBotState(base PresenceBotState, next PresenceBotState) PresenceBotState {
	if next.LastSeenSec > base.LastSeenSec {
		base.LastSeenSec = next.LastSeenSec
	}
	if next.ExpiresAtSec > base.ExpiresAtSec {
		base.ExpiresAtSec = next.ExpiresAtSec
	}

	peerSet := make(map[string]struct{}, len(base.PeerIDs)+len(next.PeerIDs))
	for _, rawPeerID := range append(append([]string{}, base.PeerIDs...), next.PeerIDs...) {
		peerID := strings.TrimSpace(rawPeerID)
		if peerID == "" {
			continue
		}
		peerSet[peerID] = struct{}{}
	}

	base.PeerIDs = base.PeerIDs[:0]
	for peerID := range peerSet {
		base.PeerIDs = append(base.PeerIDs, peerID)
	}
	sort.Strings(base.PeerIDs)
	return base
}

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
	c.pruneExpiredLocked(receivedAt)

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

func (c *PresenceCache) pruneExpiredLocked(now time.Time) {
	for globalMetaID, peers := range c.entries {
		for peerID, expiresAt := range peers {
			if !now.Before(expiresAt) {
				delete(peers, peerID)
			}
		}
		if len(peers) == 0 {
			delete(c.entries, globalMetaID)
		}
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
	mu        sync.RWMutex
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
