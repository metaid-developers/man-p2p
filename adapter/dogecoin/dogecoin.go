package dogecoin

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"man-p2p/common"
	"man-p2p/pin"
	"time"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

var (
	client *rpcclient.Client
)

type DogecoinChain struct {
	IsTest bool
}

func (chain *DogecoinChain) InitChain() {
	doge := common.Config.Doge
	rpcConfig := &rpcclient.ConnConfig{
		Host:                 doge.RpcHost,
		User:                 doge.RpcUser,
		Pass:                 doge.RpcPass,
		HTTPPostMode:         doge.RpcHTTPPostMode, // Dogecoin core only supports HTTP POST mode
		DisableTLS:           doge.RpcDisableTLS,   // Dogecoin core does not provide TLS by default
		DisableAutoReconnect: false,
		DisableConnectOnNew:  false,
	}
	var err error
	client, err = rpcclient.New(rpcConfig, nil)
	if err != nil {
		panic(err)
	}
	log.Printf("Dogecoin RPC client initialized: %s", doge.RpcHost)
}

func (chain *DogecoinChain) GetBlock(blockHeight int64) (block interface{}, err error) {
	blockhash, err := client.GetBlockHash(blockHeight)
	if err != nil {
		return
	}

	// Dogecoin uses AuxPoW (merged mining) which btcsuite cannot parse correctly
	// Always use our custom parser that handles Dogecoin's block format
	msgBlock, err := chain.getBlockByRPC(blockhash)
	if err != nil {
		return
	}

	block = msgBlock
	return
}

func (chain *DogecoinChain) GetBlockTime(blockHeight int64) (timestamp int64, err error) {
	block, err := chain.GetBlock(blockHeight)
	if err != nil {
		return
	}
	b := block.(*wire.MsgBlock)
	timestamp = b.Header.Timestamp.Unix()
	return
}

func (chain *DogecoinChain) GetBlockByHash(hash string) (block *btcjson.GetBlockVerboseResult, err error) {
	blockhash, err := chainhash.NewHashFromStr(hash)
	if err != nil {
		return
	}
	block, err = client.GetBlockVerbose(blockhash)
	return
}

func (chain *DogecoinChain) GetTransaction(txId string) (tx interface{}, err error) {
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

func (chain *DogecoinChain) GetInitialHeight() (height int64) {
	return common.Config.Doge.InitialHeight
}

func (chain *DogecoinChain) GetBestHeight() (height int64) {
	info, err := client.GetBlockChainInfo()
	if err != nil {
		//log.Printf("GetBlockChainInfo error: %v, trying GetBlockCount", err)
		height, err = client.GetBlockCount()
		if err == nil {
			//log.Printf("Dogecoin best height: %d", height)
			return height
		}
		//log.Printf("GetBlockCount error: %v", err)
		return 0
	}
	height = int64(info.Blocks)
	//log.Printf("Dogecoin best height: %d", height)
	return
}

func (chain *DogecoinChain) GetBlockMsg(height int64) (blockMsg *pin.BlockMsg) {
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
	blockMsg.Weight = int64(block.Weight)
	blockMsg.Timestamp = time.Unix(block.Time, 0).Format("2006-01-02 15:04:05")
	blockMsg.Size = int64(block.Size)
	blockMsg.Transaction = block.Tx
	blockMsg.TransactionNum = len(block.Tx)
	return
}

func (chain *DogecoinChain) GetCreatorAddress(txHashStr string, idx uint32, netParams *chaincfg.Params) (address string) {
	txHash, err := chainhash.NewHashFromStr(txHashStr)
	if err != nil {
		return "errorAddr"
	}
	// Get commit tx
	tx, err := client.GetRawTransaction(txHash)
	if err != nil {
		return "errorAddr"
	}
	// Get commit tx first input
	inputHash := tx.MsgTx().TxIn[0].PreviousOutPoint.Hash
	inputIdx := tx.MsgTx().TxIn[0].PreviousOutPoint.Index
	inputTx, err := client.GetRawTransaction(&inputHash)
	if err != nil {
		return "errorAddr"
	}
	_, addresses, _, _ := txscript.ExtractPkScriptAddrs(inputTx.MsgTx().TxOut[inputIdx].PkScript, netParams)
	if len(addresses) > 0 {
		address = addresses[0].String()
	} else {
		address = "errorAddr"
	}
	return
}

func (chain *DogecoinChain) GetMempoolTransactionList() (list []interface{}, err error) {
	txIdList, err := client.GetRawMempool()
	if err != nil {
		return
	}
	for _, txHash := range txIdList {
		tx, err := client.GetRawTransaction(txHash)
		if err != nil {
			continue
		}
		list = append(list, tx.MsgTx())
	}
	return
}

func (chain *DogecoinChain) GetTxSizeAndFees(txHash string) (fee int64, size int64, blockHash string, err error) {
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

// Helper function to parse chainhash, panic on error since this should not fail for valid block data
func mustParseChainhash(hashStr string) chainhash.Hash {
	hash, err := chainhash.NewHashFromStr(hashStr)
	if err != nil {
		panic(fmt.Sprintf("Invalid hash: %s, error: %v", hashStr, err))
	}
	return *hash
}

// Parse hex string to uint32 for the Bits field
func parseHexUint32(hexStr string) (uint32, error) {
	var val uint32
	_, err := fmt.Sscanf(hexStr, "%x", &val)
	if err != nil {
		return 0, fmt.Errorf("failed to parse hex uint32: %v", err)
	}
	return val, nil
}

// getBlockByRPC fetches block data using verbose mode and parses transactions from raw block
// This handles Dogecoin's AuxPoW block format which btcsuite cannot parse directly
func (chain *DogecoinChain) getBlockByRPC(blockhash *chainhash.Hash) (*wire.MsgBlock, error) {
	// Get block with verbosity=1 to get tx ids and header info
	blockVerbose, err := client.GetBlockVerbose(blockhash)
	if err != nil {
		return nil, fmt.Errorf("GetBlockVerbose failed: %v", err)
	}

	// Parse bits
	bits, err := parseHexUint32(blockVerbose.Bits)
	if err != nil {
		return nil, fmt.Errorf("failed to parse bits: %v", err)
	}

	// Build MsgBlock header
	msgBlock := &wire.MsgBlock{
		Header: wire.BlockHeader{
			Version:    blockVerbose.Version,
			PrevBlock:  mustParseChainhash(blockVerbose.PreviousHash),
			MerkleRoot: mustParseChainhash(blockVerbose.MerkleRoot),
			Timestamp:  time.Unix(blockVerbose.Time, 0),
			Bits:       bits,
			Nonce:      blockVerbose.Nonce,
		},
		Transactions: make([]*wire.MsgTx, 0, len(blockVerbose.Tx)),
	}

	// Get raw block hex (verbosity=0) and manually extract transactions
	blockHexResult, err := client.RawRequest("getblock", []json.RawMessage{
		json.RawMessage(fmt.Sprintf(`"%s"`, blockhash.String())),
		json.RawMessage("0"), // verbosity 0 = raw hex
	})
	if err != nil {
		return nil, fmt.Errorf("getblock raw failed: %v", err)
	}

	var blockHex string
	if err := json.Unmarshal(blockHexResult, &blockHex); err != nil {
		return nil, fmt.Errorf("failed to unmarshal block hex: %v", err)
	}

	blockBytes, err := hex.DecodeString(blockHex)
	if err != nil {
		return nil, fmt.Errorf("failed to decode block hex: %v", err)
	}

	// Parse transactions from raw block data
	// Dogecoin block format: header (80+ bytes with AuxPoW) + varint(txcount) + transactions
	txs, err := parseDogeBlockTransactions(blockBytes, len(blockVerbose.Tx))
	if err != nil {
		return nil, fmt.Errorf("failed to parse transactions: %v", err)
	}

	msgBlock.Transactions = txs
	return msgBlock, nil
}

// parseDogeBlockTransactions extracts transactions from raw Dogecoin block data
// Dogecoin uses AuxPoW which adds extra data after the standard 80-byte header
func parseDogeBlockTransactions(blockBytes []byte, expectedTxCount int) ([]*wire.MsgTx, error) {
	// Standard block header is 80 bytes
	// For AuxPoW blocks (version & 0x100 != 0), there's additional AuxPoW data
	// We need to find where transactions start

	if len(blockBytes) < 80 {
		return nil, fmt.Errorf("block too short: %d bytes", len(blockBytes))
	}

	// Read version from first 4 bytes
	version := int32(blockBytes[0]) | int32(blockBytes[1])<<8 | int32(blockBytes[2])<<16 | int32(blockBytes[3])<<24

	offset := 80 // Start after standard header

	// Check if this is an AuxPoW block (version has bit 8 set, which is 0x100)
	if version&0x100 != 0 {
		// Skip AuxPoW data
		// AuxPoW structure:
		// - coinbase tx (variable, may be SegWit from parent chain like Bitcoin/Litecoin)
		// - block hash (32 bytes)
		// - merkle branch (varint count + 32*count bytes)
		// - merkle index (4 bytes)
		// - aux merkle branch (varint count + 32*count bytes)
		// - aux merkle index (4 bytes)
		// - parent block header (80 bytes)

		// Parse coinbase transaction using WitnessEncoding because the parent chain
		// (typically Bitcoin or Litecoin) may use SegWit. Using BaseEncoding on a
		// SegWit tx interprets the 0x00 marker as "0 inputs", consuming far fewer
		// bytes and corrupting all subsequent offset calculations.
		coinbaseTx := &wire.MsgTx{}
		r := bytes.NewReader(blockBytes[offset:])
		if err := coinbaseTx.BtcDecode(r, 0, wire.WitnessEncoding); err != nil {
			// Fall back to BaseEncoding for non-SegWit parent chains
			r = bytes.NewReader(blockBytes[offset:])
			if err2 := coinbaseTx.BtcDecode(r, 0, wire.BaseEncoding); err2 != nil {
				return nil, fmt.Errorf("failed to parse AuxPoW coinbase: witness=%v base=%v", err, err2)
			}
		}
		offset += len(blockBytes[offset:]) - r.Len()

		// Skip block hash (32 bytes)
		if offset+32 > len(blockBytes) {
			return nil, fmt.Errorf("AuxPoW block too short for block hash at offset %d (len=%d)", offset, len(blockBytes))
		}
		offset += 32

		// Skip merkle branch
		if offset >= len(blockBytes) {
			return nil, fmt.Errorf("AuxPoW block too short for merkle branch at offset %d (len=%d)", offset, len(blockBytes))
		}
		branchCount, n := readVarInt(blockBytes[offset:])
		if branchCount > 256 {
			return nil, fmt.Errorf("AuxPoW merkle branch count implausibly large: %d (offset=%d)", branchCount, offset)
		}
		if offset+n+int(branchCount)*32 > len(blockBytes) {
			return nil, fmt.Errorf("AuxPoW block too short for merkle branches at offset %d (count=%d, len=%d)", offset, branchCount, len(blockBytes))
		}
		offset += n + int(branchCount)*32

		// Skip merkle index (4 bytes)
		if offset+4 > len(blockBytes) {
			return nil, fmt.Errorf("AuxPoW block too short for merkle index at offset %d (len=%d)", offset, len(blockBytes))
		}
		offset += 4

		// Skip aux merkle branch
		if offset >= len(blockBytes) {
			return nil, fmt.Errorf("AuxPoW block too short for aux merkle branch at offset %d (len=%d)", offset, len(blockBytes))
		}
		auxBranchCount, n := readVarInt(blockBytes[offset:])
		if auxBranchCount > 256 {
			return nil, fmt.Errorf("AuxPoW aux merkle branch count implausibly large: %d (offset=%d)", auxBranchCount, offset)
		}
		if offset+n+int(auxBranchCount)*32 > len(blockBytes) {
			return nil, fmt.Errorf("AuxPoW block too short for aux merkle branches at offset %d (count=%d, len=%d)", offset, auxBranchCount, len(blockBytes))
		}
		offset += n + int(auxBranchCount)*32

		// Skip aux merkle index (4 bytes)
		if offset+4 > len(blockBytes) {
			return nil, fmt.Errorf("AuxPoW block too short for aux merkle index at offset %d (len=%d)", offset, len(blockBytes))
		}
		offset += 4

		// Skip parent block header (80 bytes)
		if offset+80 > len(blockBytes) {
			return nil, fmt.Errorf("AuxPoW block too short for parent header at offset %d (len=%d)", offset, len(blockBytes))
		}
		offset += 80
	}

	// Bounds check before reading transaction count
	if offset >= len(blockBytes) {
		return nil, fmt.Errorf("block too short for tx count at offset %d (len=%d)", offset, len(blockBytes))
	}

	// Now we're at the transaction count varint
	txCount, n := readVarInt(blockBytes[offset:])
	offset += n

	if int(txCount) != expectedTxCount {
		log.Printf("Warning: parsed tx count %d differs from expected %d", txCount, expectedTxCount)
	}

	// Parse transactions
	transactions := make([]*wire.MsgTx, 0, txCount)
	r := bytes.NewReader(blockBytes[offset:])
	for i := uint64(0); i < txCount; i++ {
		tx := &wire.MsgTx{}
		if err := tx.BtcDecode(r, 0, wire.BaseEncoding); err != nil {
			return nil, fmt.Errorf("failed to parse tx %d: %v", i, err)
		}
		transactions = append(transactions, tx)
	}

	return transactions, nil
}

// readVarInt reads a variable-length integer from a byte slice
// Returns the value and the number of bytes consumed
func readVarInt(data []byte) (uint64, int) {
	if len(data) == 0 {
		return 0, 0
	}

	first := data[0]
	switch {
	case first < 0xfd:
		return uint64(first), 1
	case first == 0xfd:
		if len(data) < 3 {
			return 0, 0
		}
		return uint64(data[1]) | uint64(data[2])<<8, 3
	case first == 0xfe:
		if len(data) < 5 {
			return 0, 0
		}
		return uint64(data[1]) | uint64(data[2])<<8 | uint64(data[3])<<16 | uint64(data[4])<<24, 5
	default: // 0xff
		if len(data) < 9 {
			return 0, 0
		}
		return uint64(data[1]) | uint64(data[2])<<8 | uint64(data[3])<<16 | uint64(data[4])<<24 |
			uint64(data[5])<<32 | uint64(data[6])<<40 | uint64(data[7])<<48 | uint64(data[8])<<56, 9
	}
}

// Parse transaction from verbose JSON format to wire.MsgTx
func parseTxFromVerbose(txVerbose *btcjson.TxRawResult) (*wire.MsgTx, error) {
	// Use the Hex field which contains the raw transaction bytes
	txBytes, err := hex.DecodeString(txVerbose.Hex)
	if err != nil {
		return nil, fmt.Errorf("failed to decode tx hex: %v", err)
	}

	// Deserialize the transaction without witness flag (Dogecoin doesn't support SegWit)
	tx := &wire.MsgTx{}
	err = tx.Deserialize(bytes.NewReader(txBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize tx: %v", err)
	}

	return tx, nil
}
