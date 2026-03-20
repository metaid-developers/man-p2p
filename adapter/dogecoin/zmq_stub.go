//go:build !cgo
// +build !cgo

package dogecoin

import (
	"man-p2p/pin"

	"github.com/btcsuite/btcd/wire"
)

func (indexer *Indexer) ZmqHashblock() {}

func (indexer *Indexer) ZmqRun(chanMsg chan pin.MempollChanMsg) {}

func (indexer *Indexer) TransferCheck(tx *wire.MsgTx) (transferPinList []*pin.PinInscription, err error) {
	return nil, nil
}

func (indexer *Indexer) Mrc20NativeTransferCheck(txMsg interface{}) {}
