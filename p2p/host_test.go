package p2p

import (
	"context"
	"crypto/rand"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

func mustCIDR(t *testing.T, cidr string) net.Addr {
	t.Helper()
	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatalf("parse cidr %s: %v", cidr, err)
	}
	ipNet.IP = ip
	return ipNet
}

func TestBuildListenAddrStringsPrefersBootstrapSubnetIPv4(t *testing.T) {
	addrs := []net.Addr{
		mustCIDR(t, "127.0.0.1/8"),
		mustCIDR(t, "10.211.55.2/24"),
		mustCIDR(t, "192.168.3.52/24"),
	}

	got := buildListenAddrStrings(P2PSyncConfig{
		BootstrapNodes: []string{
			"/ip4/192.168.3.30/tcp/53172/p2p/12D3KooWBootstrap",
		},
	}, addrs)

	want := []string{
		"/ip4/127.0.0.1/tcp/0",
		"/ip4/192.168.3.52/tcp/0",
		"/ip6/::/tcp/0",
	}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestBuildListenAddrStringsFallsBackToWildcardWhenNoSubnetMatch(t *testing.T) {
	addrs := []net.Addr{
		mustCIDR(t, "127.0.0.1/8"),
		mustCIDR(t, "10.211.55.2/24"),
	}

	got := buildListenAddrStrings(P2PSyncConfig{
		BootstrapNodes: []string{
			"/ip4/192.168.3.30/tcp/53172/p2p/12D3KooWBootstrap",
		},
	}, addrs)

	want := []string{
		"/ip4/0.0.0.0/tcp/0",
		"/ip6/::/tcp/0",
	}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

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

func reserveTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected listener addr type %T", listener.Addr())
	}
	return addr.Port
}

func waitForPeerConnection(t *testing.T, expected peer.ID, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, connected := range Node.Network().Peers() {
			if connected == expected {
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("expected peer %s to connect within %s, peers=%v", expected, timeout, Node.Network().Peers())
}

func TestConnectBootstrapNodesRetriesUntilPeerBecomesAvailable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	originalNode := Node
	originalRetryInterval := bootstrapRetryInterval
	originalRetryAttempts := bootstrapRetryAttempts
	originalConfig := GetConfig()
	t.Cleanup(func() {
		Node = originalNode
		bootstrapRetryInterval = originalRetryInterval
		bootstrapRetryAttempts = originalRetryAttempts
		configMu.Lock()
		currentConfig = originalConfig
		configMu.Unlock()
	})

	localHost, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	if err != nil {
		t.Fatal(err)
	}
	defer localHost.Close()
	Node = localHost

	bootstrapPort := reserveTCPPort(t)
	bootstrapPriv, _, err := libp2pcrypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	bootstrapPeerID, err := peer.IDFromPrivateKey(bootstrapPriv)
	if err != nil {
		t.Fatal(err)
	}

	configMu.Lock()
	currentConfig = P2PSyncConfig{
		BootstrapNodes: []string{
			"/ip4/127.0.0.1/tcp/" + strconv.Itoa(bootstrapPort) + "/p2p/" + bootstrapPeerID.String(),
		},
	}
	configMu.Unlock()

	bootstrapRetryInterval = 50 * time.Millisecond
	bootstrapRetryAttempts = 20

	go connectBootstrapNodes(ctx)

	time.Sleep(150 * time.Millisecond)

	bootstrapHost, err := libp2p.New(
		libp2p.Identity(bootstrapPriv),
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/"+strconv.Itoa(bootstrapPort)),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer bootstrapHost.Close()

	waitForPeerConnection(t, bootstrapHost.ID(), 2*time.Second)
}
