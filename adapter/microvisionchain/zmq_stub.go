//go:build !cgo
// +build !cgo

package microvisionchain

import (
	"man-p2p/pin"

	"github.com/bitcoinsv/bsvd/wire"
)

func (indexer *Indexer) ZmqHashblock() {}

func (indexer *Indexer) ZmqRun1(chanMsg chan pin.MempollChanMsg) {}

func (indexer *Indexer) ZmqRun(chanMsg chan pin.MempollChanMsg) {}

func (indexer *Indexer) TransferCheck(tx *wire.MsgTx) (transferPinList []*pin.PinInscription, err error) {
	return nil, nil
}
