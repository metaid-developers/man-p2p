package opcat

import (
	"log"
	"man-p2p/common"
	"man-p2p/pin"
	"time"

	"github.com/bitcoinsv/bsvd/chaincfg/chainhash"
	"github.com/bitcoinsv/bsvd/rpcclient"
	"github.com/bitcoinsv/bsvd/wire"
)

var (
	client            *rpcclient.Client
	getRawMempool     = func() ([]*chainhash.Hash, error) { return client.GetRawMempool() }
	getRawTransaction = func(txHash *chainhash.Hash) (*wire.MsgTx, error) {
		tx, err := client.GetRawTransaction(txHash)
		if err != nil {
			return nil, err
		}
		return tx.MsgTx(), nil
	}
)

type OpcatChain struct{}

func (chain *OpcatChain) InitChain() {
	opcat := common.Config.Opcat
	rpcConfig := &rpcclient.ConnConfig{
		Host:                 opcat.RpcHost,
		User:                 opcat.RpcUser,
		Pass:                 opcat.RpcPass,
		HTTPPostMode:         opcat.RpcHTTPPostMode,
		DisableTLS:           opcat.RpcDisableTLS,
		DisableAutoReconnect: true,
		DisableConnectOnNew:  true,
	}
	var err error
	client, err = rpcclient.New(rpcConfig, nil)
	if err != nil {
		panic(err)
	}
	log.Println("opcat rpc connect")
}

func (chain *OpcatChain) GetBlock(blockHeight int64) (block interface{}, err error) {
	blockhash, err := client.GetBlockHash(blockHeight)
	if err != nil {
		return
	}
	block, err = client.GetBlock(blockhash)
	return
}

func (chain *OpcatChain) GetBlockTime(blockHeight int64) (timestamp int64, err error) {
	block, err := chain.GetBlock(blockHeight)
	if err != nil {
		return
	}
	b := block.(*wire.MsgBlock)
	timestamp = b.Header.Timestamp.Unix()
	return
}

func (chain *OpcatChain) GetTransaction(txId string) (tx interface{}, err error) {
	txHash, _ := chainhash.NewHashFromStr(txId)
	return client.GetRawTransaction(txHash)
}

func GetValueByTx(txId string, txIdx int) (value int64, err error) {
	txHash, _ := chainhash.NewHashFromStr(txId)
	tx, err := client.GetRawTransaction(txHash)
	if err != nil {
		return
	}
	value = tx.MsgTx().TxOut[txIdx].Value
	return
}

func (chain *OpcatChain) GetInitialHeight() (height int64) {
	return common.Config.Opcat.InitialHeight
}

func (chain *OpcatChain) GetBestHeight() (height int64) {
	blockhash, err := client.GetBestBlockHash()
	if err != nil {
		log.Println("GetBestHeight err:", err)
		return
	}
	block, err := client.GetBlockVerbose(blockhash)
	if err != nil {
		return
	}
	height = block.Height
	return
}

func (chain *OpcatChain) GetBlockMsg(height int64) (blockMsg *pin.BlockMsg) {
	blockhash, err := client.GetBlockHash(height)
	if err != nil {
		return
	}
	block, err := client.GetBlockVerbose(blockhash)
	if err != nil {
		return
	}
	blockMsg = &pin.BlockMsg{}
	blockMsg.BlockHash = block.Hash
	blockMsg.Target = block.MerkleRoot
	blockMsg.Timestamp = time.Unix(block.Time, 0).Format("2006-01-02 15:04:05")
	blockMsg.Size = int64(block.Size)
	blockMsg.Transaction = block.Tx
	blockMsg.TransactionNum = len(block.Tx)
	return
}

func (chain *OpcatChain) GetMempoolTransactionList() (list []interface{}, err error) {
	txIdList, err := getRawMempool()
	if err != nil {
		return
	}
	for _, txHash := range txIdList {
		tx, err := getRawTransaction(txHash)
		if err != nil {
			continue
		}
		list = append(list, tx)
	}
	return
}

func (chain *OpcatChain) GetTxSizeAndFees(txHash string) (fee int64, size int64, blockHash string, err error) {
	hash, err := chainhash.NewHashFromStr(txHash)
	if err != nil {
		return
	}
	tx, err := client.GetRawTransactionVerbose(hash)
	if err != nil {
		return
	}
	var inputAmount int64
	for _, vin := range tx.Vin {
		inputTxHash, err := chainhash.NewHashFromStr(vin.Txid)
		if err != nil {
			continue
		}
		inputTx, err := client.GetRawTransactionVerbose(inputTxHash)
		if err != nil {
			continue
		}
		inputAmount += int64(inputTx.Vout[vin.Vout].Value * 1e8)
	}
	var outputAmount int64
	for _, vout := range tx.Vout {
		outputAmount += int64(vout.Value * 1e8)
	}
	fee = inputAmount - outputAmount
	size = int64(tx.Size)
	blockHash = tx.BlockHash
	return
}
