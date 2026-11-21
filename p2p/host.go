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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

var (
	Node                   host.Host
	KadDHT                 *dht.IpfsDHT
	bootstrapRetryInterval = 2 * time.Second
	bootstrapRetryAttempts = 15
	nodeMu                 sync.RWMutex
	bootstrapLoopMu        sync.Mutex
	bootstrapLoopRunning   bool
	bootstrapWakeCh        = make(chan struct{}, 1)
)

func InitHost(ctx context.Context, dataDir string) error {
	privKey, err := loadOrCreateIdentity(dataDir)
	if err != nil {
		return fmt.Errorf("identity: %w", err)
	}

	cfg := GetConfig()
	natOpts := NATOptions()
	ifaceAddrs, err := net.InterfaceAddrs()
	if err != nil {
		return fmt.Errorf("net.InterfaceAddrs: %w", err)
	}
	allOpts := append([]libp2p.Option{
		libp2p.Identity(privKey),
		libp2p.ListenAddrStrings(buildListenAddrStrings(cfg, ifaceAddrs)...),
		libp2p.NATPortMap(),
	}, natOpts...)
	announceAddrs, err := buildAnnounceAddrs(cfg)
	if err != nil {
		return fmt.Errorf("announce addrs: %w", err)
	}
	if len(announceAddrs) > 0 {
		allOpts = append(allOpts, libp2p.AddrsFactory(func([]multiaddr.Multiaddr) []multiaddr.Multiaddr {
			out := make([]multiaddr.Multiaddr, len(announceAddrs))
			copy(out, announceAddrs)
			return out
		}))
	}
	node, err := libp2p.New(allOpts...)
	if err != nil {
		return fmt.Errorf("libp2p.New: %w", err)
	}
	setNode(node)

	KadDHT, err = dht.New(ctx, node, dht.Mode(dht.ModeAuto))
	if err != nil {
		return fmt.Errorf("dht.New: %w", err)
	}
	if err := KadDHT.Bootstrap(ctx); err != nil {
		return fmt.Errorf("dht.Bootstrap: %w", err)
	}

	go func() {
		defer recoverPanic("bootstrap.connectLoop")
		connectBootstrapNodes(ctx)
	}()
	go InitMDNS(ctx)
	return nil
}

func buildListenAddrStrings(cfg P2PSyncConfig, ifaceAddrs []net.Addr) []string {
	port := listenPortSegment(cfg)
	defaultAddrs := []string{"/ip4/0.0.0.0/tcp/" + port, "/ip6/::/tcp/" + port}
	bootstrapIPv4s := extractBootstrapIPv4s(cfg.BootstrapNodes)
	if len(bootstrapIPv4s) == 0 {
		return defaultAddrs
	}

	seen := map[string]struct{}{}
	listenAddrs := []string{"/ip4/127.0.0.1/tcp/" + port}
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
				listenAddrs = append(listenAddrs, fmt.Sprintf("/ip4/%s/tcp/%s", key, port))
				break
			}
		}
	}

	if len(listenAddrs) == 1 {
		return defaultAddrs
	}
	return append(listenAddrs, "/ip6/::/tcp/"+port)
}

func listenPortSegment(cfg P2PSyncConfig) string {
	if cfg.ListenPort > 0 {
		return strconv.Itoa(cfg.ListenPort)
	}
	return "0"
}

func buildAnnounceAddrs(cfg P2PSyncConfig) ([]multiaddr.Multiaddr, error) {
	if len(cfg.AnnounceAddrs) == 0 {
		return nil, nil
	}
	if cfg.ListenPort <= 0 {
		return nil, fmt.Errorf("p2p_announce_addrs requires p2p_listen_port > 0")
	}

	port := strconv.Itoa(cfg.ListenPort)
	out := make([]multiaddr.Multiaddr, 0, len(cfg.AnnounceAddrs))
	for _, raw := range cfg.AnnounceAddrs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		addr, err := multiaddr.NewMultiaddr(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid announce addr %q: %w", raw, err)
		}
		tcpPort, err := addr.ValueForProtocol(multiaddr.P_TCP)
		if err != nil {
			return nil, fmt.Errorf("announce addr %q must include tcp port: %w", raw, err)
		}
		if tcpPort != port {
			return nil, fmt.Errorf("announce addr %q must use tcp port %s", raw, port)
		}
		out = append(out, addr)
	}
	return out, nil
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
	bootstrapLoopMu.Lock()
	if bootstrapLoopRunning {
		bootstrapLoopMu.Unlock()
		return
	}
	bootstrapLoopRunning = true
	bootstrapLoopMu.Unlock()
	defer func() {
		bootstrapLoopMu.Lock()
		bootstrapLoopRunning = false
		bootstrapLoopMu.Unlock()
	}()

	connectBootstrapNodesOnce(ctx)

	ticker := time.NewTicker(bootstrapRetryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-bootstrapWakeCh:
			connectBootstrapNodesOnce(ctx)
		case <-ticker.C:
			connectBootstrapNodesOnce(ctx)
		}
	}
}

func triggerBootstrapReconnect() {
	bootstrapLoopMu.Lock()
	running := bootstrapLoopRunning
	bootstrapLoopMu.Unlock()

	if running {
		select {
		case bootstrapWakeCh <- struct{}{}:
		default:
		}
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(bootstrapRetryAttempts)*bootstrapRetryInterval)
		defer cancel()
		connectBootstrapNodesOnce(ctx)
	}()
}

// bootstrapFailureState 记录每个 bootstrap 节点的连续失败状态，
// 用于失败日志降采样（R3/ROB-4：旧实现每轮 15 条失败日志刷屏）。
var (
	bootstrapFailMu    sync.Mutex
	bootstrapFailCount = map[peer.ID]int{}
	bootstrapLastWarn  = map[peer.ID]time.Time{}
)

const bootstrapWarnInterval = 60 * time.Second

func connectBootstrapNodesOnce(ctx context.Context) {
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
		node := currentNode()
		if node == nil {
			return
		}
		// R3：不再清除 libp2p 指数退避（旧实现每 2s 硬拨死节点），
		// 单次尝试交给外层 ticker 重试；Connected 则跳过。
		if node.Network().Connectedness(pi.ID) == network.Connected {
			bootstrapFailMu.Lock()
			delete(bootstrapFailCount, pi.ID)
			bootstrapFailMu.Unlock()
			continue
		}
		if err := node.Connect(ctx, *pi); err != nil {
			bootstrapFailMu.Lock()
			bootstrapFailCount[pi.ID]++
			count := bootstrapFailCount[pi.ID]
			last, hasLast := bootstrapLastWarn[pi.ID]
			now := time.Now()
			if !hasLast || now.Sub(last) >= bootstrapWarnInterval {
				bootstrapLastWarn[pi.ID] = now
				bootstrapFailMu.Unlock()
				log.Printf("p2p: bootstrap connect failed to %s (consecutive=%d, next warn in %s): %v", pi.ID, count, bootstrapWarnInterval, err)
			} else {
				bootstrapFailMu.Unlock()
			}
		} else {
			bootstrapFailMu.Lock()
			delete(bootstrapFailCount, pi.ID)
			delete(bootstrapLastWarn, pi.ID)
			bootstrapFailMu.Unlock()
			log.Printf("p2p: bootstrap connected %s", pi.ID)
		}
	}
}

func currentNode() host.Host {
	nodeMu.RLock()
	defer nodeMu.RUnlock()
	return Node
}

func setNode(node host.Host) {
	nodeMu.Lock()
	Node = node
	nodeMu.Unlock()
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
	closePresenceRuntime()
	if sub != nil {
		sub.Cancel()
		sub = nil
	}
	if topic != nil {
		_ = topic.Close()
		topic = nil
	}
	PS = nil
	if KadDHT != nil {
		_ = KadDHT.Close()
		KadDHT = nil
	}
	nodeMu.Lock()
	node := Node
	Node = nil
	nodeMu.Unlock()
	if node != nil {
		err := node.Close()
		return err
	}
	return nil
}
