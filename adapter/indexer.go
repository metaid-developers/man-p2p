package adapter

import (
	"man-p2p/mrc20"
	"man-p2p/pin"
)

type Indexer interface {
	InitIndexer()
	CatchPins(blockHeight int64) (pinInscriptions *[]*pin.PinInscription, txInList *[]string, creatorMap *map[string]string)
	CatchPinsByTx(msgTx interface{}, blockHeight int64, timestamp int64, blockHash string, merkleRoot string, txIndex int) (pinInscriptions []*pin.PinInscription)
	CatchMempoolPins(txList []interface{}) (pinInscriptions []*pin.PinInscription, txInList []string)
	CatchTransfer(idMap map[string]string) (trasferMap map[string]*pin.PinTransferInfo)
	// MempoolSpends mempool 转移检测:CatchTransfer 的 mempool 对应物,遍历给定
	// 交易而非 indexer.Block(mempool 交易不经块路径)。idMap 为待检 outpoint
	// ("txid:idx")→ 持有人地址;返回 被花费 outpoint → 转移要素(花费 txid 为
	// 账本口径)。类型不符/无法解析的交易跳过。
	MempoolSpends(txList []interface{}, idMap map[string]string) (trasferMap map[string]*pin.PinTransferInfo)
	GetAddress(pkScript []byte) (address string)
	ZmqRun(chanMsg chan pin.MempollChanMsg)
	GetBlockTxHash(blockHeight int64) (txhashList []string, pinIdList []string)
	ZmqHashblock()
	CatchNativeMrc20Transfer(blockHeight int64, utxoList []*mrc20.Mrc20Utxo, mrc20TransferPinTx map[string]struct{}) (savelist []*mrc20.Mrc20Utxo)
	CatchMempoolNativeMrc20Transfer(txList []interface{}, utxoList []*mrc20.Mrc20Utxo, mrc20TransferPinTx map[string]struct{}) (savelist []*mrc20.Mrc20Utxo)
}
