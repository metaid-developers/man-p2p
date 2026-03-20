package pebblestore

import (
	"fmt"
	"man-p2p/common"
	"man-p2p/pin"
	"strconv"
	"strings"

	"github.com/bytedance/sonic"
	"github.com/cockroachdb/pebble"
)

func (db *Database) GetPinListByIdList(outputList []string, batchSize int, replace bool) (transferCheck []*pin.PinInscription, err error) {
	// num := len(transferCheck)
	// for i := 0; i < num; i += batchSize {
	// 	end := i + batchSize
	// 	if end > num {
	// 		end = num
	// 	}
	// 	vals := db.BatchGetPinListByKeys(outputList[i:end], replace)
	// 	for _, val := range vals {
	// 		var pinNode pin.PinInscription
	// 		err := sonic.Unmarshal(val, &pinNode)
	// 		if err == nil {
	// 			transferCheck = append(transferCheck, &pinNode)
	// 		}
	// 	}
	// }
	vals := db.BatchGetPinListByKeys(outputList, replace)
	for _, val := range vals {
		var pinNode pin.PinInscription
		err := sonic.Unmarshal(val, &pinNode)
		if err == nil {
			transferCheck = append(transferCheck, &pinNode)
		}
	}
	return
}

func (db *Database) UpdateTransferPin(trasferMap map[string]*pin.PinTransferInfo) (err error) {
	var updateList []pin.PinInscription
	for id, info := range trasferMap {
		val, err := db.GetPinByKey(id)
		if err != nil {
			continue
		}
		var pinNode pin.PinInscription
		err = sonic.Unmarshal(val, &pinNode)
		if err != nil {
			continue
		}

		// 检查是否为熔化交易
		if info.IsMeltdown {
			// 熔化交易：只更新状态为熔化，不更新其他信息
			pinNode.Status = pin.PinStatusMeltdown
		} else {
			// 普通转移：更新转移信息
			pinNode.IsTransfered = true
			pinNode.Address = info.Address
			pinNode.MetaId = common.GetMetaIdByAddress(info.Address)
			pinNode.Location = info.Location
			pinNode.Offset = info.Offset
			pinNode.Output = info.Output
			pinNode.OutputValue = info.OutputValue
		}
		updateList = append(updateList, pinNode)
	}
	if len(updateList) > 0 {
		err = db.BatchInsertPins(updateList)
	}
	return
}
func (db *Database) BatchUpdatePins(pins []*pin.PinInscription) (err error) {
	for _, oldPin := range pins {
		if oldPin.OriginalId == "" || oldPin.Status == 0 {
			continue
		}
		dbshard := db.getShard(oldPin.Id)
		val, closer, err := dbshard.Get([]byte(oldPin.Id))
		if err == nil {
			var newPin pin.PinInscription
			err := sonic.Unmarshal(val, &newPin)
			if err == nil {
				newPin.Status = oldPin.Status
			}
			newVal, err := sonic.Marshal(newPin)
			if err == nil {
				dbshard.Set([]byte(newPin.Id), newVal, pebble.Sync)
			}
			closer.Close()
		}
	}
	return
}

func (db *Database) SetAllPins(height int64, pinList []*pin.PinInscription, batchSize int) (err error) {
	num := len(pinList)
	//fmt.Println("SetAllPins num:", num)
	if num <= 0 {
		return
	}
	first := pinList[0]
	chainName := first.ChainName
	blockTime := first.Timestamp
	if height == -1 {
		blockTime = 4096715623 // 固定时间，用于metaid导入
	}
	//blockTime_chainName_height_pinId
	publicKeyStr := pin.GetPublicKeyStr(blockTime, chainName, height)
	keys := make([]string, 0, num)
	pinSortkeys := make([]string, 0, num)
	for i := 0; i < num; i += batchSize {
		end := i + batchSize
		if end > num {
			end = num
		}
		batch := pinList[i:end]

		// 处理本批数据
		list := make([]pin.PinInscription, 0, len(batch))
		pathList := []string{}
		addressList := []string{}
		for _, item := range batch {
			p := item
			if p == nil {
				continue
			}
			list = append(list, *p)
			keys = append(keys, p.Id)
			if height > -1 {
				//sortKey := common.ConcatBytesOptimized([]string{publicKeyStr, "&", p.Path, "&", p.MetaId, "&", p.Id}, "")
				sortKey := pin.GenPinSortKey(p, blockTime, chainName, height)
				pinSortkeys = append(pinSortkeys, sortKey)
			}
			if p.Path != "" {
				//key是 path_blockTime_chainName_height_pinId
				//k := common.ConcatBytesOptimized([]string{common.GetMetaIdByAddress(p.Path), "&", publicKeyStr, "&", p.Id}, "")
				k := pin.GenPathSortKey(p, blockTime, chainName, height)
				pathList = append(pathList, k)
			}
			if p.MetaId != "" {
				//key是metaid_path_blockTime_chainName_height_pinId
				k := pin.GenAddressSortKey(p, blockTime, chainName, height)
				addressList = append(addressList, k)
			}
		}

		// 批量插入/处理
		if len(list) > 0 {
			//st := time.Now()
			err = db.BatchInsertPins(list)
			if err != nil {
				fmt.Printf("插入区块PIN失败: %v\n", err)
			}
			//fmt.Println("  >BatchInsertPins:", time.Since(st))
		}

		if len(pathList) > 0 {
			db.BatchInsertPathPins(&pathList)
		}
		if len(addressList) > 0 {
			db.BatchSetAddressData(&addressList)
		}
		// 本批处理完后，keys等会被GC回收
		list = list[:0]
		list = nil
		pathList = nil
		addressList = nil
	}
	db.InsertPinSort(db.PinSort, pinSortkeys)
	db.InsertBlockTxs(publicKeyStr, strings.Join(keys, ","))

	// 更新统计计数
	if num > 0 {
		db.CountAdd("pins", int64(num))
		db.CountAdd("blocks", 1)
	}

	keys = nil
	pinSortkeys = nil
	return
}
func (db *Database) CountSet(key string, value int64) (err error) {
	return db.CountDB.Set([]byte(key), []byte(strconv.FormatInt(value, 10)), pebble.Sync)
}
func (db *Database) CountAdd(key string, value int64) error {
	val, closer, err := db.CountDB.Get([]byte(key))
	if err == pebble.ErrNotFound {
		return db.CountDB.Set([]byte(key), []byte(strconv.FormatInt(value, 10)), pebble.Sync)
	} else if err != nil {
		return err
	}
	old, err := strconv.ParseInt(string(val), 10, 64)
	closer.Close()
	if err != nil {
		return err
	}
	return db.CountDB.Set([]byte(key), []byte(strconv.FormatInt(old+value, 10)), pebble.Sync)
}

func (db *Database) SetMempool(pinNode *pin.PinInscription) error {
	//key是 pinid,value是空
	//key := common.ConcatBytesOptimized([]string{fmt.Sprintf("%010d", pinNode.Timestamp), "_", pinNode.ChainName, "_", pinNode.Id}, "")
	return db.PinsMempoolDb.Set([]byte(pinNode.Id), nil, pebble.Sync)
}

// 内存池PIN数据分页
func (db *Database) GetMempoolPageList(page int64, size int64) ([]*pin.PinInscription, error) {
	var result []*pin.PinInscription
	iter, err := db.PinsMempoolDb.NewIter(nil)
	if err != nil {
		return result, err
	}
	defer iter.Close()

	skip := page * size
	count := int64(0)
	for iter.Last(); iter.Valid(); iter.Prev() {
		if skip > 0 {
			skip--
			continue
		}
		if count >= size {
			break
		}
		key := string(iter.Key())

		data, err := db.GetPinByKey(key)
		if err != nil {
			continue
		}
		var pinNode pin.PinInscription
		err = sonic.Unmarshal(data, &pinNode)
		if err != nil {
			continue
		}
		result = append(result, &pinNode)
		count++
	}
	return result, nil
}
func (db *Database) GetMempool(key string) ([]byte, error) {
	result, closer, err := db.PinsMempoolDb.Get([]byte(key))
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("GetMempool error: %v", err)
	}
	defer closer.Close()
	return result, nil
}
func (db *Database) GetMempoolPin(key string) (pinNode pin.PinInscription, err error) {
	result, err := db.GetMempool(key)
	if err != nil {
		return
	}
	err = sonic.Unmarshal(result, &pinNode)
	return
}
func (db *Database) DeleteMempool(key string) error {
	return db.PinsMempoolDb.Delete([]byte(key), pebble.Sync)
}
func DeleteBatchByKeyList(db *pebble.DB, keyList *[]string) error {
	//key是time_chain_pinid
	batch := db.NewBatch()
	for _, v := range *keyList {
		batch.Delete([]byte(v), nil)
	}
	if err := batch.Commit(pebble.Sync); err != nil {
		batch.Close()
		return err
	}
	batch.Close()
	return nil
}
func (db *Database) BatchDeleteMempool(key []string) error {
	batch := db.PinsMempoolDb.NewBatch()
	for _, v := range key {
		batch.Delete([]byte(v), nil)
	}
	if err := batch.Commit(pebble.Sync); err != nil {
		batch.Close()
		return err
	}
	batch.Close()
	return nil
}

// 新的通知存储方式：每条通知独立存储
// Key格式: address_notifcationId_fromPinId
// 使用 notifcationId (时间戳) 保证顺序，fromPinId 保证唯一性
func (db *Database) SetNotifcationV2(address string, notifcationId int64, fromPinId string, data []byte) error {
	// 使用反向时间戳使数据按时间倒序排列
	const maxInt64 int64 = 9223372036854775807
	key := fmt.Sprintf("%s_%019d_%s", address, maxInt64-notifcationId, fromPinId)
	return db.NotifcationDb.Set([]byte(key), data, pebble.Sync)
}

// 分页查询通知列表
func (db *Database) GetNotifcationListV2(address string, lastId int64, size int) ([]pin.NotifcationData, int64, error) {
	prefix := address + "_"
	it, err := db.NotifcationDb.NewIter(nil)
	if err != nil {
		return nil, 0, err
	}
	defer it.Close()

	var list []pin.NotifcationData
	seenPins := make(map[string]struct{})
	var total int64

	// 从前缀开始遍历（已经按时间倒序排列）
	for it.First(); it.Valid(); it.Next() {
		key := it.Key()
		if !strings.HasPrefix(string(key), prefix) {
			break
		}

		// 解析 key 获取 notifcationId
		parts := strings.Split(string(key), "_")
		if len(parts) < 3 {
			continue
		}
		const maxInt64 int64 = 9223372036854775807
		reversedId, _ := strconv.ParseInt(parts[1], 10, 64)
		currentId := maxInt64 - reversedId
		fromPinId := parts[2]

		// 去重检查
		if _, exists := seenPins[fromPinId]; exists {
			continue
		}

		// lastId 过滤
		if lastId > 0 && currentId <= lastId {
			continue
		}

		// 解析数据
		var notif pin.NotifcationData
		if err := sonic.Unmarshal(it.Value(), &notif); err == nil {
			seenPins[fromPinId] = struct{}{}
			list = append(list, notif)
			if len(list) >= size {
				break
			}
		}
	}

	// 统计总数（所有符合条件的，去重后）
	if lastId == 0 {
		total = int64(len(list))
		// 继续统计剩余的
		for it.Valid() {
			key := it.Key()
			if !strings.HasPrefix(string(key), prefix) {
				break
			}
			parts := strings.Split(string(key), "_")
			if len(parts) >= 3 {
				fromPinId := parts[2]
				if _, exists := seenPins[fromPinId]; !exists {
					seenPins[fromPinId] = struct{}{}
					total++
				}
			}
			it.Next()
		}
	} else {
		total = int64(len(list))
	}

	return list, total, nil
}

// 清理旧通知 - 保留最近的 N 条
func (db *Database) CleanUpNotifcationV2(address string, keepCount int) error {
	prefix := address + "_"
	it, err := db.NotifcationDb.NewIter(nil)
	if err != nil {
		return err
	}
	defer it.Close()

	var keysToDelete [][]byte
	seenPins := make(map[string]struct{})
	count := 0

	// 遍历找出需要删除的key
	for it.First(); it.Valid(); it.Next() {
		key := it.Key()
		if !strings.HasPrefix(string(key), prefix) {
			break
		}

		parts := strings.Split(string(key), "_")
		if len(parts) >= 3 {
			fromPinId := parts[2]
			if _, exists := seenPins[fromPinId]; !exists {
				seenPins[fromPinId] = struct{}{}
				count++
				if count > keepCount {
					keyCopy := make([]byte, len(key))
					copy(keyCopy, key)
					keysToDelete = append(keysToDelete, keyCopy)
				}
			}
		}
	}

	// 批量删除
	if len(keysToDelete) > 0 {
		batch := db.NotifcationDb.NewBatch()
		for _, key := range keysToDelete {
			batch.Delete(key, nil)
		}
		if err := batch.Commit(pebble.Sync); err != nil {
			batch.Close()
			return err
		}
		batch.Close()
	}

	return nil
}

// 兼容旧接口 - 保留用于数据迁移
func (db *Database) SetNotifcation(key string, value []byte) error {
	sep := []byte("@*@")
	return db.NotifcationDb.Merge([]byte(key), append(value, sep...), pebble.Sync)
}
func (db *Database) GetNotifcation(key string) ([]byte, error) {
	result, closer, err := db.NotifcationDb.Get([]byte(key))
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("GetNotifcation error: %v", err)
	}
	defer closer.Close()
	return result, nil
}
func (db *Database) DeleteNotifcation(key string) error {
	return db.NotifcationDb.Delete([]byte(key), pebble.Sync)
}

func (db *Database) CleanUpNotifcation(key string) error {
	result, err := db.GetNotifcation(key)
	if err != nil {
		return err
	}

	// 分割数据
	arr := strings.Split(string(result), "@*@")

	// 如果数据大于300条，滚动删除
	if len(arr) > 300 {
		remaining := arr[len(arr)-200:] // 保留最后200条
		newValue := strings.Join(remaining, "@*@")
		return db.NotifcationDb.Set([]byte(key), []byte(newValue), pebble.Sync)
	}

	return nil
}
