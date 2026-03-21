package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

const TopicName = "metaid-pins"

type PinAnnouncement struct {
	PinId     string `json:"pinId"`
	Path      string `json:"path"`
	Address   string `json:"address"`
	MetaId    string `json:"metaId"`
	ChainName string `json:"chainName"`
	Timestamp int64  `json:"timestamp"`
	GenesisHeight int64 `json:"genesisHeight"`
	Confirmed bool   `json:"confirmed"`
	SizeBytes int64  `json:"sizeBytes"`
	PeerID    string `json:"peerId"`
}

var (
	PS    *pubsub.PubSub
	topic *pubsub.Topic
	sub   *pubsub.Subscription
)

func InitGossip(ctx context.Context) error {
	var err error
	PS, err = pubsub.NewGossipSub(ctx, Node)
	if err != nil {
		return err
	}
	topic, err = PS.Join(TopicName)
	if err != nil {
		return err
	}
	sub, err = topic.Subscribe()
	if err != nil {
		return err
	}
	go receiveLoop(ctx)
	return nil
}

func PublishPin(ctx context.Context, ann PinAnnouncement) error {
	if Node == nil || topic == nil {
		return fmt.Errorf("p2p gossip not initialized")
	}
	ann.PeerID = Node.ID().String()
	data, err := json.Marshal(ann)
	if err != nil {
		return err
	}
	return topic.Publish(ctx, data)
}

func receiveLoop(ctx context.Context) {
	for {
		msg, err := sub.Next(ctx)
		if err != nil {
			return
		}
		if msg.ReceivedFrom == Node.ID() {
			continue
		}
		var ann PinAnnouncement
		if err := json.Unmarshal(msg.Data, &ann); err != nil {
			log.Printf("gossip: bad message from %s: %v", msg.ReceivedFrom, err)
			continue
		}
		HandleIncomingAnnouncement(ctx, ann)
	}
}
