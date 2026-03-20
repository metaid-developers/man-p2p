package bitcoin

import (
	"encoding/hex"
	"errors"
	"fmt"
	"man-p2p/common"
	"man-p2p/mrc20"
	"man-p2p/pin"
	"strconv"
	"strings"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

var PopCutNum int = 0
var netParams *chaincfg.Params

type Indexer struct {
	ChainParams string
	Block       interface{}
	PopCutNum   int
	ChainName   string
}

func (indexer *Indexer) InitIndexer() {
	switch indexer.ChainParams {
	case "mainnet":
		netParams = &chaincfg.MainNetParams
	case "testnet":
		netParams = &chaincfg.TestNet3Params
	case "regtest":
		netParams = &chaincfg.RegressionNetParams
	}
	PopCutNum = common.Config.Btc.PopCutNum
}
func (indexer *Indexer) GetCurHeight() (height int64) {
	return
}
func (indexer *Indexer) GetAddress(pkScript []byte) (address string) {
	_, addresses, _, _ := txscript.ExtractPkScriptAddrs(pkScript, netParams)
	if len(addresses) > 0 {
		address = addresses[0].String()
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

	chain := BitcoinChain{}
	blockMsg, err := chain.GetBlock(blockHeight)
	if err != nil {
		return
	}
	indexer.Block = blockMsg
	block := blockMsg.(*wire.MsgBlock)
	timestamp := block.Header.Timestamp.Unix()
	blockHash := block.BlockHash().String()
	merkleRoot := block.Header.MerkleRoot.String()

	for i, tx := range block.Transactions {
		for _, in := range tx.TxIn {
			//id := fmt.Sprintf("%s:%d", in.PreviousOutPoint.Hash.String(), in.PreviousOutPoint.Index)
			id := common.ConcatBytesOptimized([]string{in.PreviousOutPoint.Hash.String(), ":", strconv.FormatUint(uint64(in.PreviousOutPoint.Index), 10)}, "")
			*txInList = append(*txInList, id)
		}
		if !tx.HasWitness() {
			continue
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
	blockHash := "none"
	merkleRoot := "none"
	for i, item := range txList {
		tx := item.(*wire.MsgTx)
		for _, in := range tx.TxIn {
			id := fmt.Sprintf("%s:%d", in.PreviousOutPoint.Hash.String(), in.PreviousOutPoint.Index)
			txInList = append(txInList, id)
		}
		if !tx.HasWitness() {
			continue
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
		// 检测是否为熔化交易
		isMeltdown := indexer.IsMeltdownTransaction(tx, idMap)

		for _, in := range tx.TxIn {
			id := fmt.Sprintf("%s:%d", in.PreviousOutPoint.Hash.String(), in.PreviousOutPoint.Index)
			if fromAddress, ok := idMap[id]; ok {
				info, err := indexer.GetOWnerAddress(id, tx)
				if err == nil && info != nil {
					info.FromAddress = fromAddress
					info.IsMeltdown = isMeltdown
					trasferMap[id] = info
				}
			}
		}
	}
	return
}

// IsMeltdownTransaction 检测是否为熔化交易
// 熔化条件:
// 1. 输入有 ≥3 个  PIN-UTXO
// 2. 输出只有 1 个
// 3. 输入和输出地址相同
func (indexer *Indexer) IsMeltdownTransaction(tx *wire.MsgTx, idMap map[string]string) bool {
	// 条件2: 输出只有1个
	if len(tx.TxOut) != 1 {
		return false
	}

	// 获取输出地址
	outAddress := ""
	_, addresses, _, _ := txscript.ExtractPkScriptAddrs(tx.TxOut[0].PkScript, netParams)
	if len(addresses) > 0 {
		outAddress = addresses[0].String()
	}
	if outAddress == "" {
		return false
	}

	// 统计输入中符合条件的 PIN-UTXO 数量
	pinUtxoCount := 0
	allSameAddress := true

	for _, in := range tx.TxIn {
		id := fmt.Sprintf("%s:%d", in.PreviousOutPoint.Hash.String(), in.PreviousOutPoint.Index)
		if fromAddress, ok := idMap[id]; ok {
			// 这是一个 PIN-UTXO，检查金额是否为 546
			value, err := GetValueByTx(in.PreviousOutPoint.Hash.String(), int(in.PreviousOutPoint.Index))
			if err == nil && value == pin.StandardPinUtxoValue {
				pinUtxoCount++
				// 条件3: 检查输入地址是否与输出地址相同
				if fromAddress != outAddress {
					allSameAddress = false
				}
			}
		}
	}

	// 条件1: 输入有 ≥3 个 546 聪的 PIN-UTXO
	// 条件3: 所有 PIN 输入地址都与输出地址相同
	return pinUtxoCount >= pin.MeltdownMinPinCount && allSameAddress
}
func (indexer *Indexer) CatchNativeMrc20Transfer(blockHeight int64, utxoList []*mrc20.Mrc20Utxo, mrc20TransferPinTx map[string]struct{}) (savelist []*mrc20.Mrc20Utxo) {
	pointMap := make(map[string][]*mrc20.Mrc20Utxo)
	//keyMap := make(map[string]*mrc20.Mrc20Utxo) //key point-tickid
	for _, u := range utxoList {
		if u.MrcOption == mrc20.OptionDeploy {
			continue
		}
		pointMap[u.TxPoint] = append(pointMap[u.TxPoint], u)
	}
	block := indexer.Block.(*wire.MsgBlock)
	t := block.Header.Timestamp.Unix()
	for _, tx := range block.Transactions {
		//if have data transfer
		_, ok := mrc20TransferPinTx[tx.TxHash().String()]
		if ok {
			continue
		}
		list := indexer.createMrc20NativeTransfer(tx, blockHeight, t, pointMap)
		if len(list) > 0 {
			savelist = append(savelist, list...)
		}
	}
	// for _, u := range keyMap {
	// 	savelist = append(savelist, u)
	// }
	return
}
func (indexer *Indexer) CatchMempoolNativeMrc20Transfer(txList []interface{}, utxoList []*mrc20.Mrc20Utxo, mrc20TransferPinTx map[string]struct{}) (savelist []*mrc20.Mrc20Utxo) {
	pointMap := make(map[string][]*mrc20.Mrc20Utxo)
	//keyMap := make(map[string]*mrc20.Mrc20Utxo) //key point-tickid
	for _, u := range utxoList {
		if u.MrcOption == mrc20.OptionDeploy {
			continue
		}
		pointMap[u.TxPoint] = append(pointMap[u.TxPoint], u)
	}
	t := time.Now().Unix()
	for _, item := range txList {
		tx := item.(*wire.MsgTx)
		//if have data transfer
		_, ok := mrc20TransferPinTx[tx.TxHash().String()]
		if ok {
			continue
		}
		list := indexer.createMrc20NativeTransfer(tx, -1, t, pointMap)
		if len(list) > 0 {
			savelist = append(savelist, list...)
		}
	}
	// for _, u := range keyMap {
	// 	savelist = append(savelist, u)
	// }
	return
}
func (indexer *Indexer) createMrc20NativeTransfer(tx *wire.MsgTx, blockHeight int64, blockTime int64, pointMap map[string][]*mrc20.Mrc20Utxo) (mrc20Utxolist []*mrc20.Mrc20Utxo) {
	keyMap := make(map[string]*mrc20.Mrc20Utxo)
	for _, in := range tx.TxIn {
		id := fmt.Sprintf("%s:%d", in.PreviousOutPoint.Hash.String(), in.PreviousOutPoint.Index)
		if v, ok := pointMap[id]; ok {
			for _, utxo := range v {
				// 【修复】跳过 TeleportPending 状态的 UTXO
				// TeleportPending 的 UTXO 已经被 teleport 处理，不应该被 native transfer 再次处理
				if utxo.Status == mrc20.UtxoStatusTeleportPending {
					continue
				}

				send := *utxo

				// 检测 UTXO 是否已经被 mempool 阶段处理过
				// 如果状态是 TransferPending，说明 AmtChange 已经是负数了
				alreadyProcessedByMempool := utxo.Status == mrc20.UtxoStatusTransferPending

				// 获取原始的正数金额（用于创建接收方 UTXO）
				originalAmt := utxo.AmtChange
				if alreadyProcessedByMempool {
					// UTXO 已被 mempool 处理，AmtChange 是负数，需要取绝对值
					originalAmt = utxo.AmtChange.Abs()
				}

				// 根据 blockHeight 判断是 mempool 还是出块
				if blockHeight == -1 {
					// mempool 阶段：设置为 TransferPending（待转出）
					send.Status = mrc20.UtxoStatusTransferPending
					// 转出的 UTXO，AmtChange 应该是负数
					send.AmtChange = send.AmtChange.Neg()
				} else {
					// 出块确认：设置为 Spent（已消耗）
					send.Status = mrc20.UtxoStatusSpent
					if alreadyProcessedByMempool {
						// 已被 mempool 处理过，AmtChange 已经是负数，不需要再取负
						// 但需要恢复为正数作为 spent 金额（因为 ProcessNativeTransferSuccess 会处理）
						send.AmtChange = originalAmt
					}
				}
				send.MrcOption = mrc20.OptionNativeTransfer
				send.OperationTx = tx.TxHash().String()
				mrc20Utxolist = append(mrc20Utxolist, &send)
				//key := fmt.Sprintf("%s-%s", send.Mrc20Id, send.TxPoint)
				key := send.Mrc20Id
				_, find := keyMap[key]
				if find {
					// 多个输入情况下累加金额
					if alreadyProcessedByMempool {
						keyMap[key].AmtChange = keyMap[key].AmtChange.Add(originalAmt)
					} else {
						keyMap[key].AmtChange = keyMap[key].AmtChange.Add(send.AmtChange)
					}
				} else {
					recive := *utxo
					recive.MrcOption = mrc20.OptionNativeTransfer
					recive.FromAddress = recive.ToAddress
					recive.ToAddress = indexer.GetAddress(tx.TxOut[0].PkScript)
					recive.BlockHeight = blockHeight
					recive.TxPoint = fmt.Sprintf("%s:%d", tx.TxHash().String(), 0)
					recive.Timestamp = blockTime
					recive.Chain = "btc"
					recive.Msg = "native-transfer"
					recive.OperationTx = tx.TxHash().String()
					// 关键：接收方的新 UTXO 应该是 Available 状态（0），而不是继承原始 UTXO 的状态
					recive.Status = mrc20.UtxoStatusAvailable
					// 重要：AmtChange 应该是转入的金额，使用原始 UTXO 的金额（正数）
					// 如果 UTXO 已被 mempool 处理，AmtChange 可能是负数，需要使用 originalAmt
					recive.AmtChange = originalAmt
					keyMap[key] = &recive
				}
			}
		}
	}
	for _, u := range keyMap {
		mrc20Utxolist = append(mrc20Utxolist, u)
	}
	return
}

func (indexer *Indexer) GetOWnerAddress(inputId string, tx *wire.MsgTx) (info *pin.PinTransferInfo, err error) {
	//fmt.Println("tx:", tx.TxHash().String(), inputId)
	info = &pin.PinTransferInfo{}
	firstInputId := fmt.Sprintf("%s:%d", tx.TxIn[0].PreviousOutPoint.Hash, tx.TxIn[0].PreviousOutPoint.Index)
	if len(tx.TxIn) == 1 || firstInputId == inputId {
		class, addresses, _, _ := txscript.ExtractPkScriptAddrs(tx.TxOut[0].PkScript, netParams)
		if len(addresses) > 0 {
			info.Address = addresses[0].String()
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
				info.Address = addresses[0].String()
			} else if class.String() == "nulldata" {
				info.Address = hex.EncodeToString(out.PkScript)
			}
			info.Output = fmt.Sprintf("%s:%d", tx.TxHash().String(), i)
			//count offset
			info.Location = fmt.Sprintf("%s:%d", info.Output, out.Value-(outputValue-inputValue))
			info.Offset = uint64(i)
			info.OutputValue = out.Value
			break
		}
	}

	return
}
func (indexer *Indexer) CatchPinsByTx(msgTxInf interface{}, blockHeight int64, timestamp int64, blockHash string, merkleRoot string, txIndex int) (pinInscriptions []*pin.PinInscription) {
	//No witness data
	// if !msgTx.HasWitness() {
	// 	return nil
	// }
	msgTx := msgTxInf.(*wire.MsgTx)
	chain := BitcoinChain{}
	haveOpReturn := false
	for i, out := range msgTx.TxOut {
		class, _, _, _ := txscript.ExtractPkScriptAddrs(out.PkScript, netParams)
		//fmt.Println(class.String())
		if class.String() == "nonstandard" {
			pinInscription := indexer.ParseOpReturnPin(out.PkScript)
			if pinInscription == nil {
				continue
			}
			_, host, path := pin.ValidHostPath(pinInscription.Path)
			if common.CheckBlockedHost(host) {
				continue //blocked host
			}
			if !common.CheckHost(host) {
				continue //not in host list
			}
			address, outIdx, locationIdx := indexer.GetOpReturnPinOwner(msgTx)
			if address == "" {
				continue
			}
			txHash := msgTx.TxHash().String()
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
				creator = chain.GetCreatorAddress(msgTx.TxIn[0].PreviousOutPoint.Hash.String(), msgTx.TxIn[0].PreviousOutPoint.Index, netParams)
				// if v, ok := pin.AllCreatorAddress.Load(msgTx.TxIn[0].PreviousOutPoint.Hash.String()); ok {
				// 	creator = v.(string)
				// }
			}
			//_, host, path := pin.ValidHostPath(pinInscription.Path)
			pinInscriptions = append(pinInscriptions, &pin.PinInscription{
				//Pin:                pinInscription,
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
				PoPScore:           pin.GetPoPScore(pop, int64(popLv), common.Config.Btc.PopCutNum),
				PoPScoreV1:         pin.GetPoPScoreV1(pop, popLv),
				DataValue:          pin.RarityScoreBinary(indexer.ChainName, pop),
				Mrc20MintId:        []string{},
				Host:               host,
			})
			haveOpReturn = true
			break
		}
	}
	if haveOpReturn {
		return
	}
	for i, v := range msgTx.TxIn {
		index, input := i, v
		//Witness length error
		if len(input.Witness) <= 1 {
			continue
		}
		if len(input.Witness[len(input.Witness)-1]) <= 1 {
			continue
		}
		//Witness length error,Taproot
		if len(input.Witness) == 2 && input.Witness[len(input.Witness)-1][0] == txscript.TaprootAnnexTag {
			continue
		}
		// If Taproot Annex data exists, take the last element of the witness as the script data, otherwise,
		// take the penultimate element of the witness as the script data
		var witnessScript []byte
		if input.Witness[len(input.Witness)-1][0] == txscript.TaprootAnnexTag {
			witnessScript = input.Witness[len(input.Witness)-1]
		} else {
			if len(input.Witness) >= 2 {
				witnessScript = input.Witness[len(input.Witness)-2]
			}
		}
		// Parse script and get pin content
		pinInscription := indexer.ParsePin(witnessScript)
		if pinInscription == nil {
			continue
		}
		address, outIdx, locationIdx := indexer.GetPinOwner(msgTx, index)
		id := fmt.Sprintf("%si%d", msgTx.TxHash().String(), outIdx)
		metaId := common.GetMetaIdByAddress(address)
		contentTypeDetect := common.DetectContentType(&pinInscription.ContentBody)
		pop := ""
		if merkleRoot != "" && blockHash != "" {
			if merkleRoot == "none" && blockHash == "none" {
				pop = "none"
			} else {
				pop, _ = common.GenPop(id, merkleRoot, blockHash)
			}

		}
		popLv, _ := pin.PopLevelCount(indexer.ChainName, pop)
		creator := address
		if common.Config.Sync.IsFullNode {
			creator = chain.GetCreatorAddress(v.PreviousOutPoint.Hash.String(), v.PreviousOutPoint.Index, netParams)
			// if val, ok := pin.AllCreatorAddress.Load(v.PreviousOutPoint.Hash.String()); ok {
			// 	creator = val.(string)
			// }
		}
		_, host, path := pin.ValidHostPath(pinInscription.Path)
		pinInscriptions = append(pinInscriptions, &pin.PinInscription{
			//Pin:                pinInscription,
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
			GenesisTransaction: msgTx.TxHash().String(),
			Output:             fmt.Sprintf("%s:%d", msgTx.TxHash().String(), outIdx),
			OutputValue:        msgTx.TxOut[outIdx].Value,
			TxInIndex:          uint32(index),
			Offset:             uint64(outIdx),
			TxIndex:            txIndex,
			Operation:          pinInscription.Operation,
			Location:           fmt.Sprintf("%s:%d:%d", msgTx.TxHash().String(), outIdx, locationIdx),
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
			PoPScore:           pin.GetPoPScore(pop, int64(popLv), common.Config.Btc.PopCutNum),
			PoPScoreV1:         pin.GetPoPScoreV1(pop, popLv),
			DataValue:          pin.RarityScoreBinary(indexer.ChainName, pop),
			Mrc20MintId:        []string{},
			Host:               host,
		})
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

func (indexer *Indexer) ParseOpReturnPin(pkScript []byte) (pinode *pin.PersonalInformationNode) {
	// Parse pins content from witness script
	tokenizer := txscript.MakeScriptTokenizer(0, pkScript)
	for tokenizer.Next() {
		// Check inscription envelop header: OP_FALSE(0x00), OP_IF(0x63), PROTOCOL_ID
		if tokenizer.Opcode() == txscript.OP_RETURN {
			if !tokenizer.Next() || hex.EncodeToString(tokenizer.Data()) != common.Config.ProtocolID {
				return
			}
			pinode = indexer.parseOpReturnOnePin(&tokenizer)
		}
	}
	return
}
func (indexer *Indexer) parseOpReturnOnePin(tokenizer *txscript.ScriptTokenizer) *pin.PersonalInformationNode {
	// Find any pushed data in the script. This includes OP_0, but not OP_1 - OP_16.
	var infoList [][]byte
	for tokenizer.Next() {
		infoList = append(infoList, tokenizer.Data())
	}
	// Error occurred
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
func (indexer *Indexer) GetOpReturnPinOwner(tx *wire.MsgTx) (address string, outIdx int, locationIdx int64) {
	for i, out := range tx.TxOut {
		class, addresses, _, _ := txscript.ExtractPkScriptAddrs(out.PkScript, netParams)
		if class.String() != "nonstandard" && len(addresses) > 0 {
			outIdx = i
			address = addresses[0].String()
			locationIdx = 0
		}
	}
	return
}
func (indexer *Indexer) GetPinOwner(tx *wire.MsgTx, inIdx int) (address string, outIdx int, locationIdx int64) {
	if len(tx.TxIn) == 1 || len(tx.TxOut) == 1 || inIdx == 0 {
		_, addresses, _, _ := txscript.ExtractPkScriptAddrs(tx.TxOut[0].PkScript, netParams)
		if len(addresses) > 0 {
			address = addresses[0].String()
		}
		return
	}
	inputValue := int64(0)
	for i, in := range tx.TxIn {
		if i == inIdx {
			break
		}
		value, err := GetValueByTx(in.PreviousOutPoint.Hash.String(), int(in.PreviousOutPoint.Index))
		if err != nil {
			return
		}
		inputValue += value
	}
	outputValue := int64(0)
	for x, out := range tx.TxOut {
		outputValue += out.Value
		if outputValue > inputValue {
			locationIdx = outputValue - inputValue
			_, addresses, _, _ := txscript.ExtractPkScriptAddrs(out.PkScript, netParams)
			if len(addresses) > 0 {
				address = addresses[0].String()
				outIdx = x
			}
			break
		}
	}
	return
}
func (indexer *Indexer) ParsePins(witnessScript []byte) (pins []*pin.PersonalInformationNode) {
	// Parse pins content from witness script
	tokenizer := txscript.MakeScriptTokenizer(0, witnessScript)
	for tokenizer.Next() {
		// Check inscription envelop header: OP_FALSE(0x00), OP_IF(0x63), PROTOCOL_ID
		if tokenizer.Opcode() == txscript.OP_FALSE {
			if !tokenizer.Next() || tokenizer.Opcode() != txscript.OP_IF {
				return
			}
			if !tokenizer.Next() || hex.EncodeToString(tokenizer.Data()) != common.Config.ProtocolID {
				return
			}
			pinode := indexer.parseOnePin(&tokenizer)
			if pinode != nil {
				pins = append(pins, pinode)
			}
		}
	}
	return
}
func (indexer *Indexer) ParsePin(witnessScript []byte) (pinode *pin.PersonalInformationNode) {
	// Parse pins content from witness script
	tokenizer := txscript.MakeScriptTokenizer(0, witnessScript)
	for tokenizer.Next() {
		// Check inscription envelop header: OP_FALSE(0x00), OP_IF(0x63), PROTOCOL_ID
		if tokenizer.Opcode() == txscript.OP_FALSE {
			if !tokenizer.Next() || tokenizer.Opcode() != txscript.OP_IF {
				return
			}
			if !tokenizer.Next() || hex.EncodeToString(tokenizer.Data()) != common.Config.ProtocolID {
				return
			}
			pinode = indexer.parseOnePin(&tokenizer)
		}
	}
	return
}
func (indexer *Indexer) parseOnePin(tokenizer *txscript.ScriptTokenizer) *pin.PersonalInformationNode {
	// Find any pushed data in the script. This includes OP_0, but not OP_1 - OP_16.
	var infoList [][]byte
	for tokenizer.Next() {
		if tokenizer.Opcode() == txscript.OP_ENDIF {
			break
		}
		infoList = append(infoList, tokenizer.Data())
		if len(tokenizer.Data()) > 520 {
			//log.Errorf("data is longer than 520")
			return nil
		}
	}
	// No OP_ENDIF
	if tokenizer.Opcode() != txscript.OP_ENDIF {
		return nil
	}
	// Error occurred
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
	chain := BitcoinChain{}
	blockMsg, err := chain.GetBlock(blockHeight)
	if err != nil {
		return
	}
	block := blockMsg.(*wire.MsgBlock)
	for _, tx := range block.Transactions {
		for i := range tx.Copy().TxOut {
			var pinId strings.Builder
			pinId.WriteString(tx.TxHash().String())
			pinId.WriteString("i")
			pinId.WriteString(strconv.Itoa(i))
			pinIdList = append(pinIdList, pinId.String())
		}
		txhashList = append(txhashList, tx.TxHash().String())
	}
	return
}
