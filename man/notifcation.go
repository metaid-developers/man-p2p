package man

import (
	"fmt"
	"log"
	"man-p2p/common"
	"man-p2p/pin"
	"regexp"
	"strings"
	"time"

	"github.com/bytedance/sonic"
	"github.com/cockroachdb/pebble"
)

var notifcationPath = map[string]bool{
	"/follow":                 true,
	"/protocols/simpledonate": true,
	"/protocols/paylike":      true,
	"/protocols/paycomment":   true,
	"/protocols/simplebuzz":   true,
}

// getStringFromMap safely extracts a string value from map[string]interface{}
// It handles string, float64, int, and other numeric types
func getStringFromMap(dataMap map[string]interface{}, key string) string {
	val, ok := dataMap[key]
	if !ok || val == nil {
		return ""
	}
	switch v := val.(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%.0f", v)
	case int:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	case bool:
		if v {
			return "1"
		}
		return "0"
	default:
		return fmt.Sprintf("%v", v)
	}
}

func handNotifcation(pinNode *pin.PinInscription) {
	if !common.ModuleExist("metaso_notifcation") {
		return
	}
	if _, ok := common.NotifcationBlackedHost[pinNode.Host]; ok {
		return
	}
	if _, ok := notifcationPath[pinNode.Path]; !ok {
		return
	}
	toPINList := getNotifcationToAddress(pinNode)
	if len(toPINList) == 0 {
		return
	}
	for _, toPIN := range toPINList {
		notifcationType := pinNode.Path
		if toPIN.Path == "Mention" {
			notifcationType = "Mention"
		}
		notifcationId := time.Now().UnixMilli()
		notifcationData := pin.NotifcationData{
			NotifcationId:   notifcationId,
			NotifcationType: notifcationType,
			FromPinId:       pinNode.Id,
			FromAddress:     pinNode.Address,
			FromPinHost:     pinNode.Host,
			FromPinChain:    pinNode.ChainName,
			NotifcationPin:  toPIN.Id,
			NotifcationTime: time.Now().Unix(),
			NotifcationHost: toPIN.Host,
		}
		// Save the notification data to DB using V2
		content, err := sonic.Marshal(notifcationData)
		if err != nil {
			return
		}
		if toPIN.Path == "Mention" {
			log.Printf("==> handNotifcation to %s, content: %s", toPIN.Address, content)
		}
		// 使用新的V2方法存储
		PebbleStore.Database.SetNotifcationV2(toPIN.Address, notifcationId, pinNode.Id, content)
		// 异步清理旧通知（保留最近300条）
		go PebbleStore.Database.CleanUpNotifcationV2(toPIN.Address, 300)
	}
}

func getNotifcationToAddress(pinNode *pin.PinInscription) (toPIN []pin.PinInscription) {
	switch pinNode.Path {
	case "/follow":
		toPIN, _ = getFollowPin(pinNode)
	case "/protocols/simpledonate":
		toPIN, _ = getDonatePin(pinNode)
	case "/protocols/paylike":
		toPIN, _ = getPayLikePin(pinNode)
	case "/protocols/paycomment":
		toPIN, _ = getPaycommentPin(pinNode)
	case "/protocols/simplebuzz":
		toPIN, _ = getRepostPin(pinNode)
		toPIN2, _ := getAtIdCoinPin(pinNode)
		if len(toPIN2) > 0 {
			toPIN = append(toPIN, toPIN2...)
		}
	}
	return
}
func getPINbyId(pinId string) (pinNode pin.PinInscription, err error) {
	pinNode, err = PebbleStore.Database.GetPinInscriptionByKey(pinId)
	switch err {
	case nil:
		return
	case pebble.ErrNotFound:
		pinNode, err = PebbleStore.Database.GetMempoolPin(pinId)
	}
	return
}
func getFollowPin(pinNode *pin.PinInscription) (toPIN []pin.PinInscription, err error) {
	metaid := string(pinNode.ContentBody)
	//从MetaidInfoDB获取数据
	data, closer, err := PebbleStore.Database.MetaidInfoDB.Get([]byte(metaid))
	if err != nil {
		return
	}
	defer closer.Close()
	var info pin.MetaIdInfo
	err = sonic.Unmarshal(data, &info)
	if err != nil {
		return
	}
	toPIN = []pin.PinInscription{
		{
			Id:      pinNode.Id,
			Address: info.Address,
		},
	}
	return
}
func getDonatePin(pinNode *pin.PinInscription) (toPIN []pin.PinInscription, err error) {
	var dataMap map[string]interface{}
	err = sonic.Unmarshal(pinNode.ContentBody, &dataMap)
	if err != nil {
		return
	}
	toPinId := getStringFromMap(dataMap, "toPin")
	if toPinId == "" {
		return
	}
	to, _ := getPINbyId(toPinId)
	return []pin.PinInscription{to}, nil
}
func getPayLikePin(pinNode *pin.PinInscription) (toPIN []pin.PinInscription, err error) {
	var dataMap map[string]interface{}
	err = sonic.Unmarshal(pinNode.ContentBody, &dataMap)
	if err != nil {
		return
	}
	if _, ok := dataMap["likeTo"]; !ok {
		return
	}
	if _, ok := dataMap["isLike"]; !ok {
		return
	}
	likeTo := getStringFromMap(dataMap, "likeTo")
	isLike := getStringFromMap(dataMap, "isLike")
	if likeTo == "" || isLike != "1" {
		return
	} else {
		toPINItem, err1 := getPINbyId(likeTo)
		if err1 == nil {
			toPIN = []pin.PinInscription{toPINItem}
		}
		return
	}
}
func getPaycommentPin(pinNode *pin.PinInscription) (toPIN []pin.PinInscription, err error) {
	var dataMap map[string]interface{}
	err = sonic.Unmarshal(pinNode.ContentBody, &dataMap)
	if err != nil {
		return
	}
	if _, ok := dataMap["commentTo"]; !ok {
		return
	} else {
		commentTo := getStringFromMap(dataMap, "commentTo")
		if commentTo == "" {
			return
		}
		toPINItem, err1 := getPINbyId(commentTo)
		if err1 == nil {
			toPIN = []pin.PinInscription{toPINItem}
		}
		return
	}
}
func getRepostPin(pinNode *pin.PinInscription) (toPIN []pin.PinInscription, err error) {
	var dataMap map[string]interface{}
	err = sonic.Unmarshal(pinNode.ContentBody, &dataMap)
	if err != nil {
		return
	}
	if _, ok := dataMap["quotePin"]; !ok {
		return
	} else {
		quotePin := getStringFromMap(dataMap, "quotePin")
		if quotePin == "" {
			return
		}
		toPINItem, err1 := getPINbyId(quotePin)
		if err1 == nil {
			toPIN = []pin.PinInscription{toPINItem}
		}
		return
	}
}
func getAtIdCoinPin(pinNode *pin.PinInscription) (toPIN []pin.PinInscription, err error) {
	content := string(pinNode.ContentBody)
	list := ExtractAtList(content)
	if len(list) <= 0 {
		return
	}
	for _, atId := range list {
		key := strings.ToLower(atId)
		if address, ok := common.IDCOINS[key]; ok {
			toPINItem := pin.PinInscription{
				Id:        pinNode.Id,
				Address:   address,
				Host:      pinNode.Host,
				ChainName: pinNode.ChainName,
				Path:      "Mention",
			}
			toPIN = append(toPIN, toPINItem)
		}
	}
	return
}
func ExtractAtList(content string) []string {
	re := regexp.MustCompile(`@(\S+?)\s`)
	matches := re.FindAllStringSubmatch(content, -1)
	var atList []string
	for _, m := range matches {
		if len(m) > 1 {
			atList = append(atList, m[1])
		}
	}
	return atList
}
