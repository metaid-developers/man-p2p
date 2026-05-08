//go:build !cgo
// +build !cgo

package opcat

import (
	"man-p2p/pin"

	"github.com/bitcoinsv/bsvd/wire"
)

func (indexer *Indexer) ZmqHashblock() {}

func (indexer *Indexer) ZmqRun(chanMsg chan pin.MempollChanMsg) {}

func (indexer *Indexer) TransferCheck(tx *wire.MsgTx) (transferPinList []*pin.PinInscription, err error) {
	return nil, nil
}
