package p2p

import (
	"context"
	"testing"
)

func TestPublishPinWithoutInitializedHost(t *testing.T) {
	Node = nil
	topic = nil

	if err := PublishPin(context.Background(), PinAnnouncement{PinId: "pin-1"}); err == nil {
		t.Fatal("expected error when host/topic are not initialized")
	}
}
