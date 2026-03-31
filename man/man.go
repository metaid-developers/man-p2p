package man

import (
	"fmt"
	"log"
	"man-p2p/adapter"
	"man-p2p/adapter/bitcoin"
	"man-p2p/adapter/dogecoin"
	"man-p2p/adapter/microvisionchain"
	"man-p2p/common"
	"man-p2p/pebblestore"
	"man-p2p/pin"
	"strconv"
	"strings"

	"github.com/bytedance/sonic"
	"github.com/cockroachdb/pebble"
	"github.com/schollz/progressbar/v3"
)

var (
	ChainAdapter    map[string]adapter.Chain
	IndexerAdapter  map[string]adapter.Indexer
	ChainParams     map[string]string
	MaxHeight       map[string]int64
	CurBlockHeight  map[string]int64
	BaseFilter      []string = []string{"/info", "/file", "/flow", "ft", "/metaaccess", "/metaname"}
	SyncBaseFilter  map[string]struct{}
	ProtocolsFilter map[string]struct{}

	BarMap         map[string]*progressbar.ProgressBar
	FirstCompleted bool
	IsSync         bool
	IsTestNet      bool = false
	PebbleStore    *PebbleData
	ChainList      []string // 保持链的顺序
	mempoolTaskSem chan struct{}
)

func runMempoolTask(task func()) {
	if mempoolTaskSem == nil {
		task()
		return
	}
	mempoolTaskSem <- struct{}{}
	go func() {
		defer func() {
			<-mempoolTaskSem
		}()
		task()
	}()
}

// InitAdapter 初始化区块链适配器
func InitAdapter(chainType, dbType, test, server string) {
	InitRuntime(chainType, dbType, test, server, true)
}

func InitRuntime(chainType, dbType, test, server string, enableChain bool) {
	PebbleStore = &PebbleData{}
	PebbleStore.Init(common.Config.Pebble.Num)
	common.Chain = chainType
	ChainAdapter = make(map[string]adapter.Chain)
	ChainParams = make(map[string]string)
	IndexerAdapter = make(map[string]adapter.Indexer)
	MaxHeight = make(map[string]int64)
	CurBlockHeight = make(map[string]int64)
	ProtocolsFilter = make(map[string]struct{})
	SyncBaseFilter = make(map[string]struct{})
	BarMap = make(map[string]*progressbar.ProgressBar)
	mempoolTaskSem = make(chan struct{}, 256)
	syncConfig := common.Config.Sync
	if len(syncConfig.SyncProtocols) > 0 {
		for _, f := range BaseFilter {
			SyncBaseFilter[f] = struct{}{}
		}
		for _, protocol := range syncConfig.SyncProtocols {
			p := strings.ToLower("/protocols/" + protocol)
			ProtocolsFilter[p] = struct{}{}
		}
	}
	chainList := strings.Split(chainType, ",")
	ChainList = chainList // 保存顺序
	if !enableChain {
		return
	}
	for _, chain := range chainList {
		ChainParams[chain] = "mainnet"
		if test == "1" {
			ChainParams[chain] = "testnet"
			IsTestNet = true
		}
		if test == "2" && chain == "btc" {
			IsTestNet = true
			ChainParams[chain] = "regtest"
		}
		switch chain {
		case "btc":
			ChainAdapter[chain] = &bitcoin.BitcoinChain{}
			IndexerAdapter[chain] = &bitcoin.Indexer{
				ChainParams: ChainParams[chain],
				PopCutNum:   common.Config.Btc.PopCutNum,
				ChainName:   chain,
			}
		case "mvc":
			ChainAdapter[chain] = &microvisionchain.MicroVisionChain{}
			IndexerAdapter[chain] = &microvisionchain.Indexer{
				ChainParams: ChainParams[chain],
				PopCutNum:   common.Config.Mvc.PopCutNum,
				ChainName:   chain,
			}
		case "doge":
			ChainAdapter[chain] = &dogecoin.DogecoinChain{}
			IndexerAdapter[chain] = &dogecoin.Indexer{
				ChainParams: ChainParams[chain],
				PopCutNum:   common.Config.Doge.PopCutNum,
				ChainName:   chain,
			}
		}
		ChainAdapter[chain].InitChain()
		IndexerAdapter[chain].InitIndexer()
		//bestHeight := ChainAdapter[chain].GetBestHeight()
		//TODO bestHeight写入数据库
	}

	// 启动时修复所有 pending 状态的 UTXO（确保 mempool 中的状态与链上一致）
	for _, chain := range chainList {
		fixed, err := PebbleStore.FixPendingUtxoStatus(chain)
		if err != nil {
			log.Printf("[WARN] FixPendingUtxoStatus failed for %s: %v", chain, err)
		} else if fixed > 0 {
			log.Printf("[MRC20] FixPendingUtxoStatus: fixed %d pending UTXOs for %s", fixed, chain)
		}
	}
}

// ZmqRun 启动ZMQ监听
// TODO 支持同一个链接监听多个节点
func ZmqRun() {
	for chain, indexer := range IndexerAdapter {
		go doZmqRun(chain, indexer)
	}

}
func doZmqRun(chain string, indexer adapter.Indexer) {
	msg := make(chan pin.MempollChanMsg, 64)
	go indexer.ZmqRun(msg)
	if err := syncExistingMempool(chain, ChainAdapter[chain], indexer, processMempoolPin); err != nil {
		log.Printf("[WARN] startup mempool sync failed for %s: %v", chain, err)
	}
	for x := range msg {
		processMempoolMsg(x)
	}
}

func syncExistingMempool(chain string, chainAdapter adapter.Chain, indexer adapter.Indexer, handler func(*pin.PinInscription)) error {
	txList, err := chainAdapter.GetMempoolTransactionList()
	if err != nil {
		return err
	}
	if len(txList) == 0 {
		return nil
	}
	pinList, _ := indexer.CatchMempoolPins(txList)
	for _, pinNode := range pinList {
		handler(pinNode)
	}
	log.Printf("[Mempool] startup sync for %s: %d txs, %d pins", chain, len(txList), len(pinList))
	return nil
}

func processMempoolMsg(x pin.MempollChanMsg) {
	for _, pinNode := range x.PinList {
		processMempoolPin(pinNode)
	}
}

func processMempoolPin(pinNode *pin.PinInscription) {
	if pinNode == nil {
		return
	}
	onlyHost := common.Config.MetaSo.OnlyHost
	if onlyHost != "" && pinNode.Host != onlyHost {
		return
	}
	node := pinNode
	runMempoolTask(func() {
		handleMempoolPin(node)
	})
	runMempoolTask(func() {
		handleMetaIdInfo(&[]*pin.PinInscription{node})
	})
}

// IndexerRun 执行区块链索引
func IndexerRun(test string) {
	// 使用 ChainList 保证按 -chain 参数顺序索引
	for _, chainName := range ChainList {
		from, to := getSyncHeight(chainName, test)
		//log.Printf("IndexerRun for chain: %s, from: %d, to: %d", chainName, from, to)
		if from >= to {
			FirstCompleted = true
			continue
		}
		barinfo := fmt.Sprintf("[%s %d-%d]", chainName, from, to)
		BarMap[chainName] = progressbar.Default(to-from, barinfo)
		for i := from + 1; i <= to; i++ {
			//DoIndexerRun(chainName, i, false)
			//startTime := time.Now()
			//log.Println("=====", chainName, i, "======")
			PebbleStore.DoIndexerRun(chainName, i, false)
			//log.Println("==========finish use", time.Since(startTime), "=================")
			BarMap[chainName].Add(1)
			syncKey := fmt.Sprintf("%s_sync_height", chainName)
			PebbleStore.Database.MetaDb.Set([]byte(syncKey), []byte(strconv.FormatInt(i, 10)), pebble.Sync)
			//PebbleStore.Database.StatPinSortTable()
		}

	}
	FirstCompleted = true

}

// Mrc20CatchUpRun MRC20 补索引：处理从 mrc20Height 到当前主索引高度之间的区块
func Mrc20CatchUpRun() {
	if !isModuleEnabled("mrc20") {
		return
	}

	// 使用 ChainList 保证按 -chain 参数顺序处理
	for _, chainName := range ChainList {
		// 获取 MRC20 配置的启动高度
		var mrc20StartHeight int64
		switch chainName {
		case "btc":
			mrc20StartHeight = common.Config.Btc.Mrc20Height
		case "mvc":
			mrc20StartHeight = common.Config.Mvc.Mrc20Height
		case "doge":
			mrc20StartHeight = common.Config.Doge.Mrc20Height
		default:
			continue
		}

		if mrc20StartHeight <= 0 {
			continue
		}

		// 获取 MRC20 当前索引进度
		mrc20CurrentHeight := PebbleStore.GetMrc20IndexHeight(chainName)

		// 获取主索引进度
		syncKey := fmt.Sprintf("%s_sync_height", chainName)
		dbHeight, closer, err := PebbleStore.Database.MetaDb.Get([]byte(syncKey))
		var mainHeight int64
		if err != nil || len(dbHeight) == 0 {
			mainHeight = 0
			if closer != nil {
				closer.Close()
			}
		} else {
			mainHeight, _ = strconv.ParseInt(string(dbHeight), 10, 64)
			closer.Close()
		}

		// MRC20-ONLY 模式特殊处理：如果 MRC20 进度更高，同步主索引进度
		if common.Config.Sync.Mrc20Only && mrc20CurrentHeight > mainHeight {
			log.Printf("[MRC20-ONLY] Syncing main index height to MRC20 height for %s: %d -> %d",
				chainName, mainHeight, mrc20CurrentHeight)
			PebbleStore.Database.MetaDb.Set([]byte(syncKey), []byte(strconv.FormatInt(mrc20CurrentHeight, 10)), pebble.Sync)
			mainHeight = mrc20CurrentHeight
			log.Printf("[MRC20-ONLY] Main index height updated. MRC20 will continue from %d", mrc20CurrentHeight+1)
			continue // 不需要补索引，直接进入 ZMQ 模式
		}

		// 如果 MRC20 还未开始索引，从启动高度开始
		from := mrc20CurrentHeight
		if from == 0 {
			from = mrc20StartHeight - 1
		}

		to := mainHeight

		// 如果 MRC20 索引落后于主索引，需要补索引
		if from < to && to >= mrc20StartHeight {
			log.Printf("MRC20 catch-up for chain: %s, from: %d, to: %d", chainName, from+1, to)

			barinfo := fmt.Sprintf("[MRC20 %s %d-%d]", chainName, from+1, to)
			bar := progressbar.Default(to-from, barinfo)

			for height := from + 1; height <= to; height++ {
				// 读取该区块的 PIN 数据（已经被主索引处理过）
				// CatchPins 返回 (pinList, txInList, creatorMap)，第三个参数不是 error
				pinList, txInList, _ := IndexerAdapter[chainName].CatchPins(height)

				// 只处理 MRC20
				PebbleStore.handleMrc20(chainName, height, pinList, txInList)

				bar.Add(1)

				// 定期输出进度（每100个区块）
				if height%100 == 0 {
					log.Printf("MRC20 catch-up progress for %s: %d/%d", chainName, height, to)
				}
			}

			log.Printf("MRC20 catch-up completed for chain: %s", chainName)
		} else if mrc20CurrentHeight >= mainHeight {
			log.Printf("MRC20 for chain %s is up to date: %d", chainName, mrc20CurrentHeight)
		}
	}
}

// getSyncHeight 获取需要同步的区块高度范围
func getSyncHeight(chainName string, test string) (from, to int64) {
	//initialHeight := ChainAdapter[chainName].GetInitialHeight()
	// Always use the initialHeight from config for each chain
	initialHeight := ChainAdapter[chainName].GetInitialHeight()

	// Fallback to hardcoded values only if config is not set
	if test == "" && initialHeight == 0 {
		if chainName == "mvc" {
			initialHeight = int64(86500)
		} else if chainName == "btc" {
			initialHeight = int64(844446)
		} else if chainName == "doge" {
			initialHeight = int64(6005462)
		}
	}
	dbLast := make(map[string]int64)
	syncKey := fmt.Sprintf("%s_sync_height", chainName)
	dbHeight, closer, err := PebbleStore.Database.MetaDb.Get([]byte(syncKey))
	if err == nil && len(dbHeight) > 0 {
		height, err := strconv.ParseInt(string(dbHeight), 10, 64)
		if err == nil {
			dbLast[chainName] = height
		}
	}
	if err == nil {
		closer.Close()
	}
	if MaxHeight[chainName] <= 0 {
		MaxHeight[chainName] = dbLast[chainName]
	}
	bestHeight := ChainAdapter[chainName].GetBestHeight()
	if MaxHeight[chainName] >= bestHeight || initialHeight > bestHeight {
		return
	}
	if MaxHeight[chainName] < initialHeight {
		from = initialHeight
	} else {
		from = MaxHeight[chainName]
	}
	to = bestHeight
	return
}

// CheckNewBlock 检查新块，删除mempool数据
func CheckNewBlock() {
	for k, chain := range ChainAdapter {
		bestHeight := chain.GetBestHeight()
		deleteKey := fmt.Sprintf("%s_del_mempool_height", k)
		dbHeight, closer, err := PebbleStore.Database.MetaDb.Get([]byte(deleteKey))
		if err != nil {
			continue
		}
		closer.Close()
		localLastHeight, err := strconv.ParseInt(string(dbHeight), 10, 64)
		if err != nil {
			continue
		}
		if localLastHeight <= 0 {
			PebbleStore.Database.MetaDb.Set([]byte(deleteKey), []byte(strconv.FormatInt(bestHeight, 10)), pebble.Sync)
			continue
		}
		if localLastHeight >= bestHeight {
			continue
		}
		for i := localLastHeight; i <= bestHeight; i++ {
			log.Printf("DeleteMempoolData, chain=%s, height=%d", k, i)
			DeleteMempoolData(i, k)
			PebbleStore.Database.MetaDb.Set([]byte(deleteKey), []byte(strconv.FormatInt(i, 10)), pebble.Sync)
		}
	}
}

// DeleteMempoolData 删除mempool数据
// 优化版本：从数据库读取PIN列表，避免重复解析区块
func DeleteMempoolData(bestHeight int64, chainName string) {
	// 从数据库获取该区块的PIN ID列表
	result, err := PebbleStore.Database.GetlBlocksDB(chainName, int(bestHeight))
	if err != nil || result == nil || *result == "" {
		// 如果数据库中没有数据，说明该区块还未被索引或没有inscription
		// 此时无需删除mempool数据
		return
	}

	// 解析PIN ID列表
	pinIds := strings.Split(*result, ",")
	if len(pinIds) == 0 {
		return
	}

	// 批量从数据库获取PIN数据
	pinDataMap := PebbleStore.Database.BatchGetPinByKeys(pinIds, false)

	var pinIdList []string
	var addressList []string
	var pathList []string
	defer func() {
		pinIdList = nil
		addressList = nil
		pathList = nil
	}()

	blockTime := int64(4096715623)
	publicKeyStr := common.ConcatBytesOptimized([]string{fmt.Sprintf("%010d", blockTime), "&", chainName, "&", fmt.Sprintf("%010d", -1)}, "")

	// 遍历PIN数据，生成需要删除的key
	for pinId, pinData := range pinDataMap {
		if len(pinData) == 0 {
			continue
		}

		var p pin.PinInscription
		if err := sonic.Unmarshal(pinData, &p); err != nil {
			continue
		}

		pinIdList = append(pinIdList, pinId)
		addressKey := pin.GenAddressSortKey(&p, blockTime, chainName, -1)
		addressList = append(addressList, addressKey)
		pathKey := pin.GenPathSortKey(&p, blockTime, chainName, -1)
		pathList = append(pathList, pathKey)
	}

	// 批量删除mempool数据
	pebblestore.DeleteBatchByKeyList(PebbleStore.Database.PinsMempoolDb, &pinIdList)
	// 删除Address数据
	pebblestore.DeleteBatchByKeyList(PebbleStore.Database.AddressDB, &addressList)
	// 删除path数据
	pebblestore.DeleteBatchByKeyList(PebbleStore.Database.PathPinDB, &pathList)
	// 删除block数据
	PebbleStore.Database.BlocksDB.Delete([]byte(publicKeyStr), pebble.Sync)
}
