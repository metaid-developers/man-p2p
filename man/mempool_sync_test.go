package man

import (
	"errors"
	"testing"

	"man-p2p/mrc20"
	"man-p2p/pin"
)

type syncTestChain struct {
	txList []interface{}
	err    error
	calls  int
}

func (c *syncTestChain) InitChain()                                      {}
func (c *syncTestChain) GetBlock(blockHeight int64) (interface{}, error) { return nil, nil }
func (c *syncTestChain) GetBlockTime(blockHeight int64) (int64, error)   { return 0, nil }
func (c *syncTestChain) GetTransaction(txId string) (interface{}, error) { return nil, nil }
func (c *syncTestChain) GetInitialHeight() int64                         { return 0 }
func (c *syncTestChain) GetBestHeight() int64                            { return 0 }
func (c *syncTestChain) GetBlockMsg(height int64) *pin.BlockMsg          { return nil }
func (c *syncTestChain) GetTxSizeAndFees(txHash string) (int64, int64, string, error) {
	return 0, 0, "", nil
}
func (c *syncTestChain) GetMempoolTransactionList() ([]interface{}, error) {
	c.calls++
	return c.txList, c.err
}

type syncTestIndexer struct {
	pins  []*pin.PinInscription
	txIns []string
	calls int
}

func (i *syncTestIndexer) InitIndexer() {}
func (i *syncTestIndexer) CatchPins(blockHeight int64) (*[]*pin.PinInscription, *[]string, *map[string]string) {
	return nil, nil, nil
}
func (i *syncTestIndexer) CatchPinsByTx(msgTx interface{}, blockHeight int64, timestamp int64, blockHash string, merkleRoot string, txIndex int) []*pin.PinInscription {
	return nil
}
func (i *syncTestIndexer) CatchMempoolPins(txList []interface{}) ([]*pin.PinInscription, []string) {
	i.calls++
	return i.pins, i.txIns
}
func (i *syncTestIndexer) CatchTransfer(idMap map[string]string) map[string]*pin.PinTransferInfo {
	return nil
}
func (i *syncTestIndexer) GetAddress(pkScript []byte) string      { return "" }
func (i *syncTestIndexer) ZmqRun(chanMsg chan pin.MempollChanMsg) {}
func (i *syncTestIndexer) GetBlockTxHash(blockHeight int64) ([]string, []string) {
	return nil, nil
}
func (i *syncTestIndexer) ZmqHashblock() {}
func (i *syncTestIndexer) CatchNativeMrc20Transfer(blockHeight int64, utxoList []*mrc20.Mrc20Utxo, mrc20TransferPinTx map[string]struct{}) []*mrc20.Mrc20Utxo {
	return nil
}
func (i *syncTestIndexer) CatchMempoolNativeMrc20Transfer(txList []interface{}, utxoList []*mrc20.Mrc20Utxo, mrc20TransferPinTx map[string]struct{}) []*mrc20.Mrc20Utxo {
	return nil
}

func TestSyncExistingMempoolProcessesPins(t *testing.T) {
	t.Parallel()

	chain := &syncTestChain{txList: []interface{}{"tx-1", "tx-2"}}
	indexer := &syncTestIndexer{
		pins: []*pin.PinInscription{
			{Id: "pin-1"},
			{Id: "pin-2"},
		},
	}

	var handled []string
	err := syncExistingMempool("mvc", chain, indexer, func(pinNode *pin.PinInscription) {
		handled = append(handled, pinNode.Id)
	})
	if err != nil {
		t.Fatalf("syncExistingMempool returned error: %v", err)
	}

	if chain.calls != 1 {
		t.Fatalf("expected chain mempool fetch once, got %d", chain.calls)
	}
	if indexer.calls != 1 {
		t.Fatalf("expected mempool pin parsing once, got %d", indexer.calls)
	}
	if len(handled) != 2 || handled[0] != "pin-1" || handled[1] != "pin-2" {
		t.Fatalf("unexpected handled pins: %#v", handled)
	}
}

func TestSyncExistingMempoolReturnsFetchError(t *testing.T) {
	t.Parallel()

	chain := &syncTestChain{err: errors.New("boom")}
	indexer := &syncTestIndexer{}

	err := syncExistingMempool("mvc", chain, indexer, func(pinNode *pin.PinInscription) {})
	if err == nil {
		t.Fatal("expected fetch error, got nil")
	}
	if indexer.calls != 0 {
		t.Fatalf("expected indexer not to be called on fetch error, got %d", indexer.calls)
	}
}
