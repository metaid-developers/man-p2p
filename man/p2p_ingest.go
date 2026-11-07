package man

import (
	"fmt"
	"man-p2p/common"
	"man-p2p/pin"
	"time"
)

func IngestP2PPin(pinNode *pin.PinInscription) error {
	if pinNode == nil || pinNode.Id == "" {
		return fmt.Errorf("missing pin payload")
	}
	if PebbleStore.Database == nil {
		return fmt.Errorf("pebble database not initialized")
	}

	pinCopy := *pinNode
	if pinCopy.MetaId == "" && pinCopy.Address != "" {
		pinCopy.MetaId = common.GetMetaIdByAddress(pinCopy.Address)
	}
	if pinCopy.Timestamp == 0 {
		pinCopy.Timestamp = time.Now().Unix()
	}
	if pinCopy.ContentLength == 0 && len(pinCopy.ContentBody) > 0 {
		pinCopy.ContentLength = uint64(len(pinCopy.ContentBody))
	}
	if pinCopy.ContentTypeDetect == "" && len(pinCopy.ContentBody) > 0 {
		pinCopy.ContentTypeDetect = common.DetectContentType(&pinCopy.ContentBody)
	}

	height := pinCopy.GenesisHeight
	if height < 0 {
		if err := PebbleStore.Database.SetMempool(&pinCopy); err != nil {
			return err
		}
	}
	if err := PebbleStore.Database.SetAllPins(height, []*pin.PinInscription{&pinCopy}, 1); err != nil {
		return err
	}

	pins := []*pin.PinInscription{&pinCopy}
	handlePathAndOperation(&pins)
	handleMetaIdInfo(&pins)
	return nil
}
