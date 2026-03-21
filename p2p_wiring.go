package main

import (
	"fmt"
	"man-p2p/man"
	"man-p2p/p2p"
	"man-p2p/pin"
)

func configureP2PCallbacks() {
	p2p.GetPinFn = func(pinId string) (*p2p.PinResponse, error) {
		pinNode, err := man.PebbleStore.Database.GetPinInscriptionByKey(pinId)
		if err != nil || pinNode.Id == "" {
			return nil, fmt.Errorf("not found")
		}
		pinCopy := pinNode
		return &p2p.PinResponse{Pin: &pinCopy}, nil
	}

	p2p.StorePinFn = func(resp *p2p.PinResponse) error {
		if resp == nil || resp.Pin == nil {
			return fmt.Errorf("missing pin payload")
		}
		return man.IngestP2PPin(resp.Pin)
	}

	p2p.StorePinMetadataOnlyFn = func(ann p2p.PinAnnouncement) error {
		contentLength := uint64(0)
		if ann.SizeBytes > 0 {
			contentLength = uint64(ann.SizeBytes)
		}
		return man.IngestP2PPin(&pin.PinInscription{
			Id:            ann.PinId,
			Path:          ann.Path,
			Address:       ann.Address,
			MetaId:        ann.MetaId,
			ChainName:     ann.ChainName,
			Timestamp:     ann.Timestamp,
			GenesisHeight: ann.GenesisHeight,
			ContentLength: contentLength,
		})
	}
}
