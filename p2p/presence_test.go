package p2p

import (
	"reflect"
	"testing"
	"time"
)

func TestPresenceCacheCanonicalizesGlobalMetaID(t *testing.T) {
	cache := NewPresenceCache()
	receivedAt := time.Unix(1_700_000_000, 0)

	cache.Observe("peer-A", PresenceAnnouncement{
		TTLSec:        30,
		GlobalMetaIDs: []string{"  IDAbC  ", "metaid:idabc", "  ", "idabc"},
	}, receivedAt)

	snapshot := cache.Snapshot(receivedAt.Add(2 * time.Second))
	peers, ok := snapshot["idabc"]
	if !ok {
		t.Fatalf("expected canonical key idabc in snapshot, got %v", snapshot)
	}
	if !reflect.DeepEqual(peers, []string{"peer-A"}) {
		t.Fatalf("expected one peer for idabc, got %v", peers)
	}
	if _, bad := snapshot["IDAbC"]; bad {
		t.Fatalf("expected no non-canonical key, got %v", snapshot)
	}
}

func TestPresenceCacheUsesReceiveTimeAndClampsTTL(t *testing.T) {
	cache := NewPresenceCache()
	receivedAt := time.Unix(1_700_000_000, 0)

	cache.Observe("peer-A", PresenceAnnouncement{
		TTLSec:        999, // clamp to 120
		GlobalMetaIDs: []string{"idlong"},
	}, receivedAt)
	cache.Observe("peer-A", PresenceAnnouncement{
		TTLSec:        0, // clamp to 1
		GlobalMetaIDs: []string{"idshort"},
	}, receivedAt)

	at2s := cache.Snapshot(receivedAt.Add(2 * time.Second))
	if _, ok := at2s["idlong"]; !ok {
		t.Fatalf("expected idlong to still be active at +2s, got %v", at2s)
	}
	if _, ok := at2s["idshort"]; ok {
		t.Fatalf("expected idshort to expire by +2s due to ttl clamp, got %v", at2s)
	}

	at121s := cache.Snapshot(receivedAt.Add(121 * time.Second))
	if _, ok := at121s["idlong"]; ok {
		t.Fatalf("expected idlong to expire by +121s due to ttl clamp, got %v", at121s)
	}
}

func TestPresenceCacheAggregatesOneGlobalMetaIDAcrossMultiplePeers(t *testing.T) {
	cache := NewPresenceCache()
	receivedAt := time.Unix(1_700_000_000, 0)

	cache.Observe("peer-A", PresenceAnnouncement{
		TTLSec:        30,
		GlobalMetaIDs: []string{"  idshared "},
	}, receivedAt)
	cache.Observe("peer-B", PresenceAnnouncement{
		TTLSec:        30,
		GlobalMetaIDs: []string{"IDSHARED"},
	}, receivedAt.Add(time.Second))

	snapshot := cache.Snapshot(receivedAt.Add(5 * time.Second))
	peers := snapshot["idshared"]
	if !reflect.DeepEqual(peers, []string{"peer-A", "peer-B"}) {
		t.Fatalf("expected aggregated peers [peer-A peer-B], got %v", peers)
	}
}

func TestPresenceCacheRejectsMetaIDPrefixedForm(t *testing.T) {
	cache := NewPresenceCache()
	receivedAt := time.Unix(1_700_000_000, 0)

	cache.Observe("peer-A", PresenceAnnouncement{
		TTLSec:        30,
		GlobalMetaIDs: []string{"metaid:IDABC"},
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
		GlobalMetaIDs: []string{"id-receive-based"},
	}, receivedAt)

	snapshot := cache.Snapshot(receivedAt.Add(10 * time.Second))
	if _, ok := snapshot["id-receive-based"]; !ok {
		t.Fatalf("expected entry to remain active based on receive time despite old sentAt, got %v", snapshot)
	}
}

func TestPresenceCachePrunesExpiredEntriesOnObserve(t *testing.T) {
	cache := NewPresenceCache()
	t0 := time.Unix(1_700_000_000, 0)

	cache.Observe("peer-old", PresenceAnnouncement{
		TTLSec:        1,
		GlobalMetaIDs: []string{"id-expired"},
	}, t0)

	cache.Observe("peer-new", PresenceAnnouncement{
		TTLSec:        30,
		GlobalMetaIDs: []string{"id-fresh"},
	}, t0.Add(10*time.Second))

	cache.mu.RLock()
	_, hasExpired := cache.entries["id-expired"]
	_, hasFresh := cache.entries["id-fresh"]
	cache.mu.RUnlock()

	if hasExpired {
		t.Fatalf("expected expired entries to be pruned on observe, cache=%v", cache.entries)
	}
	if !hasFresh {
		t.Fatalf("expected fresh entry to remain after pruning, cache=%v", cache.entries)
	}
}
