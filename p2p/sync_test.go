package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
)

func TestContentPull(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Node A: start host and serve PINs
	dirA := t.TempDir()
	if err := InitHost(ctx, dirA); err != nil {
		t.Fatal(err)
	}
	defer CloseHost()

	GetPinFn = func(pinId string) (*PinResponse, error) {
		if pinId == "pin001" {
			return &PinResponse{PinId: "pin001", Path: "/info/name",
				Address: "1Addr", Confirmed: true, Content: []byte("Alice")}, nil
		}
		return nil, fmt.Errorf("not found")
	}
	RegisterSyncHandler()

	nodeA := Node

	// Node B: separate host
	nodeB, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	if err != nil {
		t.Fatal(err)
	}
	defer nodeB.Close()

	if err := nodeB.Connect(ctx, peer.AddrInfo{ID: nodeA.ID(), Addrs: nodeA.Addrs()}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)

	// Fetch from node B using nodeB's stream
	s, err := nodeB.NewStream(ctx, nodeA.ID(), SyncProtocol)
	if err != nil {
		t.Fatal(err)
	}
	json.NewEncoder(s).Encode(PinRequest{PinId: "pin001"})
	s.CloseWrite()
	var resp PinResponse
	json.NewDecoder(s).Decode(&resp)
	s.Close()

	if string(resp.Content) != "Alice" {
		t.Errorf("expected Alice, got %s", resp.Content)
	}
}
