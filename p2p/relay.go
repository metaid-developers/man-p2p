package p2p

import (
	"context"
	"log"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
)

func NATOptions() []libp2p.Option {
	cfg := GetConfig()
	opts := []libp2p.Option{
		libp2p.EnableNATService(),
		libp2p.EnableHolePunching(),
	}
	if cfg.EnableRelay {
		relayAddrs := parseRelayAddrs(cfg.BootstrapNodes)
		if len(relayAddrs) > 0 {
			opts = append(opts, libp2p.EnableAutoRelayWithStaticRelays(relayAddrs))
		}
	}
	return opts
}

func parseRelayAddrs(addrs []string) []peer.AddrInfo {
	var result []peer.AddrInfo
	for _, a := range addrs {
		pi, err := peer.AddrInfoFromString(a)
		if err == nil {
			result = append(result, *pi)
		}
	}
	return result
}

type mdnsNotifee struct{ h host.Host }

func (n *mdnsNotifee) HandlePeerFound(pi peer.AddrInfo) {
	log.Printf("mdns: discovered peer %s", pi.ID)
	_ = n.h.Connect(context.Background(), pi)
}

func InitMDNS(ctx context.Context) {
	svc := mdns.NewMdnsService(Node, "metaid-p2p", &mdnsNotifee{h: Node})
	if err := svc.Start(); err != nil {
		log.Printf("mdns: start failed: %v", err)
		return
	}
	go func() {
		<-ctx.Done()
		svc.Close()
	}()
}
