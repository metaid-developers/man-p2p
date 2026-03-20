package p2p

import (
	"context"
	"log"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
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

// mdnsNotifee implements local mDNS peer discovery callbacks.
// It is used by mdnsDiscovery (set at runtime when the mDNS service is
// available) and kept here so that the interface contract is visible.
type mdnsNotifee struct{ h host.Host }

func (n *mdnsNotifee) HandlePeerFound(pi peer.AddrInfo) {
	log.Printf("mdns: discovered peer %s", pi.ID)
	_ = n.h.Connect(context.Background(), pi)
}

// mdnsDiscovery is a hook that InitMDNS uses to start the mDNS service.
// The default implementation is a no-op. In environments where the
// github.com/libp2p/zeroconf/v2 transitive dependency is resolvable,
// replace this with a real mdns.NewMdnsService call.
//
// Example (from a separate file once zeroconf is in the module cache):
//
//	func init() {
//	    mdnsDiscovery = func(ctx context.Context, h host.Host) {
//	        svc := mdns.NewMdnsService(h, "metaid-p2p", &mdnsNotifee{h: h})
//	        if err := svc.Start(); err != nil { ... }
//	        go func() { <-ctx.Done(); svc.Close() }()
//	    }
//	}
var mdnsDiscovery func(ctx context.Context, h host.Host) = nil

// InitMDNS starts local-network peer discovery via mDNS.
// When mdnsDiscovery is nil (default) the function is a no-op; the
// network will still operate via DHT and bootstrap nodes.
func InitMDNS(ctx context.Context) {
	if mdnsDiscovery == nil {
		log.Printf("mdns: skipped (zeroconf dependency not available)")
		return
	}
	mdnsDiscovery(ctx, Node)
}
