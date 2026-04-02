package p2p

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
)

func TestPresenceCacheCanonicalizesGlobalMetaID(t *testing.T) {
	cache := NewPresenceCache()
	receivedAt := time.Unix(1_700_000_000, 0)

	cache.Observe("peer-A", PresenceAnnouncement{
		TTLSec:        30,
		GlobalMetaIDs: []string{"  IDQ1ABC  ", "metaid:idq1abc", "  ", "idq1abc"},
	}, receivedAt)

	snapshot := cache.Snapshot(receivedAt.Add(2 * time.Second))
	peers, ok := snapshot["idq1abc"]
	if !ok {
		t.Fatalf("expected canonical key idq1abc in snapshot, got %v", snapshot)
	}
	if !reflect.DeepEqual(peers, []string{"peer-A"}) {
		t.Fatalf("expected one peer for idq1abc, got %v", peers)
	}
	if _, bad := snapshot["IDQ1ABC"]; bad {
		t.Fatalf("expected no non-canonical key, got %v", snapshot)
	}
}

func TestPresenceCacheUsesReceiveTimeAndClampsTTL(t *testing.T) {
	cache := NewPresenceCache()
	receivedAt := time.Unix(1_700_000_000, 0)

	cache.Observe("peer-A", PresenceAnnouncement{
		TTLSec:        999, // clamp to 120
		GlobalMetaIDs: []string{"idq1long"},
	}, receivedAt)
	cache.Observe("peer-A", PresenceAnnouncement{
		TTLSec:        0, // clamp to 1
		GlobalMetaIDs: []string{"idq1short"},
	}, receivedAt)

	at2s := cache.Snapshot(receivedAt.Add(2 * time.Second))
	if _, ok := at2s["idq1long"]; !ok {
		t.Fatalf("expected idq1long to still be active at +2s, got %v", at2s)
	}
	if _, ok := at2s["idq1short"]; ok {
		t.Fatalf("expected idq1short to expire by +2s due to ttl clamp, got %v", at2s)
	}

	at121s := cache.Snapshot(receivedAt.Add(121 * time.Second))
	if _, ok := at121s["idq1long"]; ok {
		t.Fatalf("expected idq1long to expire by +121s due to ttl clamp, got %v", at121s)
	}
}

func TestPresenceCacheAggregatesOneGlobalMetaIDAcrossMultiplePeers(t *testing.T) {
	cache := NewPresenceCache()
	receivedAt := time.Unix(1_700_000_000, 0)

	cache.Observe("peer-A", PresenceAnnouncement{
		TTLSec:        30,
		GlobalMetaIDs: []string{"  idq1shared "},
	}, receivedAt)
	cache.Observe("peer-B", PresenceAnnouncement{
		TTLSec:        30,
		GlobalMetaIDs: []string{"IDQ1SHARED"},
	}, receivedAt.Add(time.Second))

	snapshot := cache.Snapshot(receivedAt.Add(5 * time.Second))
	peers := snapshot["idq1shared"]
	if !reflect.DeepEqual(peers, []string{"peer-A", "peer-B"}) {
		t.Fatalf("expected aggregated peers [peer-A peer-B], got %v", peers)
	}
}

func TestPresenceCacheRejectsMetaIDPrefixedForm(t *testing.T) {
	cache := NewPresenceCache()
	receivedAt := time.Unix(1_700_000_000, 0)

	cache.Observe("peer-A", PresenceAnnouncement{
		TTLSec:        30,
		GlobalMetaIDs: []string{"metaid:IDQ1ABC"},
	}, receivedAt)

	snapshot := cache.Snapshot(receivedAt.Add(time.Second))
	if len(snapshot) != 0 {
		t.Fatalf("expected metaid: form to be rejected, got %v", snapshot)
	}
}

func TestPresenceCacheExpiryUsesReceiveTimeNotSentAt(t *testing.T) {
	cache := NewPresenceCache()
	receivedAt := time.Unix(1_700_000_000, 0)

	cache.Observe("peer-A", PresenceAnnouncement{
		SentAt:        receivedAt.Add(-24 * time.Hour).Unix(),
		TTLSec:        30,
		GlobalMetaIDs: []string{"idq1receivebased"},
	}, receivedAt)

	snapshot := cache.Snapshot(receivedAt.Add(10 * time.Second))
	if _, ok := snapshot["idq1receivebased"]; !ok {
		t.Fatalf("expected entry to remain active based on receive time despite old sentAt, got %v", snapshot)
	}
}

func TestPresenceCachePrunesExpiredEntriesOnObserve(t *testing.T) {
	cache := NewPresenceCache()
	t0 := time.Unix(1_700_000_000, 0)

	cache.Observe("peer-old", PresenceAnnouncement{
		TTLSec:        1,
		GlobalMetaIDs: []string{"idq1expired"},
	}, t0)

	cache.Observe("peer-new", PresenceAnnouncement{
		TTLSec:        30,
		GlobalMetaIDs: []string{"idq1fresh"},
	}, t0.Add(10*time.Second))

	cache.mu.RLock()
	_, hasExpired := cache.entries["idq1expired"]
	_, hasFresh := cache.entries["idq1fresh"]
	cache.mu.RUnlock()

	if hasExpired {
		t.Fatalf("expected expired entries to be pruned on observe, cache=%v", cache.entries)
	}
	if !hasFresh {
		t.Fatalf("expected fresh entry to remain after pruning, cache=%v", cache.entries)
	}
}

func TestPresenceCacheRejectsInvalidRawGlobalMetaID(t *testing.T) {
	cache := NewPresenceCache()
	receivedAt := time.Unix(1_700_000_000, 0)

	cache.Observe("peer-A", PresenceAnnouncement{
		TTLSec:        30,
		GlobalMetaIDs: []string{"idabc", "id-receive-based", "idq1valid"},
	}, receivedAt)

	snapshot := cache.Snapshot(receivedAt.Add(time.Second))
	if _, ok := snapshot["idq1valid"]; !ok {
		t.Fatalf("expected valid raw globalMetaId to remain, got %v", snapshot)
	}
	if _, ok := snapshot["idabc"]; ok {
		t.Fatalf("expected invalid raw globalMetaId idabc to be rejected, got %v", snapshot)
	}
	if _, ok := snapshot["id-receive-based"]; ok {
		t.Fatalf("expected invalid raw globalMetaId id-receive-based to be rejected, got %v", snapshot)
	}
}

func TestGetPresenceStatusDefaultsToPresenceNotInitialized(t *testing.T) {
	restorePresenceStatusTestState(t)

	status := GetPresenceStatus()
	if status.Healthy {
		t.Fatalf("expected default presence status to be unhealthy, got %#v", status)
	}
	if status.PeerCount != 0 {
		t.Fatalf("expected peerCount 0 by default, got %#v", status)
	}
	if status.UnhealthyReason != "presence_not_initialized" {
		t.Fatalf("expected presence_not_initialized, got %#v", status)
	}
	if status.NowSec <= 0 {
		t.Fatalf("expected nowSec to be populated, got %#v", status)
	}
	if status.OnlineBots == nil || len(status.OnlineBots) != 0 {
		t.Fatalf("expected empty onlineBots map, got %#v", status.OnlineBots)
	}
}

func TestGetPresenceStatusReadyWithoutActivePeersIsUnhealthy(t *testing.T) {
	restorePresenceStatusTestState(t)

	host, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()

	setNode(host)
	SetPresenceSubsystemReady(true)

	status := GetPresenceStatus()
	if status.Healthy {
		t.Fatalf("expected ready status without peers to remain unhealthy, got %#v", status)
	}
	if status.PeerCount != 0 {
		t.Fatalf("expected peerCount 0 without peers, got %#v", status)
	}
	if status.UnhealthyReason != "no_active_peers" {
		t.Fatalf("expected no_active_peers, got %#v", status)
	}
}

func TestGetPresenceStatusReadyWithActivePeerIsHealthy(t *testing.T) {
	restorePresenceStatusTestState(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hostA, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	if err != nil {
		t.Fatal(err)
	}
	defer hostA.Close()

	hostB, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	if err != nil {
		t.Fatal(err)
	}
	defer hostB.Close()

	if err := hostB.Connect(ctx, peer.AddrInfo{ID: hostA.ID(), Addrs: hostA.Addrs()}); err != nil {
		t.Fatal(err)
	}

	setNode(hostA)
	SetPresenceSubsystemReady(true)

	deadline := time.Now().Add(2 * time.Second)
	for len(Node.Network().Peers()) == 0 && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}

	status := GetPresenceStatus()
	if !status.Healthy {
		t.Fatalf("expected ready status with active peers to be healthy, got %#v", status)
	}
	if status.PeerCount < 1 {
		t.Fatalf("expected peerCount >= 1 with active peers, got %#v", status)
	}
	if status.UnhealthyReason != "" {
		t.Fatalf("expected empty unhealthyReason for healthy status, got %#v", status)
	}
}

func restorePresenceStatusTestState(t *testing.T) {
	t.Helper()

	originalNode := currentNode()
	presenceSubsystemStateMu.RLock()
	originalReady := presenceSubsystemReady
	originalReloadError := presenceLastConfigReloadError
	presenceSubsystemStateMu.RUnlock()

	presenceStatusTestMu.RLock()
	originalOverride := clonePresenceStatusPtr(presenceStatusTestOverride)
	presenceStatusTestMu.RUnlock()

	setNode(nil)
	SetPresenceSubsystemReady(false)
	SetPresenceLastConfigReloadError("")
	ResetPresenceStatusForTests()

	t.Cleanup(func() {
		setNode(originalNode)

		presenceSubsystemStateMu.Lock()
		presenceSubsystemReady = originalReady
		presenceLastConfigReloadError = originalReloadError
		presenceSubsystemStateMu.Unlock()

		presenceStatusTestMu.Lock()
		presenceStatusTestOverride = originalOverride
		presenceStatusTestMu.Unlock()
	})
}
