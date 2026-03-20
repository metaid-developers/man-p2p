package p2p

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

const SyncProtocol = protocol.ID("/metaid/pin-sync/1.0.0")

// storageLimitReached is declared in storage.go.

type PinRequest struct {
	PinId string `json:"pinId"`
}

type PinResponse struct {
	PinId     string `json:"pinId"`
	Path      string `json:"path"`
	Address   string `json:"address"`
	Confirmed bool   `json:"confirmed"`
	Content   []byte `json:"content"`
	Error     string `json:"error,omitempty"`
}

var GetPinFn func(pinId string) (*PinResponse, error)
var StorePinFn func(resp *PinResponse) error
var StorePinMetadataOnlyFn func(ann PinAnnouncement) error

func RegisterSyncHandler() {
	Node.SetStreamHandler(SyncProtocol, func(s network.Stream) {
		defer s.Close()
		var req PinRequest
		if err := json.NewDecoder(s).Decode(&req); err != nil {
			return
		}
		resp, err := GetPinFn(req.PinId)
		if err != nil {
			resp = &PinResponse{PinId: req.PinId, Error: err.Error()}
		}
		json.NewEncoder(s).Encode(resp)
	})
}

func FetchPin(ctx context.Context, peerID peer.ID, pinId string) (*PinResponse, error) {
	s, err := Node.NewStream(ctx, peerID, SyncProtocol)
	if err != nil {
		return nil, fmt.Errorf("open stream to %s: %w", peerID, err)
	}
	defer s.Close()

	if err := json.NewEncoder(s).Encode(PinRequest{PinId: pinId}); err != nil {
		return nil, err
	}
	s.CloseWrite()

	var resp PinResponse
	if err := json.NewDecoder(bufio.NewReader(s)).Decode(&resp); err != nil && err != io.EOF {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("peer error: %s", resp.Error)
	}
	return &resp, nil
}

func HandleIncomingAnnouncement(ctx context.Context, ann PinAnnouncement) {
	if storageLimitReached.Load() {
		return
	}
	if !ShouldSync(ann) {
		return
	}

	cfg := GetConfig()
	if cfg.MaxContentSizeKB > 0 && ann.SizeBytes > cfg.MaxContentSizeKB*1024 {
		if StorePinMetadataOnlyFn != nil {
			if err := StorePinMetadataOnlyFn(ann); err != nil {
				log.Printf("sync: store metadata-only for %s failed: %v", ann.PinId, err)
			}
		}
		return
	}

	peerID, err := peer.Decode(ann.PeerID)
	if err != nil {
		log.Printf("sync: invalid peer ID %s: %v", ann.PeerID, err)
		return
	}
	resp, err := FetchPin(ctx, peerID, ann.PinId)
	if err != nil {
		log.Printf("sync: fetch %s from %s failed: %v", ann.PinId, peerID, err)
		return
	}
	if StorePinFn != nil {
		if err := StorePinFn(resp); err != nil {
			log.Printf("sync: store %s failed: %v", ann.PinId, err)
		}
	}
}
