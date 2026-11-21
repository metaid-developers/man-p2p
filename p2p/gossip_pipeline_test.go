package p2p

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

// TestGossipSeenMarkDedup 回归 R3：同 pinId 在 TTL 窗口内只放行一次；
// 不同 pinId 互不影响；超限裁剪后 map 仍有界。
func TestGossipSeenMarkDedup(t *testing.T) {
	gossipSeenMu.Lock()
	gossipSeen = map[string]time.Time{}
	gossipSeenMu.Unlock()
	t.Cleanup(func() {
		gossipSeenMu.Lock()
		gossipSeen = map[string]time.Time{}
		gossipSeenMu.Unlock()
	})

	if !gossipSeenMark("pin-a") {
		t.Fatal("first mark must pass")
	}
	if gossipSeenMark("pin-a") {
		t.Fatal("second mark within TTL must be deduped")
	}
	if !gossipSeenMark("pin-b") {
		t.Fatal("different pinId must pass")
	}

	// 洪峰裁剪：塞满超过 gossipSeenMax 后 map 仍有界且仍可去重
	for i := 0; i < gossipSeenMax+500; i++ {
		gossipSeenMark(time.Now().String() + ":" + string(rune('a'+i%26)) + itoa(i))
	}
	gossipSeenMu.Lock()
	size := len(gossipSeen)
	gossipSeenMu.Unlock()
	if size > gossipSeenMax+1000 {
		t.Fatalf("seen cache unbounded: %d", size)
	}
}

// TestHandleIncomingAnnouncementErrorReturn 回归 R3：fetch/store 失败必须
// 返回 error（驱动 gossip 流水线重试）；语义性跳过返回 nil。
func TestHandleIncomingAnnouncementErrorReturn(t *testing.T) {
	// ShouldSync 需 full 模式才放行
	if err := LoadConfig(writeTempConfig(t, `{"p2p_sync_mode":"full"}`)); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	ctx := context.Background()
	origGet, origStore, origMeta := GetPinFn, StorePinFn, StorePinMetadataOnlyFn
	t.Cleanup(func() { GetPinFn, StorePinFn, StorePinMetadataOnlyFn = origGet, origStore, origMeta })

	ann := PinAnnouncement{
		PinId:     "abc:0",
		PeerID:    validPeerIDString(t),
		ChainName: "mvc",
	}

	// 语义性跳过：ShouldSync=false（默认同步过滤规则拒绝该公告）→ nil
	if err := HandleIncomingAnnouncement(ctx, ann); err != nil {
		t.Fatalf("skip case should return nil, got %v", err)
	}

	// fetch 失败 → error：起真实 host（peerstore 无目标地址，拨号立即失败）
	origNode := Node
	testHost, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	if err != nil {
		t.Fatalf("libp2p.New: %v", err)
	}
	setNode(testHost)
	t.Cleanup(func() {
		_ = testHost.Close()
		setNode(origNode)
	})
	GetPinFn = nil
	StorePinFn = func(resp *PinResponse) error { return nil }
	ann2 := ann
	ann2.PinId = "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdefi0"
	ann2.Path = "/x"
	ann2.Address = "addr"
	ann2.MetaId = "m"
	ann2.Confirmed = true
	if err := HandleIncomingAnnouncement(ctx, ann2); err == nil {
		t.Fatal("fetch failure must return error")
	}
}

func validPeerIDString(t *testing.T) string {
	t.Helper()
	ma, err := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/4001/p2p/12D3KooWSvVfJ7s37hsCfRHuhccWxocxyjU6uKGKF4czBGZk8f5H")
	if err != nil {
		t.Fatalf("multiaddr: %v", err)
	}
	pi, err := peer.AddrInfoFromP2pAddr(ma)
	if err != nil {
		t.Fatalf("addrinfo: %v", err)
	}
	return pi.ID.String()
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

var errTest = errors.New("test")
