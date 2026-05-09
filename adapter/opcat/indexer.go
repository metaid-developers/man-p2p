package opcat

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
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
	"github.com/shopspring/decimal"
)

var netParams *chaincfg.Params
var btcNetParams *btcdChaincfg.Params

type Indexer struct {
	ChainParams string
	Block       interface{}
	PopCutNum   int
	ChainName   string
}

type opcatVerboseBlock struct {
	Hash       string   `json:"hash"`
	MerkleRoot string   `json:"merkleroot"`
	Time       int64    `json:"time"`
	Tx         []string `json:"tx"`
}

type opcatVerboseTx struct {
	TxID string `json:"txid"`
	Vin  []struct {
		TxID string `json:"txid"`
		Vout uint32 `json:"vout"`
	} `json:"vin"`
	Vout []opcatVerboseVout `json:"vout"`
}

type opcatVerboseVout struct {
	N            uint32      `json:"n"`
	Value        json.Number `json:"value"`
	ScriptPubKey struct {
		Hex string `json:"hex"`
	} `json:"scriptPubKey"`
}

func (indexer *Indexer) InitIndexer() {
	netParams = &chaincfg.MainNetParams
	btcNetParams = &btcdChaincfg.MainNetParams
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

	blockHash, err := client.GetBlockHash(blockHeight)
	if err != nil {
		log.Println("GetBlockHash Error:", err)
		return
	}
	var block opcatVerboseBlock
	if err := opcatRawRequest("getblock", []interface{}{blockHash.String(), 1}, &block); err != nil {
		log.Println("GetBlock Error:", err)
		return
	}
	indexer.Block = block

	for i, txid := range block.Tx {
		var tx opcatVerboseTx
		if err := opcatRawRequest("getrawtransaction", []interface{}{txid, true}, &tx); err != nil {
			log.Println("GetRawTransaction Error:", err)
			continue
		}
		for _, in := range tx.Vin {
			if in.TxID == "" {
				continue
			}
			id := common.ConcatBytesOptimized([]string{in.TxID, ":", strconv.FormatUint(uint64(in.Vout), 10)}, "")
			*txInList = append(*txInList, id)
		}
		txPins := indexer.catchPinsByVerboseTx(tx, blockHeight, block.Time, block.Hash, block.MerkleRoot, i)
		for _, p := range txPins {
			if pin.ManValidator(p) == nil {
				*pinInscriptions = append(*pinInscriptions, p)
			}
		}
	}
	return
}

func opcatRawRequest(method string, params []interface{}, result interface{}) error {
	rawParams := make([]json.RawMessage, 0, len(params))
	for _, param := range params {
		data, err := json.Marshal(param)
		if err != nil {
			return err
		}
		rawParams = append(rawParams, data)
	}
	raw, err := client.RawRequest(method, rawParams)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return decoder.Decode(result)
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
		address, _, _ := indexer.GetPinOwner(msgTx, 0)
		txHash := msgTx.TxHash().String()
		id := fmt.Sprintf("%si%d", txHash, i)
		metaId := common.GetMetaIdByAddress(address)
		contentTypeDetect := common.DetectContentType(&pinInscription.ContentBody)
		pop := ""
		if merkleRoot != "" && blockHash != "" {
			pop, _ = common.GenPop(id, merkleRoot, blockHash)
		}
		popLv, _ := pin.PopLevelCount(indexer.ChainName, pop)
		creator := address
		txInIndex := uint32(0)
		if i > 0 {
			txInIndex = uint32(i - 1)
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
			Output:             fmt.Sprintf("%s:%d", txHash, i),
			OutputValue:        out.Value,
			TxInIndex:          txInIndex,
			Offset:             uint64(i),
			TxIndex:            txIndex,
			Operation:          pinInscription.Operation,
			Location:           fmt.Sprintf("%s:%d:%d", txHash, i, 0),
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
	if !haveOpReturn {
		return nil
	}
	return
}

func (indexer *Indexer) catchPinsByVerboseTx(tx opcatVerboseTx, blockHeight int64, timestamp int64, blockHash string, merkleRoot string, txIndex int) (pinInscriptions []*pin.PinInscription) {
	ownerAddress := ""
	for _, out := range tx.Vout {
		pkScript, err := hex.DecodeString(out.ScriptPubKey.Hex)
		if err != nil {
			continue
		}
		if indexer.ParsePin(pkScript) != nil {
			continue
		}
		if address := indexer.GetAddress(pkScript); address != "" {
			ownerAddress = address
			break
		}
	}

	for _, out := range tx.Vout {
		pkScript, err := hex.DecodeString(out.ScriptPubKey.Hex)
		if err != nil {
			continue
		}
		pinInscription := indexer.ParsePin(pkScript)
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
		id := fmt.Sprintf("%si%d", tx.TxID, out.N)
		metaId := common.GetMetaIdByAddress(ownerAddress)
		contentTypeDetect := common.DetectContentType(&pinInscription.ContentBody)
		pop := ""
		if merkleRoot != "" && blockHash != "" {
			pop, _ = common.GenPop(id, merkleRoot, blockHash)
		}
		popLv, _ := pin.PopLevelCount(indexer.ChainName, pop)
		outputValue, _ := decimal.NewFromString(out.Value.String())
		outputValue = outputValue.Mul(decimal.NewFromInt(100000000))
		pinInscriptions = append(pinInscriptions, &pin.PinInscription{
			ChainName:          indexer.ChainName,
			Id:                 id,
			MetaId:             metaId,
			Number:             0,
			Address:            ownerAddress,
			InitialOwner:       ownerAddress,
			CreateAddress:      ownerAddress,
			CreateMetaId:       common.GetMetaIdByAddress(ownerAddress),
			GlobalMetaId:       common.ConvertToGlobalMetaId(ownerAddress),
			Timestamp:          timestamp,
			GenesisHeight:      blockHeight,
			GenesisTransaction: tx.TxID,
			Output:             fmt.Sprintf("%s:%d", tx.TxID, out.N),
			OutputValue:        outputValue.IntPart(),
			TxInIndex:          0,
			Offset:             uint64(out.N),
			TxIndex:            txIndex,
			Operation:          pinInscription.Operation,
			Location:           fmt.Sprintf("%s:%d:%d", tx.TxID, out.N, 0),
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
		break
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
			if !tokenizer.Next() {
				return
			}
			if hex.EncodeToString(tokenizer.Data()) == common.Config.ProtocolID {
				pinode = indexer.parseOnePin(&tokenizer)
			} else {
				pinode = indexer.parseNestedPin(tokenizer.Data())
			}
		}
	}
	return
}

func (indexer *Indexer) parseNestedPin(payload []byte) *pin.PersonalInformationNode {
	tokenizer := btcdtxscript.MakeScriptTokenizer(0, payload)
	if !tokenizer.Next() || hex.EncodeToString(tokenizer.Data()) != common.Config.ProtocolID {
		return nil
	}
	return indexer.parseOnePin(&tokenizer)
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
		txHash := tx.TxHash().String()
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
