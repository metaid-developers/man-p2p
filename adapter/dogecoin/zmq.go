//go:build cgo
// +build cgo

package dogecoin

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"man-p2p/common"
	"man-p2p/pin"

	"github.com/btcsuite/btcd/wire"
	zmq "github.com/pebbe/zmq4"
)

func (indexer *Indexer) ZmqHashblock() {
	q, _ := zmq.NewSocket(zmq.SUB)
	defer q.Close()
	q.Connect("tcp://127.0.0.1:28337")
	q.SetSubscribe("hashblock")

	for {
		msg, err := q.RecvBytes(0)
		if err == nil {
			blockHeightBytes := msg[4:8]
			blockHeight := binary.LittleEndian.Uint32(blockHeightBytes)
			fmt.Println("Received Dogecoin block height:", blockHeight)
		}
	}
}

func (indexer *Indexer) ZmqRun(chanMsg chan pin.MempollChanMsg) {
	q, _ := zmq.NewSocket(zmq.SUB)
	defer q.Close()
	err := q.Connect(common.Config.Doge.ZmqHost)
	if err != nil {
		log.Println("Dogecoin ZmqRun:", err)
	}
	q.SetSubscribe("rawtx")
	q.SetTcpKeepalive(120)
	for {
		msg, _ := q.RecvMessage(0)
		var msgTx wire.MsgTx
		if err := msgTx.Deserialize(bytes.NewReader([]byte(msg[1]))); err != nil {
			continue
		}

		pinInscriptions := indexer.CatchPinsByTx(&msgTx, 0, 0, "", "", 0)
		if len(pinInscriptions) > 0 {
			chanMsg <- pin.MempollChanMsg{PinList: pinInscriptions, Tx: &msgTx}
		}

		// PIN transfer check
		tansferList, err := indexer.TransferCheck(&msgTx)
		if err == nil && len(tansferList) > 0 {
			chanMsg <- pin.MempollChanMsg{PinList: tansferList, Tx: &msgTx}
		}
		chanMsg <- pin.MempollChanMsg{PinList: []*pin.PinInscription{}, Tx: &msgTx}
	}
}

func (indexer *Indexer) TransferCheck(tx *wire.MsgTx) (transferPinList []*pin.PinInscription, err error) {
	outputList := make([]string, 0, len(tx.TxIn))
	for _, in := range tx.TxIn {
		output := fmt.Sprintf("%s:%d", in.PreviousOutPoint.Hash.String(), in.PreviousOutPoint.Index)
		outputList = append(outputList, output)
	}

	// TODO: Get pinList from database
	// pinList, err := (*indexer.DbAdapter).GetPinListByOutPutList(outputList)
	// if err != nil {
	// 	return transferPinList, err
	// }

	// Placeholder: Return empty list until database integration is complete
	_ = outputList // Use outputList to avoid unused variable warning
	return transferPinList, nil
}

func (indexer *Indexer) Mrc20NativeTransferCheck(txMsg interface{}) {
	tx := txMsg.(*wire.MsgTx)
	outputList := make([]string, 0, len(tx.TxIn))
	for _, in := range tx.TxIn {
		output := fmt.Sprintf("%s:%d", in.PreviousOutPoint.Hash.String(), in.PreviousOutPoint.Index)
		outputList = append(outputList, output)
	}

	// TODO: MRC20 transfer check for Dogecoin
	// mrc20transferCheck, err := (*indexer.DbAdapter).GetMrc20UtxoByOutPutList(outputList)
	// if err == nil && len(mrc20transferCheck) > 0 {
	// 	mrc20TrasferList := indexer.CatchMempoolNativeMrc20Transfer(tx, mrc20transferCheck)
	// 	if len(mrc20TrasferList) > 0 {
	// 		// Update mempool db
	// 	}
	// }

	// Placeholder to avoid unused variable warning
	_ = outputList
}
