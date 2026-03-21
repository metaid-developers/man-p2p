package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"

	"man-p2p/pin"
)

func TestAlphaDualInstanceRealtimeSync(t *testing.T) {
	_ = LoadConfig(writeTempConfig(t, `{"p2p_sync_mode":"full"}`))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	dirA := t.TempDir()
	if err := InitHost(ctx, dirA); err != nil {
		t.Fatal(err)
	}
	defer CloseHost()

	GetPinFn = func(pinId string) (*PinResponse, error) {
		if pinId != "alpha-pin-001" {
			return nil, fmt.Errorf("not found")
		}
		return &PinResponse{Pin: &pin.PinInscription{
			Id:            "alpha-pin-001",
			Path:          "/info/name",
			Address:       "1AlphaAddr",
			MetaId:        "alpha-metaid",
			ChainName:     "btc",
			Timestamp:     1710000000,
			GenesisHeight: 900000,
			ContentBody:   []byte("Alice"),
			ContentLength: 5,
		}}, nil
	}
	RegisterSyncHandler()
	if err := InitGossip(ctx); err != nil {
		t.Fatal(err)
	}

	nodeA := Node

	nodeB, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	if err != nil {
		t.Fatal(err)
	}
	defer nodeB.Close()

	if err := nodeB.Connect(ctx, peer.AddrInfo{ID: nodeA.ID(), Addrs: nodeA.Addrs()}); err != nil {
		t.Fatal(err)
	}

	psB, err := pubsub.NewGossipSub(ctx, nodeB)
	if err != nil {
		t.Fatal(err)
	}
	topicB, err := psB.Join(TopicName)
	if err != nil {
		t.Fatal(err)
	}
	subB, err := topicB.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer subB.Cancel()

	time.Sleep(500 * time.Millisecond)

	if err := PublishPin(ctx, PinAnnouncement{
		PinId:         "alpha-pin-001",
		Path:          "/info/name",
		Address:       "1AlphaAddr",
		MetaId:        "alpha-metaid",
		ChainName:     "btc",
		Timestamp:     1710000000,
		GenesisHeight: 900000,
		Confirmed:     true,
		SizeBytes:     5,
	}); err != nil {
		t.Fatal(err)
	}

	msg, err := subB.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}

	var ann PinAnnouncement
	if err := json.Unmarshal(msg.Data, &ann); err != nil {
		t.Fatal(err)
	}

	origNode := Node
	origStorePinFn := StorePinFn
	defer func() {
		Node = origNode
		StorePinFn = origStorePinFn
	}()

	var stored *pin.PinInscription
	Node = nodeB
	StorePinFn = func(resp *PinResponse) error {
		if resp == nil || resp.Pin == nil {
			return fmt.Errorf("missing stored pin")
		}
		pinCopy := *resp.Pin
		stored = &pinCopy
		return nil
	}

	HandleIncomingAnnouncement(ctx, ann)

	if stored == nil {
		t.Fatal("expected receiver side to store fetched pin")
	}
	if stored.Id != "alpha-pin-001" {
		t.Fatalf("expected stored pin alpha-pin-001, got %s", stored.Id)
	}
	if string(stored.ContentBody) != "Alice" {
		t.Fatalf("expected stored content Alice, got %q", string(stored.ContentBody))
	}
}
