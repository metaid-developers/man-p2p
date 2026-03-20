package p2p

import (
	"context"
	"testing"
)

func TestInitHost(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := InitHost(ctx, dir); err != nil {
		t.Fatal(err)
	}
	defer CloseHost()

	if Node == nil {
		t.Fatal("Node is nil")
	}
	if Node.ID() == "" {
		t.Fatal("empty peer ID")
	}
	if KadDHT == nil {
		t.Fatal("KadDHT is nil")
	}

	// Identity persists across restarts
	id1 := Node.ID()
	CloseHost()
	if err := InitHost(ctx, dir); err != nil {
		t.Fatal(err)
	}
	if Node.ID() != id1 {
		t.Errorf("identity changed: %s != %s", Node.ID(), id1)
	}
}
