package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"man-p2p/mrc20"
	"man-p2p/pebblestore"

	"github.com/bytedance/sonic"
	"github.com/cockroachdb/pebble"
	"github.com/shopspring/decimal"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	mongoURI       = flag.String("mongo", "mongodb://localhost:27017", "MongoDB connection URI")
	dbName         = flag.String("db", "metaso", "MongoDB database name")
	pebbleDir      = flag.String("pebble", "./man_base_data_pebble", "PebbleDB directory")
	chainName      = flag.String("chain", "btc", "Chain name (btc/mvc/doge)")
	continueHeight = flag.Int64("height", 0, "Block height to continue indexing from (set mrc20_sync_height)")
	endHeight      = flag.Int64("end-height", 0, "Only migrate data with blockheight < end-height, and auto-reset UTXOs spent after this height")
	dryRun         = flag.Bool("dry-run", false, "Dry run mode, only show statistics")
	batchSize      = flag.Int("batch", 1000, "Batch size for import")
)

type MigrationStats struct {
	UtxoCount      int64
	TickCount      int64
	ShovelCount    int64
	AddressCount   int64
	OperationCount int64
	StartTime      time.Time
	EndTime        time.Time
}

func main() {
	flag.Parse()

	if *continueHeight <= 0 {
		log.Fatal("Please specify --height to set the starting block height for new indexing")
	}

	log.Printf("=== MRC20 Data Migration Tool ===")
	log.Printf("MongoDB: %s/%s", *mongoURI, *dbName)
	log.Printf("PebbleDB: %s", *pebbleDir)
	log.Printf("Chain: %s", *chainName)
	log.Printf("Continue from height: %d", *continueHeight)
	if *endHeight > 0 {
		log.Printf("End height filter: < %d (only migrate data before this height)", *endHeight)
		log.Printf("UTXO status reset: auto (UTXOs spent at height >= %d will be reset to status=0)", *endHeight)
	}
	log.Printf("Dry run: %v", *dryRun)
	log.Printf("=================================\n")

	stats := &MigrationStats{StartTime: time.Now()}

	// 1. Connect to MongoDB
	mongoClient, err := connectMongo()
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}
	defer mongoClient.Disconnect(context.Background())

	// 2. Open PebbleDB
	var pebbleDB *pebblestore.Database
	if !*dryRun {
		pebbleDB, err = openPebbleDB()
		if err != nil {
			log.Fatalf("Failed to open PebbleDB: %v", err)
		}
		defer pebbleDB.Close()
	}

	// 3. Export and import data
	log.Println("\n[1/5] Migrating MRC20 Tick (token info)...")
	if err := migrateTickData(mongoClient, pebbleDB, stats); err != nil {
		log.Fatalf("Failed to migrate tick data: %v", err)
	}

	log.Println("\n[2/5] Migrating MRC20 UTXO (balances)...")
	if err := migrateUtxoData(mongoClient, pebbleDB, stats); err != nil {
		log.Fatalf("Failed to migrate UTXO data: %v", err)
	}

	log.Println("\n[3/5] Migrating MRC20 Shovel (used PINs)...")
	if err := migrateShovelData(mongoClient, pebbleDB, stats); err != nil {
		log.Fatalf("Failed to migrate shovel data: %v", err)
	}

	log.Println("\n[4/5] Migrating MRC20 Operation TX...")
	if err := migrateOperationTx(mongoClient, pebbleDB, stats); err != nil {
		log.Fatalf("Failed to migrate operation tx: %v", err)
	}

	log.Println("\n[5/5] Setting MRC20 index height...")
	if !*dryRun && *continueHeight > 0 {
		if err := setIndexHeight(pebbleDB); err != nil {
			log.Fatalf("Failed to set index height: %v", err)
		}
	} else if *continueHeight == 0 {
		log.Printf("⚠ Skipping height setting (continueHeight not specified)")
	}

	stats.EndTime = time.Now()
	printStats(stats)
}

func connectMongo() (*mongo.Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(*mongoURI))
	if err != nil {
		return nil, err
	}

	// Test connection
	if err := client.Ping(ctx, nil); err != nil {
		return nil, err
	}

	log.Println("✓ Connected to MongoDB")
	return client, nil
}

func openPebbleDB() (*pebblestore.Database, error) {
	if _, err := os.Stat(*pebbleDir); os.IsNotExist(err) {
		if err := os.MkdirAll(*pebbleDir, 0755); err != nil {
			return nil, err
		}
	}

	db, err := pebblestore.NewDataBase(*pebbleDir, 16)
	if err != nil {
		return nil, err
	}

	log.Println("✓ Opened PebbleDB")
	return db, nil
}

func migrateTickData(mongoClient *mongo.Client, pebbleDB *pebblestore.Database, stats *MigrationStats) error {
	// 实际集合名是 mrc20ticks（不是 mrc20_tick）
	collection := mongoClient.Database(*dbName).Collection("mrc20ticks")

	// 构建查询条件
	// 注意：Tick 数据通常没有 blockheight 字段（为 null），所以不对 tick 应用 end-height 过滤
	// Tick 是 deploy 操作产生的元数据，需要全部迁移才能保证 UTXO 数据的完整性
	filter := bson.M{"chain": *chainName}
	log.Printf("  Note: Tick data migration ignores --end-height filter (tick blockheight is usually null)")

	cursor, err := collection.Find(context.Background(), filter)
	if err != nil {
		return err
	}
	defer cursor.Close(context.Background())

	var batch *pebble.Batch
	if !*dryRun {
		batch = pebbleDB.MrcDb.NewBatch()
	}
	count := int64(0)

	for cursor.Next(context.Background()) {
		var tick map[string]interface{}
		if err := cursor.Decode(&tick); err != nil {
			log.Printf("Warning: failed to decode tick: %v", err)
			continue
		}

		if *dryRun {
			count++
			continue
		}

		// Convert to mrc20.Mrc20DeployInfo
		tickData, err := convertTickData(tick)
		if err != nil {
			log.Printf("Warning: failed to convert tick %v: %v", tick["_id"], err)
			continue
		}

		// Save to PebbleDB
		tickBytes, _ := sonic.Marshal(tickData)
		tickId := tickData.Mrc20Id

		// Save tick by ID
		key := []byte("mrc20_tick_" + tickId)
		batch.Set(key, tickBytes, pebble.Sync)

		// Save tick name index
		nameKey := []byte("mrc20_tick_name_" + tickData.Tick)
		batch.Set(nameKey, []byte(tickId), pebble.Sync)

		count++
		if count%100 == 0 {
			if err := batch.Commit(pebble.Sync); err != nil {
				return err
			}
			batch = pebbleDB.MrcDb.NewBatch()
			fmt.Printf("\r  Processed %d ticks...", count)
		}
	}

	if !*dryRun && batch.Count() > 0 {
		if err := batch.Commit(pebble.Sync); err != nil {
			return err
		}
	}

	stats.TickCount = count
	log.Printf("\n✓ Migrated %d ticks", count)
	return nil
}

// buildOperationTxHeightMap 构建 operationTx -> blockHeight 的映射
// 用于判断已消费的 UTXO 是否需要重置状态
// 注意：需要从 operationTx 产生的输出 UTXO 获取高度，而不是被消费的输入 UTXO
func buildOperationTxHeightMap(mongoClient *mongo.Client) (map[string]int64, error) {
	result := make(map[string]int64)

	// 从 mrc20utxos 集合中获取所有记录
	// 通过 txpoint 的前缀（txid）来确定该交易发生的实际高度
	collection := mongoClient.Database(*dbName).Collection("mrc20utxos")
	filter := bson.M{
		"chain": *chainName,
	}

	// 只获取 txpoint 和 blockheight 字段
	projection := bson.M{"txpoint": 1, "blockheight": 1}
	cursor, err := collection.Find(context.Background(), filter, options.Find().SetProjection(projection))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(context.Background())

	for cursor.Next(context.Background()) {
		var doc struct {
			TxPoint     string `bson:"txpoint"`
			BlockHeight int64  `bson:"blockheight"`
		}
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		if doc.TxPoint != "" && doc.BlockHeight > 0 {
			// 从 txpoint 提取 txid（格式：txid:index）
			parts := strings.Split(doc.TxPoint, ":")
			if len(parts) >= 1 {
				txid := parts[0]
				// 存储 txid 对应的区块高度
				// 这是该交易实际发生时的高度
				result[txid] = doc.BlockHeight
			}
		}
	}

	return result, nil
}

func migrateUtxoData(mongoClient *mongo.Client, pebbleDB *pebblestore.Database, stats *MigrationStats) error {
	// 实际集合名是 mrc20utxos（不是 mrc20_utxo）
	collection := mongoClient.Database(*dbName).Collection("mrc20utxos")

	// 如果指定了 end-height，先构建 operationTx -> blockHeight 映射
	var opTxHeightMap map[string]int64
	if *endHeight > 0 {
		log.Printf("  Building operationTx height map for smart reset...")
		var err error
		opTxHeightMap, err = buildOperationTxHeightMap(mongoClient)
		if err != nil {
			log.Printf("  Warning: failed to build operationTx height map: %v", err)
			opTxHeightMap = make(map[string]int64)
		}
		log.Printf("  Found %d operation transactions", len(opTxHeightMap))
	}

	// ========== 第一步：读取所有 UTXO，生成完整 Transaction 历史 ==========
	log.Printf("\n  Step 1: Building complete transaction history from all UTXOs...")

	// 第一遍：读取所有 UTXO（包括 spent 的和验证失败的），构建 Transaction 历史
	filter1 := bson.M{"chain": *chainName}
	if *endHeight > 0 {
		filter1["blockheight"] = bson.M{"$lt": *endHeight}
	}

	cursor1, err := collection.Find(context.Background(), filter1)
	if err != nil {
		return err
	}

	// 新架构：尝试重建 Transaction 历史（从所有 UTXO）
	// 每条 UTXO 都对应一条流水记录（即使是同一笔交易的多个输出）
	transactionList := []*mrc20.Mrc20Transaction{}
	txPointMap := make(map[string]bool) // txPoint -> 是否已处理，避免重复
	nextTxIndex := int64(1)

	allUtxoCount := 0
	for cursor1.Next(context.Background()) {
		var utxo map[string]interface{}
		if err := cursor1.Decode(&utxo); err != nil {
			continue
		}
		allUtxoCount++

		// 提取字段
		txPoint, _ := utxo["txpoint"].(string)
		if txPoint == "" {
			continue
		}
		parts := strings.Split(txPoint, ":")
		if len(parts) != 2 {
			continue
		}
		txId := parts[0]

		// 跳过重复的 txPoint（同一个 UTXO 不重复记录）
		if txPointMap[txPoint] {
			continue
		}
		txPointMap[txPoint] = true

		mrcOption, _ := utxo["mrcoption"].(string)
		tick, _ := utxo["tick"].(string)
		mrc20Id, _ := utxo["mrc20id"].(string)
		pinId, _ := utxo["pinid"].(string)
		toAddress, _ := utxo["toaddress"].(string)
		fromAddress, _ := utxo["fromaddress"].(string)
		blockHeight, _ := utxo["blockheight"].(int64)
		timestamp, _ := utxo["timestamp"].(int64)

		// 解析金额
		var amount decimal.Decimal
		if amtChangeRaw, ok := utxo["amtchange"]; ok {
			switch v := amtChangeRaw.(type) {
			case string:
				amount, _ = decimal.NewFromString(v)
			case primitive.Decimal128:
				amount, _ = decimal.NewFromString(v.String())
			case float64:
				amount = decimal.NewFromFloat(v)
			case int64:
				amount = decimal.NewFromInt(v)
			}
		}

		// 根据 MrcOption 生成 Transaction 记录
		// 注意：历史数据的 mrcoption 可能为空，默认当作 transfer 处理
		var txType string
		switch mrcOption {
		case "mint":
			txType = "mint"
		case "deploy":
			txType = "deploy"
		case "pre-mint":
			txType = "pre-mint"
		case "transfer", "data-transfer", "native-transfer", "":
			// 空值、transfer、data-transfer、native-transfer 都当作 transfer
			txType = "transfer"
		default:
			// 其他操作（teleport 等）跳过
			continue
		}

		// 获取验证状态和消息
		verify, _ := utxo["verify"].(bool)
		msg, _ := utxo["msg"].(string) // 消息字段是 msg，不是 verifymsg
		txStatus := -1                 // 默认失败
		if verify {
			txStatus = 1 // 验证成功
		}

		// 新架构：每条记录都有 Direction 和 Address 字段
		// 迁移时只生成收入记录 (支出记录会在实时处理时生成)
		tx := &mrc20.Mrc20Transaction{
			Chain:        *chainName,
			TxId:         txId,
			TxPoint:      txPoint,
			TxIndex:      nextTxIndex,
			PinId:        pinId,
			TickId:       mrc20Id,
			Tick:         tick,
			TxType:       txType,
			Direction:    "in", // 迁移数据都是收入记录
			Address:      toAddress,
			FromAddress:  fromAddress,
			ToAddress:    toAddress,
			Amount:       amount,
			IsChange:     false,
			SpentUtxos:   "[]",
			CreatedUtxos: fmt.Sprintf("[\"%s\"]", txPoint),
			BlockHeight:  blockHeight,
			Timestamp:    timestamp,
			Msg:          msg,
			Status:       txStatus,
			RelatedChain: "",
			RelatedTxId:  "",
			RelatedPinId: "",
		}

		transactionList = append(transactionList, tx)
		nextTxIndex++
	}
	cursor1.Close(context.Background())

	log.Printf("  → Found %d UTXOs (all statuses)", allUtxoCount)
	log.Printf("  → Generated %d transaction records", len(transactionList))

	// ========== 第二步：迁移可用 UTXO 和生成 AccountBalance ==========
	// 重要：需要迁移两类 UTXO：
	// 1. status=0 的 UTXO（当前可用）
	// 2. status=-1 但 operationtx 的高度 >= end-height 的 UTXO（在 end-height 之后才被消费，需要恢复）
	log.Printf("\n  Step 2: Migrating available UTXOs (status=0) and recovering UTXOs spent after end-height...")

	// 构建查询条件：
	// - status=0 且 blockheight < end-height（当前可用的 UTXO）
	// - 或者 status=-1 且 blockheight < end-height（已消费的 UTXO，后面会检查是否需要恢复）
	var filter bson.M
	if *endHeight > 0 {
		filter = bson.M{
			"chain":       *chainName,
			"verify":      true,
			"blockheight": bson.M{"$lt": *endHeight},
			"$or": []bson.M{
				{"status": 0},  // 可用的 UTXO
				{"status": -1}, // 已消费的 UTXO（需要检查是否应该恢复）
			},
		}
	} else {
		filter = bson.M{"chain": *chainName, "status": 0, "verify": true}
	}

	cursor, err := collection.Find(context.Background(), filter)
	if err != nil {
		return err
	}
	defer cursor.Close(context.Background())

	var batch *pebble.Batch
	if !*dryRun {
		batch = pebbleDB.MrcDb.NewBatch()
	}

	// 统计
	count := int64(0)
	addressMap := make(map[string]bool)
	shovelMap := make(map[string]string)  // mrc20id_pinid -> mintPinId
	operationMap := make(map[string]bool) // Track operation tx

	// 新架构：余额聚合（只从 status=0 的 UTXO 计算）
	// key: chain_address_tickId
	balanceMap := make(map[string]*mrc20.Mrc20AccountBalance)

	for cursor.Next(context.Background()) {
		var utxo map[string]interface{}
		if err := cursor.Decode(&utxo); err != nil {
			log.Printf("Warning: failed to decode utxo: %v", err)
			continue
		}

		// 提取 shovel 数据（mint 操作使用的 PIN）
		if mrcoption, ok := utxo["mrcoption"].(string); ok && mrcoption == "mint" {
			if verify, ok := utxo["verify"].(bool); ok && verify {
				if mrc20id, ok := utxo["mrc20id"].(string); ok && mrc20id != "" {
					if pinid, ok := utxo["pinid"].(string); ok && pinid != "" {
						shovelKey := mrc20id + "_" + pinid
						shovelMap[shovelKey] = pinid
					}
				}
			}
		}

		// 提取 operation tx
		if optx, ok := utxo["operationtx"].(string); ok && optx != "" {
			if *endHeight > 0 {
				if opHeight, ok := opTxHeightMap[optx]; ok && opHeight < *endHeight {
					operationMap[optx] = true
				}
			} else {
				operationMap[optx] = true
			}
		}

		// 收集地址统计
		if addr, ok := utxo["toaddress"].(string); ok && addr != "" {
			addressMap[addr] = true
		}

		if *dryRun {
			count++
			continue
		}

		// Convert to mrc20.Mrc20Utxo
		utxoData, err := convertUtxoData(utxo)
		if err != nil {
			log.Printf("Warning: failed to convert utxo: %v", err)
			continue
		}

		// 如果指定了 end-height，检查 status=-1 的 UTXO 是否需要恢复
		// 如果 operationtx 的高度 >= endHeight，说明这个 UTXO 在目标高度时还是可用的
		if *endHeight > 0 && utxoData.Status == -1 {
			if utxoData.OperationTx != "" {
				if opHeight, ok := opTxHeightMap[utxoData.OperationTx]; ok {
					if opHeight >= *endHeight {
						// operationtx 高度 >= endHeight，恢复为可用状态
						log.Printf("Restoring UTXO %s: consumed at height %d (>= endHeight %d), restoring to status=0",
							utxoData.TxPoint, opHeight, *endHeight)
						utxoData.Status = 0
						utxoData.OperationTx = ""  // 清除消费交易
						utxoData.SpentAtHeight = 0 // 清除消费高度
					} else {
						// operationtx 高度 < endHeight，确实已消费，跳过
						log.Printf("Skipping consumed UTXO %s: consumed at height %d (< endHeight %d)",
							utxoData.TxPoint, opHeight, *endHeight)
						continue
					}
				} else {
					// operationtx 不在 opTxHeightMap 中，可能是无效的，跳过
					log.Printf("Warning: UTXO %s has operationtx %s but height not found, skipping",
						utxoData.TxPoint, utxoData.OperationTx)
					continue
				}
			} else {
				// status=-1 但没有 operationtx，异常情况，跳过
				log.Printf("Warning: UTXO %s has status=-1 but no operationtx, skipping", utxoData.TxPoint)
				continue
			}
		}

		// 新架构：聚合余额
		balanceKey := fmt.Sprintf("%s_%s_%s", utxoData.Chain, utxoData.ToAddress, utxoData.Mrc20Id)
		if balance, exists := balanceMap[balanceKey]; exists {
			balance.Balance = balance.Balance.Add(utxoData.AmtChange)
			balance.UtxoCount += 1
			if utxoData.BlockHeight > balance.LastUpdateHeight {
				balance.LastUpdateHeight = utxoData.BlockHeight
				balance.LastUpdateTime = utxoData.Timestamp
				balance.LastUpdateTx = utxoData.OperationTx
			}
		} else {
			balanceMap[balanceKey] = &mrc20.Mrc20AccountBalance{
				Chain:            utxoData.Chain,
				Address:          utxoData.ToAddress,
				TickId:           utxoData.Mrc20Id,
				Tick:             utxoData.Tick,
				Balance:          utxoData.AmtChange,
				PendingOut:       decimal.Zero,
				PendingIn:        decimal.Zero,
				LastUpdateTx:     utxoData.OperationTx,
				LastUpdateHeight: utxoData.BlockHeight,
				LastUpdateTime:   utxoData.Timestamp,
				UtxoCount:        1,
			}
		}

		// Save UTXO to PebbleDB (新架构：只保存 status=0 的可用 UTXO)
		utxoBytes, _ := sonic.Marshal(utxoData)
		key := []byte("mrc20_utxo_" + utxoData.TxPoint)
		batch.Set(key, utxoBytes, pebble.Sync)

		// 收入索引: mrc20_in_{address}_{tickId}_{txPoint}
		if utxoData.ToAddress != "" {
			inKey := fmt.Sprintf("mrc20_in_%s_%s_%s",
				utxoData.ToAddress, utxoData.Mrc20Id, utxoData.TxPoint)
			batch.Set([]byte(inKey), utxoBytes, pebble.Sync)
		}

		// 可用 UTXO 索引: available_utxo_{chain}_{address}_{tickId}_{txPoint}
		// 这是新架构的核心索引，用于快速查询可用 UTXO
		if utxoData.ToAddress != "" && utxoData.Status == 0 {
			availableKey := fmt.Sprintf("available_utxo_%s_%s_%s_%s",
				utxoData.Chain, utxoData.ToAddress, utxoData.Mrc20Id, utxoData.TxPoint)
			batch.Set([]byte(availableKey), utxoBytes, pebble.Sync)
		}

		// 区块创建索引: block_created_{chain}_{height}_{txPoint}
		// 用于区块级重跑/回滚功能
		if utxoData.BlockHeight > 0 {
			blockCreatedKey := fmt.Sprintf("block_created_%s_%d_%s",
				utxoData.Chain, utxoData.BlockHeight, utxoData.TxPoint)
			batch.Set([]byte(blockCreatedKey), []byte("1"), pebble.Sync)
		}

		count++
		if count%int64(*batchSize) == 0 {
			if err := batch.Commit(pebble.Sync); err != nil {
				return err
			}
			batch = pebbleDB.MrcDb.NewBatch()
			fmt.Printf("\r  Processed %d available UTXOs...", count)
		}
	}

	if !*dryRun && batch.Count() > 0 {
		if err := batch.Commit(pebble.Sync); err != nil {
			return err
		}
	}

	// 新架构：保存 AccountBalance
	// 注意：索引键格式必须与 man/mrc20_new_methods.go 中的 SaveMrc20AccountBalance 一致
	// 格式: balance_{chain}_{address}_{tickId}
	if !*dryRun && len(balanceMap) > 0 {
		log.Printf("\n  Saving %d account balances...", len(balanceMap))
		balanceBatch := pebbleDB.MrcDb.NewBatch()
		balanceCount := 0
		for _, balance := range balanceMap {
			data, _ := sonic.Marshal(balance)
			// 正确的格式: balance_{chain}_{address}_{tickId}
			key := fmt.Sprintf("balance_%s_%s_%s", balance.Chain, balance.Address, balance.TickId)
			balanceBatch.Set([]byte(key), data, pebble.Sync)
			balanceCount++
			if balanceCount%*batchSize == 0 {
				if err := balanceBatch.Commit(pebble.Sync); err != nil {
					return err
				}
				balanceBatch = pebbleDB.MrcDb.NewBatch()
				fmt.Printf("\r  Saved %d balances...", balanceCount)
			}
		}
		if balanceBatch.Count() > 0 {
			if err := balanceBatch.Commit(pebble.Sync); err != nil {
				return err
			}
		}
		log.Printf("\n✓ Saved %d account balances", len(balanceMap))
	}

	// 新架构：保存 Transaction 历史（尽力而为）
	// 注意：索引键格式必须与 man/mrc20_new_methods.go 中的 SaveMrc20Transaction 一致
	// 新设计：每个地址的每个 UTXO 变动一条记录，使用 tx_addr 索引
	if !*dryRun && len(transactionList) > 0 {
		log.Printf("\n  Saving %d transaction records (reconstructed from UTXOs)...", len(transactionList))
		txBatch := pebbleDB.MrcDb.NewBatch()
		txCount := 0
		for _, tx := range transactionList {
			data, _ := sonic.Marshal(tx)

			// 使用 TxPoint 作为主键
			txPointForKey := tx.TxPoint

			// 主键: tx_{txPoint}
			key := []byte(fmt.Sprintf("tx_%s", txPointForKey))
			txBatch.Set(key, data, pebble.Sync)

			// 按 tick 查历史索引: tx_tick_{tickId}_{blockHeight}_{timestamp}_{txPoint}
			tickKey := []byte(fmt.Sprintf("tx_tick_%s_%012d_%012d_%s", tx.TickId, tx.BlockHeight, tx.Timestamp, txPointForKey))
			txBatch.Set(tickKey, key, pebble.Sync)

			// 按地址查索引: tx_addr_{address}_{tickId}_{blockHeight}_{timestamp}_{txPoint}
			if tx.Address != "" {
				addrKey := []byte(fmt.Sprintf("tx_addr_%s_%s_%012d_%012d_%s", tx.Address, tx.TickId, tx.BlockHeight, tx.Timestamp, txPointForKey))
				txBatch.Set(addrKey, key, pebble.Sync)
			}

			txCount++
			if txCount%*batchSize == 0 {
				if err := txBatch.Commit(pebble.Sync); err != nil {
					return err
				}
				txBatch = pebbleDB.MrcDb.NewBatch()
				fmt.Printf("\r  Saved %d transactions...", txCount)
			}
		}

		if txBatch.Count() > 0 {
			if err := txBatch.Commit(pebble.Sync); err != nil {
				return err
			}
		}
		log.Printf("\n✓ Saved %d transaction records", len(transactionList))
		log.Printf("  Note: Transaction history reconstructed from UTXOs (mint/deploy/transfer)")
	}

	// 保存 shovel 数据
	if !*dryRun && len(shovelMap) > 0 {
		shovelBatch := pebbleDB.MrcDb.NewBatch()
		for shovelKey, mintPinId := range shovelMap {
			parts := strings.Split(shovelKey, "_")
			if len(parts) < 2 {
				continue
			}
			pinId := parts[len(parts)-1]
			mrc20Id := strings.Join(parts[:len(parts)-1], "_")

			shovel := mrc20.Mrc20Shovel{
				Id:           pinId,
				Mrc20MintPin: mintPinId,
			}
			data, _ := sonic.Marshal(shovel)
			key := []byte("mrc20_shovel_" + mrc20Id + "_" + pinId)
			shovelBatch.Set(key, data, pebble.Sync)
		}
		if err := shovelBatch.Commit(pebble.Sync); err != nil {
			log.Printf("Warning: failed to save shovel data: %v", err)
		}
	}

	// 保存 operation tx 数据
	if !*dryRun && len(operationMap) > 0 {
		opBatch := pebbleDB.MrcDb.NewBatch()
		for opTx := range operationMap {
			key := []byte("mrc20_op_tx_" + opTx)
			opBatch.Set(key, []byte("1"), pebble.Sync)
		}
		if err := opBatch.Commit(pebble.Sync); err != nil {
			log.Printf("Warning: failed to save operation tx data: %v", err)
		}
	}

	stats.UtxoCount = count
	stats.AddressCount = int64(len(addressMap))
	stats.ShovelCount = int64(len(shovelMap))
	stats.OperationCount = int64(len(operationMap))

	log.Printf("\n✓ Migration Summary:")
	log.Printf("  → Available UTXOs (status=0): %d", count)
	log.Printf("  → Unique addresses:           %d", len(addressMap))
	log.Printf("  → Account balances:           %d", len(balanceMap))
	log.Printf("  → Transaction records:        %d (all operations)", len(transactionList))
	log.Printf("  → Extracted shovels:          %d", len(shovelMap))
	log.Printf("  → Extracted op txs:           %d", len(operationMap))
	return nil
}

func migrateShovelData(mongoClient *mongo.Client, pebbleDB *pebblestore.Database, stats *MigrationStats) error {
	// Shovel 数据已经在 migrateUtxoData 中从 UTXO 记录提取
	// 这里尝试从独立集合补充数据（如果存在的话）
	collection := mongoClient.Database(*dbName).Collection("mrc20_shovel")
	countDocs, err := collection.CountDocuments(context.Background(), bson.M{})
	if err != nil || countDocs == 0 {
		log.Println("ℹ Collection mrc20_shovel not found - shovel data extracted from UTXOs above")
		// stats.ShovelCount 已在 migrateUtxoData 中设置
		return nil
	}

	cursor, err := collection.Find(context.Background(), bson.M{"chain": *chainName})
	if err != nil {
		return err
	}
	defer cursor.Close(context.Background())

	var batch *pebble.Batch
	if !*dryRun {
		batch = pebbleDB.MrcDb.NewBatch()
	}
	count := int64(0)

	for cursor.Next(context.Background()) {
		var shovel map[string]interface{}
		if err := cursor.Decode(&shovel); err != nil {
			log.Printf("Warning: failed to decode shovel: %v", err)
			continue
		}

		if *dryRun {
			count++
			continue
		}

		mrc20Id := shovel["mrc20_id"].(string)
		pinId := shovel["pin_id"].(string)

		// Save to PebbleDB: mrc20_shovel_{mrc20Id}_{pinId} = "1"
		key := []byte(fmt.Sprintf("mrc20_shovel_%s_%s", mrc20Id, pinId))
		batch.Set(key, []byte("1"), pebble.Sync)

		count++
		if count%int64(*batchSize) == 0 {
			if err := batch.Commit(pebble.Sync); err != nil {
				return err
			}
			batch = pebbleDB.MrcDb.NewBatch()
			fmt.Printf("\r  Processed %d shovels...", count)
		}
	}

	if !*dryRun && batch.Count() > 0 {
		if err := batch.Commit(pebble.Sync); err != nil {
			return err
		}
	}

	stats.ShovelCount = count
	log.Printf("\n✓ Migrated %d shovels", count)
	return nil
}

func migrateOperationTx(mongoClient *mongo.Client, pebbleDB *pebblestore.Database, stats *MigrationStats) error {
	// Operation TX 数据已经在 migrateUtxoData 中从 UTXO 记录提取
	// 这里尝试从独立集合补充数据（如果存在的话）
	collection := mongoClient.Database(*dbName).Collection("mrc20_operation_tx")
	countDocs, err := collection.CountDocuments(context.Background(), bson.M{})
	if err != nil || countDocs == 0 {
		log.Println("ℹ Collection mrc20_operation_tx not found - operation data extracted from UTXOs above")
		// stats.OperationCount 已在 migrateUtxoData 中设置
		return nil
	}

	cursor, err := collection.Find(context.Background(), bson.M{"chain": *chainName})
	if err != nil {
		return err
	}
	defer cursor.Close(context.Background())

	var batch *pebble.Batch
	if !*dryRun {
		batch = pebbleDB.MrcDb.NewBatch()
	}
	count := int64(0)

	for cursor.Next(context.Background()) {
		var opTx map[string]interface{}
		if err := cursor.Decode(&opTx); err != nil {
			log.Printf("Warning: failed to decode operation tx: %v", err)
			continue
		}

		if *dryRun {
			count++
			continue
		}

		txId := opTx["tx_id"].(string)

		// Save to PebbleDB: mrc20_op_tx_{txId} = "1"
		key := []byte("mrc20_op_tx_" + txId)
		batch.Set(key, []byte("1"), pebble.Sync)

		count++
		if count%int64(*batchSize) == 0 {
			if err := batch.Commit(pebble.Sync); err != nil {
				return err
			}
			batch = pebbleDB.MrcDb.NewBatch()
			fmt.Printf("\r  Processed %d operations...", count)
		}
	}

	if !*dryRun && batch.Count() > 0 {
		if err := batch.Commit(pebble.Sync); err != nil {
			return err
		}
	}

	stats.OperationCount = count
	log.Printf("\n✓ Migrated %d operation records", count)
	return nil
}

func setIndexHeight(pebbleDB *pebblestore.Database) error {
	syncKey := fmt.Sprintf("%s_mrc20_sync_height", *chainName)
	heightBytes := []byte(strconv.FormatInt(*continueHeight, 10))

	if err := pebbleDB.MetaDb.Set([]byte(syncKey), heightBytes, pebble.Sync); err != nil {
		return err
	}

	log.Printf("✓ Set %s = %d", syncKey, *continueHeight)
	return nil
}

func convertTickData(raw map[string]interface{}) (*mrc20.Mrc20DeployInfo, error) {
	// MongoDB 字段名是小写的，需要转换为 Go 结构体的驼峰格式
	tick := &mrc20.Mrc20DeployInfo{}

	if v, ok := raw["tick"].(string); ok {
		tick.Tick = v
	}
	if v, ok := raw["tokenname"].(string); ok {
		tick.TokenName = v
	}
	if v, ok := raw["decimals"].(string); ok {
		tick.Decimals = v
	}
	if v, ok := raw["amtpermint"].(string); ok {
		tick.AmtPerMint = v
	}
	if v, ok := raw["mintcount"]; ok {
		tick.MintCount = toUint64(v)
	}
	if v, ok := raw["beginheight"].(string); ok {
		tick.BeginHeight = v
	}
	if v, ok := raw["endheight"].(string); ok {
		tick.EndHeight = v
	}
	if v, ok := raw["metadata"].(string); ok {
		tick.Metadata = v
	}
	if v, ok := raw["deploytype"].(string); ok {
		tick.DeployType = v
	}
	if v, ok := raw["preminecount"]; ok {
		tick.PremineCount = toUint64(v)
	}
	if v, ok := raw["totalminted"]; ok {
		tick.TotalMinted = toUint64(v)
	}
	if v, ok := raw["mrc20id"].(string); ok {
		tick.Mrc20Id = v
	}
	if v, ok := raw["pinnumber"]; ok {
		tick.PinNumber = toInt64(v)
	}
	if v, ok := raw["chain"].(string); ok {
		tick.Chain = v
	}
	if v, ok := raw["holders"]; ok {
		tick.Holders = toUint64(v)
	}
	if v, ok := raw["txcount"]; ok {
		tick.TxCount = toUint64(v)
	}
	if v, ok := raw["metaid"].(string); ok {
		tick.MetaId = v
	}
	if v, ok := raw["address"].(string); ok {
		tick.Address = v
	}
	if v, ok := raw["deploytime"]; ok {
		tick.DeployTime = toInt64(v)
	}
	// Handle pincheck
	if pc, ok := raw["pincheck"].(map[string]interface{}); ok {
		if v, ok := pc["creator"].(string); ok {
			tick.PinCheck.Creator = v
		}
		if v, ok := pc["lvl"].(string); ok {
			tick.PinCheck.Lv = v
		}
		if v, ok := pc["path"].(string); ok {
			tick.PinCheck.Path = v
		}
		if v, ok := pc["count"].(string); ok {
			tick.PinCheck.Count = v
		}
	}
	// Handle paycheck
	if pc, ok := raw["paycheck"].(map[string]interface{}); ok {
		if v, ok := pc["payto"].(string); ok {
			tick.PayCheck.PayTo = v
		}
		if v, ok := pc["payamount"].(string); ok {
			tick.PayCheck.PayAmount = v
		}
	}

	return tick, nil
}

func convertUtxoData(raw map[string]interface{}) (*mrc20.Mrc20Utxo, error) {
	// MongoDB 字段名是小写的，需要转换为 Go 结构体的驼峰格式
	utxo := &mrc20.Mrc20Utxo{}

	if v, ok := raw["tick"].(string); ok {
		utxo.Tick = v
	}
	if v, ok := raw["mrc20id"].(string); ok {
		utxo.Mrc20Id = v
	}
	if v, ok := raw["txpoint"].(string); ok {
		utxo.TxPoint = v
	}
	if v, ok := raw["pointvalue"]; ok {
		utxo.PointValue = toUint64(v)
	}
	if v, ok := raw["pinid"].(string); ok {
		utxo.PinId = v
	}
	if v, ok := raw["pincontent"].(string); ok {
		utxo.PinContent = v
	}
	if v, ok := raw["verify"].(bool); ok {
		utxo.Verify = v
	}
	if v, ok := raw["blockheight"]; ok {
		utxo.BlockHeight = toInt64(v)
	}
	if v, ok := raw["mrcoption"].(string); ok {
		utxo.MrcOption = v
	}
	if v, ok := raw["fromaddress"].(string); ok {
		utxo.FromAddress = v
	}
	if v, ok := raw["toaddress"].(string); ok {
		utxo.ToAddress = v
	}
	if v, ok := raw["msg"].(string); ok {
		utxo.Msg = v
	}
	if v, ok := raw["amtchange"]; ok {
		utxo.AmtChange = toDecimal(v)
	}
	if v, ok := raw["status"]; ok {
		utxo.Status = toInt(v)
	}
	if v, ok := raw["chain"].(string); ok {
		utxo.Chain = v
	}
	if v, ok := raw["index"]; ok {
		utxo.Index = toInt(v)
	}
	if v, ok := raw["timestamp"]; ok {
		utxo.Timestamp = toInt64(v)
	}
	if v, ok := raw["operationtx"].(string); ok {
		utxo.OperationTx = v
	}
	if v, ok := raw["spentatheight"]; ok {
		utxo.SpentAtHeight = toInt64(v)
	}

	// Direction 字段已删除，方向由前缀决定：
	// - mrc20_in_: 收入记录
	// - mrc20_out_: 支出记录 (Status=-1)

	return utxo, nil
}

// 类型转换辅助函数
func toUint64(v interface{}) uint64 {
	switch val := v.(type) {
	case int:
		return uint64(val)
	case int32:
		return uint64(val)
	case int64:
		return uint64(val)
	case float64:
		return uint64(val)
	case uint64:
		return val
	default:
		return 0
	}
}

func toInt64(v interface{}) int64 {
	switch val := v.(type) {
	case int:
		return int64(val)
	case int32:
		return int64(val)
	case int64:
		return val
	case float64:
		return int64(val)
	default:
		return 0
	}
}

func toInt(v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case int32:
		return int(val)
	case int64:
		return int(val)
	case float64:
		return int(val)
	default:
		return 0
	}
}

func toDecimal(v interface{}) decimal.Decimal {
	switch val := v.(type) {
	case string:
		d, _ := decimal.NewFromString(val)
		return d
	case float64:
		return decimal.NewFromFloat(val)
	case int64:
		return decimal.NewFromInt(val)
	case int32:
		return decimal.NewFromInt(int64(val))
	case int:
		return decimal.NewFromInt(int64(val))
	case primitive.Decimal128:
		// Handle MongoDB Decimal128 type
		d, _ := decimal.NewFromString(val.String())
		return d
	default:
		log.Printf("toDecimal: unknown type %T for value %v", v, v)
		return decimal.Zero
	}
}

func printStats(stats *MigrationStats) {
	duration := stats.EndTime.Sub(stats.StartTime)

	log.Println("\n=== Migration Statistics ===")
	log.Printf("Ticks:      %d", stats.TickCount)
	log.Printf("UTXOs:      %d", stats.UtxoCount)
	log.Printf("Addresses:  %d", stats.AddressCount)
	log.Printf("Shovels:    %d", stats.ShovelCount)
	log.Printf("Operations: %d", stats.OperationCount)
	log.Printf("Duration:   %v", duration)
	log.Println("============================")

	if *dryRun {
		log.Println("\n✓ Dry run completed. No data was written.")
		log.Println("  Run without --dry-run to perform actual migration.")
	} else {
		log.Println("\n✓ Migration completed successfully!")
		log.Printf("  MRC20 will continue indexing from block %d", *continueHeight)
	}
}
