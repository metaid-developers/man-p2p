# OpcatLayer Chain Support — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add OpcatLayer (`opcat`) as the fourth supported chain in man-p2p (PIN indexing only, no MRC20).

**Architecture:** New `adapter/opcat/` package mirroring MVC's pattern — 5 files implementing `adapter.Chain` and `adapter.Indexer` interfaces. Six registration-point modifications in existing files wire the new chain into config, runtime, indexing loop, and POP scoring.

**Tech Stack:** Go 1.x, `github.com/bitcoinsv/bsvd` (wire/chaincfg/txscript/rpcclient), `github.com/btcsuite/btcd/btcutil` (address encoding), `github.com/pebbe/zmq4` (ZMQ).

**Spec:** `docs/superpowers/specs/2026-05-08-opcat-chain-support-design.md`

---

## File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Create | `adapter/opcat/opcat.go` | `Chain` interface — RPC connection, block/tx/mempool access |
| Create | `adapter/opcat/util.go` | `GetNewHash`, address encoding, `DecodeRawTransaction` |
| Create | `adapter/opcat/indexer.go` | `Indexer` interface — PIN parsing, transfer detection |
| Create | `adapter/opcat/zmq.go` | ZMQ subscriber (cgo build) |
| Create | `adapter/opcat/zmq_stub.go` | ZMQ stub (non-cgo build) |
| Modify | `common/config.go` | `opcatConfig` struct, `AllConfig` field, CLI flags |
| Modify | `man/man.go` | Import, `InitRuntime` case, `getSyncHeight` fallback |
| Modify | `man/indexer_pebble.go` | `handleMrc20` case |
| Modify | `pin/pop.go` | `PopLevelCount` + `RarityScoreBinary` cases |
| Modify | `config.toml` | `[opcat]` section |

---

### Task 1: Create `adapter/opcat/opcat.go` (Chain interface)

**Files:**
- Create: `adapter/opcat/opcat.go`

- [ ] **Step 1: Write opcat.go**

Mirrors `adapter/microvisionchain/mvc.go` with these differences:
- Config reads from `common.Config.Opcat` instead of `common.Config.Mvc`
- `GetBlockMsg` includes `.Weight` field (MVC omits it; BTC/Doge include it; include for opcat)
- `GetInitialHeight` returns `common.Config.Opcat.InitialHeight`
- Network magic: `wire.BitcoinNet(0xe3e1f3e8)` — but since we use `bsvd/rpcclient` (which auto-handles magic), no manual magic setting needed in the chain adapter itself

```go
package opcat

import (
	"log"
	"man-p2p/common"
	"man-p2p/pin"
	"time"

	"github.com/bitcoinsv/bsvd/btcjson"
	"github.com/bitcoinsv/bsvd/chaincfg"
	"github.com/bitcoinsv/bsvd/chaincfg/chainhash"
	"github.com/bitcoinsv/bsvd/rpcclient"
	"github.com/bitcoinsv/bsvd/txscript"
	"github.com/bitcoinsv/bsvd/wire"
)

var (
	client         *rpcclient.Client
	getRawMempool  = func() ([]*chainhash.Hash, error) { return client.GetRawMempool() }
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
```

- [ ] **Step 2: Verify compilation**

```bash
cd .worktrees/feat-opcat-chain && go build ./adapter/opcat/ 2>&1
```

Expected: compilation errors about missing types from indexer.go/util.go (expected — we'll add those files next). The key check is that opcat.go itself compiles without syntax errors.

---

### Task 2: Create `adapter/opcat/util.go`

**Files:**
- Create: `adapter/opcat/util.go`

- [ ] **Step 1: Write util.go**

Identical to `adapter/microvisionchain/util.go` (same `GetNewHash`, `DecodeRawTransaction`, `GetBase58AddressFromPkScript`, `DecodeVarIntForTx`, SHA256/Endian helpers). These are pure BSV-wire functions with no chain-specific config.

Copy the entire contents of `adapter/microvisionchain/util.go` into `adapter/opcat/util.go`, changing only the package declaration from `package microvisionchain` to `package opcat`.

---

### Task 3: Create `adapter/opcat/indexer.go`

**Files:**
- Create: `adapter/opcat/indexer.go`

- [ ] **Step 1: Write indexer.go**

Mirrors `adapter/microvisionchain/indexer.go` with these differences:
- Config reads from `common.Config.Opcat` instead of `common.Config.Mvc`
- `ChainName` set to `"opcat"` instead of `"mvc"`
- `CatchNativeMrc20Transfer` and `CatchMempoolNativeMrc20Transfer` return `nil`
- MRC20 UTXO `Chain` field set to `"opcat"` (in the CatchNativeMrc20Transfer no-op this is irrelevant, but kept for consistency)
- Network params use `bsvd/chaincfg.MainNetParams` (same as MVC mainnet)
- `GetAddress` uses `btcdChaincfg.MainNetParams` for btcutil address encoding (same as MVC)
- `parseOnePin`, `ParsePin`, `ParsePins` logic identical to MVC

```go
package opcat

import (
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"man-p2p/common"
	"man-p2p/mrc20"
	"man-p2p/pin"
	"strconv"
	"strings"
	"time"

	btcdChaincfg "github.com/btcsuite/btcd/chaincfg"
	btcdtxscript "github.com/btcsuite/btcd/txscript"

	"github.com/bitcoinsv/bsvd/chaincfg"
	"github.com/bitcoinsv/bsvd/txscript"
	"github.com/bitcoinsv/bsvd/wire"
)

var netParams *chaincfg.Params
var btcNetParams *btcdChaincfg.Params

type Indexer struct {
	ChainParams string
	Block       interface{}
	PopCutNum   int
	ChainName   string
}

func (indexer *Indexer) InitIndexer() {
	netParams = &chaincfg.MainNetParams
	btcNetParams = &btcdChaincfg.MainNetParams
	PopCutNum := common.Config.Opcat.PopCutNum
	_ = PopCutNum // reserved for future use
}

func (indexer *Indexer) GetAddress(pkScript []byte) (address string) {
	_, addresses, _, _ := txscript.ExtractPkScriptAddrs(pkScript, netParams)
	if len(addresses) > 0 {
		address = GetBase58AddressFromPkScript(addresses[0].ScriptAddress(), btcNetParams)
	}
	return
}

func (indexer *Indexer) CatchPins(blockHeight int64) (pinInscriptions *[]*pin.PinInscription, txInList *[]string, creatorMap *map[string]string) {
	m := make(map[string]string)
	creatorMap = &m
	var txInListLocal []string
	var pinInscriptionsLocal []*pin.PinInscription
	txInList = &txInListLocal
	pinInscriptions = &pinInscriptionsLocal

	chain := OpcatChain{}
	blockMsg, err := chain.GetBlock(blockHeight)
	if err != nil {
		log.Println("GetBlock Error:", err)
		return
	}
	indexer.Block = blockMsg
	block := blockMsg.(*wire.MsgBlock)

	timestamp := block.Header.Timestamp.Unix()
	blockHash := block.BlockHash().String()
	merkleRoot := block.Header.MerkleRoot.String()

	for i, tx := range block.Transactions {
		for _, in := range tx.TxIn {
			id := common.ConcatBytesOptimized([]string{in.PreviousOutPoint.Hash.String(), ":", strconv.FormatUint(uint64(in.PreviousOutPoint.Index), 10)}, "")
			*txInList = append(*txInList, id)
		}
		txPins := indexer.CatchPinsByTx(tx, blockHeight, timestamp, blockHash, merkleRoot, i)
		for _, p := range txPins {
			if pin.ManValidator(p) == nil {
				*pinInscriptions = append(*pinInscriptions, p)
			}
		}
	}
	return
}

func (indexer *Indexer) CatchMempoolPins(txList []interface{}) (pinInscriptions []*pin.PinInscription, txInList []string) {
	timestamp := time.Now().Unix()
	blockHash := ""
	merkleRoot := ""
	for i, item := range txList {
		tx := item.(*wire.MsgTx)
		for _, in := range tx.TxIn {
			id := fmt.Sprintf("%s:%d", in.PreviousOutPoint.Hash.String(), in.PreviousOutPoint.Index)
			txInList = append(txInList, id)
		}
		txPins := indexer.CatchPinsByTx(tx, -1, timestamp, blockHash, merkleRoot, i)
		if len(txPins) > 0 {
			pinInscriptions = append(pinInscriptions, txPins...)
		}
	}
	return
}

func (indexer *Indexer) CatchTransfer(idMap map[string]string) (trasferMap map[string]*pin.PinTransferInfo) {
	trasferMap = make(map[string]*pin.PinTransferInfo)
	block := indexer.Block.(*wire.MsgBlock)
	for _, tx := range block.Transactions {
		for _, in := range tx.TxIn {
			id := fmt.Sprintf("%s:%d", in.PreviousOutPoint.Hash.String(), in.PreviousOutPoint.Index)
			if fromAddress, ok := idMap[id]; ok {
				info, err := indexer.GetOWnerAddress(id, tx)
				if err == nil && info != nil {
					info.FromAddress = fromAddress
					trasferMap[id] = info
				}
			}
		}
	}
	return
}

func (indexer *Indexer) GetOWnerAddress(inputId string, tx *wire.MsgTx) (info *pin.PinTransferInfo, err error) {
	info = &pin.PinTransferInfo{}
	firstInputId := fmt.Sprintf("%s:%d", tx.TxIn[0].PreviousOutPoint.Hash, tx.TxIn[0].PreviousOutPoint.Index)
	if len(tx.TxIn) == 1 || firstInputId == inputId {
		class, addresses, _, _ := txscript.ExtractPkScriptAddrs(tx.TxOut[0].PkScript, netParams)
		if len(addresses) > 0 {
			info.Address = GetBase58AddressFromPkScript(addresses[0].ScriptAddress(), btcNetParams)
		} else if class.String() == "nulldata" {
			info.Address = hex.EncodeToString(tx.TxOut[0].PkScript)
		}
		info.Location = fmt.Sprintf("%s:%d:%d", tx.TxHash().String(), 0, 0)
		info.Offset = 0
		info.Output = fmt.Sprintf("%s:%d", tx.TxHash().String(), 0)
		info.OutputValue = tx.TxOut[0].Value
		return
	}
	totalOutputValue := int64(0)
	for _, out := range tx.TxOut {
		totalOutputValue += out.Value
	}
	inputValue := int64(0)
	for _, in := range tx.TxIn {
		id := fmt.Sprintf("%s:%d", in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index)
		if id == inputId {
			break
		}
		value, err1 := GetValueByTx(in.PreviousOutPoint.Hash.String(), int(in.PreviousOutPoint.Index))
		if err1 != nil {
			err = errors.New("get value error")
			return
		}
		inputValue += value
		if inputValue > totalOutputValue {
			return
		}
	}
	outputValue := int64(0)
	for i, out := range tx.TxOut {
		outputValue += out.Value
		if outputValue > inputValue {
			class, addresses, _, _ := txscript.ExtractPkScriptAddrs(out.PkScript, netParams)
			if len(addresses) > 0 {
				info.Address = GetBase58AddressFromPkScript(addresses[0].ScriptAddress(), btcNetParams)
			} else if class.String() == "nulldata" {
				info.Address = hex.EncodeToString(out.PkScript)
			}
			info.Output = fmt.Sprintf("%s:%d", tx.TxHash().String(), i)
			info.Location = fmt.Sprintf("%s:%d", info.Output, out.Value-(outputValue-inputValue))
			info.Offset = uint64(i)
			info.OutputValue = out.Value
			break
		}
	}
	return
}

func (indexer *Indexer) CatchPinsByTx(msgTxInf interface{}, blockHeight int64, timestamp int64, blockHash string, merkleRoot string, txIndex int) (pinInscriptions []*pin.PinInscription) {
	msgTx := msgTxInf.(*wire.MsgTx)
	haveOpReturn := false
	for i, out := range msgTx.TxOut {
		class, _, _, _ := txscript.ExtractPkScriptAddrs(out.PkScript, netParams)
		if class.String() == "nonstandard" {
			pinInscription := indexer.ParsePin(out.PkScript)
			if pinInscription == nil {
				continue
			}
			_, host, path := pin.ValidHostPath(pinInscription.Path)
			if common.CheckBlockedHost(host) {
				continue
			}
			if !common.CheckHost(host) {
				continue
			}
			address, outIdx, locationIdx := indexer.GetPinOwner(msgTx, 0)
			txHash, err := GetNewHash(msgTx)
			if err != nil {
				continue
			}
			id := fmt.Sprintf("%si%d", txHash, outIdx)
			metaId := common.GetMetaIdByAddress(address)
			contentTypeDetect := common.DetectContentType(&pinInscription.ContentBody)
			pop := ""
			if merkleRoot != "" && blockHash != "" {
				pop, _ = common.GenPop(id, merkleRoot, blockHash)
			}
			popLv, _ := pin.PopLevelCount(indexer.ChainName, pop)
			creator := address
			if common.Config.Sync.IsFullNode {
				// creator lookup via first input's previous output — same as MVC
			}
			pinInscriptions = append(pinInscriptions, &pin.PinInscription{
				ChainName:          indexer.ChainName,
				Id:                 id,
				MetaId:             metaId,
				Number:             0,
				Address:            address,
				InitialOwner:       address,
				CreateAddress:      creator,
				CreateMetaId:       common.GetMetaIdByAddress(creator),
				GlobalMetaId:       common.ConvertToGlobalMetaId(creator),
				Timestamp:          timestamp,
				GenesisHeight:      blockHeight,
				GenesisTransaction: txHash,
				Output:             fmt.Sprintf("%s:%d", txHash, outIdx),
				OutputValue:        msgTx.TxOut[outIdx].Value,
				TxInIndex:          uint32(i - 1),
				Offset:             uint64(outIdx),
				TxIndex:            txIndex,
				Operation:          pinInscription.Operation,
				Location:           fmt.Sprintf("%s:%d:%d", txHash, outIdx, locationIdx),
				Path:               strings.TrimSpace(path),
				OriginalPath:       strings.TrimSpace(pinInscription.Path),
				ParentPath:         strings.TrimSpace(pinInscription.ParentPath),
				Encryption:         pinInscription.Encryption,
				Version:            pinInscription.Version,
				ContentType:        pinInscription.ContentType,
				ContentTypeDetect:  contentTypeDetect,
				ContentBody:        pinInscription.ContentBody,
				ContentLength:      pinInscription.ContentLength,
				ContentSummary:     getContentSummary(pinInscription, id, contentTypeDetect),
				Pop:                pop,
				PopLv:              popLv,
				PoPScore:           pin.GetPoPScore(pop, int64(popLv), common.Config.Opcat.PopCutNum),
				PoPScoreV1:         pin.GetPoPScoreV1(pop, popLv),
				DataValue:          pin.RarityScoreBinary(indexer.ChainName, pop),
				Mrc20MintId:        []string{},
				Host:               host,
			})
			haveOpReturn = true
			break
		}
	}
	if !haveOpReturn {
		return nil
	}
	return
}

func getParentPath(path string) (parentPath string) {
	arr := strings.Split(path, "/")
	if len(arr) < 3 {
		return
	}
	parentPath = strings.Join(arr[0:len(arr)-1], "/")
	return
}

func getContentSummary(pinode *pin.PersonalInformationNode, id string, contentTypeDetect string) (content string) {
	if contentTypeDetect[0:4] != "text" {
		return fmt.Sprintf("/content/%s", id)
	} else {
		c := string(pinode.ContentBody)
		if len(c) > 150 {
			return c[0:150]
		} else {
			return string(pinode.ContentBody)
		}
	}
}

func (indexer *Indexer) GetPinOwner(tx *wire.MsgTx, inIdx int) (address string, outIdx int, locationIdx int64) {
	for i, out := range tx.TxOut {
		class, addresses, _, _ := txscript.ExtractPkScriptAddrs(out.PkScript, netParams)
		if class.String() != "nulldata" && class.String() != "nonstandard" && len(addresses) > 0 {
			address = GetBase58AddressFromPkScript(addresses[0].ScriptAddress(), btcNetParams)
			outIdx = i
			locationIdx = 0
			break
		}
	}
	return
}

func (indexer *Indexer) ParsePin(pkScript []byte) (pinode *pin.PersonalInformationNode) {
	tokenizer := btcdtxscript.MakeScriptTokenizer(0, pkScript)
	for tokenizer.Next() {
		if tokenizer.Opcode() == txscript.OP_RETURN {
			if !tokenizer.Next() || hex.EncodeToString(tokenizer.Data()) != common.Config.ProtocolID {
				return
			}
			pinode = indexer.parseOnePin(&tokenizer)
		}
	}
	return
}

func (indexer *Indexer) parseOnePin(tokenizer *btcdtxscript.ScriptTokenizer) *pin.PersonalInformationNode {
	var infoList [][]byte
	for tokenizer.Next() {
		infoList = append(infoList, tokenizer.Data())
	}
	if err := tokenizer.Err(); err != nil {
		return nil
	}
	if len(infoList) < 1 {
		return nil
	}

	pinode := pin.PersonalInformationNode{}
	pinode.Operation = strings.ToLower(string(infoList[0]))
	if pinode.Operation == "init" {
		pinode.Path = "/"
		return &pinode
	}
	if len(infoList) < 6 && pinode.Operation != "revoke" {
		return nil
	}
	if pinode.Operation == "revoke" && len(infoList) < 5 {
		return nil
	}
	pinode.Path = strings.ToLower(string(infoList[1]))
	pinode.ParentPath = getParentPath(pinode.Path)
	encryption := "0"
	if infoList[2] != nil {
		encryption = string(infoList[2])
	}
	pinode.Encryption = encryption
	version := "0"
	if infoList[3] != nil {
		version = string(infoList[3])
	}
	pinode.Version = version
	contentType := "application/json"
	if infoList[4] != nil {
		contentType = strings.ToLower(string(infoList[4]))
	}
	pinode.ContentType = contentType
	var body []byte
	for i := 5; i < len(infoList); i++ {
		body = append(body, infoList[i]...)
	}
	pinode.ContentBody = body
	pinode.ContentLength = uint64(len(body))
	return &pinode
}

func (indexer *Indexer) GetBlockTxHash(blockHeight int64) (txhashList []string, pinIdList []string) {
	chain := OpcatChain{}
	blockMsg, err := chain.GetBlock(blockHeight)
	if err != nil {
		return
	}
	block := blockMsg.(*wire.MsgBlock)
	for _, tx := range block.Transactions {
		txHash, err := GetNewHash(tx)
		if err != nil {
			continue
		}
		for i := range tx.Copy().TxOut {
			var pinId strings.Builder
			pinId.WriteString(txHash)
			pinId.WriteString("i")
			pinId.WriteString(strconv.Itoa(i))
			pinIdList = append(pinIdList, pinId.String())
		}
		txhashList = append(txhashList, tx.TxHash().String())
	}
	return
}

// MRC20 methods are no-ops for opcat
func (indexer *Indexer) CatchNativeMrc20Transfer(blockHeight int64, utxoList []*mrc20.Mrc20Utxo, mrc20TransferPinTx map[string]struct{}) (savelist []*mrc20.Mrc20Utxo) {
	return nil
}

func (indexer *Indexer) CatchMempoolNativeMrc20Transfer(txList []interface{}, utxoList []*mrc20.Mrc20Utxo, mrc20TransferPinTx map[string]struct{}) (savelist []*mrc20.Mrc20Utxo) {
	return nil
}
```

- [ ] **Step 2: Verify compilation with all three new files**

```bash
cd .worktrees/feat-opcat-chain && go build ./adapter/opcat/ 2>&1
```

Expected: "zmq.go" errors about undefined `ZmqRun`/`ZmqHashblock` (we add those next). The indexer.go itself should compile.
```

---

### Task 4: Create `adapter/opcat/zmq.go` and `zmq_stub.go`

**Files:**
- Create: `adapter/opcat/zmq.go`
- Create: `adapter/opcat/zmq_stub.go`

- [ ] **Step 1: Write zmq.go (cgo build)**

Mirrors `adapter/microvisionchain/zmq.go`, changing:
- ZMQ host reads from `common.Config.Opcat.ZmqHost`
- Log prefix says "OPCAT ZMQ"

```go
//go:build cgo
// +build cgo

package opcat

import (
	"bytes"
	"log"
	"man-p2p/common"
	"man-p2p/pin"

	"github.com/bitcoinsv/bsvd/wire"
	zmq "github.com/pebbe/zmq4"
)

func (indexer *Indexer) ZmqHashblock() {}

func (indexer *Indexer) ZmqRun(chanMsg chan pin.MempollChanMsg) {
	subscriber, _ := zmq.NewSocket(zmq.SUB)
	defer subscriber.Close()
	subscriber.SetSubscribe("rawtx")
	err := subscriber.SetTcpKeepalive(1)
	if err != nil {
		log.Println("SetTcpKeepalive err,", err)
	}
	err = subscriber.SetTcpKeepaliveIdle(60)
	if err != nil {
		log.Println("SetTcpKeepaliveIdle err,", err)
	}
	err = subscriber.SetTcpKeepaliveIntvl(1)
	if err != nil {
		log.Println("SetTcpKeepaliveIntvl err,", err)
	}
	subscriber.SetRcvhwm(20000)
	subscriber.SetRcvbuf(1024 * 200)
	err = subscriber.Connect(common.Config.Opcat.ZmqHost)
	if err != nil {
		log.Println("Connect to OPCAT ZMQ error", err)
		return
	} else {
		log.Println("OPCAT ZMQ connected")
	}

	for {
		recvmsg, err := subscriber.Recv(0)
		if err != nil {
			log.Println("OPCAT ZMQ RecvMessage Err,", err)
			continue
		} else {
			if recvmsg == "rawtx" || len(recvmsg) < 10 {
				continue
			}
			var msgTx wire.MsgTx
			if err := msgTx.Deserialize(bytes.NewReader([]byte(recvmsg))); err != nil {
				continue
			}
			pinInscriptions := indexer.CatchPinsByTx(&msgTx, 0, 0, "", "", 0)
			if len(pinInscriptions) > 0 {
				chanMsg <- pin.MempollChanMsg{PinList: pinInscriptions, Tx: msgTx}
			}
			tansferList, err := indexer.TransferCheck(&msgTx)
			if err == nil && len(tansferList) > 0 {
				chanMsg <- pin.MempollChanMsg{PinList: tansferList, Tx: msgTx}
			}
		}
	}
}

func (indexer *Indexer) TransferCheck(tx *wire.MsgTx) (transferPinList []*pin.PinInscription, err error) {
	// Stub for now — same pattern as MVC, requires db adapter integration
	return nil, nil
}
```

- [ ] **Step 2: Write zmq_stub.go (non-cgo build)**

```go
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
```

- [ ] **Step 3: Verify adapter/opcat/ compiles**

```bash
cd .worktrees/feat-opcat-chain && CGO_ENABLED=1 go build ./adapter/opcat/ 2>&1
```

Expected: SUCCESS (no output). If CGO is unavailable, test non-cgo path:

```bash
CGO_ENABLED=0 go build ./adapter/opcat/ 2>&1
```

Expected: SUCCESS (uses zmq_stub.go).

- [ ] **Step 4: Commit adapter/opcat/**

```bash
git add adapter/opcat/
git commit -m "feat: add adapter/opcat package for OpcatLayer chain support"
```

---

### Task 5: Modify `common/config.go`

**Files:**
- Modify: `common/config.go`

- [ ] **Step 1: Add `opcatConfig` struct**

After the `dogeConfig` struct definition (~line 120), add:

```go
type opcatConfig struct {
	InitialHeight   int64  `toml:"initialHeight"`
	Mrc20Height     int64  `toml:"mrc20Height"`
	RpcHost         string `toml:"rpcHost"`
	RpcUser         string `toml:"rpcUser"`
	RpcPass         string `toml:"rpcPass"`
	RpcHTTPPostMode bool   `toml:"rpcHttpPostMode"`
	RpcDisableTLS   bool   `toml:"rpcDisableTLS"`
	ZmqHost         string `toml:"zmqHost"`
	PopCutNum       int    `toml:"popCutNum"`
}
```

- [ ] **Step 2: Add `Opcat` field to `AllConfig`**

In the `AllConfig` struct, after `Doge dogeConfig`:

```go
Opcat opcatConfig
```

- [ ] **Step 3: Add CLI flags in `GetFlagConfig()`**

After `doge_zmqpubrawtx` flag line, add:

```go
flagConfig["opcat_height"] = flag.String("opcat_height", "", "opcat starting block height")
flagConfig["opcat_rpc_host"] = flag.String("opcat_rpc_host", "", "opcat rpc host")
flagConfig["opcat_rpc_user"] = flag.String("opcat_rpc_user", "", "opcat rpcuser")
flagConfig["opcat_rpc_password"] = flag.String("opcat_rpc_password", "", "opcat rpc password")
flagConfig["opcat_zmqpubrawtx"] = flag.String("opcat_zmqpubrawtx", "", "opcat zmqpubrawtx")
```

- [ ] **Step 4: Add flag→config mapping in `InitConfig()`**

After the `doge_zmqpubrawtx` case (~line 260), add:

```go
case "opcat_height":
	Config.Opcat.InitialHeight, _ = strconv.ParseInt(*v, 10, 64)
case "opcat_rpc_host":
	Config.Opcat.RpcHost = *v
case "opcat_rpc_user":
	Config.Opcat.RpcUser = *v
case "opcat_rpc_password":
	Config.Opcat.RpcPass = *v
case "opcat_zmqpubrawtx":
	Config.Opcat.ZmqHost = *v
```

- [ ] **Step 5: Add PopCutNum setting in `InitConfig()`**

In the `switch TestNet` block where `Config.Doge.PopCutNum` is set, add after each:

For mainnet:
```go
Config.Opcat.PopCutNum = 21
```

For testnet:
```go
Config.Opcat.PopCutNum = 8
```

For regtest:
```go
Config.Opcat.PopCutNum = 0
```

- [ ] **Step 6: Verify compilation**

```bash
cd .worktrees/feat-opcat-chain && go build ./common/ 2>&1
```

Expected: SUCCESS.

- [ ] **Step 7: Commit**

```bash
git add common/config.go
git commit -m "feat: add opcat config struct, CLI flags, and PopCutNum settings"
```

---

### Task 6: Modify `man/man.go` (registration points)

**Files:**
- Modify: `man/man.go`

- [ ] **Step 1: Add import**

In the import block, add:

```go
"man-p2p/adapter/opcat"
```

- [ ] **Step 2: Add InitRuntime case**

In the `switch chain` block inside `InitRuntime()`, after `case "doge":`:

```go
case "opcat":
	ChainAdapter[chain] = &opcat.OpcatChain{}
	IndexerAdapter[chain] = &opcat.Indexer{
		ChainParams: ChainParams[chain],
		PopCutNum:   common.Config.Opcat.PopCutNum,
		ChainName:   chain,
	}
```

- [ ] **Step 3: Add getSyncHeight fallback**

In `getSyncHeight()`, in the `if test == "" && initialHeight == 0` block, after the Doge else-if:

```go
} else if chainName == "opcat" {
	initialHeight = int64(0)
```

(Zero means "use config value only" — which is 120418 from config.toml.)

- [ ] **Step 4: Verify compilation**

```bash
cd .worktrees/feat-opcat-chain && go build ./man/ 2>&1
```

Expected: SUCCESS.

- [ ] **Step 5: Commit**

```bash
git add man/man.go
git commit -m "feat: register opcat chain in InitRuntime and getSyncHeight"
```

---

### Task 7: Modify `man/indexer_pebble.go` (handleMrc20)

**Files:**
- Modify: `man/indexer_pebble.go`

- [ ] **Step 1: Add switch case**

In the `handleMrc20()` function's first switch (~line 472), after `case "doge":`:

```go
case "opcat":
	mrc20Height = common.Config.Opcat.Mrc20Height
```

This will return -1 (from config), causing the function to return early via the `if mrc20Height < 0 { return }` check.

- [ ] **Step 2: Verify compilation**

```bash
cd .worktrees/feat-opcat-chain && go build ./man/ 2>&1
```

Expected: SUCCESS.

- [ ] **Step 3: Commit**

```bash
git add man/indexer_pebble.go
git commit -m "feat: add opcat MRC20 height lookup in handleMrc20"
```

---

### Task 8: Modify `pin/pop.go` (POP scoring)

**Files:**
- Modify: `pin/pop.go`

- [ ] **Step 1: Add opcat case to PopLevelCount**

In the `PopLevelCount()` switch, after `case "mvc":`:

```go
case "opcat":
	PopCutNum = common.Config.Opcat.PopCutNum
case "doge":
	PopCutNum = common.Config.Doge.PopCutNum
```

(Also add the Doge case — it was previously missing.)

- [ ] **Step 2: Add opcat case to RarityScoreBinary**

In the `RarityScoreBinary()` switch, after `case "mvc":`:

```go
case "opcat":
	popCutNum = common.Config.Opcat.PopCutNum
case "doge":
	popCutNum = common.Config.Doge.PopCutNum
```

- [ ] **Step 3: Verify compilation**

```bash
cd .worktrees/feat-opcat-chain && go build ./pin/ 2>&1
```

Expected: SUCCESS.

- [ ] **Step 4: Commit**

```bash
git add pin/pop.go
git commit -m "feat: add opcat and doge POP scoring cases in pin/pop.go"
```

---

### Task 9: Modify `config.toml`

**Files:**
- Modify: `config.toml`

- [ ] **Step 1: Add [opcat] section**

After the `[doge]` section, add:

```toml
[opcat]
initialHeight = 120418
mrc20Height = -1
rpcHost = "34.92.196.81:18443"
rpcUser = "scrypt"
rpcPass = "W4iULFuKidPY6wCv"
rpcHttpPostMode = true
rpcDisableTLS = true
zmqHost = "tcp://34.92.196.81:18442"
popCutNum = 21
```

- [ ] **Step 2: Verify TOML syntax**

```bash
cd .worktrees/feat-opcat-chain && go run . -config ./config.toml -server=0 2>&1 | head -5
```

Expected: Configuration loads without TOML parse errors. Expected to see the banner and config initialization (but will likely fail at chain init because opcat isn't the sole chain and we haven't started it).

- [ ] **Step 3: Commit**

```bash
git add config.toml
git commit -m "feat: add [opcat] section to config.toml"
```

---

### Task 10: Full build verification

**Files:**
- None (verification only)

- [ ] **Step 1: Build the full binary**

```bash
cd .worktrees/feat-opcat-chain && CGO_ENABLED=0 go build -o /dev/null . 2>&1
```

Expected: SUCCESS (binary compiles, no link errors).

- [ ] **Step 2: Test opcat-specific package compiles with CGO**

```bash
CGO_ENABLED=1 go build ./adapter/opcat/ 2>&1
```

Expected: SUCCESS.

- [ ] **Step 3: Run existing tests to verify no regressions**

```bash
CGO_ENABLED=0 go test ./... 2>&1
```

Expected: All existing tests pass (no new failures introduced).

---

## Summary

| Task | Files | Lines |
|------|-------|-------|
| 1 | `adapter/opcat/opcat.go` | ~150 |
| 2 | `adapter/opcat/util.go` | ~200 (copied from MVC) |
| 3 | `adapter/opcat/indexer.go` | ~350 |
| 4 | `adapter/opcat/zmq.go` + `zmq_stub.go` | ~80 |
| 5 | `common/config.go` | ~30 |
| 6 | `man/man.go` | ~10 |
| 7 | `man/indexer_pebble.go` | ~3 |
| 8 | `pin/pop.go` | ~8 |
| 9 | `config.toml` | ~10 |
| 10 | Build verification | — |
| **Total** | | **~840 new + ~50 modified** |
