package p2p

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	swarm "github.com/libp2p/go-libp2p/p2p/net/swarm"
	"github.com/multiformats/go-multiaddr"
)

var (
	Node                   host.Host
	KadDHT                 *dht.IpfsDHT
	bootstrapRetryInterval = 2 * time.Second
	bootstrapRetryAttempts = 15
)

func InitHost(ctx context.Context, dataDir string) error {
	privKey, err := loadOrCreateIdentity(dataDir)
	if err != nil {
		return fmt.Errorf("identity: %w", err)
	}

	natOpts := NATOptions()
	ifaceAddrs, err := net.InterfaceAddrs()
	if err != nil {
		return fmt.Errorf("net.InterfaceAddrs: %w", err)
	}
	allOpts := append([]libp2p.Option{
		libp2p.Identity(privKey),
		libp2p.ListenAddrStrings(buildListenAddrStrings(GetConfig(), ifaceAddrs)...),
		libp2p.NATPortMap(),
	}, natOpts...)
	Node, err = libp2p.New(allOpts...)
	if err != nil {
		return fmt.Errorf("libp2p.New: %w", err)
	}

	KadDHT, err = dht.New(ctx, Node, dht.Mode(dht.ModeAuto))
	if err != nil {
		return fmt.Errorf("dht.New: %w", err)
	}
	if err := KadDHT.Bootstrap(ctx); err != nil {
		return fmt.Errorf("dht.Bootstrap: %w", err)
	}

	go connectBootstrapNodes(ctx)
	go InitMDNS(ctx)
	return nil
}

func buildListenAddrStrings(cfg P2PSyncConfig, ifaceAddrs []net.Addr) []string {
	defaultAddrs := []string{"/ip4/0.0.0.0/tcp/0", "/ip6/::/tcp/0"}
	bootstrapIPv4s := extractBootstrapIPv4s(cfg.BootstrapNodes)
	if len(bootstrapIPv4s) == 0 {
		return defaultAddrs
	}

	seen := map[string]struct{}{}
	listenAddrs := []string{"/ip4/127.0.0.1/tcp/0"}
	seen["127.0.0.1"] = struct{}{}

	for _, addr := range ifaceAddrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet == nil {
			continue
		}
		ip := ipNet.IP.To4()
		if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
			continue
		}
		for _, bootstrapIP := range bootstrapIPv4s {
			if ipNet.Contains(bootstrapIP) {
				key := ip.String()
				if _, exists := seen[key]; exists {
					break
				}
				seen[key] = struct{}{}
				listenAddrs = append(listenAddrs, fmt.Sprintf("/ip4/%s/tcp/0", key))
				break
			}
		}
	}

	if len(listenAddrs) == 1 {
		return defaultAddrs
	}
	return append(listenAddrs, "/ip6/::/tcp/0")
}

func extractBootstrapIPv4s(addrs []string) []net.IP {
	var result []net.IP
	for _, addr := range addrs {
		parts := strings.Split(strings.TrimSpace(addr), "/")
		if len(parts) < 5 || parts[1] != "ip4" || parts[3] != "tcp" {
			continue
		}
		ip := net.ParseIP(strings.TrimSpace(parts[2])).To4()
		if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
			continue
		}
		result = append(result, ip)
	}
	return result
}

func connectBootstrapNodes(ctx context.Context) {
	cfg := GetConfig()
	for _, addrStr := range cfg.BootstrapNodes {
		ma, err := multiaddr.NewMultiaddr(addrStr)
		if err != nil {
			log.Printf("p2p: invalid bootstrap addr %q: %v", addrStr, err)
			continue
		}
		pi, err := peer.AddrInfoFromP2pAddr(ma)
		if err != nil {
			log.Printf("p2p: invalid bootstrap peer addr %q: %v", addrStr, err)
			continue
		}
		if Node == nil {
			return
		}
		for attempt := 1; attempt <= bootstrapRetryAttempts; attempt++ {
			if Node.Network().Connectedness(pi.ID) == network.Connected {
				break
			}
			clearBootstrapDialBackoff(pi.ID)
			err = Node.Connect(ctx, *pi)
			if err == nil {
				log.Printf("p2p: bootstrap connected %s on attempt %d", pi.ID, attempt)
				break
			}
			log.Printf("p2p: bootstrap connect failed to %s on attempt %d/%d: %v", pi.ID, attempt, bootstrapRetryAttempts, err)
			if attempt == bootstrapRetryAttempts {
				break
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(bootstrapRetryInterval):
			}
		}
	}
}

func clearBootstrapDialBackoff(peerID peer.ID) {
	if Node == nil {
		return
	}
	swarmNet, ok := Node.Network().(*swarm.Swarm)
	if !ok {
		return
	}
	swarmNet.Backoff().Clear(peerID)
}

func loadOrCreateIdentity(dataDir string) (crypto.PrivKey, error) {
	keyPath := filepath.Join(dataDir, "identity.key")
	if data, err := os.ReadFile(keyPath); err == nil {
		b, err := hex.DecodeString(string(data))
		if err == nil {
			return crypto.UnmarshalPrivateKey(b)
		}
	}
	priv, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, err
	}
	b, err := crypto.MarshalPrivateKey(priv)
	if err != nil {
		return nil, err
	}
	_ = os.MkdirAll(dataDir, 0700)
	_ = os.WriteFile(keyPath, []byte(hex.EncodeToString(b)), 0600)
	return priv, nil
}

func CloseHost() error {
	if Node != nil {
		return Node.Close()
	}
	return nil
}
