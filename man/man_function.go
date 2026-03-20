package man

import (
	"fmt"
	"man-p2p/common"
	"man-p2p/pin"
	"strings"

	"github.com/bytedance/sonic"
)

const DefaultBatchSize = 1000
const (
	StatusBlockHeightLower      = -101
	StatusPinIsTransfered       = -102
	StatusModifyPinIdNotExist   = -201
	StatusModifyPinAddrNotExist = -202
	StatusModifyPinAddrDenied   = -203
	StatusModifyPinIsModifyed   = -204
	StatusModifyPinOptIsInit    = -205
	//Revoke
	StatusRevokePinIdNotExist   = -301
	StatusRevokePinAddrNotExist = -302
	StatusRevokePinAddrDenied   = -303
	StatusRevokePinIsRevoked    = -304
	StatusRevokePinOptIsInit    = -305
)

func handlePathAndOperation(pinList *[]*pin.PinInscription) {
	var modifyPinList []*pin.PinInscription
	var revokePinList []*pin.PinInscription
	defer func() {
		modifyPinList = nil
		revokePinList = nil
	}()

	for _, pinNode := range *pinList {
		if pinNode.MetaId == "" {
			pinNode.MetaId = common.GetMetaIdByAddress(pinNode.Address)
		}
		switch pinNode.Operation {
		case "modify":
			modifyPinList = append(modifyPinList, pinNode)
		case "revoke":
			revokePinList = append(revokePinList, pinNode)
		}
	}
	//处理modify的PIN
	handleModifyPin(modifyPinList)
	//处理revoke的PIN
	handleRevokePin(revokePinList)

}
func handleRevokePin(pinList []*pin.PinInscription) {
	if len(pinList) <= 0 {
		return
	}
	var revokePins []pin.PinInscription
	//找出待修改的目标PIN，修改状态
	for _, pinNode := range pinList {
		pinNode.OriginalPath = pinNode.Path
		pinNode.OriginalId = strings.Replace(pinNode.Path, "@", "", -1)
		targetPinId := pinNode.OriginalId
		if targetPinId == "" {
			continue
		}
		targetPin := findModifyTargetPin(targetPinId, "revoke")
		if targetPin == nil {
			continue
		}
		// 修改目标PIN的状态
		targetPin.Status = -1 //已撤销
		revokePins = append(revokePins, *targetPin)
	}
	if len(revokePins) > 0 {
		PebbleStore.Database.BatchInsertPins(revokePins)
	}
	revokePins = nil
}
func handleModifyPin(pinList []*pin.PinInscription) {
	if len(pinList) <= 0 {
		return
	}
	modifPinsMap := make(map[string]*pin.PinInscription)
	//找出待修改的目标PIN，修改content
	for _, pinNode := range pinList {
		pinNode.OriginalPath = pinNode.Path
		pinNode.OriginalId = strings.Replace(pinNode.Path, "@", "", -1)
		targetPinId := pinNode.OriginalId
		if targetPinId == "" {
			continue
		}
		var targetPin *pin.PinInscription
		if _, ok := modifPinsMap[targetPinId]; ok {
			targetPin = modifPinsMap[targetPinId]
		} else {
			targetPin = findModifyTargetPin(targetPinId, "modify")
		}
		if targetPin == nil {
			continue
		}
		if len(targetPin.OriginalContentBody) == 0 {
			oriPin := *targetPin
			targetPin.OriginalContentBody = oriPin.ContentBody
		}
		if targetPin.OriginalContentSummary == "" {
			oriPin := *targetPin
			targetPin.OriginalContentSummary = oriPin.ContentSummary
		}
		// 修改目标PIN的content
		targetPin.ContentBody = pinNode.ContentBody
		targetPin.ContentLength = pinNode.ContentLength
		targetPin.ContentSummary = pinNode.ContentSummary
		targetPin.ContentType = pinNode.ContentType
		targetPin.ContentTypeDetect = pinNode.ContentTypeDetect
		targetPin.Status = 1 //已修改

		// 记录修改历史
		// 查看是否重复记录修改历史
		find := false
		findSelf := false
		for _, mid := range targetPin.ModifyHistory {
			if mid == pinNode.Id {
				find = true
			}
			if mid == targetPin.Id {
				findSelf = true
			}
		}
		if !findSelf {
			targetPin.ModifyHistory = append(targetPin.ModifyHistory, targetPin.Id)
		}
		if !find {
			targetPin.ModifyHistory = append(targetPin.ModifyHistory, pinNode.Id)
		}
		// Save history to the modify PIN itself
		pinNode.ModifyHistory = make([]string, len(targetPin.ModifyHistory))
		copy(pinNode.ModifyHistory, targetPin.ModifyHistory)
		modifPinsMap[pinNode.Id] = pinNode

		modifPinsMap[targetPin.Id] = targetPin
	}
	var updatePins []pin.PinInscription
	//更新modify的PIN状态为已修改
	for _, pinNode := range modifPinsMap {
		updatePins = append(updatePins, *pinNode)
	}
	if len(updatePins) > 0 {
		PebbleStore.Database.BatchInsertPins(updatePins)
	}
	modifPinsMap = nil
	updatePins = nil
}
func findModifyTargetPin(targetPinId string, option string) (targetPin *pin.PinInscription) {
	//用pinid查找数据库，返回pin对象，如果PIN对象的Operation是modify，则继续查找，直到找到原始PIN
	val, err := PebbleStore.Database.GetPinByKey(targetPinId)
	if err != nil {
		return nil
	}
	var pinNode pin.PinInscription
	err = sonic.Unmarshal(val, &pinNode)
	if err != nil {
		return nil
	}
	if pinNode.Operation == option {
		return findModifyTargetPin(pinNode.OriginalId, option)
	} else {
		return &pinNode
	}
}
func createInfoAdditional(pinNode *pin.PinInscription, path string) (addition pin.MetaIdInfoAdditional) {
	if len(path) > 7 && path[0:6] == "/info/" {
		infoPathArr := strings.Split(path, "/")
		if len(infoPathArr) < 3 || infoPathArr[2] == "name" || infoPathArr[2] == "avatar" || infoPathArr[2] == "bio" || infoPathArr[2] == "background" {
			return
		}
		addition = pin.MetaIdInfoAdditional{
			MetaId:    pinNode.MetaId,
			InfoKey:   infoPathArr[2],
			InfoValue: string(pinNode.ContentBody),
			PinId:     pinNode.Id,
		}
	}
	return
}
func creatFollowData(pinNode *pin.PinInscription, follow bool) (followData *pin.FollowData) {
	if pinNode.MetaId == "" {
		pinNode.MetaId = common.GetMetaIdByAddress(pinNode.Address)
	}
	followData = &pin.FollowData{}
	if follow {
		followData.MetaId = string(pinNode.ContentBody)
		//followData.FollowMetaId = pinNode.MetaId
		followData.FollowMetaId = pinNode.CreateMetaId
		followData.FollowPinId = pinNode.Id
		followData.FollowTime = pinNode.Timestamp
		followData.Status = true
	} else {
		followData.FollowPinId = strings.Replace(pinNode.Path, "@", "", -1)
		followData.UnFollowPinId = pinNode.Id
		followData.Status = false
	}
	return
}
func getModifyPinStatus(curPinMap map[string]*pin.PinInscription, originalPinMap map[string]*pin.PinInscription) (statusMap map[string]int) {
	statusMap = make(map[string]int)
	for cid, np := range curPinMap {
		id := np.OriginalId
		if np.Operation == "modify" {
			if _, ok := originalPinMap[id]; !ok {
				statusMap[cid] = StatusModifyPinIdNotExist
				continue
			}
			if np.Address != originalPinMap[id].Address {
				statusMap[cid] = StatusModifyPinAddrDenied
				continue
			}
			if originalPinMap[id].Status == 1 {
				statusMap[cid] = StatusModifyPinIsModifyed
				continue
			}
			if originalPinMap[id].Operation == "init" {
				statusMap[cid] = StatusModifyPinOptIsInit
				continue
			}
		} else if np.Operation == "revoke" {
			if _, ok := originalPinMap[id]; !ok {
				statusMap[cid] = StatusRevokePinIdNotExist
				continue
			}
			if np.Address != originalPinMap[id].Address {
				statusMap[cid] = StatusRevokePinAddrDenied
				continue
			}
			if originalPinMap[id].Status == -1 {
				statusMap[cid] = StatusRevokePinIsRevoked
				continue
			}
			if originalPinMap[id].Operation == "init" {
				statusMap[cid] = StatusRevokePinOptIsInit
				continue
			}
			if len(originalPinMap[id].Path) > 5 && originalPinMap[id].Path[0:5] == "/info" {
				statusMap[cid] = StatusRevokePinOptIsInit
				continue
			}
		}
		if np.GenesisHeight <= originalPinMap[id].GenesisHeight {
			statusMap[cid] = StatusBlockHeightLower
			continue
		} else if originalPinMap[id].IsTransfered {
			statusMap[cid] = StatusPinIsTransfered
			continue
		}
	}
	return
}

func metaIdInfoParse(pinNode *pin.PinInscription, path string, metaIdData *map[string]*pin.MetaIdInfo) {
	if path == "" {
		path = pinNode.Path
	}
	if len(path) < 6 || path[0:6] != "/info/" {
		return
	}
	var metaIdInfo *pin.MetaIdInfo
	var ok bool
	//var err error
	metaIdInfo, ok = (*metaIdData)[pinNode.Address]
	if !ok {
		metaIdInfo, _ = PebbleStore.Database.GetMetaidInfo(common.GetMetaIdByAddress(pinNode.Address))
	}
	if metaIdInfo == nil {
		metaIdInfo = &pin.MetaIdInfo{MetaId: common.GetMetaIdByAddress(pinNode.Address), Address: pinNode.Address, PinId: pinNode.Id}
	}

	if metaIdInfo.MetaId == "" {
		metaIdInfo.MetaId = common.GetMetaIdByAddress(pinNode.Address)
	}
	if metaIdInfo.ChainName == "" {
		metaIdInfo.ChainName = pinNode.ChainName
	}
	switch path {
	case "/info/name":
		metaIdInfo.Name = string(pinNode.ContentBody)
		metaIdInfo.NameId = pinNode.Id
	case "/info/avatar":
		metaIdInfo.Avatar = fmt.Sprintf("/content/%s", pinNode.Id)
		metaIdInfo.AvatarId = pinNode.Id
	case "/info/nft-avatar":
		metaIdInfo.NftAvatar = fmt.Sprintf("/content/%s", pinNode.Id)
		metaIdInfo.NftAvatar = pinNode.Id
	case "/info/bio":
		metaIdInfo.Bio = string(pinNode.ContentBody)
		metaIdInfo.BioId = pinNode.Id
	case "/info/background":
		metaIdInfo.Background = fmt.Sprintf("/content/%s", pinNode.Id)
	case "/info/chatpubkey":
		metaIdInfo.ChatPubKey = string(pinNode.ContentBody)
	}
	(*metaIdData)[pinNode.Address] = metaIdInfo
}
