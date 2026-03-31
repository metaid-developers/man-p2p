package man

import (
	"man-p2p/pin"

	"github.com/bytedance/sonic"
)

func applyMempoolSeenTime(pinNode *pin.PinInscription, now int64, existingSeenTime int64) {
	pinNode.Timestamp = now
	if existingSeenTime > 0 {
		pinNode.SeenTime = existingSeenTime
		return
	}
	if pinNode.SeenTime == 0 {
		pinNode.SeenTime = now
	}
}

func mergeConfirmedSeenTimes(pinList []*pin.PinInscription, existingSeenTimes map[string]int64) {
	for _, pinNode := range pinList {
		if pinNode == nil {
			continue
		}
		if pinNode.SeenTime > 0 {
			continue
		}
		if existingSeenTime := existingSeenTimes[pinNode.Id]; existingSeenTime > 0 {
			pinNode.SeenTime = existingSeenTime
			continue
		}
		pinNode.SeenTime = pinNode.Timestamp
	}
}

func getExistingSeenTime(pinID string) int64 {
	if PebbleStore == nil || PebbleStore.Database == nil || pinID == "" {
		return 0
	}
	data, err := PebbleStore.Database.GetPinByKey(pinID)
	if err != nil || len(data) == 0 {
		return 0
	}
	var existing pin.PinInscription
	if err := sonic.Unmarshal(data, &existing); err != nil {
		return 0
	}
	return existing.SeenTime
}

func (pd *PebbleData) preserveSeenTimes(pinList []*pin.PinInscription) {
	if len(pinList) == 0 {
		return
	}
	pinIDs := make([]string, 0, len(pinList))
	for _, pinNode := range pinList {
		if pinNode == nil || pinNode.Id == "" {
			continue
		}
		pinIDs = append(pinIDs, pinNode.Id)
	}
	if len(pinIDs) == 0 {
		return
	}

	existingData := pd.Database.BatchGetPinByKeys(pinIDs, false)
	existingSeenTimes := make(map[string]int64, len(existingData))
	for pinID, data := range existingData {
		if len(data) == 0 {
			continue
		}
		var existing pin.PinInscription
		if err := sonic.Unmarshal(data, &existing); err != nil {
			continue
		}
		if existing.SeenTime > 0 {
			existingSeenTimes[pinID] = existing.SeenTime
		}
	}

	mergeConfirmedSeenTimes(pinList, existingSeenTimes)
}
