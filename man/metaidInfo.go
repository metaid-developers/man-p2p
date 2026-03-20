package man

import (
	"man-p2p/pin"
)

func handleMetaIdInfo(pinList *[]*pin.PinInscription) {
	metaIdData := make(map[string]*pin.MetaIdInfo)
	for _, pinNode := range *pinList {
		metaIdInfoParse(pinNode, "", &metaIdData)
	}
	if len(metaIdData) > 0 {
		PebbleStore.Database.BatchSetMetaidInfo(&metaIdData)
	}
}
