package api

import (
	"man-p2p/common"
	"man-p2p/man"
	"man-p2p/pin"
)

var infoBackfillPaths = []string{
	"/info/name",
	"/info/avatar",
	"/info/bio",
	"/info/chatpubkey",
	"/info/background",
	"/info/nft-avatar",
}

func enrichMetaIdInfo(info *pin.MetaIdInfo, addressHint string) *pin.MetaIdInfo {
	if info == nil {
		info = &pin.MetaIdInfo{}
	} else {
		copyInfo := *info
		info = &copyInfo
	}

	if info.Address == "" && addressHint != "" {
		info.Address = addressHint
	}
	if info.MetaId == "" && info.Address != "" {
		info.MetaId = common.GetMetaIdByAddress(info.Address)
	}
	if info.Address == "" && info.MetaId != "" {
		if pinNode := firstInfoPin(info.MetaId, infoBackfillPaths...); pinNode != nil {
			info.Address = pinNode.Address
		}
	}
	if info.GlobalMetaId == "" && info.Address != "" {
		info.GlobalMetaId = common.ConvertToGlobalMetaId(info.Address)
	}
	if info.ChatPubKeyId == "" && info.MetaId != "" {
		if pinNode := firstInfoPin(info.MetaId, "/info/chatpubkey"); pinNode != nil {
			info.ChatPubKeyId = pinNode.Id
			if info.ChatPubKey == "" {
				info.ChatPubKey = pinNode.ContentSummary
			}
			if info.Address == "" {
				info.Address = pinNode.Address
			}
			if info.GlobalMetaId == "" && info.Address != "" {
				info.GlobalMetaId = common.ConvertToGlobalMetaId(info.Address)
			}
		}
	}

	return info
}

func firstInfoPin(metaid string, paths ...string) *pin.PinInscription {
	if man.PebbleStore == nil || man.PebbleStore.Database == nil {
		return nil
	}
	for _, path := range paths {
		pinList, _, _, err := man.PebbleStore.GetPinByMetaIdAndPathPageList(metaid, path, "0", 1)
		if err != nil || len(pinList) == 0 {
			continue
		}
		return pinList[0]
	}
	return nil
}
