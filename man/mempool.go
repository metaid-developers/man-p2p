package man

import (
	"context"
	"log"
	"man-p2p/common"
	"man-p2p/p2p"
	"man-p2p/pin"
	"strings"
	"time"
)

func handleMempoolPin(pinNode *pin.PinInscription) {
	if pinNode.Operation == "modify" || pinNode.Operation == "revoke" {
		pinNode.OriginalPath = GetModifyPath(pinNode.Path)
		pinNode.OriginalId = strings.Replace(pinNode.Path, "@", "", -1)
		handlePathAndOperation(&[]*pin.PinInscription{pinNode})
	}
	pinNode.Timestamp = time.Now().Unix()
	pinNode.Number = -1
	pinNode.GenesisHeight = -1 // mempool 标记
	pinNode.ContentTypeDetect = common.DetectContentType(&pinNode.ContentBody)
	//增加到pebble数据库
	PebbleStore.Database.SetMempool(pinNode)
	//增加PIN相关数据
	PebbleStore.Database.SetAllPins(-1, []*pin.PinInscription{pinNode}, 20000)
	_ = p2p.PublishPin(context.Background(), p2p.PinAnnouncement{
		PinId:         pinNode.Id,
		Path:          pinNode.Path,
		Address:       pinNode.Address,
		MetaId:        pinNode.MetaId,
		ChainName:     pinNode.ChainName,
		Timestamp:     pinNode.Timestamp,
		GenesisHeight: pinNode.GenesisHeight,
		Confirmed:     false,
		SizeBytes:     int64(pinNode.ContentLength),
	})
	//通知
	handNotifcation(pinNode)

	// 处理 mempool 中的 MRC20
	if strings.HasPrefix(pinNode.Path, "/ft/mrc20/") && isModuleEnabled("mrc20") {
		log.Printf("[Mempool] 🎯 MRC20 PIN detected, calling handleMempoolMrc20: path=%s, pinId=%s", pinNode.Path, pinNode.Id)
		handleMempoolMrc20(pinNode)
	}
}

// handleMempoolMrc20 处理 mempool 中的 MRC20 交易
func handleMempoolMrc20(pinNode *pin.PinInscription) {
	log.Printf("[Mempool] 📨 handleMempoolMrc20: path=%s, pinId=%s, txId=%s, chain=%s",
		pinNode.Path, pinNode.Id, pinNode.GenesisTransaction, pinNode.ChainName)

	mrc20List := []*pin.PinInscription{pinNode}
	mrc20TransferPinTx := make(map[string]struct{})

	if pinNode.Path == "/ft/mrc20/transfer" {
		mrc20TransferPinTx[pinNode.GenesisTransaction] = struct{}{}
	}

	// 获取该交易的输入列表用于 native transfer 检测
	// mempool 时无法获取完整的输入列表，设为空
	txInList := []string{}

	// 调用 MRC20 处理函数，isMempool=true
	Mrc20Handle(pinNode.ChainName, -1, mrc20List, mrc20TransferPinTx, txInList, true)
}
