package man

import (
	"man-p2p/common"
	"man-p2p/pin"
)

func handleMetaIdInfo(pinList *[]*pin.PinInscription) {
	metaIdData := make(map[string]*pin.MetaIdInfo)
	for _, pinNode := range *pinList {
		metaIdInfoParse(pinNode, "", &metaIdData)
	}
	if len(metaIdData) > 0 {
		metaIdIndex := make(map[string]*pin.MetaIdInfo, len(metaIdData))
		for _, info := range metaIdData {
			if info == nil {
				continue
			}
			metaId := info.MetaId
			if metaId == "" && info.Address != "" {
				metaId = common.GetMetaIdByAddress(info.Address)
			}
			if metaId == "" {
				continue
			}
			metaIdIndex[metaId] = info
		}
		if len(metaIdIndex) > 0 {
			PebbleStore.Database.BatchSetMetaidInfo(&metaIdIndex)
		}
	}
}
