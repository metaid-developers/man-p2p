package man

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"man-p2p/adapter/dogecoin"
	"man-p2p/common"
	"man-p2p/mrc20"
	"man-p2p/pin"
	"strconv"
	"strings"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
	"github.com/shopspring/decimal"
)

// getBtcNetParams 获取链的网络参数
func getBtcNetParams(chainName string) *chaincfg.Params {
	chainParam := ChainParams[chainName]

	// 针对不同链返回对应的网络参数
	switch chainName {
	case "doge":
		switch chainParam {
		case "mainnet":
			return &dogecoin.DogeMainNetParams
		case "testnet":
			return &dogecoin.DogeTestNetParams
		case "regtest":
			return &dogecoin.DogeRegTestParams
		default:
			return &dogecoin.DogeMainNetParams
		}
	default: // btc, mvc 等
		switch chainParam {
		case "mainnet":
			return &chaincfg.MainNetParams
		case "testnet":
			return &chaincfg.TestNet3Params
		case "regtest":
			return &chaincfg.RegressionNetParams
		default:
			return &chaincfg.MainNetParams
		}
	}
}

type Mrc20Validator struct {
}

func (validator *Mrc20Validator) Check(pinNode *pin.PinInscription) {
}

func (validator *Mrc20Validator) Deploy(content []byte, pinNode *pin.PinInscription) (string, int64, error) {
	lowerContent := strings.ToLower(string(content))
	var data mrc20.Mrc20DeployLow
	err := json.Unmarshal([]byte(lowerContent), &data)
	if err != nil {
		return "", 0, errors.New(mrc20.ErrDeployContent)
	}
	if len(data.Tick) < 2 || len(data.Tick) > 24 {
		return "", 0, errors.New(mrc20.ErrDeployTickLength)
	}
	if data.TokenName != "" {
		if len(data.TokenName) < 1 || len(data.TokenName) > 48 {
			return "", 0, errors.New(mrc20.ErrDeployTickNameLength)
		}
	}
	decimals := int64(8)
	if data.Decimals != "" {
		decimals, err := strconv.ParseInt(data.Decimals, 10, 64)
		if err != nil {
			return "", 0, err
		}

		if decimals < 0 || decimals > 12 {
			return "", 0, errors.New(mrc20.ErrDeployNum)
		}
	}

	amtPerMint, err := strconv.ParseInt(data.AmtPerMint, 10, 64)
	if err != nil {
		return "", 0, err
	}
	if amtPerMint < 1 || amtPerMint > 1000000000000 {
		return "", 0, errors.New(mrc20.ErrDeployNum)
	}

	mintCount, err := strconv.ParseInt(data.MintCount, 10, 64)
	if err != nil {
		return "", 0, err
	}
	if mintCount < 1 || mintCount > 1000000000000 {
		return "", 0, errors.New(mrc20.ErrDeployNum)
	}

	premineCount := int64(0)
	if data.PremineCount != "" {
		premineCount, err = strconv.ParseInt(data.PremineCount, 10, 64)
		if err != nil {
			return "", 0, err
		}
	}
	if premineCount > mintCount {
		return "", 0, errors.New(mrc20.ErrDeployNum)
	}
	if data.PayCheck != (mrc20.Mrc20DeployPayCheckLower{}) {
		if data.PayCheck.PayAmount == "" || data.PayCheck.PayTo == "" {
			return "", 0, errors.New(mrc20.ErrDeployNum)
		}
		_, err := strconv.ParseInt(data.PayCheck.PayAmount, 10, 64)
		if err != nil {
			return "", 0, errors.New(mrc20.ErrDeployNum)
		}
	}

	//check tick name
	//ErrDeployTickExists
	tickName := strings.ToUpper(data.Tick)
	info, _ := PebbleStore.GetMrc20TickInfo("", tickName)
	if info != (mrc20.Mrc20DeployInfo{}) {
		if tickName == info.Tick {
			return "", 0, errors.New(mrc20.ErrDeployTickExists)
		}
	}

	if premineCount <= 0 {
		return "", 0, nil
	}
	t := getDigitsCount(amtPerMint*mintCount) + decimals
	if t > 20 {
		return "", 0, errors.New(mrc20.ErrDeployNum)
	}

	txb, err := GetTransactionWithCache(pinNode.ChainName, pinNode.GenesisTransaction)
	if err != nil {
		return "", 0, errors.New(mrc20.ErrDeployTxGet)
	}
	//premineCount check
	if len(txb.MsgTx().TxOut) < 2 {
		return "", 0, errors.New("tx error")
	}
	if pinNode.Offset != 0 {
		return "", 0, errors.New("tx error")
	}
	toAddress := ""
	class, addresses, _, _ := txscript.ExtractPkScriptAddrs(txb.MsgTx().TxOut[1].PkScript, getBtcNetParams(pinNode.ChainName))
	if class.String() != "nulldata" && class.String() != "nonstandard" && len(addresses) > 0 {
		toAddress = addresses[0].String()
	}
	return toAddress, txb.MsgTx().TxOut[1].Value, nil
}

func getDigitsCount(n int64) int64 {
	return int64(len(strconv.FormatInt(n, 10)))
}

func (validator *Mrc20Validator) Mint(content mrc20.Mrc20MintData, pinNode *pin.PinInscription) (info mrc20.Mrc20DeployInfo, shovelList []string, toAddress string, vout int, err error) {
	if content.Id == "" {
		err = errors.New(mrc20.ErrMintTickIdNull)
		return
	}

	info, err = PebbleStore.GetMrc20TickInfo(content.Id, "")
	if err != nil {
		log.Println("GetMrc20TickInfo:", err)
		return
	}
	if info == (mrc20.Mrc20DeployInfo{}) {
		err = errors.New(mrc20.ErrMintTickNotExists)
		return
	}
	if info.Chain != pinNode.ChainName {
		err = errors.New(mrc20.ErrCrossChain)
		return
	}
	if (info.MintCount - info.TotalMinted) < 1 {
		err = errors.New(mrc20.ErrMintLimit)
		return
	}

	if info.BeginHeight != "" {
		beginHeight, e1 := strconv.ParseInt(info.BeginHeight, 10, 64)
		if e1 != nil || pinNode.GenesisHeight < beginHeight {
			err = errors.New(mrc20.ErrMintHeight)
			return
		}
	}
	if info.EndHeight != "" {
		endHeight, e1 := strconv.ParseInt(info.EndHeight, 10, 64)
		if e1 != nil || pinNode.GenesisHeight > endHeight {
			err = errors.New(mrc20.ErrMintHeight)
			return
		}
	}

	txb, err := GetTransactionWithCache(pinNode.ChainName, pinNode.GenesisTransaction)
	if err != nil {
		log.Println("Mint Validator GetTransactionWithCache:", err)
		return
	}
	//check vout
	if content.Vout != "" {
		mintVout, err1 := strconv.Atoi(content.Vout)
		if err1 != nil {
			err = errors.New(mrc20.ErrMintVout)
			return
		}
		if mintVout > (len(txb.MsgTx().TxOut)-1) || mintVout < 0 {
			err = errors.New(mrc20.ErrMintVout)
			return
		}
		class, addresses, _, _ := txscript.ExtractPkScriptAddrs(txb.MsgTx().TxOut[mintVout].PkScript, getBtcNetParams(pinNode.ChainName))
		if class.String() != "nulldata" && class.String() != "nonstandard" && len(addresses) > 0 {
			toAddress = addresses[0].String()
			vout = mintVout
		} else {
			err = errors.New(mrc20.ErrMintVout)
			return
		}
	}
	if info.PinCheck.Count == "" || info.PinCheck.Count == "0" {
		return
	}
	var inputList []string
	//Because the PIN has been transferred,
	//use the output to find the PIN attributes.
	//pay check
	findPayCheck := false
	payAmt := int64(0)
	if info.PayCheck.PayAmount != "" && info.PayCheck.PayTo != "" {
		findPayCheck = true
		payAmt, _ = strconv.ParseInt(info.PayCheck.PayAmount, 10, 64)
	}
	if payAmt < 0 {
		payAmt = int64(0)
	}
	isHavePayCheck := false
	for i, out := range txb.MsgTx().TxOut {
		s := fmt.Sprintf("%s:%d", txb.Hash().String(), i)
		tmpId := fmt.Sprintf("%si%d", txb.Hash().String(), i)
		if tmpId != pinNode.Id {
			inputList = append(inputList, s)
		}
		if findPayCheck {
			class, addresses, _, _ := txscript.ExtractPkScriptAddrs(out.PkScript, getBtcNetParams(pinNode.ChainName))
			if class.String() != "nulldata" && class.String() != "nonstandard" && len(addresses) > 0 {
				checkAddress := addresses[0].String()
				if checkAddress == info.PayCheck.PayTo && out.Value >= payAmt {
					isHavePayCheck = true
				}
			}
		}
	}
	if findPayCheck && !isHavePayCheck {
		err = errors.New(mrc20.ErrMintPayCheck)
		return
	}
	if len(inputList) <= 0 {
		err = errors.New(mrc20.ErrMintPopNull)
		return
	}
	pinsTmp, err := PebbleStore.GetPinListByOutPutList(inputList)
	if err != nil {
		log.Println("GetPinListByOutPutList:", err, inputList)
		return
	}
	if len(pinsTmp) <= 0 {
		err = errors.New(mrc20.ErrMintPopNull)
		return
	}
	var pins []*pin.PinInscription
	for _, pinNode := range pinsTmp {
		if pinNode.Operation == "hide" {
			continue
		}
		pins = append(pins, pinNode)
	}

	var pinIds []string
	for _, pinNode := range pins {
		pinIds = append(pinIds, pinNode.Id)
	}
	if len(pinIds) <= 0 {
		err = errors.New(mrc20.ErrMintPopNull)
		return
	}
	usedShovels, err := PebbleStore.GetMrc20Shovel(pinIds, content.Id)

	shovelsCount, _ := strconv.Atoi(info.PinCheck.Count)
	shovelChcek := true
	var lvShovelList []string
	var creatorShovelList []string
	if info.PinCheck.Lv != "" {
		popLimit, _ := strconv.Atoi(info.PinCheck.Lv)
		shovelChcek, lvShovelList = lvCheck(usedShovels, pins, shovelsCount, popLimit)
		if !shovelChcek {
			err = errors.New(mrc20.ErrMintPopDiff)
			return
		}
	}
	//creator check
	if info.PinCheck.Creator != "" {
		shovelChcek, creatorShovelList = creatorCheck(usedShovels, pins, shovelsCount, info.PinCheck.Creator)
		if !shovelChcek {
			err = errors.New(mrc20.ErrMintCreator)
			return
		}
	}

	var pathShovelList []string
	if info.PinCheck.Path != "" {
		shovelChcek, pathShovelList = pathCheck(usedShovels, pins, shovelsCount, info.PinCheck.Path)
		if !shovelChcek {
			err = errors.New(mrc20.ErrMintPathCheck)
			return
		}
	}
	var countShovelList []string
	if info.PinCheck.Creator == "" && info.PinCheck.Lv == "" && info.PinCheck.Path == "" {
		shovelChcek, countShovelList = onlyCountCheck(usedShovels, pins, shovelsCount)
		if !shovelChcek {
			err = errors.New(mrc20.ErrMintCountCheck)
			return
		}
	}
	if len(lvShovelList) > 0 {
		shovelList = append(shovelList, lvShovelList...)
	}
	if len(creatorShovelList) > 0 {
		shovelList = append(shovelList, creatorShovelList...)
	}
	if len(pathShovelList) > 0 {
		shovelList = append(shovelList, pathShovelList...)
	}
	if len(countShovelList) > 0 {
		shovelList = append(shovelList, countShovelList...)
	}
	return
}

func lvCheck(usedShovels map[string]mrc20.Mrc20Shovel, pins []*pin.PinInscription, shovelsCount int, popLimit int) (verified bool, shovelList []string) {
	x := 0
	for _, pinNode := range pins {
		if _, ok := usedShovels[pinNode.Id]; ok {
			continue
		}
		find := countLeadingZeros(pinNode.Pop)
		if find >= popLimit {
			x += 1
			shovelList = append(shovelList, pinNode.Id)
		}
		if x == shovelsCount {
			break
		}
	}
	if x >= shovelsCount {
		verified = true
	}
	return
}

func creatorCheck(usedShovels map[string]mrc20.Mrc20Shovel, pins []*pin.PinInscription, shovelsCount int, creator string) (verified bool, shovelList []string) {
	x := 0
	for _, pinNode := range pins {
		if _, ok := usedShovels[pinNode.Id]; ok {
			continue
		}
		if common.GetMetaIdByAddress(pinNode.CreateAddress) == creator {
			x += 1
			shovelList = append(shovelList, pinNode.Id)
		}
		if x == shovelsCount {
			break
		}
	}
	if x >= shovelsCount {
		verified = true
	}
	return
}

func pathCheck(usedShovels map[string]mrc20.Mrc20Shovel, pins []*pin.PinInscription, shovelsCount int, pathStr string) (verified bool, shovelList []string) {
	path, query, key, operator, value := mrc20.PathParse(pathStr)
	if path == "" && query == "" {
		verified, shovelList = onlyPathCheck(usedShovels, pins, shovelsCount, pathStr)
		return
	}
	if path != "" && query != "" {
		if key == "" && operator == "" && value == "" {
			query = query[2 : len(query)-2]
			verified, shovelList = followPathCheck(usedShovels, pins, shovelsCount, path, query)
		} else if key != "" && operator != "" && value != "" {
			if operator == "=" {
				verified, shovelList = equalPathCheck(usedShovels, pins, shovelsCount, path, key, value)
			} else if operator == "#=" {
				verified, shovelList = contentPathCheck(usedShovels, pins, shovelsCount, path, key, value)
			}
		}
	}
	return
}

func onlyPathCheck(usedShovels map[string]mrc20.Mrc20Shovel, pins []*pin.PinInscription, shovelsCount int, pathStr string) (verified bool, shovelList []string) {
	pathArr := strings.Split(pathStr, "/")
	//Wildcard
	if pathArr[len(pathArr)-1] == "*" {
		pathStr = pathStr[0 : len(pathStr)-2]
	}
	x := 0
	for _, pinNode := range pins {
		if _, ok := usedShovels[pinNode.Id]; ok {
			continue
		}
		if len(pinNode.Path) < len(pathStr) {
			continue
		}
		//Wildcard
		if pinNode.Path[0:len(pathStr)] == pathStr {
			x += 1
			shovelList = append(shovelList, pinNode.Id)
		}
		if x == shovelsCount {
			break
		}
	}
	if x >= shovelsCount {
		verified = true
	}
	return
}

func followPathCheck(usedShovels map[string]mrc20.Mrc20Shovel, pins []*pin.PinInscription, shovelsCount int, pathStr string, queryStr string) (verified bool, shovelList []string) {
	x := 0
	if pathStr != "/follow" {
		return
	}
	for _, pinNode := range pins {
		if _, ok := usedShovels[pinNode.Id]; ok {
			continue
		}
		if string(pinNode.ContentBody) != queryStr {
			continue
		}
		x += 1
		shovelList = append(shovelList, pinNode.Id)
		if x == shovelsCount {
			break
		}
	}
	if x >= shovelsCount {
		verified = true
	}
	return
}

func equalPathCheck(usedShovels map[string]mrc20.Mrc20Shovel, pins []*pin.PinInscription, shovelsCount int, pathStr string, key string, value string) (verified bool, shovelList []string) {
	x := 0
	for _, pinNode := range pins {
		if _, ok := usedShovels[pinNode.Id]; ok {
			continue
		}
		if pinNode.Path != pathStr {
			continue
		}
		m := make(map[string]interface{})
		err := json.Unmarshal(pinNode.ContentBody, &m)
		if err != nil {
			continue
		}
		if _, ok := m[key]; !ok {
			continue
		}
		c := fmt.Sprintf("%s", m[key])
		if c != value {
			continue
		}
		x += 1
		shovelList = append(shovelList, pinNode.Id)
		if x == shovelsCount {
			break
		}
	}
	if x >= shovelsCount {
		verified = true
	}
	return
}

func contentPathCheck(usedShovels map[string]mrc20.Mrc20Shovel, pins []*pin.PinInscription, shovelsCount int, pathStr string, key string, value string) (verified bool, shovelList []string) {
	x := 0
	for _, pinNode := range pins {
		if _, ok := usedShovels[pinNode.Id]; ok {
			continue
		}
		if pinNode.Path != pathStr {
			continue
		}

		m := make(map[string]interface{})
		err := json.Unmarshal(pinNode.ContentBody, &m)
		if err != nil {
			continue
		}
		if _, ok := m[key]; !ok {
			continue
		}
		c := fmt.Sprintf("%s", m[key])
		if !strings.Contains(c, value) {
			continue
		}
		x += 1
		shovelList = append(shovelList, pinNode.Id)
		if x == shovelsCount {
			break
		}
	}
	if x >= shovelsCount {
		verified = true
	}
	return
}

func onlyCountCheck(usedShovels map[string]mrc20.Mrc20Shovel, pins []*pin.PinInscription, shovelsCount int) (verified bool, shovelList []string) {
	x := 0
	for _, pinNode := range pins {
		if _, ok := usedShovels[pinNode.Id]; ok {
			continue
		}
		x += 1
		shovelList = append(shovelList, pinNode.Id)
		if x == shovelsCount {
			break
		}
	}
	if x >= shovelsCount {
		verified = true
	}
	return
}

func countLeadingZeros(str string) int {
	count := 0
	for _, char := range str {
		if char == '0' {
			count++
		} else {
			break
		}
	}
	return count
}

func (validator *Mrc20Validator) Transfer(content []mrc20.Mrc20TranferData, pinNode *pin.PinInscription, isMempool bool) (toAddress map[int]string, utxoList []*mrc20.Mrc20Utxo, outputValueList []int64, msg string, firstIdx int, err error) {
	////log.Printf("[DEBUG] Validator.Transfer START: pinId=%s, tx=%s, contentLen=%d", pinNode.Id, pinNode.GenesisTransaction, len(content))

	if len(content) <= 0 {
		err = errors.New(mrc20.ErrTranferReqData)
		msg = mrc20.ErrTranferReqData
		return
	}
	outMap := make(map[string]decimal.Decimal)
	maxVout := 0
	for _, item := range content {
		if item.Id == "" || item.Amount == "" {
			err = errors.New(mrc20.ErrTranferReqData)
			msg = mrc20.ErrTranferReqData
			return
		}
		if maxVout < item.Vout {
			maxVout = item.Vout
		}
		amt, _ := decimal.NewFromString(item.Amount)
		if amt.Cmp(decimal.Zero) == -1 || amt.Cmp(decimal.Zero) == 0 {
			err = errors.New(mrc20.ErrTranferAmt)
			msg = mrc20.ErrTranferAmt
			return
		}
		outMap[item.Id] = outMap[item.Id].Add(amt)

		tick, err1 := PebbleStore.GetMrc20TickInfo(item.Id, "")
		if err1 != nil {
			////log.Printf("[DEBUG] Validator.Transfer: GetMrc20TickInfo failed, tickId=%s, err=%v", item.Id, err1)
			err = errors.New(mrc20.ErrMintTickIdNull)
			msg = mrc20.ErrMintTickIdNull
			return
		}
		decimals, _ := strconv.ParseInt(tick.Decimals, 10, 64)
		if getDecimalPlaces(item.Amount) > decimals {
			err = errors.New(mrc20.ErrMintDecimals)
			msg = mrc20.ErrMintDecimals
			return
		}

	}

	//get  mrc20 list in tx input
	txb, err := GetTransactionWithCache(pinNode.ChainName, pinNode.GenesisTransaction)
	if err != nil {
		log.Println("GetTransactionWithCache:", err)
		return
	}
	//check output
	if maxVout > len(txb.MsgTx().TxOut) {
		msg = "Incorrect number of outputs in the transfer transaction"
		err = errors.New("valueErr")
		return
	}
	for _, item := range content {
		if item.Vout >= len(txb.MsgTx().TxOut) {
			msg = "Incorrect vout target for the transfer"
			err = errors.New("valueErr")
			return
		}
		class, _, _, _ := txscript.ExtractPkScriptAddrs(txb.MsgTx().TxOut[item.Vout].PkScript, getBtcNetParams(pinNode.ChainName))
		//log.Printf("[DEBUG] Validator.Transfer: checking output vout=%d, class=%s, pkScriptLen=%d", item.Vout, class.String(), len(txb.MsgTx().TxOut[item.Vout].PkScript))
		if class.String() == "nulldata" || class.String() == "nonstandard" {
			msg = "Incorrect vout target for the transfer"
			err = errors.New("valueErr")
			//log.Printf("[DEBUG] Validator.Transfer: output check failed, vout=%d, class=%s", item.Vout, class.String())
			return
		}
	}
	var inputList []string
	for _, in := range txb.MsgTx().TxIn {
		s := fmt.Sprintf("%s:%d", in.PreviousOutPoint.Hash.String(), in.PreviousOutPoint.Index)
		inputList = append(inputList, s)
	}
	//log.Printf("[DEBUG] Validator.Transfer: looking up input UTXOs, inputList=%v", inputList)

	list, err := PebbleStore.GetMrc20UtxoByOutPutList(inputList, isMempool)
	if err != nil {
		log.Println("GetMrc20UtxoByOutPutList:", err, isMempool)
		return
	}
	//log.Printf("[DEBUG] Validator.Transfer: found %d UTXOs in inputs", len(list))

	inMap := make(map[string]decimal.Decimal)
	for _, item := range list {
		////log.Printf("[DEBUG] Validator.Transfer: input UTXO found - txPoint=%s, tickId=%s, amt=%s",	item.TxPoint, item.Mrc20Id, item.AmtChange.String())
		inMap[item.Mrc20Id] = inMap[item.Mrc20Id].Add(item.AmtChange)
		utxoList = append(utxoList, item)
	}
	//if out list value error
	for k, v := range outMap {
		if in, ok := inMap[k]; ok {
			//in < v
			if in.Compare(v) == -1 {
				////log.Printf("[DEBUG] Validator.Transfer: input amount %s < output amount %s for tickId %s", in.String(), v.String(), k)
				msg = "The total input amount is less than the output"
				err = errors.New("valueErr")
				return
			}
		} else {
			////log.Printf("[DEBUG] Validator.Transfer: tickId %s not found in inputs, outMap keys=%v, inMap keys=%v", k, outMap, inMap)
			msg = "No available tick in the input"
			err = errors.New("valueErr")
			return
		}
	}
	toAddress = make(map[int]string)
	firstIdx = -1
	for i, out := range txb.MsgTx().TxOut {
		class, addresses, _, _ := txscript.ExtractPkScriptAddrs(out.PkScript, getBtcNetParams(pinNode.ChainName))
		if class.String() != "nulldata" && class.String() != "nonstandard" && len(addresses) > 0 {
			toAddress[i] = addresses[0].String()
			if firstIdx < 0 {
				firstIdx = i
			}
		} else {
			toAddress[i] = "nonexistent"
		}
		outputValueList = append(outputValueList, out.Value)
	}
	return
}

func getDecimalPlaces(str string) int64 {
	if dotIndex := strings.IndexByte(str, '.'); dotIndex != -1 {
		return int64(len(str) - dotIndex - 1)
	}
	return int64(0)
}

func getBalanceByaddressAndTick(address string, tickId string) (blance decimal.Decimal, tick string, err error) {
	list, err := PebbleStore.GetMrc20ByAddressAndTick(address, tickId)
	if err != nil {
		return
	}
	for _, item := range list {
		if item.AmtChange.Compare(decimal.Zero) == 0 {
			continue
		}
		blance = blance.Add(item.AmtChange)
		if tick == "" {
			tick = item.Tick
		}
	}
	return
}
