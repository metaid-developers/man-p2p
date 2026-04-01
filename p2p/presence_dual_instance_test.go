package p2p

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
)

func TestPresenceAnnouncementPropagatesAndExpires(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	nodeA, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	if err != nil {
		t.Fatal(err)
	}
	defer nodeA.Close()

	nodeB, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	if err != nil {
		t.Fatal(err)
	}
	defer nodeB.Close()

	if err := nodeB.Connect(ctx, peer.AddrInfo{ID: nodeA.ID(), Addrs: nodeA.Addrs()}); err != nil {
		t.Fatal(err)
	}

	psA, err := pubsub.NewGossipSub(ctx, nodeA)
	if err != nil {
		t.Fatal(err)
	}
	psB, err := pubsub.NewGossipSub(ctx, nodeB)
	if err != nil {
		t.Fatal(err)
	}

	runtimeA, err := newPresenceRuntime(ctx, nodeA, psA, presenceRuntimeOptions{
		localGlobalMetaIDs: func() []string { return []string{"idq1providera"} },
		broadcastInterval:  50 * time.Millisecond,
		ttlSec:             1,
		jitterRange:        0,
		runtimeMode:        "p2p-only",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeA.close()

	runtimeB, err := newPresenceRuntime(ctx, nodeB, psB, presenceRuntimeOptions{
		localGlobalMetaIDs: func() []string { return nil },
		broadcastInterval:  50 * time.Millisecond,
		ttlSec:             1,
		jitterRange:        0,
		runtimeMode:        "p2p-only",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeB.close()

	runtimeA.startBroadcastLoop()

	waitForPresenceBot(t, runtimeB, "idq1providera", nodeA.ID().String(), 3*time.Second)

	runtimeA.close()
	waitForPresenceBroadcastLoopStopped(t, runtimeA, 2*time.Second)

	waitForPresenceBotGone(t, runtimeB, "idq1providera", 3*time.Second)
}

func waitForPresenceBot(t *testing.T, runtime *presenceRuntime, globalMetaID, peerID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status := runtime.status()
		bot, ok := status.OnlineBots[globalMetaID]
		if ok {
			for _, candidate := range bot.PeerIDs {
				if candidate == peerID {
					return
				}
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("expected %s from %s within %s, last status=%#v", globalMetaID, peerID, timeout, runtime.status())
}

func waitForPresenceBotGone(t *testing.T, runtime *presenceRuntime, globalMetaID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status := runtime.status()
		if _, ok := status.OnlineBots[globalMetaID]; !ok {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("expected %s to expire within %s, last status=%#v", globalMetaID, timeout, runtime.status())
}

func waitForPresenceBroadcastLoopStopped(t *testing.T, runtime *presenceRuntime, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		runtime.broadcastMu.Lock()
		started := runtime.broadcastStarted
		runtime.broadcastMu.Unlock()
		if !started {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	runtime.broadcastMu.Lock()
	started := runtime.broadcastStarted
	runtime.broadcastMu.Unlock()
	t.Fatalf("expected broadcast loop to stop within %s, started=%v", timeout, started)
}
