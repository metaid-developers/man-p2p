package main

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"man-p2p/mrc20"

	"github.com/BurntSushi/toml"
	"github.com/cockroachdb/pebble"
	"github.com/shopspring/decimal"
)

var (
	configFile = flag.String("config", "config_dev_main.toml", "config file path")
	chainName  = flag.String("chainName", "btc", "chain name: btc, doge")
	txidParam  = flag.String("txid", "", "transaction id to check")
	pebbleDir  = flag.String("pebble", "./man_base_data_pebble", "pebble database directory")
)

type TeleportCheckResult struct {
	TxId             string                 `json:"txid"`
	Chain            string                 `json:"chain"`
	IsTeleport       bool                   `json:"is_teleport"`
	TransferData     []TeleportTransferInfo `json:"transfer_data,omitempty"`
	Inputs           []InputInfo            `json:"inputs"`
	Outputs          []OutputInfo           `json:"outputs"`
	MatchedUtxo      *UtxoInfo              `json:"matched_utxo,omitempty"`
	ArrivalInfo      *ArrivalInfo           `json:"arrival_info,omitempty"`
	PendingInfo      *PendingInfo           `json:"pending_info,omitempty"`
	ValidationResult *ValidationResult      `json:"validation_result,omitempty"`
	Error            string                 `json:"error,omitempty"`
}

type TeleportTransferInfo struct {
	Vout   int    `json:"vout"`
	Id     string `json:"id"`
	Amount string `json:"amount"`
	Coord  string `json:"coord"`
	Chain  string `json:"chain"`
	Type   string `json:"type"`
}

type InputInfo struct {
	Index    int       `json:"index"`
	TxPoint  string    `json:"txpoint"`
	HasUtxo  bool      `json:"has_utxo"`
	UtxoInfo *UtxoInfo `json:"utxo_info,omitempty"`
}

type OutputInfo struct {
	Index   int    `json:"index"`
	Address string `json:"address"`
	Value   int64  `json:"value"`
	Type    string `json:"type"`
}

type UtxoInfo struct {
	TxPoint   string `json:"txpoint"`
	Mrc20Id   string `json:"mrc20_id"`
	Tick      string `json:"tick"`
	Amount    string `json:"amount"`
	ToAddress string `json:"to_address"`
	Status    int    `json:"status"`
	StatusStr string `json:"status_str"`
}

type ArrivalInfo struct {
	PinId         string `json:"pin_id"`
	TxId          string `json:"txid"`
	AssetOutpoint string `json:"asset_outpoint"`
	Amount        string `json:"amount"`
	TickId        string `json:"tick_id"`
	Chain         string `json:"chain"`
	Status        int    `json:"status"`
	StatusStr     string `json:"status_str"`
	Msg           string `json:"msg,omitempty"`
}

type PendingInfo struct {
	PinId         string `json:"pin_id"`
	Coord         string `json:"coord"`
	TickId        string `json:"tick_id"`
	Amount        string `json:"amount"`
	AssetOutpoint string `json:"asset_outpoint"`
	TargetChain   string `json:"target_chain"`
	Status        int    `json:"status"`
	StatusStr     string `json:"status_str"`
}

type ValidationResult struct {
	IsValid         bool     `json:"is_valid"`
	Errors          []string `json:"errors,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
	Recommendations []string `json:"recommendations,omitempty"`
}

// RPC Response structs
type RpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *RpcError       `json:"error"`
	Id     string          `json:"id"`
}

type RpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type TxInfo struct {
	Txid string `json:"txid"`
	Vin  []struct {
		Txid      string   `json:"txid"`
		Vout      int      `json:"vout"`
		Witness   []string `json:"txinwitness"`
		ScriptSig struct {
			Hex string `json:"hex"`
		} `json:"scriptSig"`
	} `json:"vin"`
	Vout []struct {
		Value        float64 `json:"value"`
		N            int     `json:"n"`
		ScriptPubKey struct {
			Type      string   `json:"type"`
			Address   string   `json:"address"`
			Addresses []string `json:"addresses"`
		} `json:"scriptPubKey"`
	} `json:"vout"`
}

// Config structures
type ToolConfig struct {
	Btc  ChainConfig `toml:"btc"`
	Doge ChainConfig `toml:"doge"`
}

type ChainConfig struct {
	RpcHost string `toml:"rpcHost"`
	RpcUser string `toml:"rpcUser"`
	RpcPass string `toml:"rpcPass"`
}

var toolConfig ToolConfig

func main() {
	flag.Parse()

	if *txidParam == "" {
		log.Fatal("txid is required, use -txid flag")
	}

	// Load config file directly
	if _, err := toml.DecodeFile(*configFile, &toolConfig); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	mrcDb, err := pebble.Open(*pebbleDir+"/mrc20/db", &pebble.Options{})
	if err != nil {
		log.Fatalf("Failed to open pebble database: %v", err)
	}
	defer mrcDb.Close()

	result := checkTeleportTransaction(mrcDb, *txidParam, *chainName)

	output, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(output))
}

func rpcCall(chainName, method string, params []interface{}) (json.RawMessage, error) {
	var host, user, pass string

	switch chainName {
	case "btc":
		host = toolConfig.Btc.RpcHost
		user = toolConfig.Btc.RpcUser
		pass = toolConfig.Btc.RpcPass
	case "doge":
		host = toolConfig.Doge.RpcHost
		user = toolConfig.Doge.RpcUser
		pass = toolConfig.Doge.RpcPass
	default:
		return nil, fmt.Errorf("unsupported chain: %s", chainName)
	}

	reqBody := map[string]interface{}{
		"jsonrpc": "1.0",
		"id":      "teleport_check",
		"method":  method,
		"params":  params,
	}

	bodyBytes, _ := json.Marshal(reqBody)
	req, err := http.NewRequest("POST", "http://"+host+"/", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	auth := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Content-Type", "text/plain")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var rpcResp RpcResponse
	err = json.Unmarshal(respBody, &rpcResp)
	if err != nil {
		return nil, fmt.Errorf("unmarshal error: %v, body: %s", err, string(respBody))
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("rpc error: %s", rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

func checkTeleportTransaction(mrcDb *pebble.DB, txid, chainName string) *TeleportCheckResult {
	result := &TeleportCheckResult{
		TxId:  txid,
		Chain: chainName,
	}

	// Get raw transaction
	rawResult, err := rpcCall(chainName, "getrawtransaction", []interface{}{txid, true})
	if err != nil {
		result.Error = fmt.Sprintf("Failed to get transaction: %v", err)
		return result
	}

	var txInfo TxInfo
	err = json.Unmarshal(rawResult, &txInfo)
	if err != nil {
		result.Error = fmt.Sprintf("Failed to parse transaction: %v", err)
		return result
	}

	// Parse inputs
	for i, vin := range txInfo.Vin {
		txPoint := fmt.Sprintf("%s:%d", vin.Txid, vin.Vout)
		inputInfo := InputInfo{
			Index:   i,
			TxPoint: txPoint,
		}

		utxoInfo := getUtxoFromPebble(mrcDb, txPoint)
		if utxoInfo != nil {
			inputInfo.HasUtxo = true
			inputInfo.UtxoInfo = utxoInfo
		}

		result.Inputs = append(result.Inputs, inputInfo)
	}

	// Parse outputs
	for _, vout := range txInfo.Vout {
		outputInfo := OutputInfo{
			Index: vout.N,
			Value: int64(vout.Value * 1e8),
			Type:  vout.ScriptPubKey.Type,
		}
		if vout.ScriptPubKey.Address != "" {
			outputInfo.Address = vout.ScriptPubKey.Address
		} else if len(vout.ScriptPubKey.Addresses) > 0 {
			outputInfo.Address = vout.ScriptPubKey.Addresses[0]
		}
		result.Outputs = append(result.Outputs, outputInfo)
	}

	// Parse witness for transfer data
	transferData, pinContent := parseWitnessForTransfer(txInfo)
	if len(transferData) > 0 {
		result.TransferData = transferData
		for _, td := range transferData {
			if td.Type == "teleport" {
				result.IsTeleport = true
				break
			}
		}
	}

	// Validate teleport if found
	if result.IsTeleport {
		result.ValidationResult = validateTeleport(result, mrcDb, pinContent)
	}

	return result
}

func getUtxoFromPebble(db *pebble.DB, txPoint string) *UtxoInfo {
	key := fmt.Sprintf("mrc20_utxo_%s", txPoint)
	value, closer, err := db.Get([]byte(key))
	if err != nil {
		return nil
	}
	defer closer.Close()

	var utxo mrc20.Mrc20Utxo
	err = json.Unmarshal(value, &utxo)
	if err != nil {
		return nil
	}

	statusStr := "unknown"
	switch utxo.Status {
	case 0:
		statusStr = "available"
	case 1:
		statusStr = "teleport_pending"
	case -1:
		statusStr = "spent"
	}

	return &UtxoInfo{
		TxPoint:   utxo.TxPoint,
		Mrc20Id:   utxo.Mrc20Id,
		Tick:      utxo.Tick,
		Amount:    utxo.AmtChange.String(),
		ToAddress: utxo.ToAddress,
		Status:    utxo.Status,
		StatusStr: statusStr,
	}
}

func getArrivalFromPebble(db *pebble.DB, coord string) *ArrivalInfo {
	key := fmt.Sprintf("mrc20_arrival_%s", coord)
	value, closer, err := db.Get([]byte(key))
	if err != nil {
		return nil
	}
	defer closer.Close()

	var arrival mrc20.Mrc20Arrival
	err = json.Unmarshal(value, &arrival)
	if err != nil {
		return nil
	}

	statusStr := "unknown"
	switch arrival.Status {
	case 0:
		statusStr = "pending"
	case 1:
		statusStr = "completed"
	case 2:
		statusStr = "invalid"
	}

	return &ArrivalInfo{
		PinId:         arrival.PinId,
		TxId:          arrival.TxId,
		AssetOutpoint: arrival.AssetOutpoint,
		Amount:        arrival.Amount.String(),
		TickId:        arrival.TickId,
		Chain:         arrival.Chain,
		Status:        int(arrival.Status),
		StatusStr:     statusStr,
		Msg:           arrival.Msg,
	}
}

func getPendingTeleportFromPebble(db *pebble.DB, coord string) *PendingInfo {
	coordKey := fmt.Sprintf("pending_teleport_coord_%s", coord)
	pinIdBytes, closer, err := db.Get([]byte(coordKey))
	if err != nil {
		return nil
	}
	pinId := string(pinIdBytes)
	closer.Close()

	key := fmt.Sprintf("pending_teleport_%s", pinId)
	value, closer2, err := db.Get([]byte(key))
	if err != nil {
		return nil
	}
	defer closer2.Close()

	var pending mrc20.PendingTeleport
	err = json.Unmarshal(value, &pending)
	if err != nil {
		return nil
	}

	statusStr := "unknown"
	switch pending.Status {
	case 0:
		statusStr = "pending"
	case 1:
		statusStr = "completed"
	case -1:
		statusStr = "failed"
	}

	return &PendingInfo{
		PinId:         pending.PinId,
		Coord:         pending.Coord,
		TickId:        pending.TickId,
		Amount:        pending.Amount,
		AssetOutpoint: pending.AssetOutpoint,
		TargetChain:   pending.TargetChain,
		Status:        pending.Status,
		StatusStr:     statusStr,
	}
}

func parseWitnessForTransfer(txInfo TxInfo) ([]TeleportTransferInfo, []byte) {
	var transferData []TeleportTransferInfo
	var pinContent []byte

	for _, vin := range txInfo.Vin {
		for _, witnessHexStr := range vin.Witness {
			witnessBytes, err := hex.DecodeString(witnessHexStr)
			if err != nil || len(witnessBytes) < 50 {
				continue
			}

			witnessHex := hex.EncodeToString(witnessBytes)

			startIdx := strings.Index(witnessHex, "5b7b")
			if startIdx < 0 {
				startIdx = strings.Index(witnessHex, "7b22")
			}
			if startIdx < 0 {
				continue
			}

			endIdx := strings.LastIndex(witnessHex, "7d5d")
			if endIdx < 0 {
				endIdx = strings.LastIndex(witnessHex, "7d")
			}
			if endIdx < 0 || endIdx <= startIdx {
				continue
			}

			jsonHex := witnessHex[startIdx : endIdx+2]
			if strings.HasPrefix(witnessHex[startIdx:], "5b7b") {
				jsonHex = witnessHex[startIdx : endIdx+4]
			}

			jsonBytes, err := hex.DecodeString(jsonHex)
			if err != nil {
				continue
			}

			var dataArray []TeleportTransferInfo
			err = json.Unmarshal(jsonBytes, &dataArray)
			if err == nil && len(dataArray) > 0 {
				transferData = dataArray
				pinContent = jsonBytes
				return transferData, pinContent
			}

			var singleData TeleportTransferInfo
			err = json.Unmarshal(jsonBytes, &singleData)
			if err == nil && singleData.Type != "" {
				transferData = []TeleportTransferInfo{singleData}
				pinContent = jsonBytes
				return transferData, pinContent
			}
		}
	}

	return transferData, pinContent
}

func validateTeleport(result *TeleportCheckResult, db *pebble.DB, pinContent []byte) *ValidationResult {
	validation := &ValidationResult{
		IsValid: true,
	}

	for _, td := range result.TransferData {
		if td.Type != "teleport" {
			continue
		}

		// 1. Validate required fields
		if td.Coord == "" {
			validation.Errors = append(validation.Errors, "coord is required for teleport")
			validation.IsValid = false
		}
		if td.Id == "" {
			validation.Errors = append(validation.Errors, "id (tickId) is required for teleport")
			validation.IsValid = false
		}
		if td.Amount == "" {
			validation.Errors = append(validation.Errors, "amount is required for teleport")
			validation.IsValid = false
		}
		if td.Chain == "" {
			validation.Errors = append(validation.Errors, "chain (target chain) is required for teleport")
			validation.IsValid = false
		}

		// 2. Find matching input UTXO
		var matchedUtxo *UtxoInfo
		teleportAmount, _ := decimal.NewFromString(td.Amount)

		for _, input := range result.Inputs {
			if input.UtxoInfo != nil {
				utxoAmount, _ := decimal.NewFromString(input.UtxoInfo.Amount)
				if input.UtxoInfo.Mrc20Id == td.Id && utxoAmount.Equal(teleportAmount) {
					matchedUtxo = input.UtxoInfo
					result.MatchedUtxo = matchedUtxo
					break
				}
			}
		}

		if matchedUtxo == nil {
			validation.Errors = append(validation.Errors,
				fmt.Sprintf("No matching MRC20 UTXO found in inputs for tickId=%s, amount=%s", td.Id, td.Amount))
			validation.IsValid = false

			validation.Recommendations = append(validation.Recommendations,
				"Check if the MRC20 UTXO exists in database")
			validation.Recommendations = append(validation.Recommendations,
				"Verify that the input UTXO has the correct tickId and amount")

			for _, input := range result.Inputs {
				if input.UtxoInfo != nil {
					validation.Warnings = append(validation.Warnings,
						fmt.Sprintf("Found UTXO in input %d: tickId=%s, amount=%s",
							input.Index, input.UtxoInfo.Mrc20Id, input.UtxoInfo.Amount))
				}
			}
		} else {
			if matchedUtxo.Status == -1 {
				validation.Errors = append(validation.Errors,
					fmt.Sprintf("UTXO %s is already spent (status=-1)", matchedUtxo.TxPoint))
				validation.IsValid = false
			} else if matchedUtxo.Status == 1 {
				validation.Warnings = append(validation.Warnings,
					fmt.Sprintf("UTXO %s is in teleport pending state (status=1)", matchedUtxo.TxPoint))
			}
		}

		// 3. Check arrival
		arrivalInfo := getArrivalFromPebble(db, td.Coord)
		if arrivalInfo != nil {
			result.ArrivalInfo = arrivalInfo

			if arrivalInfo.Status == 1 {
				validation.Warnings = append(validation.Warnings,
					"Arrival already completed")
			} else if arrivalInfo.Status == 2 {
				validation.Errors = append(validation.Errors,
					"Arrival is invalid")
				validation.IsValid = false
			}

			if arrivalInfo.TickId != td.Id {
				validation.Errors = append(validation.Errors,
					fmt.Sprintf("TickId mismatch: arrival=%s, teleport=%s", arrivalInfo.TickId, td.Id))
				validation.IsValid = false
			}

			arrivalAmount, _ := decimal.NewFromString(arrivalInfo.Amount)
			if !arrivalAmount.Equal(teleportAmount) {
				validation.Errors = append(validation.Errors,
					fmt.Sprintf("Amount mismatch: arrival=%s, teleport=%s", arrivalInfo.Amount, td.Amount))
				validation.IsValid = false
			}

			if matchedUtxo != nil && arrivalInfo.AssetOutpoint != matchedUtxo.TxPoint {
				validation.Errors = append(validation.Errors,
					fmt.Sprintf("AssetOutpoint mismatch: arrival expects %s, found %s",
						arrivalInfo.AssetOutpoint, matchedUtxo.TxPoint))
				validation.IsValid = false
			}
		} else {
			validation.Warnings = append(validation.Warnings,
				fmt.Sprintf("Arrival not found for coord: %s", td.Coord))
			validation.Recommendations = append(validation.Recommendations,
				"If arrival exists on target chain but not indexed yet, teleport will be queued as pending")
		}

		// 4. Check pending teleport
		pendingInfo := getPendingTeleportFromPebble(db, td.Coord)
		if pendingInfo != nil {
			result.PendingInfo = pendingInfo
			validation.Warnings = append(validation.Warnings,
				fmt.Sprintf("Pending teleport exists for coord %s (status=%s)", td.Coord, pendingInfo.StatusStr))
		}

		// 5. Check if teleport record exists
		teleportKey := fmt.Sprintf("mrc20_teleport_%s", td.Coord)
		_, closer, err := db.Get([]byte(teleportKey))
		if err == nil {
			closer.Close()
			validation.Warnings = append(validation.Warnings,
				"Teleport record already exists for this coord")
		}
	}

	return validation
}
