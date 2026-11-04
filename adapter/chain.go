package adapter

import "man-p2p/pin"

type Chain interface {
	InitChain()
	GetBlock(blockHeight int64) (block interface{}, err error)
	GetBlockTime(blockHeight int64) (timestamp int64, err error)
	GetTransaction(txId string) (tx interface{}, err error)
	GetInitialHeight() (height int64)
	GetBestHeight() (height int64)
	GetBlockMsg(height int64) (blockMsg *pin.BlockMsg)
	GetMempoolTransactionList() (list []interface{}, err error)
	GetTxSizeAndFees(txHash string) (fee int64, size int64, blockHash string, err error)
	// GetTransactionStatus 交易状态查询:confirmed=false 即 mempool 未确认
	// (confirmations=0);交易不存在或 RPC 异常返回 error。
	GetTransactionStatus(txId string) (confirmed bool, confirmations int64, blockHash string, height int64, err error)
}
