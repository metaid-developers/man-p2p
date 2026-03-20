package p2p

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

var (
	Node   host.Host
	KadDHT *dht.IpfsDHT
)

func InitHost(ctx context.Context, dataDir string) error {
	privKey, err := loadOrCreateIdentity(dataDir)
	if err != nil {
		return fmt.Errorf("identity: %w", err)
	}

	natOpts := NATOptions()
	allOpts := append([]libp2p.Option{
		libp2p.Identity(privKey),
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0", "/ip6/::/tcp/0"),
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

func connectBootstrapNodes(ctx context.Context) {
	cfg := GetConfig()
	for _, addrStr := range cfg.BootstrapNodes {
		ma, err := multiaddr.NewMultiaddr(addrStr)
		if err != nil {
			continue
		}
		pi, err := peer.AddrInfoFromP2pAddr(ma)
		if err != nil {
			continue
		}
		_ = Node.Connect(ctx, *pi)
	}
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
