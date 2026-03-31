package man

import (
	"testing"

	"man-p2p/pin"
)

func TestApplyMempoolSeenTimePreservesFirstSeen(t *testing.T) {
	t.Parallel()

	pinNode := &pin.PinInscription{
		Id:        "pin-1",
		Timestamp: 111,
		SeenTime:  111,
	}

	applyMempoolSeenTime(pinNode, 222, 111)

	if pinNode.Timestamp != 222 {
		t.Fatalf("expected timestamp to update to latest mempool processing time, got %d", pinNode.Timestamp)
	}
	if pinNode.SeenTime != 111 {
		t.Fatalf("expected seenTime to preserve first seen time, got %d", pinNode.SeenTime)
	}
}

func TestApplyMempoolSeenTimeInitializesSeenTime(t *testing.T) {
	t.Parallel()

	pinNode := &pin.PinInscription{Id: "pin-1"}

	applyMempoolSeenTime(pinNode, 333, 0)

	if pinNode.Timestamp != 333 {
		t.Fatalf("expected timestamp to be set, got %d", pinNode.Timestamp)
	}
	if pinNode.SeenTime != 333 {
		t.Fatalf("expected seenTime to initialize from first processing time, got %d", pinNode.SeenTime)
	}
}

func TestMergeConfirmedSeenTimesPrefersExistingValue(t *testing.T) {
	t.Parallel()

	pins := []*pin.PinInscription{
		{Id: "pin-1", Timestamp: 500},
		{Id: "pin-2", Timestamp: 600},
	}

	mergeConfirmedSeenTimes(pins, map[string]int64{
		"pin-1": 100,
	})

	if pins[0].SeenTime != 100 {
		t.Fatalf("expected existing seenTime to win, got %d", pins[0].SeenTime)
	}
	if pins[1].SeenTime != 600 {
		t.Fatalf("expected missing seenTime to fall back to block timestamp, got %d", pins[1].SeenTime)
	}
}
