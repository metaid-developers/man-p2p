package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"man-p2p/pin"
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
			return &PinResponse{Pin: &pin.PinInscription{
				Id:            "pin001",
				Path:          "/info/name",
				Address:       "1Addr",
				MetaId:        "metaid-001",
				ChainName:     "btc",
				Timestamp:     1710000000,
				GenesisHeight: 900000,
				ContentBody:   []byte("Alice"),
				ContentLength: 5,
			}}, nil
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
	if err := json.NewDecoder(s).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	s.Close()

	if resp.Pin == nil {
		t.Fatal("expected full pin payload")
	}
	if string(resp.Pin.ContentBody) != "Alice" {
		t.Errorf("expected Alice, got %s", resp.Pin.ContentBody)
	}
	if resp.Pin.ChainName != "btc" {
		t.Errorf("expected chain btc, got %s", resp.Pin.ChainName)
	}
	if resp.Pin.GenesisHeight != 900000 {
		t.Errorf("expected height 900000, got %d", resp.Pin.GenesisHeight)
	}
}
