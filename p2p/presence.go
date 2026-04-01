package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
)

const (
	presenceTopicName                = "metaid-presence-v1"
	presenceSchemaVersion            = 1
	defaultPresenceBroadcastInterval = 20 * time.Second
	defaultPresenceAnnouncementTTL   = 55
	defaultPresenceBroadcastJitter   = 3 * time.Second
	minPresenceTTLSeconds            = 1
	maxPresenceTTLSeconds            = 120
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

type PresenceAnnouncement struct {
	SchemaVersion int      `json:"schemaVersion"`
	PeerID        string   `json:"peerId"`
	SentAt        int64    `json:"sentAt"`
	TTLSec        int      `json:"ttlSec"`
	RuntimeMode   string   `json:"runtimeMode"`
	GlobalMetaIDs []string `json:"globalMetaIds"`
}

type presencePeerState struct {
	lastSeen  time.Time
	expiresAt time.Time
}

type PresenceCache struct {
	mu      sync.RWMutex
	entries map[string]map[string]presencePeerState // globalMetaID -> peerID -> state
}

type presenceRuntimeOptions struct {
	localGlobalMetaIDs func() []string
	broadcastInterval  time.Duration
	ttlSec             int
	jitterRange        time.Duration
	runtimeMode        string
	now                func() time.Time
}

type presenceRuntime struct {
	host               host.Host
	topic              *pubsub.Topic
	sub                *pubsub.Subscription
	cache              *PresenceCache
	localGlobalMetaIDs func() []string
	broadcastInterval  time.Duration
	ttlSec             int
	jitterRange        time.Duration
	runtimeMode        string
	now                func() time.Time

	closeOnce sync.Once
	ctx       context.Context
	cancel    context.CancelFunc

	broadcastMu      sync.Mutex
	broadcastStarted bool
}

var (
	presenceSubsystemStateMu      sync.RWMutex
	presenceSubsystemReady        bool
	presenceLastConfigReloadError string

	presenceStatusTestMu       sync.RWMutex
	presenceStatusTestOverride *PresenceStatus

	currentPresenceRuntimeMu sync.RWMutex
	currentPresenceRuntime   *presenceRuntime
)

func InitPresence(ctx context.Context) error {
	node := currentNode()
	if node == nil || PS == nil {
		return fmt.Errorf("p2p presence requires initialized host and pubsub")
	}

	runtime, err := newPresenceRuntime(ctx, node, PS, presenceRuntimeOptions{
		localGlobalMetaIDs: LocalPresenceGlobalMetaIDs,
		broadcastInterval:  defaultPresenceBroadcastInterval,
		ttlSec:             defaultPresenceAnnouncementTTL,
		jitterRange:        defaultPresenceBroadcastJitter,
		runtimeMode:        currentRuntimeMode(),
	})
	if err != nil {
		SetPresenceSubsystemReady(false)
		return err
	}

	currentPresenceRuntimeMu.Lock()
	previous := currentPresenceRuntime
	currentPresenceRuntime = runtime
	currentPresenceRuntimeMu.Unlock()
	if previous != nil {
		previous.close()
	}

	SetPresenceSubsystemReady(true)
	runtime.startBroadcastLoop()
	return nil
}

func closePresenceRuntime() {
	currentPresenceRuntimeMu.Lock()
	runtime := currentPresenceRuntime
	currentPresenceRuntime = nil
	currentPresenceRuntimeMu.Unlock()

	if runtime != nil {
		runtime.close()
	}
	SetPresenceSubsystemReady(false)
}

func newPresenceRuntime(ctx context.Context, h host.Host, ps *pubsub.PubSub, opts presenceRuntimeOptions) (*presenceRuntime, error) {
	if h == nil {
		return nil, fmt.Errorf("presence host is nil")
	}
	if ps == nil {
		return nil, fmt.Errorf("presence pubsub is nil")
	}

	topic, err := ps.Join(presenceTopicName)
	if err != nil {
		return nil, err
	}
	sub, err := topic.Subscribe()
	if err != nil {
		_ = topic.Close()
		return nil, err
	}

	runtimeCtx, cancel := context.WithCancel(ctx)
	runtime := &presenceRuntime{
		host:               h,
		topic:              topic,
		sub:                sub,
		cache:              NewPresenceCache(),
		localGlobalMetaIDs: opts.localGlobalMetaIDs,
		broadcastInterval:  opts.broadcastInterval,
		ttlSec:             opts.ttlSec,
		jitterRange:        opts.jitterRange,
		runtimeMode:        opts.runtimeMode,
		now:                opts.now,
		ctx:                runtimeCtx,
		cancel:             cancel,
	}
	runtime.applyDefaults()

	go runtime.receiveLoop()

	return runtime, nil
}

func (r *presenceRuntime) applyDefaults() {
	if r.localGlobalMetaIDs == nil {
		r.localGlobalMetaIDs = LocalPresenceGlobalMetaIDs
	}
	if r.broadcastInterval <= 0 {
		r.broadcastInterval = defaultPresenceBroadcastInterval
	}
	if r.ttlSec == 0 {
		r.ttlSec = defaultPresenceAnnouncementTTL
	}
	r.ttlSec = clampPresenceTTLSeconds(r.ttlSec)
	if r.jitterRange < 0 {
		r.jitterRange = 0
	}
	if r.runtimeMode == "" {
		r.runtimeMode = currentRuntimeMode()
	}
	if r.now == nil {
		r.now = time.Now
	}
}

func (r *presenceRuntime) receiveLoop() {
	for {
		msg, err := r.sub.Next(r.ctx)
		if err != nil {
			return
		}
		if msg.ReceivedFrom == r.host.ID() {
			continue
		}

		var ann PresenceAnnouncement
		if err := json.Unmarshal(msg.Data, &ann); err != nil {
			log.Printf("presence: bad message from %s: %v", msg.ReceivedFrom, err)
			continue
		}
		r.cache.Observe(msg.ReceivedFrom.String(), ann, r.now())
	}
}

func (r *presenceRuntime) startBroadcastLoop() {
	r.broadcastMu.Lock()
	if r.broadcastStarted {
		r.broadcastMu.Unlock()
		return
	}
	r.broadcastStarted = true
	r.broadcastMu.Unlock()

	go func() {
		defer func() {
			r.broadcastMu.Lock()
			r.broadcastStarted = false
			r.broadcastMu.Unlock()
		}()

		for {
			if err := r.publishNow(r.ctx); err != nil && r.ctx.Err() == nil {
				log.Printf("presence: publish failed: %v", err)
			}

			delay := r.nextBroadcastDelay()
			timer := time.NewTimer(delay)
			select {
			case <-r.ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}

func (r *presenceRuntime) publishNow(ctx context.Context) error {
	if r.topic == nil {
		return fmt.Errorf("presence topic not initialized")
	}

	globalMetaIDs := canonicalizePresenceGlobalMetaIDs(r.localGlobalMetaIDs())
	if len(globalMetaIDs) == 0 {
		return nil
	}

	announcement := PresenceAnnouncement{
		SchemaVersion: presenceSchemaVersion,
		PeerID:        r.host.ID().String(),
		SentAt:        r.now().Unix(),
		TTLSec:        clampPresenceTTLSeconds(r.ttlSec),
		RuntimeMode:   r.runtimeMode,
		GlobalMetaIDs: globalMetaIDs,
	}

	data, err := json.Marshal(announcement)
	if err != nil {
		return err
	}
	return r.topic.Publish(ctx, data)
}

func (r *presenceRuntime) nextBroadcastDelay() time.Duration {
	if r.jitterRange <= 0 {
		return r.broadcastInterval
	}

	window := int64(r.jitterRange) * 2
	offset := time.Duration(rand.Int63n(window+1)) - r.jitterRange
	delay := r.broadcastInterval + offset
	if delay <= 0 {
		return time.Millisecond
	}
	return delay
}

func (r *presenceRuntime) status() PresenceStatus {
	now := r.now()
	status := PresenceStatus{
		Healthy:    false,
		PeerCount:  0,
		NowSec:     now.Unix(),
		OnlineBots: r.cache.SnapshotStatus(now),
	}
	if r.host != nil {
		status.PeerCount = len(r.host.Network().Peers())
	}
	if status.PeerCount >= 1 {
		status.Healthy = true
	} else {
		status.UnhealthyReason = "no_active_peers"
	}
	return status
}

func (r *presenceRuntime) close() {
	r.closeOnce.Do(func() {
		if r.cancel != nil {
			r.cancel()
		}
		if r.sub != nil {
			r.sub.Cancel()
		}
		if r.topic != nil {
			_ = r.topic.Close()
		}
	})
}

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
	node := currentNode()
	if node != nil {
		status.PeerCount = len(node.Network().Peers())
	}

	currentPresenceRuntimeMu.RLock()
	runtime := currentPresenceRuntime
	currentPresenceRuntimeMu.RUnlock()
	if runtime != nil {
		status = runtime.status()
	}

	presenceSubsystemStateMu.RLock()
	status.LastConfigReloadError = presenceLastConfigReloadError
	ready := presenceSubsystemReady
	presenceSubsystemStateMu.RUnlock()

	if ready && runtime == nil {
		status.UnhealthyReason = "no_active_peers"
		if status.PeerCount >= 1 {
			status.Healthy = true
			status.UnhealthyReason = ""
		}
	}

	if !ready {
		status.Healthy = false
		status.UnhealthyReason = "presence_not_initialized"
	}

	if status.OnlineBots == nil {
		status.OnlineBots = map[string]PresenceBotState{}
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

func NewPresenceCache() *PresenceCache {
	return &PresenceCache{
		entries: make(map[string]map[string]presencePeerState),
	}
}

func (c *PresenceCache) Observe(receivedFrom string, ann PresenceAnnouncement, receivedAt time.Time) {
	peerID := strings.TrimSpace(receivedFrom)
	if peerID == "" {
		return
	}
	lastSeen := receivedAt
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
			peers = make(map[string]presencePeerState)
			c.entries[globalMetaID] = peers
		}
		peers[peerID] = presencePeerState{
			lastSeen:  lastSeen,
			expiresAt: expiresAt,
		}
	}
}

func (c *PresenceCache) pruneExpiredLocked(now time.Time) {
	for globalMetaID, peers := range c.entries {
		for peerID, state := range peers {
			if !now.Before(state.expiresAt) {
				delete(peers, peerID)
			}
		}
		if len(peers) == 0 {
			delete(c.entries, globalMetaID)
		}
	}
}

func (c *PresenceCache) Snapshot(now time.Time) map[string][]string {
	states := c.SnapshotStatus(now)
	out := make(map[string][]string, len(states))
	for globalMetaID, state := range states {
		out[globalMetaID] = append([]string{}, state.PeerIDs...)
	}
	return out
}

func (c *PresenceCache) SnapshotStatus(now time.Time) map[string]PresenceBotState {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make(map[string]PresenceBotState)
	for globalMetaID, peers := range c.entries {
		state := PresenceBotState{}
		for peerID, peerState := range peers {
			if !now.Before(peerState.expiresAt) {
				continue
			}
			state.PeerIDs = append(state.PeerIDs, peerID)
			if peerState.lastSeen.Unix() > state.LastSeenSec {
				state.LastSeenSec = peerState.lastSeen.Unix()
			}
			if peerState.expiresAt.Unix() > state.ExpiresAtSec {
				state.ExpiresAtSec = peerState.expiresAt.Unix()
			}
		}
		if len(state.PeerIDs) == 0 {
			continue
		}
		sort.Strings(state.PeerIDs)
		out[globalMetaID] = state
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

func canonicalizePresenceGlobalMetaIDs(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, candidate := range raw {
		canonical, ok := canonicalPresenceGlobalMetaID(candidate)
		if !ok {
			continue
		}
		if _, exists := seen[canonical]; exists {
			continue
		}
		seen[canonical] = struct{}{}
		out = append(out, canonical)
	}
	sort.Strings(out)
	return out
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

func currentRuntimeMode() string {
	if GetConfig().ChainSourceEnabled() {
		return "chain-enabled"
	}
	return "p2p-only"
}
