package p2p

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"man-p2p/pin"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

const SyncProtocol = protocol.ID("/metaid/pin-sync/1.0.0")

// R3 硬化常量：
//   - syncStreamDeadline：stream handler 整个请求-响应的墙钟上限
//   - syncFetchTimeout：FetchPin 单次拉取超时（旧实现无 per-request 超时，对端
//     收下请求不回数据会永久阻塞）
//   - maxPinResponseBytes：响应解码上限（恶意/故障对端声明小 payload 回大 JSON
//     的 OOM 面；正常 pin 远小于此值）
const (
	syncStreamDeadline  = 30 * time.Second
	syncFetchTimeout    = 30 * time.Second
	maxPinResponseBytes = 32 << 20 // 32MB
)

// storageLimitReached is declared in storage.go.

type PinRequest struct {
	PinId string `json:"pinId"`
}

type PinResponse struct {
	Pin   *pin.PinInscription `json:"pin,omitempty"`
	Error string              `json:"error,omitempty"`
}

var GetPinFn func(pinId string) (*PinResponse, error)
var StorePinFn func(resp *PinResponse) error
var StorePinMetadataOnlyFn func(ann PinAnnouncement) error

func RegisterSyncHandler() {
	Node.SetStreamHandler(SyncProtocol, func(s network.Stream) {
		// R3：handler panic 不杀进程；GetPinFn 未初始化时返回错误而非 panic
		defer recoverPanic("sync.streamHandler")
		defer s.Close()
		// 整个请求-响应限制在 syncStreamDeadline 内，防止对端不读导致 goroutine 永久阻塞
		_ = s.SetDeadline(time.Now().Add(syncStreamDeadline))

		var req PinRequest
		if err := json.NewDecoder(s).Decode(&req); err != nil {
			return
		}
		var resp *PinResponse
		if GetPinFn == nil {
			resp = &PinResponse{Error: "sync handler not ready"}
		} else {
			var err error
			resp, err = GetPinFn(req.PinId)
			if err != nil {
				resp = &PinResponse{Error: err.Error()}
			}
		}
		json.NewEncoder(s).Encode(resp)
	})
}

func FetchPin(ctx context.Context, peerID peer.ID, pinId string) (*PinResponse, error) {
	// R3：per-request 超时（旧实现直接用调用方 ctx，对端不回数据则永久阻塞）
	fetchCtx, cancel := context.WithTimeout(ctx, syncFetchTimeout)
	defer cancel()

	s, err := Node.NewStream(fetchCtx, peerID, SyncProtocol)
	if err != nil {
		return nil, fmt.Errorf("open stream to %s: %w", peerID, err)
	}
	defer s.Close()
	_ = s.SetDeadline(time.Now().Add(syncFetchTimeout))

	if err := json.NewEncoder(s).Encode(PinRequest{PinId: pinId}); err != nil {
		return nil, err
	}
	s.CloseWrite()

	// R3：解码上限 32MB，防止对端回超大 payload 打爆内存
	var resp PinResponse
	decoder := json.NewDecoder(io.LimitReader(bufio.NewReader(s), maxPinResponseBytes))
	if err := decoder.Decode(&resp); err != nil && err != io.EOF {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("peer error: %s", resp.Error)
	}
	if resp.Pin == nil {
		return nil, fmt.Errorf("peer returned empty pin payload")
	}
	return &resp, nil
}

// HandleIncomingAnnouncement 处理一条 pin 公告：拉取并写库。
// R3：返回 error 驱动 gossip 流水线的有界退避重试（语义性跳过——storage
// 满额、ShouldSync 过滤、超大内容走 metadata-only、坏公告——返回 nil）。
func HandleIncomingAnnouncement(ctx context.Context, ann PinAnnouncement) error {
	if currentNode() == nil {
		return nil // p2p host 未初始化（进程关闭中/测试环境）
	}
	if storageLimitReached.Load() {
		return nil
	}
	if !ShouldSync(ann) {
		return nil
	}

	cfg := GetConfig()
	if cfg.MaxContentSizeKB > 0 && ann.SizeBytes > cfg.MaxContentSizeKB*1024 {
		if StorePinMetadataOnlyFn != nil {
			if err := StorePinMetadataOnlyFn(ann); err != nil {
				log.Printf("sync: store metadata-only for %s failed: %v", ann.PinId, err)
				return err
			}
		}
		return nil
	}

	peerID, err := peer.Decode(ann.PeerID)
	if err != nil {
		log.Printf("sync: invalid peer ID %s: %v", ann.PeerID, err)
		return nil // 坏公告重试无意义
	}
	resp, err := FetchPin(ctx, peerID, ann.PinId)
	if err != nil {
		log.Printf("sync: fetch %s from %s failed: %v", ann.PinId, peerID, err)
		return err
	}
	if StorePinFn != nil {
		if err := StorePinFn(resp); err != nil {
			log.Printf("sync: store %s failed: %v", ann.PinId, err)
			return err
		}
	}
	return nil
}
