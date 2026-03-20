package man

import (
	"fmt"
	"strconv"

	"man-p2p/adapter/dogecoin"
	"man-p2p/common"
	"man-p2p/pebblestore"

	"man-p2p/pin"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bytedance/sonic"
	"github.com/cockroachdb/pebble"
)

type PebbleData struct {
	Database *pebblestore.Database
}

func (pd *PebbleData) Init(shardNum int) (err error) {
	dbPath := strings.TrimSpace(common.Config.Pebble.Dir)
	if dbPath == "" {
		dbPath = "./man_base_data_pebble"
	}
	dbPath = filepath.Clean(dbPath)
	err = os.MkdirAll(dbPath, 0755)
	if err != nil {
		return
	}
	pd.Database, err = pebblestore.NewDataBase(dbPath, shardNum)
	return
}

func (pd *PebbleData) DoIndexerRun(chainName string, height int64, reIndex bool) (err error) {
	if !reIndex {
		MaxHeight[chainName] = height
	}

	// MRC20 Only 模式：只处理 MRC20，跳过其他所有 PIN 数据处理
	if common.Config.Sync.Mrc20Only {
		return pd.doMrc20OnlyRun(chainName, height)
	}

	txInList := &[]string{}
	pinList := &[]*pin.PinInscription{}
	pinList, txInList, _ = IndexerAdapter[chainName].CatchPins(height)

	// 对于 Doge 链，从 Indexer 获取交易缓存并设置到 MRC20 处理器
	// 这样 MRC20 处理时可以从缓存获取交易，避免 GetTransaction RPC 调用失败（Doge 节点没有 txindex）
	if chainName == "doge" {
		if dogeIndexer, ok := IndexerAdapter[chainName].(*dogecoin.Indexer); ok && dogeIndexer.TxCache != nil {
			SetDogeTxCache(dogeIndexer.TxCache)
		}
	}

	//保存PIN数据
	if len(*pinList) > 0 {
		//fmt.Println("SetAllPins start height:", height, " Num:", len(pinList))
		pd.Database.SetAllPins(height, *pinList, 20000)
		tmp := (*pinList)[0]
		blockKey := fmt.Sprintf("blocktime_%s_%d", chainName, height)
		pd.Database.CountSet(blockKey, tmp.Timestamp)
	}

	//处理modify/revoke操作
	handlePathAndOperation(pinList)
	//处理metaid信息更新，注意要先处理path(modify)操作
	handleMetaIdInfo(pinList)
	//处理通知（区块确认后）
	for _, pinNode := range *pinList {
		handNotifcation(pinNode)
	}
	//处理转移操作
	if common.Config.Sync.IsFullNode {
		pd.handleTransfer(chainName, *txInList, height)
	}

	// 处理 MRC20（只有当 MRC20 进度已追上时才处理）
	if isModuleEnabled("mrc20") {
		pd.handleMrc20(chainName, height, pinList, txInList)
	}

	pinList = nil
	txInList = nil
	if FirstCompleted {
		DeleteMempoolData(height, chainName)
	}
	return
}

// doMrc20OnlyRun 只处理 MRC20 相关数据，跳过 PIN 存储和其他操作
func (pd *PebbleData) doMrc20OnlyRun(chainName string, height int64) (err error) {
	txInList := &[]string{}
	pinList := &[]*pin.PinInscription{}
	pinList, txInList, _ = IndexerAdapter[chainName].CatchPins(height)

	// 对于 Doge 链，从 Indexer 获取交易缓存并设置到 MRC20 处理器
	if chainName == "doge" {
		if dogeIndexer, ok := IndexerAdapter[chainName].(*dogecoin.Indexer); ok && dogeIndexer.TxCache != nil {
			SetDogeTxCache(dogeIndexer.TxCache)
		}
	}

	// 只处理 MRC20
	if isModuleEnabled("mrc20") {
		pd.handleMrc20(chainName, height, pinList, txInList)
	}

	pinList = nil
	txInList = nil
	return
}

// Set PinId from block data
func (pd *PebbleData) SetPinIdList(chainName string, height int64) (err error) {
	pins, _, _ := IndexerAdapter[chainName].CatchPins(height)
	var pinIdList []string
	if len(*pins) <= 0 {
		return
	}
	for _, pinNode := range *pins {
		pinIdList = append(pinIdList, pinNode.Id)
	}
	blockTime := (*pins)[0].Timestamp
	publicKeyStr := common.ConcatBytesOptimized([]string{fmt.Sprintf("%010d", blockTime), "&", chainName, "&", fmt.Sprintf("%010d", height)}, "")
	pd.Database.InsertBlockTxs(publicKeyStr, strings.Join(pinIdList, ","))
	pinIdList = nil
	fmt.Println(">> SetPinIdList done for height:", chainName, height)
	return
}

// GetMrc20IndexHeight 获取 MRC20 索引进度
func (pd *PebbleData) GetMrc20IndexHeight(chainName string) int64 {
	syncKey := fmt.Sprintf("%s_mrc20_sync_height", chainName)
	dbHeight, closer, err := pd.Database.MetaDb.Get([]byte(syncKey))
	if err == nil && len(dbHeight) > 0 {
		defer closer.Close()
		height, err := strconv.ParseInt(string(dbHeight), 10, 64)
		if err == nil {
			return height
		}
	}
	if closer != nil {
		closer.Close()
	}
	return 0
}

// SaveMrc20IndexHeight 保存 MRC20 索引进度
func (pd *PebbleData) SaveMrc20IndexHeight(chainName string, height int64) error {
	syncKey := fmt.Sprintf("%s_mrc20_sync_height", chainName)
	return pd.Database.MetaDb.Set([]byte(syncKey), []byte(strconv.FormatInt(height, 10)), pebble.Sync)
}

func (pd *PebbleData) handleTransfer(chainName string, outputList []string, blockHeight int64) {
	defer func() {
		outputList = outputList[:0]
	}()
	transferCheck, err := pd.Database.GetPinListByIdList(outputList, 1000, true)
	if err == nil && len(transferCheck) > 0 {
		idMap := make(map[string]string)
		for _, t := range transferCheck {
			idMap[t.Output] = t.Address
		}
		transferMap := IndexerAdapter[chainName].CatchTransfer(idMap)
		pd.Database.UpdateTransferPin(transferMap)
		var transferHistoryList []*pin.PinTransferHistory
		transferTime := time.Now().Unix()
		for pinid, info := range transferMap {
			transferHistoryList = append(transferHistoryList, &pin.PinTransferHistory{
				PinId:          strings.ReplaceAll(pinid, ":", "i"),
				TransferTime:   transferTime,
				TransferHeight: blockHeight,
				TransferTx:     info.Location,
				ChainName:      chainName,
				FromAddress:    info.FromAddress,
				ToAddress:      info.Address,
			})
		}
		pd.Database.BatchInsertTransferPins(&transferHistoryList)
		idMap = nil
		transferMap = nil
		transferHistoryList = transferHistoryList[:0]
		transferHistoryList = nil
	}
}

func (pd *PebbleData) GetPinById(pinid string) (pinNode pin.PinInscription, err error) {
	result, err := pd.Database.GetPinByKey(pinid)
	if err != nil {
		return
	}
	err = sonic.Unmarshal(result, &pinNode)
	return
}

func (pd *PebbleData) GetPinByMetaIdAndPathPageList(metaid, path string, cursor string, size int64) (pinList []*pin.PinInscription, total int64, nextCursor string, err error) {
	// AddressDB: key是metaid&path&blockTime&chainName&height&pinId
	//c8e01f2e5a8aa4558290f72ced9c8acd474800cd1eb9ab33030b626796586838&929493665e09c2d991b894f15417fd4811357e8cda1360b676ff0f7e9f155c50&1761510013&btc&0000000440
	db := pd.Database.AddressDB
	pathHash := common.GetMetaIdByAddress(path)
	prefix := metaid + "&" + pathHash + "&"
	prefixBytes := []byte(prefix)
	it, err := db.NewIter(nil)
	if err != nil {
		return nil, 0, "", err
	}
	defer it.Close()

	// 从统计表获取总数
	cntKey := metaid + "_" + pathHash + "_count"
	val, closer, err1 := pd.Database.CountDB.Get([]byte(cntKey))
	if err1 == nil {
		total, _ = strconv.ParseInt(string(val), 10, 64)
		closer.Close()
	}

	// 判断是否为首页查询
	isFirstPage := cursor == "" || cursor == "0"

	// 定位迭代器
	if !isFirstPage {
		// 从游标位置继续
		it.SeekLT([]byte(cursor))
		if !it.Valid() {
			return pinList, total, "", nil
		}
	} else {
		// 首页：从最新的开始
		it.SeekLT(append(prefixBytes, 0xff))
		if !it.Valid() {
			// 尝试从头开始找到最后一个
			it.SeekGE(prefixBytes)
			if !it.Valid() {
				return pinList, total, "", nil
			}
			for it.Next() {
				key := it.Key()
				if len(key) < len(prefixBytes) || string(key[:len(prefixBytes)]) != prefix {
					it.Prev()
					break
				}
			}
			if !it.Valid() {
				it.Last()
			}
		}
	}

	// 收集 pinId（逆序遍历）
	pinIds := make([]string, 0, size)
	keys := make([]string, 0, size)
	pinIdSet := make(map[string]bool) // 用于去重
	var count int64

	for it.Valid() && count < size {
		key := it.Key()
		if len(key) < len(prefixBytes) || string(key[:len(prefixBytes)]) != prefix {
			break
		}

		keyStr := string(key)

		// 提取 pinId（第6个字段）
		sepCount := 0
		startIdx := -1
		for i := 0; i < len(key); i++ {
			if key[i] == '&' {
				sepCount++
				if sepCount == 5 {
					startIdx = i + 1
					break
				}
			}
		}
		if startIdx > 0 && startIdx < len(key) {
			pinId := string(key[startIdx:])
			// 检查是否已经存在，避免重复
			if !pinIdSet[pinId] {
				pinIdSet[pinId] = true
				pinIds = append(pinIds, pinId)
				keys = append(keys, keyStr)
				count++
			}
		}
		it.Prev()
	}

	// 批量查询 pin 数据
	if len(pinIds) > 0 {
		pinDataMap := pd.Database.BatchGetPinByKeys(pinIds, false)
		for _, pinId := range pinIds {
			if data, ok := pinDataMap[pinId]; ok {
				var pinNode pin.PinInscription
				if err := sonic.Unmarshal(data, &pinNode); err == nil {
					pinNode.ContentSummary = string(pinNode.ContentBody)
					pinNode.ContentBody = []byte{}
					pinList = append(pinList, &pinNode)
				}
			}
		}
		// 设置下一页游标
		if len(keys) > 0 {
			nextCursor = keys[len(keys)-1]
		}
	}

	return pinList, total, nextCursor, err
}
func (pd *PebbleData) GetAllPinByPathPageList(path string, cursor string, size int64) (pinList []*pin.PinInscription, total int64, nextCursor string, err error) {
	//key是 path&blockTime&chainName&height&pinId
	//5d7b6c2f61327986929fac2888fc1de467248d99bf80edc23cf7f45d87394068&1757984310&btc&0000000432&58be6bc384edc7e2709462bcee4ffa2e265e086f753456db22368cfe0162fdb8i0
	prefix := common.GetMetaIdByAddress(path) + "&"
	prefixBytes := []byte(prefix)
	db := pd.Database.PathPinDB
	it, err := db.NewIter(nil)
	if err != nil {
		return nil, 0, "", err
	}
	defer it.Close()

	// 第一步：从统计表获取总数
	// 注意：统计表的 key 使用的是 hash(path)，与 PathPinDB 的 prefix 一致
	pathHash := common.GetMetaIdByAddress(path)
	cntKey := pathHash + "_count"
	val, closer, err1 := pd.Database.CountDB.Get([]byte(cntKey))
	if err1 == nil {
		total, _ = strconv.ParseInt(string(val), 10, 64)
		closer.Close()
	}

	// 判断是否为首页查询（cursor 为空或为 "0"）
	isFirstPage := cursor == "" || cursor == "0"

	// 如果有有效游标，使用游标定位
	if !isFirstPage {
		// 从游标的下一个位置开始（逆序继续）
		it.SeekLT([]byte(cursor))
		if !it.Valid() {
			return pinList, total, "", nil
		}
	} else {
		// 首页：直接从最新的开始
		it.SeekLT(append(prefixBytes, 0xff))
		if !it.Valid() {
			// 没有数据，尝试从头开始
			it.SeekGE(prefixBytes)
			if !it.Valid() {
				return pinList, total, "", nil
			}
			// 找到最后一个
			for it.Next() {
				key := it.Key()
				if len(key) < len(prefixBytes) || string(key[:len(prefixBytes)]) != prefix {
					it.Prev()
					break
				}
			}
			if !it.Valid() {
				it.Last()
			}
		}
	}

	// 第二步：收集所有需要的 pinId（逆序收集，保持时间倒序）
	pinIds := make([]string, 0, size)
	keys := make([]string, 0, size)
	pinIdSet := make(map[string]bool) // 用于去重
	var count int64

	// 逆序收集（使用 Prev）
	for it.Valid() && count < size {
		key := it.Key()
		if len(key) < len(prefixBytes) || string(key[:len(prefixBytes)]) != prefix {
			break
		}

		keyStr := string(key)

		// 直接从字节切片中提取 pinId
		sepCount := 0
		startIdx := -1
		for i := 0; i < len(key); i++ {
			if key[i] == '&' {
				sepCount++
				if sepCount == 4 {
					startIdx = i + 1
					break
				}
			}
		}
		if startIdx > 0 && startIdx < len(key) {
			pinId := string(key[startIdx:])
			// 检查是否已经存在，避免重复
			if !pinIdSet[pinId] {
				pinIdSet[pinId] = true
				pinIds = append(pinIds, pinId)
				keys = append(keys, keyStr)
				count++
			}
		}
		it.Prev()
	}

	// 第三步：批量查询所有 pin 数据
	if len(pinIds) > 0 {
		pinDataMap := pd.Database.BatchGetPinByKeys(pinIds, false)
		// 按照 pinIds 的顺序组装结果（已经是时间倒序）
		for _, pinId := range pinIds {
			if data, ok := pinDataMap[pinId]; ok {
				var pinNode pin.PinInscription
				if err := sonic.Unmarshal(data, &pinNode); err == nil {
					// 在这里设置 ContentSummary 并清空 ContentBody，减少 API 层的处理
					pinNode.ContentSummary = string(pinNode.ContentBody)
					pinNode.ContentBody = []byte{}
					pinList = append(pinList, &pinNode)
				}
			}
		}
		// 设置下一页游标为最后一个 key（用于继续逆序遍历）
		if len(keys) > 0 {
			nextCursor = keys[len(keys)-1]
		}
	}

	return pinList, total, nextCursor, err
}

// isModuleEnabled 检查模块是否启用
func isModuleEnabled(moduleName string) bool {
	for _, m := range common.Config.Module {
		if m == moduleName {
			return true
		}
	}
	return false
}

// handleMrc20 处理 MRC20 相关交易
func (pd *PebbleData) handleMrc20(chainName string, height int64, pinList *[]*pin.PinInscription, txInList *[]string) {
	// 获取该链的 MRC20 启动高度
	var mrc20Height int64
	switch chainName {
	case "btc":
		mrc20Height = common.Config.Btc.Mrc20Height
	case "mvc":
		mrc20Height = common.Config.Mvc.Mrc20Height
	case "doge":
		mrc20Height = common.Config.Doge.Mrc20Height
	default:
		//log.Printf("[MRC20] Unknown chain: %s", chainName)
		return
	}

	//log.Printf("[MRC20] handleMrc20 called: chain=%s, height=%d, mrc20Height=%d, pinList=%d", chainName, height, mrc20Height, len(*pinList))

	// 检查是否配置了 MRC20 启动高度
	// mrc20Height < 0 表示禁用 MRC20，mrc20Height >= 0 表示从该高度开始启用
	if mrc20Height < 0 {
		//log.Printf("[MRC20] Disabled for chain %s (mrc20Height=%d)", chainName, mrc20Height)
		return
	}

	// 检查当前区块是否达到 MRC20 启动高度
	if height < mrc20Height {
		//log.Printf("[MRC20] Block height %d < mrc20Height %d, skipping", height, mrc20Height)
		return
	}

	// 【关键】检查 MRC20 进度是否已追上
	// MRC20 必须严格按顺序处理，不能跳过中间的区块
	mrc20CurrentHeight := pd.GetMrc20IndexHeight(chainName)
	//log.Printf("[MRC20] mrc20CurrentHeight=%d, height=%d", mrc20CurrentHeight, height)
	if mrc20CurrentHeight > 0 && mrc20CurrentHeight < height-1 {
		// MRC20 进度落后，需要先补索引，跳过当前区块
		// 例如：mrc20CurrentHeight=820000, height=850000
		// 必须先处理 820001-849999，才能处理 850000
		//log.Printf("[MRC20] Progress behind, need catch-up: current=%d, target=%d", mrc20CurrentHeight, height)
		return
	}

	// 筛选 MRC20 相关的 PIN
	var mrc20List []*pin.PinInscription
	mrc20TransferPinTx := make(map[string]struct{})

	//log.Printf("[DEBUG] handleMrc20: height=%d, pinList count=%d", height, len(*pinList))
	for _, pinNode := range *pinList {
		if strings.HasPrefix(pinNode.Path, "/ft/mrc20/") {
			mrc20List = append(mrc20List, pinNode)
			//log.Printf("[DEBUG] handleMrc20: found MRC20 PIN, path=%s, id=%s, genesisTx=%s", pinNode.Path, pinNode.Id, pinNode.GenesisTransaction)
			if pinNode.Path == "/ft/mrc20/transfer" {
				mrc20TransferPinTx[pinNode.GenesisTransaction] = struct{}{}
				//log.Printf("[DEBUG] handleMrc20: added Transfer PIN to mrc20TransferPinTx: %s", pinNode.GenesisTransaction)
			}
		}
	}
	//log.Printf("[DEBUG] handleMrc20: mrc20TransferPinTx count=%d", len(mrc20TransferPinTx))

	//log.Printf("[MRC20] Found %d MRC20 PINs in block %d", len(mrc20List), height)

	// 调用 MRC20 处理函数
	// 注意：即使没有 MRC20 PIN，也需要处理 Native Transfer（通过 txInList）
	// txInList 包含该区块所有交易的输入，可能花费了之前区块创建的 MRC20 UTXO
	Mrc20Handle(chainName, height, mrc20List, mrc20TransferPinTx, *txInList, false)

	// 保存 MRC20 索引进度
	pd.SaveMrc20IndexHeight(chainName, height)
	//log.Printf("[MRC20] Saved progress for %s: height=%d", chainName, height)
}
