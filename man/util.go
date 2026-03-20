package man

import (
	"man-p2p/common"
	"strings"

	"github.com/bitcoinsv/bsvutil"
	"github.com/btcsuite/btcd/btcutil"
)

func GetModifyPath(pinId string) (modifyPath string) {
	for i := 1; i < 500; i++ {
		var op string
		op, modifyPath = doGetModifyPath(pinId)
		if op == "" && modifyPath == "" {
			break //没有找到修改路径
		}
		if op == "create" && modifyPath != "" {
			break
		} else if op == "modify" {
			pinId = modifyPath
		}
	}
	return
}
func doGetModifyPath(pinId string) (op string, path string) {
	pinId = strings.ReplaceAll(pinId, "@", "")
	if len(pinId) < 2 {
		return
	}
	txhash := pinId[0 : len(pinId)-2]
	chains := strings.Split(common.Chain, ",")
	for _, chain := range chains {
		tx, err := ChainAdapter[chain].GetTransaction(txhash)
		if err != nil || tx == nil {
			//fmt.Println("GetTransaction error:", err, txhash)
			continue
		}
		var txMsg interface{}
		if chain == "mvc" {
			txMsg = tx.(*bsvutil.Tx).MsgTx()
		} else {
			txMsg = tx.(*btcutil.Tx).MsgTx()
		}

		if txMsg == nil {
			//fmt.Println("GetTransaction error:", err, txhash)
			continue
		}

		pins := IndexerAdapter[chain].CatchPinsByTx(txMsg, 0, 0, "", "", 0)
		if len(pins) <= 0 {
			//fmt.Println("CatchPinsByTx error:", txhash, "pins num:", len(pins))
			continue
		}
		//fmt.Println(">>", pins[0].Operation, pins[0].Path)
		return pins[0].Operation, pins[0].Path
	}
	return "", ""
}
