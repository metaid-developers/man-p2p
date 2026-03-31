package man

import (
	"encoding/json"
	"fmt"
	"log"
	"man-p2p/mrc20"
	"man-p2p/pin"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bitcoinsv/bsvutil"
	bsvwire "github.com/bitcoinsv/bsvd/wire"
	"github.com/btcsuite/btcd/btcutil"
	btcchainhash "github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	btcwire "github.com/btcsuite/btcd/wire"
	"github.com/bytedance/sonic"
	"github.com/shopspring/decimal"
)

// Doge 链交易缓存（保留作为备用，节点现在支持 getrawtransaction）
// 可以作为性能优化：当前区块内的交易从缓存获取更快
var dogeTxCache = make(map[string]*btcutil.Tx)
var dogeTxCacheMutex sync.RWMutex

// SetDogeTxCache 设置 Doge 区块交易缓存（由 dogecoin indexer 在 CatchPins 时调用）
// 注意：节点现在支持 getrawtransaction，缓存主要用于性能优化
func SetDogeTxCache(txMap map[string]*btcutil.Tx) {
	dogeTxCacheMutex.Lock()
	defer dogeTxCacheMutex.Unlock()
	dogeTxCache = txMap
}

// ClearDogeTxCache 清除 Doge 交易缓存
func ClearDogeTxCache() {
	dogeTxCacheMutex.Lock()
	defer dogeTxCacheMutex.Unlock()
	dogeTxCache = make(map[string]*btcutil.Tx)
}

// GetTransactionWithCache 获取交易
// Doge 链优先从缓存获取（性能优化），缓存没有则从 RPC 获取
// 注意：Doge 节点现在支持 getrawtransaction，所以即使缓存没有也能获取到
func GetTransactionWithCache(chainName string, txid string) (*btcutil.Tx, error) {
	// 测试时使用 mock 函数
	if MockGetTransactionWithCache != nil {
		result, err := MockGetTransactionWithCache(chainName, txid)
		if err != nil {
			return nil, err
		}
		return normalizeToBtcutilTx(result)
	}

	// 只有 Doge 链使用缓存优化
	if chainName == "doge" {
		dogeTxCacheMutex.RLock()
		if tx, ok := dogeTxCache[txid]; ok {
			dogeTxCacheMutex.RUnlock()
			return tx, nil
		}
		dogeTxCacheMutex.RUnlock()
		// 缓存没有，从 RPC 获取（节点现在支持 getrawtransaction）
	}

	// 从 RPC 获取
	tx, err := ChainAdapter[chainName].GetTransaction(txid)
	if err != nil {
		return nil, err
	}
	return normalizeToBtcutilTx(tx)
}

func normalizeToBtcutilTx(tx interface{}) (*btcutil.Tx, error) {
	switch v := tx.(type) {
	case *btcutil.Tx:
		return v, nil
	case *bsvutil.Tx:
		return btcutil.NewTx(convertBsvMsgTxToBtc(v.MsgTx())), nil
	default:
		return nil, fmt.Errorf("unsupported tx type: %T", tx)
	}
}

func convertBsvMsgTxToBtc(src *bsvwire.MsgTx) *btcwire.MsgTx {
	dst := btcwire.NewMsgTx(src.Version)
	dst.LockTime = src.LockTime

	for _, in := range src.TxIn {
		hash, err := btcchainhash.NewHash(in.PreviousOutPoint.Hash.CloneBytes())
		if err != nil {
			hash = &btcchainhash.Hash{}
		}
		prevOut := btcwire.NewOutPoint(hash, in.PreviousOutPoint.Index)
		txIn := btcwire.NewTxIn(prevOut, append([]byte(nil), in.SignatureScript...), nil)
		txIn.Sequence = in.Sequence
		dst.AddTxIn(txIn)
	}

	for _, out := range src.TxOut {
		dst.AddTxOut(btcwire.NewTxOut(out.Value, append([]byte(nil), out.PkScript...)))
	}

	return dst
}

func Mrc20Handle(chainName string, height int64, mrc20List []*pin.PinInscription, mrc20TransferPinTx map[string]struct{}, txInList []string, isMempool bool) {
	//log.Printf("[MRC20] Mrc20Handle: chain=%s, height=%d, mrc20List=%d, txInList=%d", chainName, height, len(mrc20List), len(txInList))

	validator := Mrc20Validator{}
	var mrc20UtxoList []mrc20.Mrc20Utxo

	var mrc20TrasferList []*mrc20.Mrc20Utxo
	//var deployHandleList []*pin.PinInscription
	var mintHandleList []*pin.PinInscription
	var transferHandleList []*pin.PinInscription
	var arrivalHandleList []*pin.PinInscription
	for _, pinNode := range mrc20List {
		//log.Printf("[MRC20] Processing PIN: path=%s, id=%s", pinNode.Path, pinNode.Id)
		switch pinNode.Path {
		case "/ft/mrc20/deploy":
			//deployHandleList = append(deployHandleList, pinNode)
			//Prioritize handling deploy
			deployResult := deployHandle(pinNode)
			if len(deployResult) > 0 {
				mrc20UtxoList = append(mrc20UtxoList, deployResult...)
			}
		case "/ft/mrc20/mint":
			mintHandleList = append(mintHandleList, pinNode)
		case "/ft/mrc20/transfer":
			transferHandleList = append(transferHandleList, pinNode)
		case "/ft/mrc20/arrival":
			arrivalHandleList = append(arrivalHandleList, pinNode)
		}
	}

	// 处理 arrival (跃迁目标)
	// arrival 处理优先于 transfer，因为 teleport transfer 需要引用 arrival
	for _, pinNode := range arrivalHandleList {
		err := arrivalHandle(pinNode)
		if err != nil {
			log.Println("arrivalHandle error:", err)
		}
	}

	for _, pinNode := range mintHandleList {
		mrc20Pin, err := CreateMrc20MintPin(pinNode, &validator, isMempool)
		// 保存所有记录，包括验证失败的记录
		if err == nil || mrc20Pin.Mrc20Id != "" {
			mrc20Pin.Chain = pinNode.ChainName
			mrc20UtxoList = append(mrc20UtxoList, mrc20Pin)
		}
	}
	changedTick := make(map[string]int64)
	if len(mrc20UtxoList) > 0 {
		PebbleStore.SaveMrc20Pin(mrc20UtxoList)

		// 保存历史记录（mempool和block阶段都保存）
		for _, utxo := range mrc20UtxoList {
			if utxo.MrcOption == mrc20.OptionMint {
				if isMempool {
					// Mempool阶段：保存pending状态的历史记录
					err := PebbleStore.ProcessMintMempool(&utxo)
					if err != nil {
						log.Printf("[ERROR] ProcessMintMempool failed for %s: %v", utxo.TxPoint, err)
					}
				} else {
					// Block阶段：更新余额 + 确认历史记录
					if utxo.Verify {
						// Mint 成功：更新余额 + 写入流水
						err := PebbleStore.ProcessMintSuccess(&utxo)
						if err != nil {
							log.Printf("[ERROR] ProcessMintSuccess failed for %s: %v", utxo.TxPoint, err)
						}
					} else {
						// Mint 失败：仅写入失败流水记录
						err := PebbleStore.ProcessMintFailure(&utxo)
						if err != nil {
							log.Printf("[ERROR] ProcessMintFailure failed for %s: %v", utxo.TxPoint, err)
						}
					}
				}
			}

			// 只在block阶段统计holder变化
			if !isMempool && utxo.MrcOption != mrc20.OptionDeploy {
				changedTick[utxo.Mrc20Id] += 1
			}
		}
	}

	//CatchNativeMrc20Transfer
	handleNativTransfer(chainName, height, mrc20TransferPinTx, txInList, isMempool)
	// mrc20transferCheck, err := PebbleStore.GetMrc20UtxoByOutPutList(txInList, isMempool)
	// if err == nil && len(mrc20transferCheck) > 0 {
	// 	mrc20TrasferList := IndexerAdapter[chainName].CatchNativeMrc20Transfer(height, mrc20transferCheck, mrc20TransferPinTx)
	// 	if len(mrc20TrasferList) > 0 {
	// 		PebbleStore.UpdateMrc20Utxo(mrc20TrasferList, isMempool)
	// 	}
	// }

	// 处理 transfer PIN（传入 isMempool 参数）
	mrc20TrasferList = transferHandleWithMempool(transferHandleList, isMempool)
	if len(mrc20TrasferList) > 0 && !isMempool {
		//PebbleStore.UpdateMrc20Utxo(mrc20TrasferList, false)
		for _, item := range mrc20TrasferList {
			if item.MrcOption != mrc20.OptionDeploy {
				changedTick[item.Mrc20Id] += 1
			}
		}
	}
	//CatchNativeMrc20Transfer Agin
	handleNativTransfer(chainName, height, mrc20TransferPinTx, txInList, isMempool)

	// 出块时：修复所有pending状态的转账
	// 这个函数会遍历所有TransferPending UTXO，检查对应的接收方UTXO是否已确认
	// 如果已确认，则调用ProcessNativeTransferSuccess更新余额
	if !isMempool && height > 0 {
		fixedCount, err := PebbleStore.FixPendingUtxoStatus(chainName)
		if err != nil {
			log.Printf("[ERROR] FixPendingUtxoStatus failed: %v", err)
		} else if fixedCount > 0 {
			log.Printf("[INFO] FixPendingUtxoStatus: fixed %d pending UTXOs", fixedCount)
		}
	}

	// 出块时：重试所有卡住的 pending teleport
	// 当 arrival 先出块而 teleport 后进 mempool 时，可能会遗漏 processPendingTeleportForArrival
	if !isMempool && height > 0 {
		// V2架构重试
		if UseTeleportV2 {
			if err := RetryStuckTeleports(); err != nil {
				log.Printf("[TeleportV2] ⚠️  Retry failed: %v", err)
			}
			// 重试等待配对的 Pending Teleport（双向等待机制的兜底）
			if err := RetryPendingTeleports(); err != nil {
				log.Printf("[PendingPair] ⚠️  Retry pending failed: %v", err)
			}
		} else {
			// V1架构重试
			retryPendingTeleports()
		}
	}

	//update holders,txCount (只在block阶段执行)
	if !isMempool {
		for id, txNum := range changedTick {
			go PebbleStore.UpdateMrc20TickHolder(id, txNum)
		}
	}
}

// retryPendingTeleports 重试所有卡住的 pending teleport
// 当 arrival 先出块而 teleport 后进 mempool 时，processPendingTeleportForArrival 可能不会被触发
// 这个函数作为安全网，在每次出块处理后扫描所有 Status=0 的 PendingTeleport，
// 检查双方是否都已确认，如果是则执行 teleport
// DiagnosePendingTeleport 诊断并尝试修复指定的pending teleport
// 返回：是否成功修复，错误信息
// 注意：这是V1 teleport的逻辑，V2架构使用新的双向等待机制
func DiagnosePendingTeleport(coord string) (bool, string) {
	return false, "V1 DiagnosePendingTeleport is deprecated, use V2 teleport architecture"
	/* V1 Logic - Commented out
	// 查找PendingTeleport
	pending, err := PebbleStore.GetPendingTeleportByCoord(coord)
	if err != nil || pending == nil {
		return false, fmt.Sprintf("PendingTeleport not found for coord: %s", coord)
	}

	if pending.Status != 0 {
		return false, fmt.Sprintf("PendingTeleport status is not pending (status=%d)", pending.Status)
	}

	// 查找Arrival - 先通过coord查找
	arrival, err := PebbleStore.GetMrc20ArrivalByPinId(coord)

	// 如果通过coord找不到，尝试通过assetOutpoint查找
	if (err != nil || arrival == nil) && pending.AssetOutpoint != "" {
		log.Printf("[DiagnoseTeleport] Arrival not found by coord, trying assetOutpoint: %s", pending.AssetOutpoint)
		arrivalByAsset, err2 := PebbleStore.GetMrc20ArrivalByAssetOutpoint(pending.AssetOutpoint)
		if err2 == nil && arrivalByAsset != nil {
			arrival = arrivalByAsset
			log.Printf("[DiagnoseTeleport] ⚠️ Coord mismatch! Expected: %s, Actual: %s", coord, arrival.PinId)
			log.Printf("[DiagnoseTeleport] Will use actual arrival and update PendingTeleport coord")

			// 更新PendingTeleport的coord为正确的值
			pending.Coord = arrival.PinId
			err := PebbleStore.SavePendingTeleport(pending)
			if err != nil {
				log.Printf("[DiagnoseTeleport] Failed to update PendingTeleport coord: %v", err)
			} else {
				log.Printf("[DiagnoseTeleport] Updated PendingTeleport coord to: %s", arrival.PinId)
			}
		}
	}

	if arrival == nil {
		return false, fmt.Sprintf("Arrival not found by coord or assetOutpoint")
	}

	// 检查双方是否都已出块
	if arrival.BlockHeight <= 0 {
		return false, fmt.Sprintf("Arrival is still in mempool (blockHeight=%d)", arrival.BlockHeight)
	}

	if pending.BlockHeight <= 0 {
		return false, fmt.Sprintf("Teleport blockHeight not updated (height=%d), may need to update from blockchain", pending.BlockHeight)
	}

	if arrival.Status != mrc20.ArrivalStatusPending {
		return false, fmt.Sprintf("Arrival status is not pending (status=%d)", arrival.Status)
	}

	// 检查是否已有TeleportPendingIn
	pendingIn, _ := PebbleStore.GetTeleportPendingInByCoord(arrival.PinId)
	if pendingIn == nil {
		log.Printf("[DiagnoseTeleport] TeleportPendingIn not found, will create during execution")
	}

	// 执行修复
	log.Printf("[DiagnoseTeleport] Executing processPendingTeleportForArrival for arrival=%s", arrival.PinId)
	processPendingTeleportForArrival(arrival)

	// 验证是否成功
	// 使用正确的coord查询（arrival.PinId）
	updatedPending, _ := PebbleStore.GetPendingTeleportByCoord(arrival.PinId)
	if updatedPending != nil && updatedPending.Status == 1 {
		return true, fmt.Sprintf("Teleport completed successfully. Arrival PinId: %s", arrival.PinId)
	}

	return false, "Teleport execution may have failed, check logs for details"
	*/
}

func retryPendingTeleports() {
	// V1 teleport 重试逻辑（已废弃）
	// V2架构使用RetryPendingTeleports和RetryStuckTeleports
	log.Printf("[TeleportV1] retryPendingTeleports is deprecated, use V2 architecture")
	/* V1 Logic - Commented out
	pendingList, err := PebbleStore.GetAllPendingTeleports()
	if err != nil || len(pendingList) == 0 {
		return
	}

	for _, pending := range pendingList {
		if pending.Status != 0 {
			continue
		}

		// 查找对应的 arrival
		arrival, err := PebbleStore.GetMrc20ArrivalByPinId(pending.Coord)
		if err != nil || arrival == nil {
			log.Printf("[Teleport Retry] PendingTeleport %s: arrival not found for coord %s", pending.PinId, pending.Coord)
			continue
		}

		// 双方都必须已出块确认
		if arrival.BlockHeight <= 0 {
			log.Printf("[Teleport Retry] PendingTeleport %s: arrival still in mempool (blockHeight=%d)", pending.PinId, arrival.BlockHeight)
			continue
		}
		if arrival.Status != mrc20.ArrivalStatusPending {
			log.Printf("[Teleport Retry] PendingTeleport %s: arrival status is not pending (status=%d)", pending.PinId, arrival.Status)
			continue
		}
		if pending.BlockHeight <= 0 {
			log.Printf("[Teleport Retry] PendingTeleport %s: still in mempool (blockHeight=%d), checking if arrival's teleport PIN confirmed...", pending.PinId, pending.BlockHeight)

			// 尝试从区块链获取teleport交易的确认高度
			// 如果teleport交易已经出块但pending.BlockHeight还是-1，更新它
			// 这里需要根据pending.TxId查询交易所在的区块高度
			// 暂时跳过，因为需要ChainAdapter支持
			continue
		}

		log.Printf("[Teleport Retry] Found stuck pending teleport: pinId=%s, coord=%s, teleportBlock=%d, arrivalBlock=%d",
			pending.PinId, pending.Coord, pending.BlockHeight, arrival.BlockHeight)

		processPendingTeleportForArrival(arrival)
	}
	*/
}

func handleNativTransfer(chainName string, height int64, mrc20TransferPinTx map[string]struct{}, txInList []string, isMempool bool) {
	//log.Printf("[DEBUG] handleNativTransfer: height=%d, txInList count=%d, isMempool=%v, transferPinTxCount=%d", height, len(txInList), isMempool, len(mrc20TransferPinTx))
	// 打印所有 Transfer PIN 交易 ID（调试用）
	// for txid := range mrc20TransferPinTx {
	// 	//log.Printf("[DEBUG] handleNativTransfer: mrc20TransferPinTx contains %s", txid)
	// }
	mrc20transferCheck, err := PebbleStore.GetMrc20UtxoByOutPutList(txInList, isMempool)
	//log.Printf("[DEBUG] handleNativTransfer: GetMrc20UtxoByOutPutList returned %d UTXOs, err=%v", len(mrc20transferCheck), err)
	if err != nil || len(mrc20transferCheck) == 0 {
		return
	}

	// 出块时：先清理 mempool 阶段创建的数据，然后重新获取干净的 UTXO 列表
	if !isMempool && height > 0 {
		// 检查是否有 mempool 阶段处理过的 UTXO
		// 注意：只清理 native transfer 的 UTXO，不清理 Transfer PIN 的 UTXO
		var pendingUtxos []*mrc20.Mrc20Utxo
		for _, utxo := range mrc20transferCheck {
			if utxo.Status == mrc20.UtxoStatusTransferPending {
				// 检查 operationTx 是否是 Transfer PIN 交易
				// 如果是 Transfer PIN，则跳过（让 transferHandleWithMempool 处理）
				if utxo.OperationTx != "" {
					if _, isTransferPin := mrc20TransferPinTx[utxo.OperationTx]; isTransferPin {
						//log.Printf("[DEBUG] handleNativTransfer: skipping Transfer PIN UTXO %s, operationTx=%s", utxo.TxPoint, utxo.OperationTx)
						continue
					}
				}
				pendingUtxos = append(pendingUtxos, utxo)
			}
		}

		if len(pendingUtxos) > 0 {
			//log.Printf("[DEBUG] handleNativTransfer: found %d TransferPending UTXOs, cleaning mempool data first", len(pendingUtxos))
			// 清理 mempool 数据
			err := PebbleStore.CleanMempoolNativeTransfer(pendingUtxos)
			if err != nil {
				log.Printf("[ERROR] handleNativTransfer: CleanMempoolNativeTransfer failed: %v", err)
			}

			// 重新获取干净的 UTXO 列表（现在应该都是 Available 状态）
			mrc20transferCheck, err = PebbleStore.GetMrc20UtxoByOutPutList(txInList, isMempool)
			if err != nil || len(mrc20transferCheck) == 0 {
				log.Printf("[WARN] handleNativTransfer: no UTXOs after cleanup")
				return
			}
			//log.Printf("[DEBUG] handleNativTransfer: after cleanup, got %d UTXOs", len(mrc20transferCheck))
			// for _, utxo := range mrc20transferCheck {
			// 	//log.Printf("[DEBUG] handleNativTransfer: cleaned UTXO %s, status=%d, amt=%s", utxo.TxPoint, utxo.Status, utxo.AmtChange)
			// }
		}
	}

	mrc20TrasferList := IndexerAdapter[chainName].CatchNativeMrc20Transfer(height, mrc20transferCheck, mrc20TransferPinTx)
	//log.Printf("[DEBUG] handleNativTransfer: CatchNativeMrc20Transfer returned %d UTXOs", len(mrc20TrasferList))
	if len(mrc20TrasferList) == 0 {
		return
	}

	// 分离 spent 和 new UTXOs
	var spentUtxos []*mrc20.Mrc20Utxo
	var newUtxos []*mrc20.Mrc20Utxo
	for _, utxo := range mrc20TrasferList {
		//log.Printf("[DEBUG] handleNativTransfer: result UTXO %s, status=%d, amt=%s, toAddr=%s, operationTx=%s", utxo.TxPoint, utxo.Status, utxo.AmtChange, utxo.ToAddress, utxo.OperationTx)
		if utxo.Status == mrc20.UtxoStatusSpent {
			spentUtxos = append(spentUtxos, utxo)
		} else if utxo.Status == mrc20.UtxoStatusAvailable {
			// 【修复】区块确认时，检查是否已存在 TeleportPending 状态的 UTXO
			// 如果已存在，跳过创建新 UTXO，保留 TeleportPending 状态
			if !isMempool {
				existingUtxo, err := PebbleStore.GetMrc20UtxoByTxPoint(utxo.TxPoint, false)
				if err == nil && existingUtxo != nil && existingUtxo.Status == mrc20.UtxoStatusTeleportPending {
					//log.Printf("[DEBUG] handleNativTransfer: skipping UTXO %s, already TeleportPending", utxo.TxPoint)
					continue
				}
			}
			newUtxos = append(newUtxos, utxo)
		}
	}
	//log.Printf("[DEBUG] handleNativTransfer: spentUtxos=%d, newUtxos=%d, isMempool=%v", len(spentUtxos), len(newUtxos), isMempool)

	// 保存新创建的 UTXOs
	if len(newUtxos) > 0 {
		newUtxoValues := make([]mrc20.Mrc20Utxo, len(newUtxos))
		for i, u := range newUtxos {
			newUtxoValues[i] = *u
		}
		PebbleStore.SaveMrc20Pin(newUtxoValues)
	}

	// 按交易分组处理
	txUtxos := make(map[string]struct {
		spent []*mrc20.Mrc20Utxo
		new   []*mrc20.Mrc20Utxo
	})
	for _, utxo := range spentUtxos {
		key := utxo.OperationTx
		entry := txUtxos[key]
		entry.spent = append(entry.spent, utxo)
		txUtxos[key] = entry
	}
	for _, utxo := range newUtxos {
		key := utxo.OperationTx
		entry := txUtxos[key]
		entry.new = append(entry.new, utxo)
		txUtxos[key] = entry
	}

	if !isMempool {
		// 出块确认：更新余额、写入/更新流水
		//log.Printf("[DEBUG] handleNativTransfer: processing %d txs for block confirmation", len(txUtxos))
		for txId, entry := range txUtxos {
			//log.Printf("[DEBUG] handleNativTransfer: tx %s, spent=%d, new=%d", txId, len(entry.spent), len(entry.new))
			if len(entry.spent) > 0 && len(entry.new) > 0 {
				// 先尝试更新已有的 mempool 流水记录的 BlockHeight
				err := PebbleStore.UpdateMrc20TransactionBlockHeight(txId, height)
				if err != nil {
					log.Printf("[WARN] UpdateMrc20TransactionBlockHeight failed for tx %s: %v", txId, err)
				}
				// 然后处理余额更新和写入新流水（如果 mempool 没有记录则新写入）
				err = PebbleStore.ProcessNativeTransferSuccess(txId, chainName, height, entry.spent, entry.new)
				if err != nil {
					log.Printf("[ERROR] ProcessNativeTransferSuccess failed for tx %s: %v", txId, err)
				}
			}
		}
	} else {
		// mempool 阶段：更新 UTXO 状态 + 写入流水（BlockHeight = -1）
		PebbleStore.UpdateMrc20Utxo(mrc20TrasferList, isMempool)
		// 写入 mempool 阶段的流水记录
		for txId, entry := range txUtxos {
			if len(entry.spent) > 0 && len(entry.new) > 0 {
				err := PebbleStore.SaveMempoolNativeTransferTransaction(txId, chainName, entry.spent, entry.new)
				if err != nil {
					log.Printf("[WARN] SaveMempoolNativeTransferTransaction failed for tx %s: %v", txId, err)
				}
			}
		}
	}
}

// transferHandleWithMempool 处理 transfer PIN，支持 mempool 阶段
func transferHandleWithMempool(transferHandleList []*pin.PinInscription, isMempool bool) (mrc20UtxoList []*mrc20.Mrc20Utxo) {
	validator := Mrc20Validator{}

	// 分离普通 transfer 和 teleport transfer
	// teleport 可能依赖同一区块内普通 transfer 创建的 UTXO，所以必须分两步处理
	var normalTransferList []*pin.PinInscription
	var teleportTransferList []*pin.PinInscription

	for _, pinNode := range transferHandleList {
		// 检查是否是 teleport 类型
		if isTeleportTransfer(pinNode) {
			log.Printf("[Teleport] 🎯 Detected teleport transfer: pinId=%s, txId=%s", pinNode.Id, pinNode.GenesisTransaction)
			teleportTransferList = append(teleportTransferList, pinNode)
		} else {
			normalTransferList = append(normalTransferList, pinNode)
		}
	}

	// 第一步：处理所有普通 transfer，创建输出 UTXO
	// 使用循环重试机制处理依赖关系（同一区块内普通 transfer 之间的依赖）
	normalSuccessMap := make(map[string]struct{})
	maxTimes := len(normalTransferList)
	for i := 0; i < maxTimes; i++ {
		if len(normalSuccessMap) >= maxTimes {
			break
		}
		for _, pinNode := range normalTransferList {
			if _, ok := normalSuccessMap[pinNode.Id]; ok {
				continue
			}

			////log.Printf("[DEBUG] Processing normal transfer PIN: %s, isMempool=%v", pinNode.Id, isMempool)

			transferPinList, _ := CreateMrc20TransferUtxo(pinNode, &validator, isMempool)
			if len(transferPinList) > 0 {
				mrc20UtxoList = append(mrc20UtxoList, transferPinList...)

				// 检查是否有验证失败的 UTXO
				hasFailedTransfer := false
				for _, utxo := range transferPinList {
					if !utxo.Verify {
						hasFailedTransfer = true
						break
					}
				}

				if hasFailedTransfer {
					// 处理失败的转账：仅保存失败记录，不更新余额
					failedUtxos := make([]*mrc20.Mrc20Utxo, 0)
					for _, utxo := range transferPinList {
						if !utxo.Verify {
							failedUtxos = append(failedUtxos, utxo)
						}
					}

					// 保存失败的 UTXO 记录
					if len(failedUtxos) > 0 {
						failedUtxoValues := make([]mrc20.Mrc20Utxo, len(failedUtxos))
						for i, u := range failedUtxos {
							failedUtxoValues[i] = *u
						}
						PebbleStore.SaveMrc20Pin(failedUtxoValues)

						// 为失败的转账创建流水记录
						err := PebbleStore.ProcessTransferFailure(failedUtxos)
						if err != nil {
							log.Printf("[ERROR] ProcessTransferFailure failed for %s: %v", pinNode.Id, err)
						}
					}
				} else {
					// 处理成功的转账
					normalSuccessMap[pinNode.Id] = struct{}{}

					// 处理成功的转账
					normalSuccessMap[pinNode.Id] = struct{}{}

					// 分离 spent 和 new UTXOs（mempool阶段基于AmtChange正负号区分）
					var spentUtxos []*mrc20.Mrc20Utxo
					var newUtxos []*mrc20.Mrc20Utxo
					for _, utxo := range transferPinList {
						if utxo.Verify { // 只处理验证成功的 UTXO
							if isMempool {
								// mempool阶段：基于AmtChange正负号区分输入输出UTXO
								if utxo.AmtChange.IsNegative() {
									// 输入UTXO（AmtChange < 0）
									spentUtxos = append(spentUtxos, utxo)
								} else {
									// 输出UTXO（AmtChange > 0）
									newUtxos = append(newUtxos, utxo)
								}
							} else {
								// block阶段：基于Status区分
								if utxo.Status == mrc20.UtxoStatusSpent {
									spentUtxos = append(spentUtxos, utxo)
								} else {
									newUtxos = append(newUtxos, utxo)
								}
							}
						}
					}

					// 保存新创建的 UTXOs
					if len(newUtxos) > 0 {
						newUtxoValues := make([]mrc20.Mrc20Utxo, len(newUtxos))
						for i, u := range newUtxos {
							newUtxoValues[i] = *u
						}
						PebbleStore.SaveMrc20Pin(newUtxoValues)
					}

					// mempool阶段：还需要保存spent UTXOs（用于余额计算的pendingOut）
					if isMempool && len(spentUtxos) > 0 {
						spentUtxoValues := make([]mrc20.Mrc20Utxo, len(spentUtxos))
						for i, u := range spentUtxos {
							spentUtxoValues[i] = *u
						}
						PebbleStore.SaveMrc20Pin(spentUtxoValues)
					}

					if !isMempool {
						// 出块确认：先更新已有 mempool 流水的 BlockHeight，然后处理余额和流水
						if len(spentUtxos) > 0 && len(newUtxos) > 0 {
							// 尝试更新已有的 mempool 流水记录的 BlockHeight
							err := PebbleStore.UpdateMrc20TransactionBlockHeight(pinNode.GenesisTransaction, pinNode.GenesisHeight)
							if err != nil {
								log.Printf("[WARN] UpdateMrc20TransactionBlockHeight failed for %s: %v", pinNode.GenesisTransaction, err)
							}
							// 处理余额更新和写入流水
							err = PebbleStore.ProcessTransferSuccess(pinNode, spentUtxos, newUtxos)
							if err != nil {
								log.Printf("[ERROR] ProcessTransferSuccess failed for %s: %v", pinNode.Id, err)
							}
						}
					} else {
						// mempool 阶段：只写流水（不更新余额），BlockHeight = -1
						if len(spentUtxos) > 0 && len(newUtxos) > 0 {
							err := PebbleStore.SaveMempoolTransferTransaction(pinNode, spentUtxos, newUtxos)
							if err != nil {
								log.Printf("[WARN] SaveMempoolTransferTransaction failed for %s: %v", pinNode.Id, err)
							}
						}
					}
				}
			} else if !isMempool {
				// 出块阶段但 CreateMrc20TransferUtxo 返回空
				// 可能是 mempool 阶段已处理，需要更新 UTXO 的 BlockHeight 并更新余额
				existingUtxos, _ := PebbleStore.CheckOperationtxAll(pinNode.GenesisTransaction, true) // 查找mempool状态的记录
				if len(existingUtxos) > 0 {
					//log.Printf("[DEBUG] transferHandleWithMempool: mempool已处理, 更新BlockHeight和余额, tx=%s, utxoCount=%d",pinNode.GenesisTransaction, len(existingUtxos))

					// 统一处理：更新 BlockHeight 并同时更新状态
					err := PebbleStore.UpdateUtxosBlockHeight(existingUtxos, pinNode.GenesisHeight)
					if err != nil {
						log.Printf("[WARN] UpdateUtxosBlockHeight failed for %s: %v", pinNode.GenesisTransaction, err)
					}
					// 同时更新 Transaction 记录的 BlockHeight
					err = PebbleStore.UpdateMrc20TransactionBlockHeight(pinNode.GenesisTransaction, pinNode.GenesisHeight)
					if err != nil {
						log.Printf("[WARN] UpdateMrc20TransactionBlockHeight failed for %s: %v", pinNode.GenesisTransaction, err)
					}

					// 分离 spent 和 new UTXOs（基于更新后的状态）
					var spentUtxos []*mrc20.Mrc20Utxo
					var newUtxos []*mrc20.Mrc20Utxo
					for _, utxo := range existingUtxos {
						// 重新获取更新后的UTXO状态
						if utxo.AmtChange.IsNegative() {
							// 输入UTXO，现在应该是Spent状态
							utxo.Status = mrc20.UtxoStatusSpent
							utxo.BlockHeight = pinNode.GenesisHeight
							spentUtxos = append(spentUtxos, utxo)
						} else {
							// 输出UTXO，现在应该是Available状态
							utxo.Status = mrc20.UtxoStatusAvailable
							utxo.BlockHeight = pinNode.GenesisHeight
							newUtxos = append(newUtxos, utxo)
						}
					}

					//log.Printf("[DEBUG] transferHandleWithMempool: spent=%d, new=%d", len(spentUtxos), len(newUtxos))

					// 直接从 UTXO 重算涉及地址的余额（和 reindex-from 一样可靠）
					affectedAddresses := make(map[string]string) // address -> tickId
					for _, utxo := range existingUtxos {
						// 接收方地址 (ToAddress)
						if utxo.ToAddress != "" && utxo.Mrc20Id != "" {
							affectedAddresses[utxo.ToAddress] = utxo.Mrc20Id
						}
						// 发送方地址 (FromAddress) - 关键：从 new UTXO 获取发送方
						if utxo.FromAddress != "" && utxo.Mrc20Id != "" {
							affectedAddresses[utxo.FromAddress] = utxo.Mrc20Id
						}
					}

					//log.Printf("[DEBUG] transferHandleWithMempool: affectedAddresses=%v", affectedAddresses)

					// 删除TransferPendingIn记录（出块确认后）
					for _, utxo := range newUtxos {
						if err := PebbleStore.DeleteTransferPendingIn(utxo.TxPoint, utxo.ToAddress); err != nil {
							log.Printf("[WARN] DeleteTransferPendingIn failed for %s: %v", utxo.TxPoint, err)
						}
					}

					for address, tickId := range affectedAddresses {
						err := PebbleStore.RecalculateBalance(pinNode.ChainName, address, tickId)
						if err != nil {
							log.Printf("[ERROR] RecalculateBalance failed for %s: %v", address, err)
						} else {
							log.Printf("[INFO] RecalculateBalance: 余额已重算, address=%s, tickId=%s", address, tickId)
						}
					}
				}
				normalSuccessMap[pinNode.Id] = struct{}{}
			}
		}
	}

	// 第二步：处理所有 teleport transfer（teleport 在 mempool 阶段暂不处理流水）
	// 此时同一区块内的普通 transfer 已经处理完毕，UTXO 已创建
	teleportSuccessMap := make(map[string]struct{})
	maxTimes = len(teleportTransferList)
	for i := 0; i < maxTimes; i++ {
		if len(teleportSuccessMap) >= maxTimes {
			break
		}
		for _, pinNode := range teleportTransferList {
			if _, ok := teleportSuccessMap[pinNode.Id]; ok {
				continue
			}

			////log.Printf("[DEBUG] Processing teleport transfer PIN: %s", pinNode.Id)

			isTeleport, teleportUtxoList, err := processTeleportTransfer(pinNode, isMempool)
			////log.Printf("[DEBUG] processTeleportTransfer result: isTeleport=%v, utxoCount=%d, err=%v",	isTeleport, len(teleportUtxoList), err)

			if !isTeleport {
				// 不应该发生，因为我们已经预先筛选过
				log.Printf("[WARN] PIN %s was classified as teleport but processTeleportTransfer returned false", pinNode.Id)
				teleportSuccessMap[pinNode.Id] = struct{}{}
				continue
			}

			if err != nil {
				log.Println("processTeleportTransfer error:", err)
				// teleport 处理失败，留给下一轮重试
				continue
			}

			if len(teleportUtxoList) > 0 {
				mrc20UtxoList = append(mrc20UtxoList, teleportUtxoList...)
				PebbleStore.UpdateMrc20Utxo(teleportUtxoList, isMempool)
			}
			teleportSuccessMap[pinNode.Id] = struct{}{}
		}
	}

	return
}

// isTeleportTransfer 检查 PIN 是否是 teleport 类型的 transfer
// 只检查 JSON 格式，不执行实际处理
func isTeleportTransfer(pinNode *pin.PinInscription) bool {
	// 尝试解析为 teleport 格式
	var teleportData []mrc20.Mrc20TeleportTransferData

	// 先尝试解析为数组
	err := json.Unmarshal(pinNode.ContentBody, &teleportData)
	if err != nil {
		// 数组解析失败，尝试解析为单个对象
		var singleData mrc20.Mrc20TeleportTransferData
		err = json.Unmarshal(pinNode.ContentBody, &singleData)
		if err != nil {
			return false // 不是有效的 teleport JSON
		}
		teleportData = []mrc20.Mrc20TeleportTransferData{singleData}
	}

	// 检查是否有 teleport 类型的项
	for _, item := range teleportData {
		if item.Type == "teleport" {
			return true
		}
	}
	return false
}

// IsTeleportTransferDebug 导出函数供测试使用
func IsTeleportTransferDebug(pinNode *pin.PinInscription) bool {
	return isTeleportTransfer(pinNode)
}

func deployHandle(pinNode *pin.PinInscription) (mrc20UtxoList []mrc20.Mrc20Utxo) {
	//log.Printf("[MRC20] deployHandle: pinId=%s", pinNode.Id)
	var deployList []mrc20.Mrc20DeployInfo
	validator := Mrc20Validator{}
	//for _, pinNode := range deployHandleList {
	mrc20Pin, preMineUtxo, info, err := CreateMrc20DeployPin(pinNode, &validator)
	//log.Printf("[MRC20] CreateMrc20DeployPin result: err=%v, mrc20Id=%s, tick=%s", err, info.Mrc20Id, info.Tick)
	if err == nil {
		if mrc20Pin.Mrc20Id != "" {
			mrc20Pin.Chain = pinNode.ChainName
			mrc20UtxoList = append(mrc20UtxoList, mrc20Pin)
		}
		if preMineUtxo.Mrc20Id != "" {
			mrc20UtxoList = append(mrc20UtxoList, preMineUtxo)
		}
		if info.Tick != "" && info.Mrc20Id != "" {
			deployList = append(deployList, info)
		}
	}
	//}
	if len(deployList) > 0 {
		PebbleStore.SaveMrc20Tick(deployList)
	}
	return
}
func CreateMrc20DeployPin(pinNode *pin.PinInscription, validator *Mrc20Validator) (mrc20Utxo mrc20.Mrc20Utxo, preMineUtxo mrc20.Mrc20Utxo, info mrc20.Mrc20DeployInfo, err error) {
	var df mrc20.Mrc20Deploy
	//log.Printf("[MRC20] CreateMrc20DeployPin: contentBody=%s", string(pinNode.ContentBody))
	err = json.Unmarshal(pinNode.ContentBody, &df)
	if err != nil {
		//log.Printf("[MRC20] CreateMrc20DeployPin: json unmarshal error: %v", err)
		return
	}
	//log.Printf("[MRC20] CreateMrc20DeployPin: parsed deploy data: tick=%s, mintCount=%s, amtPerMint=%s, premineCount=%s", df.Tick, df.MintCount, df.AmtPerMint, df.PremineCount)
	premineCount := int64(0)
	if df.PremineCount != "" {
		premineCount, err = strconv.ParseInt(df.PremineCount, 10, 64)
		if err != nil {
			//log.Printf("[MRC20] CreateMrc20DeployPin: premineCount parse error: %v", err)
			return
		}
	}
	mintCount, err := strconv.ParseInt(df.MintCount, 10, 64)
	if err != nil {
		//log.Printf("[MRC20] CreateMrc20DeployPin: mintCount parse error: %v", err)
		return
	}
	if mintCount < 0 {
		mintCount = int64(0)
	}
	amtPerMint, err := strconv.ParseInt(df.AmtPerMint, 10, 64)
	if err != nil {
		//log.Printf("[MRC20] CreateMrc20DeployPin: amtPerMint parse error: %v", err)
		return
	}
	if amtPerMint < 0 {
		amtPerMint = int64(0)
	}
	//premineCount
	if mintCount < premineCount {
		//log.Printf("[MRC20] CreateMrc20DeployPin: mintCount(%d) < premineCount(%d), returning", mintCount, premineCount)
		return
	}
	//log.Printf("[MRC20] CreateMrc20DeployPin: calling validator.Deploy")
	premineAddress, pointValue, err1 := validator.Deploy(pinNode.ContentBody, pinNode)
	//log.Printf("[MRC20] CreateMrc20DeployPin: validator.Deploy result: premineAddress=%s, pointValue=%d, err=%v", premineAddress, pointValue, err1)
	if err1 != nil {
		//mrc20Utxo.Verify = false
		//mrc20Utxo.Msg = err1.Error()
		err = err1 // 传递错误
		return
	}
	info.Tick = strings.ToUpper(df.Tick)
	info.TokenName = df.TokenName
	info.Decimals = df.Decimals
	info.AmtPerMint = df.AmtPerMint
	info.PremineCount = uint64(premineCount)
	info.MintCount = uint64(mintCount)
	info.BeginHeight = df.BeginHeight
	info.EndHeight = df.EndHeight
	info.Metadata = df.Metadata
	info.DeployType = df.DeployType
	info.PinCheck = df.PinCheck
	info.PayCheck = df.PayCheck
	info.DeployTime = pinNode.Timestamp

	info.Mrc20Id = pinNode.Id
	info.PinNumber = pinNode.Number
	info.Chain = pinNode.ChainName
	info.Address = pinNode.Address
	info.MetaId = pinNode.MetaId
	mrc20Utxo.Tick = info.Tick
	mrc20Utxo.Mrc20Id = pinNode.Id
	mrc20Utxo.PinId = pinNode.Id
	mrc20Utxo.BlockHeight = pinNode.GenesisHeight
	mrc20Utxo.MrcOption = mrc20.OptionDeploy
	mrc20Utxo.FromAddress = pinNode.CreateAddress
	mrc20Utxo.ToAddress = pinNode.Address
	mrc20Utxo.TxPoint = pinNode.Output
	mrc20Utxo.PinContent = string(pinNode.ContentBody)
	mrc20Utxo.Timestamp = pinNode.Timestamp
	mrc20Utxo.PointValue = uint64(pinNode.OutputValue)
	mrc20Utxo.Verify = true

	if premineAddress != "" && premineCount > 0 {
		preMineUtxo.Verify = true
		//preMineUtxo.PinId = pinNode.Id
		preMineUtxo.BlockHeight = pinNode.GenesisHeight
		preMineUtxo.MrcOption = mrc20.OptionPreMint
		preMineUtxo.FromAddress = pinNode.Address
		preMineUtxo.ToAddress = premineAddress
		preMineUtxo.TxPoint = fmt.Sprintf("%s:%d", pinNode.GenesisTransaction, 1)
		//mrc20Utxo.PinContent = string(pinNode.ContentBody)
		preMineUtxo.Timestamp = pinNode.Timestamp
		preMineUtxo.PointValue = uint64(pointValue)
		preMineUtxo.Mrc20Id = info.Mrc20Id
		preMineUtxo.Tick = info.Tick
		preMineUtxo.Chain = pinNode.ChainName
		//preMineUtxo.AmtChange = premineCount * amtPerMint
		num := strconv.FormatInt(premineCount*amtPerMint, 10)
		preMineUtxo.AmtChange, _ = decimal.NewFromString(num)
		info.TotalMinted = uint64(premineCount)
	}
	return
}

func CreateMrc20MintPin(pinNode *pin.PinInscription, validator *Mrc20Validator, mempool bool) (mrc20Utxo mrc20.Mrc20Utxo, err error) {
	var content mrc20.Mrc20MintData
	err = json.Unmarshal(pinNode.ContentBody, &content)
	if err != nil {
		return
	}
	mrc20Utxo.Verify = true
	mrc20Utxo.PinId = pinNode.Id
	mrc20Utxo.BlockHeight = pinNode.GenesisHeight
	mrc20Utxo.MrcOption = mrc20.OptionMint
	mrc20Utxo.FromAddress = pinNode.Address
	mrc20Utxo.ToAddress = pinNode.Address
	mrc20Utxo.TxPoint = pinNode.Output
	mrc20Utxo.PinContent = string(pinNode.ContentBody)
	mrc20Utxo.Timestamp = pinNode.Timestamp
	mrc20Utxo.PointValue = uint64(pinNode.OutputValue)
	info, shovelList, toAddress, vout, err1 := validator.Mint(content, pinNode)
	if toAddress != "" {
		mrc20Utxo.ToAddress = toAddress
		mrc20Utxo.TxPoint = fmt.Sprintf("%s:%d", pinNode.GenesisTransaction, vout)
	}
	if info != (mrc20.Mrc20DeployInfo{}) {
		mrc20Utxo.Mrc20Id = info.Mrc20Id
		mrc20Utxo.Tick = info.Tick
	}
	if mempool {
		mrc20Utxo.Mrc20Id = info.Mrc20Id
		if err1 != nil {
			// Mempool阶段：验证失败，记录错误信息
			mrc20Utxo.Verify = false
			mrc20Utxo.Msg = err1.Error()
			mrc20Utxo.Status = mrc20.UtxoStatusSpent // 验证失败，不计入余额
		} else {
			// Mempool阶段：验证成功，设置为pending状态
			mrc20Utxo.AmtChange, _ = decimal.NewFromString(info.AmtPerMint)
			mrc20Utxo.Status = mrc20.UtxoStatusMintPending // 设置为mint pending状态
		}
		mrc20Utxo.OperationTx = pinNode.GenesisTransaction
		return
	}
	if err1 != nil {
		mrc20Utxo.Mrc20Id = info.Mrc20Id
		mrc20Utxo.Verify = false
		mrc20Utxo.Msg = err1.Error()
		mrc20Utxo.Status = mrc20.UtxoStatusSpent // 验证失败
		mrc20Utxo.OperationTx = pinNode.GenesisTransaction
	} else {
		if len(shovelList) > 0 {
			PebbleStore.AddMrc20Shovel(shovelList, pinNode.Id, mrc20Utxo.Mrc20Id)
		}
		PebbleStore.UpdateMrc20TickInfo(info.Mrc20Id, mrc20Utxo.TxPoint, uint64(info.TotalMinted)+1)
		//mrc20Utxo.AmtChange, _ = strconv.ParseInt(info.AmtPerMint, 10, 64)
		mrc20Utxo.AmtChange, _ = decimal.NewFromString(info.AmtPerMint)
		mrc20Utxo.Status = mrc20.UtxoStatusAvailable // Block阶段验证成功，设置为可用
		mrc20Utxo.OperationTx = pinNode.GenesisTransaction
	}

	return
}

func CreateMrc20TransferUtxo(pinNode *pin.PinInscription, validator *Mrc20Validator, isMempool bool) (mrc20UtxoList []*mrc20.Mrc20Utxo, err error) {
	//log.Printf("[DEBUG] CreateMrc20TransferUtxo START: pinId=%s, tx=%s, isMempool=%v", pinNode.Id, pinNode.GenesisTransaction, isMempool)

	//Check if it has been processed
	find, err1 := PebbleStore.CheckOperationtx(pinNode.GenesisTransaction, isMempool)
	if err1 != nil || find != nil {
		//log.Printf("[DEBUG] CreateMrc20TransferUtxo: already processed or error, pinId=%s, err=%v, find=%v", pinNode.Id, err1, find != nil)
		return
	}

	var content []mrc20.Mrc20TranferData
	err = json.Unmarshal(pinNode.ContentBody, &content)
	if err != nil {
		//log.Printf("[DEBUG] CreateMrc20TransferUtxo: JSON parse error, pinId=%s, content=%s, err=%v", pinNode.Id, string(pinNode.ContentBody), err)
		mrc20UtxoList = sendAllAmountToFirstOutput(pinNode, "Transfer JSON format error", isMempool)
		return
	}
	//log.Printf("[DEBUG] CreateMrc20TransferUtxo: parsed content, pinId=%s, items=%d", pinNode.Id, len(content))

	//check
	toAddress, utxoList, outputValueList, msg, firstIdx, err1 := validator.Transfer(content, pinNode, isMempool)
	//if err1 != nil && err1.Error() != "valueErr" {
	if err1 != nil {
		//log.Printf("[DEBUG] CreateMrc20TransferUtxo: validator.Transfer FAILED, pinId=%s, msg=%s, err=%v", pinNode.Id, msg, err1)
		mrc20UtxoList = sendAllAmountToFirstOutput(pinNode, msg, isMempool)
		return
	}
	//log.Printf("[DEBUG] CreateMrc20TransferUtxo: validator.Transfer SUCCESS, pinId=%s, toAddressCount=%d, utxoListCount=%d, firstIdx=%d", pinNode.Id, len(toAddress), len(utxoList), firstIdx)
	address := make(map[string]string)
	name := make(map[string]string)
	inputAmtMap := make(map[string]decimal.Decimal)
	var spendUtxoList []*mrc20.Mrc20Utxo
	for _, utxo := range utxoList {
		address[utxo.Mrc20Id] = utxo.ToAddress
		name[utxo.Mrc20Id] = utxo.Tick
		// 处理输入 UTXO 状态
		mrc20Utxo := *utxo
		// 输入UTXO表示被花费的资金，AmtChange设为负数
		mrc20Utxo.AmtChange = utxo.AmtChange.Neg()
		if isMempool {
			// mempool 阶段：设置为 TransferPending（待转出）
			mrc20Utxo.Status = mrc20.UtxoStatusTransferPending
		} else {
			// 出块确认：设置为 Spent（已消耗）
			mrc20Utxo.Status = mrc20.UtxoStatusSpent
		}
		// 注意：不修改 MrcOption，保留原始操作类型（mint/deploy/teleport/transfer 等）
		// MrcOption 表示 UTXO 是如何创建的，而不是如何被花费的
		mrc20Utxo.OperationTx = pinNode.GenesisTransaction
		spendUtxoList = append(spendUtxoList, &mrc20Utxo)
		inputAmtMap[utxo.Mrc20Id] = inputAmtMap[utxo.Mrc20Id].Add(utxo.AmtChange)
	}
	outputAmtMap := make(map[string]decimal.Decimal)
	x := 0
	var reciveUtxoList []*mrc20.Mrc20Utxo
	for _, item := range content {
		mrc20Utxo := mrc20.Mrc20Utxo{}
		mrc20Utxo.Mrc20Id = item.Id
		mrc20Utxo.Tick = name[item.Id]
		mrc20Utxo.Verify = true
		mrc20Utxo.PinId = pinNode.Id
		mrc20Utxo.BlockHeight = pinNode.GenesisHeight
		mrc20Utxo.MrcOption = mrc20.OptionDataTransfer
		mrc20Utxo.FromAddress = address[item.Id]
		mrc20Utxo.ToAddress = toAddress[item.Vout]
		// 处理输出 UTXO 状态
		if isMempool {
			// mempool 阶段：设置为 TransferPending（等待确认）
			mrc20Utxo.Status = mrc20.UtxoStatusTransferPending
		} else {
			// 出块确认：设置为 Available（确认可用）
			mrc20Utxo.Status = mrc20.UtxoStatusAvailable
		}
		mrc20Utxo.Chain = pinNode.ChainName
		mrc20Utxo.TxPoint = fmt.Sprintf("%s:%d", pinNode.GenesisTransaction, item.Vout)
		mrc20Utxo.PinContent = string(pinNode.ContentBody)
		mrc20Utxo.Index = x
		mrc20Utxo.OperationTx = pinNode.GenesisTransaction
		mrc20Utxo.PointValue = uint64(outputValueList[item.Vout])
		//mrc20Utxo.AmtChange, _ = strconv.ParseInt(item.Amount, 10, 64)
		mrc20Utxo.AmtChange, _ = decimal.NewFromString(item.Amount)
		//outputAmtMap[item.Id] += mrc20Utxo.AmtChange
		outputAmtMap[item.Id] = outputAmtMap[item.Id].Add(mrc20Utxo.AmtChange)
		mrc20Utxo.Timestamp = pinNode.Timestamp
		reciveUtxoList = append(reciveUtxoList, &mrc20Utxo)
		x += 1
	}
	//Check if the input exceeds the output.
	for id, inputAmt := range inputAmtMap {
		//inputAmt > outputAmtMap[id]
		if inputAmt.Compare(outputAmtMap[id]) == 1 {
			//if !isMempool {
			// find := false
			// for _, utxo := range mrc20UtxoList {
			// 	vout := strings.Split(utxo.TxPoint, ":")[1]
			// 	if utxo.Mrc20Id == id && utxo.ToAddress == toAddress[0] && vout == "0" {
			// 		//utxo.AmtChange += (inputAmt - outputAmtMap[id])

			// 		diff := inputAmt.Sub(outputAmtMap[id])
			// 		fmt.Println("2===>", diff, utxo.AmtChange)
			// 		utxo.AmtChange = utxo.AmtChange.Add(diff)

			// 		utxo.Msg = "The total input amount is greater than the output amount"
			// 		find = true
			// 	}
			// }
			// if find {
			// 	continue
			// }
			//}
			mrc20Utxo := mrc20.Mrc20Utxo{}
			mrc20Utxo.Mrc20Id = id
			mrc20Utxo.Tick = name[id]
			mrc20Utxo.Verify = true
			mrc20Utxo.PinId = pinNode.Id
			mrc20Utxo.BlockHeight = pinNode.GenesisHeight
			mrc20Utxo.MrcOption = mrc20.OptionDataTransfer
			mrc20Utxo.FromAddress = address[id]
			mrc20Utxo.ToAddress = toAddress[0]
			// 处理找零 UTXO 状态
			if isMempool {
				// mempool 阶段：设置为 TransferPending（等待确认）
				mrc20Utxo.Status = mrc20.UtxoStatusTransferPending
			} else {
				// 出块确认：设置为 Available（确认可用）
				mrc20Utxo.Status = mrc20.UtxoStatusAvailable
			}
			mrc20Utxo.Chain = pinNode.ChainName
			mrc20Utxo.Timestamp = pinNode.Timestamp
			mrc20Utxo.TxPoint = fmt.Sprintf("%s:%d", pinNode.GenesisTransaction, firstIdx)
			mrc20Utxo.PointValue = uint64(outputValueList[firstIdx])
			mrc20Utxo.PinContent = string(pinNode.ContentBody)
			mrc20Utxo.OperationTx = pinNode.GenesisTransaction
			mrc20Utxo.Index = x
			//mrc20Utxo.AmtChange = inputAmt - outputAmtMap[id]
			mrc20Utxo.AmtChange = inputAmt.Sub(outputAmtMap[id])
			mrc20Utxo.Msg = "Native change from partial transfer"
			mrc20UtxoList = append(mrc20UtxoList, &mrc20Utxo)
			x += 1
		}
	}
	mrc20UtxoList = append(mrc20UtxoList, spendUtxoList...)
	mrc20UtxoList = append(mrc20UtxoList, reciveUtxoList...)
	return
}
func sendAllAmountToFirstOutput(pinNode *pin.PinInscription, msg string, isMempool bool) (mrc20UtxoList []*mrc20.Mrc20Utxo) {
	txb, err := GetTransactionWithCache(pinNode.ChainName, pinNode.GenesisTransaction)
	if err != nil {
		log.Println("GetTransactionWithCache:", err)
		return
	}
	toAddress := ""
	idx := 0
	value := int64(0)
	for i, out := range txb.MsgTx().TxOut {
		class, addresses, _, _ := txscript.ExtractPkScriptAddrs(out.PkScript, getBtcNetParams(pinNode.ChainName))
		if class.String() != "nulldata" && class.String() != "nonstandard" && len(addresses) > 0 {
			toAddress = addresses[0].String()
			idx = i
			value = out.Value
			break
		}
	}
	if toAddress == "" {
		return
	}
	var inputList []string
	for _, in := range txb.MsgTx().TxIn {
		s := fmt.Sprintf("%s:%d", in.PreviousOutPoint.Hash.String(), in.PreviousOutPoint.Index)
		inputList = append(inputList, s)
	}
	list, err := PebbleStore.GetMrc20UtxoByOutPutList(inputList, isMempool)
	if err != nil {
		//log.Println("GetMrc20UtxoByOutPutList:", err)
		return
	}
	utxoList := make(map[string]*mrc20.Mrc20Utxo)
	for _, item := range list {
		//Spent the input UTXO
		//amt := item.AmtChange * -1
		amt := item.AmtChange.Neg()
		mrc20Utxo := mrc20.Mrc20Utxo{TxPoint: item.TxPoint, Index: item.Index, Mrc20Id: item.Mrc20Id, Verify: true, Status: -1, AmtChange: amt}
		mrc20UtxoList = append(mrc20UtxoList, &mrc20Utxo)
		if v, ok := utxoList[item.Mrc20Id]; ok {
			//v.AmtChange += item.AmtChange
			v.AmtChange = v.AmtChange.Add(item.AmtChange)
		} else {
			utxoList[item.Mrc20Id] = &mrc20.Mrc20Utxo{
				Mrc20Id:     item.Mrc20Id,
				Tick:        item.Tick,
				Verify:      true, // 资产转移成功（回退到第一个输出）
				PinId:       pinNode.Id,
				BlockHeight: pinNode.GenesisHeight,
				MrcOption:   mrc20.OptionDataTransfer,
				FromAddress: pinNode.Address,
				ToAddress:   toAddress,
				Chain:       pinNode.ChainName,
				Timestamp:   pinNode.Timestamp,
				TxPoint:     fmt.Sprintf("%s:%d", pinNode.GenesisTransaction, idx),
				PointValue:  uint64(value),
				PinContent:  string(pinNode.ContentBody),
				Index:       0,
				AmtChange:   item.AmtChange,
				Msg:         msg,
				OperationTx: pinNode.GenesisTransaction,
				// 处理错误回退 UTXO 状态
				Status: func() int {
					if isMempool {
						// mempool 阶段：设置为 TransferPending（等待确认）
						return mrc20.UtxoStatusTransferPending
					} else {
						// 出块确认：设置为 Available（确认可用）
						return mrc20.UtxoStatusAvailable
					}
				}(),
			}
		}

	}
	for _, mrc20Utxo := range utxoList {
		mrc20UtxoList = append(mrc20UtxoList, mrc20Utxo)
	}
	return
}
func Mrc20NativeTransferHandle(sendList []*mrc20.Mrc20Utxo, reciveAddressList map[string]*string, txPointList map[string]*string) (mrc20UtxoList []mrc20.Mrc20Utxo, err error) {

	return
}

// ================ Teleport 跃迁处理 ================

// arrivalHandle 处理 /ft/mrc20/arrival PIN
// arrival 是跃迁的目标端，记录了预期从源链转移的资产信息
// arrival 出块时检查源链 UTXO 状态：
// - Status == 0 (可用)：正常保存 arrival，等待 teleport
// - Status == 1 (teleport pending)：查找 pending teleport 并执行跃迁
// - Status == -1 (已消耗)：检查是否有对应的 teleport 记录
//   - 有记录 → 跃迁已完成
//   - 无记录 → UTXO 被其他操作花费，跃迁失败
func arrivalHandle(pinNode *pin.PinInscription) error {
	log.Printf("[Arrival] 📥 Processing arrival: pinId=%s, txId=%s, chain=%s, height=%d",
		pinNode.Id, pinNode.GenesisTransaction, pinNode.ChainName, pinNode.GenesisHeight)

	var data mrc20.Mrc20ArrivalData
	err := json.Unmarshal(pinNode.ContentBody, &data)
	if err != nil {
		log.Println("arrivalHandle: JSON parse error:", err)
		return saveInvalidArrival(pinNode, "JSON parse error: "+err.Error())
	}

	log.Printf("[Arrival] ✅ Parsed arrival data: assetOutpoint=%s, tickId=%s, amount=%s", data.AssetOutpoint, data.TickId, data.Amount)

	// 验证必填字段
	if data.AssetOutpoint == "" {
		return saveInvalidArrival(pinNode, "assetOutpoint is required")
	}
	if data.Amount == "" {
		return saveInvalidArrival(pinNode, "amount is required")
	}
	if data.TickId == "" {
		return saveInvalidArrival(pinNode, "tickId is required")
	}

	// 校验 locationIndex 范围（增强安全性）
	if data.LocationIndex < 0 {
		return saveInvalidArrival(pinNode, "locationIndex must be non-negative")
	}
	if data.LocationIndex > 100 { // 防止过大的索引值
		return saveInvalidArrival(pinNode, "locationIndex too large (max 100)")
	}

	// 解析金额
	amount, err := decimal.NewFromString(string(data.Amount))
	if err != nil {
		return saveInvalidArrival(pinNode, "invalid amount format: "+err.Error())
	}
	if amount.LessThanOrEqual(decimal.Zero) {
		return saveInvalidArrival(pinNode, "amount must be greater than 0")
	}

	// 尝试获取 tickId 信息 (跨链情况下可能不存在于目标链)
	var tickName string
	tickInfo, err := PebbleStore.GetMrc20TickInfo(data.TickId, "")
	if err == nil {
		tickName = tickInfo.Tick
	} else {
		log.Printf("[Arrival] tickId %s not found locally, may be cross-chain arrival", data.TickId)
	}

	// 获取接收地址 (output[locationIndex] 的地址)
	// 使用带区块高度的版本，支持没有 txindex 的节点
	toAddress, err := getAddressFromOutputWithHeight(pinNode.ChainName, pinNode.GenesisTransaction, data.LocationIndex, pinNode.GenesisHeight)
	if err != nil {
		return saveInvalidArrival(pinNode, "invalid locationIndex: "+err.Error())
	}

	// 检查是否已存在相同 assetOutpoint 的 arrival
	existingArrival, _ := PebbleStore.GetMrc20ArrivalByAssetOutpoint(data.AssetOutpoint)
	if existingArrival != nil {
		// 如果已存在 arrival 且当前是区块确认（非 mempool），可能是 mempool→确认 的过渡
		// 🔧 修复：也处理 Invalid 状态（mempool 时可能因无法解析 ToAddress 而失败）
		if pinNode.GenesisHeight > 0 && (existingArrival.Status == mrc20.ArrivalStatusPending || existingArrival.Status == mrc20.ArrivalStatusInvalid) {
			// 更新区块高度并重新处理（从 mempool 变为确认）
			log.Printf("[Arrival] Existing arrival %s transitioning from mempool to block %d (status=%d)", existingArrival.PinId, pinNode.GenesisHeight, existingArrival.Status)

			// 检查是否已存在源链 UTXO 和 tick 信息（用于后续验证）
			var tickName string
			sourceUtxo, _ := PebbleStore.GetMrc20UtxoByTxPoint(data.AssetOutpoint, false)
			if sourceUtxo != nil {
				tickName = sourceUtxo.Tick
				// 验证 tickId 匹配
				if sourceUtxo.Mrc20Id != data.TickId {
					log.Printf("[Arrival] ❌ TickId mismatch: expected %s, got %s", sourceUtxo.Mrc20Id, data.TickId)
					return saveInvalidArrival(pinNode, fmt.Sprintf("tickId mismatch: expected %s, got %s", sourceUtxo.Mrc20Id, data.TickId))
				}
			} else {
				tickInfo, tickErr := PebbleStore.GetMrc20TickInfo(data.TickId, "")
				if tickErr == nil {
					tickName = tickInfo.Tick
				}
			}

			// 更新 arrival 信息（从 Invalid → Pending 或保持 Pending）
			existingArrival.BlockHeight = pinNode.GenesisHeight
			existingArrival.Timestamp = pinNode.Timestamp
			existingArrival.Status = mrc20.ArrivalStatusPending // 重置为 Pending
			existingArrival.Tick = tickName
			existingArrival.TickId = data.TickId
			existingArrival.LocationIndex = data.LocationIndex

			// 🔧 修复：重新解析 ToAddress（mempool 时可能为空或解析失败）
			if existingArrival.ToAddress == "" || existingArrival.Status == mrc20.ArrivalStatusInvalid {
				log.Printf("[Arrival] 🔍 Re-parsing ToAddress for arrival %s (was empty or invalid in mempool)", existingArrival.PinId)
				if toAddress != "" {
					existingArrival.ToAddress = toAddress
					log.Printf("[Arrival] ✅ Updated ToAddress: %s", toAddress)
				} else {
					log.Printf("[Arrival] ⚠️  ToAddress still empty after re-parsing")
				}
			}

			PebbleStore.SaveMrc20Arrival(existingArrival)
			// 重新处理 pending teleport（现在会执行完整 teleport）
			processPendingTeleportForArrival(existingArrival)
			return nil
		}
		return saveInvalidArrival(pinNode, "arrival already exists for this assetOutpoint")
	}

	// 获取源链 UTXO 并检查状态
	sourceChain := ""
	sourceUtxo, _ := PebbleStore.GetMrc20UtxoByTxPoint(data.AssetOutpoint, false) // 不检查状态

	if sourceUtxo != nil {
		sourceChain = sourceUtxo.Chain

		// 验证 tickId 匹配
		if sourceUtxo.Mrc20Id != data.TickId {
			return saveInvalidArrival(pinNode, fmt.Sprintf("tickId mismatch: expected %s, got %s", sourceUtxo.Mrc20Id, data.TickId))
		}
		// 验证金额必须是 UTXO 的全部金额
		if !sourceUtxo.AmtChange.Equal(amount) {
			return saveInvalidArrival(pinNode, fmt.Sprintf("amount must be the full UTXO amount: expected %s, got %s", sourceUtxo.AmtChange.String(), amount.String()))
		}
		// 获取 tick 名称
		if tickName == "" {
			tickName = sourceUtxo.Tick
		}

		// 根据 UTXO 状态处理
		switch sourceUtxo.Status {
		case mrc20.UtxoStatusSpent: // -1: 已消耗
			// 检查是否有对应的 teleport 记录
			if PebbleStore.CheckTeleportExistsByAssetOutpoint(data.AssetOutpoint) {
				// 跃迁已完成，arrival 也标记为完成
				log.Printf("[Arrival] UTXO %s already teleported, marking arrival as completed", data.AssetOutpoint)
				return saveCompletedArrival(pinNode, data, tickName, toAddress, sourceChain, "UTXO already teleported")
			}
			// UTXO 被其他操作花费了，跃迁失败
			log.Printf("[Arrival] UTXO %s spent by other operation, teleport failed", data.AssetOutpoint)
			return saveInvalidArrival(pinNode, fmt.Sprintf("UTXO %s already spent by other operation, teleport failed", data.AssetOutpoint))

		case mrc20.UtxoStatusTeleportPending: // 1: 等待跃迁
			// UTXO 已处于 pending 状态，说明 teleport transfer 先出块
			log.Printf("[Arrival] UTXO %s is in teleport pending state, processing pending teleport", data.AssetOutpoint)
		}
	}

	// 创建 arrival 记录 (状态为 pending)
	arrival := &mrc20.Mrc20Arrival{
		PinId:         pinNode.Id,
		TxId:          pinNode.GenesisTransaction,
		AssetOutpoint: data.AssetOutpoint,
		Amount:        amount,
		TickId:        data.TickId,
		Tick:          tickName,
		LocationIndex: data.LocationIndex,
		ToAddress:     toAddress,
		Chain:         pinNode.ChainName,
		SourceChain:   sourceChain,
		Status:        mrc20.ArrivalStatusPending,
		BlockHeight:   pinNode.GenesisHeight,
		Timestamp:     pinNode.Timestamp,
	}

	err = PebbleStore.SaveMrc20Arrival(arrival)
	if err != nil {
		log.Printf("[Arrival] ❌ Failed to save arrival: %v", err)
		return err
	}

	log.Printf("[Arrival] ✅ Saved arrival successfully: pinId=%s, status=pending, waiting for teleport or block confirmation", arrival.PinId)

	// 检查是否有等待此 arrival 的 pending teleport
	processPendingTeleportForArrival(arrival)

	// ✅ V2 架构：processPendingTeleportForArrival 会处理所有逻辑（包括流水记录）
	// 直接返回，不再执行后面的 V1 旧代码
	return nil
}

// ========== 以下是旧的 V1 Teleport 逻辑，已被 V2 替代，保留仅用于向后兼容 ==========

// executeTeleportTransfer 实际执行 teleport 转账 (V1 Legacy, deprecated in V2)
// 此函数在 arrival 已存在且验证通过的情况下调用
func executeTeleportTransferV1_Deprecated(pinNode *pin.PinInscription, data mrc20.Mrc20TeleportTransferData, sourceUtxo *mrc20.Mrc20Utxo, arrival *mrc20.Mrc20Arrival, isMempool bool) ([]*mrc20.Mrc20Utxo, error) {
	// V1 Logic - This code is no longer executed in V2 architecture
	// Kept for reference only, all teleport processing now handled by V2 state machine
	log.Printf("[TeleportV1] ⚠️  executeTeleportTransferV1_Deprecated called (should not happen in V2)")
	return nil, fmt.Errorf("V1 teleport logic deprecated, use V2 ProcessTeleportV2")
}

// 注意：这是V1 teleport的逻辑，V2架构使用不同的机制
// 保留此函数以保持兼容性，但V2不再使用
func saveTeleportPendingIn(arrival *mrc20.Mrc20Arrival, pending *mrc20.PendingTeleport) {
	// V1 Legacy code - V2 uses different mechanism
	log.Printf("[TeleportV1] saveTeleportPendingIn called (deprecated in V2)")
	/* V1 Implementation - Commented out
	amount, _ := decimal.NewFromString(pending.Amount)
	pendingIn := &mrc20.TeleportPendingIn{
		Coord:       arrival.PinId,
		ToAddress:   arrival.ToAddress,
		TickId:      arrival.TickId,
		Tick:        arrival.Tick,
		Amount:      amount,
		Chain:       arrival.Chain,
		SourceChain: pending.SourceChain,
		FromAddress: pending.FromAddress,
		TeleportTx:  pending.TxId,
		ArrivalTx:   arrival.TxId,
		BlockHeight: pending.BlockHeight, // 使用 teleport 的区块高度
		Timestamp:   pending.Timestamp,
	}
	err := PebbleStore.SaveTeleportPendingIn(pendingIn)
	if err != nil {
		log.Printf("SaveTeleportPendingIn error: %v", err)
	} else {
		log.Printf("[TeleportPendingIn] Saved pending in for address %s, tick %s, amount %s",
			arrival.ToAddress, arrival.Tick, amount.String())
	}

	// 【新架构】更新目标链 AccountBalance: PendingIn += amount
	err = PebbleStore.UpdateMrc20AccountBalance(
		arrival.Chain,
		arrival.ToAddress,
		arrival.TickId,
		arrival.Tick,
		decimal.Zero, // Balance 不变
		decimal.Zero, // PendingOut 不变
		amount,       // PendingIn += amount
		0,            // UTXO 数量不变
		arrival.TxId,
		arrival.BlockHeight,
		arrival.Timestamp,
	)
	if err != nil {
		log.Printf("[ERROR] UpdateMrc20AccountBalance failed for PendingIn: %v", err)
	}
	*/
}

// processPendingTeleportForArrival 处理等待特定 arrival 的 pending teleport
// 当 arrival 被处理后调用，检查是否有 teleport 在等待这个 arrival
// 【选项 B】严格等待确认：
// - mempool 时只保存 TeleportPendingIn（接收方 PendingIn += amount），不执行 teleport
// - 区块确认时才执行完整的 teleport
func processPendingTeleportForArrival(arrival *mrc20.Mrc20Arrival) {
	log.Printf("[PendingPair] 🔍 双向检查: Arrival已到达, 检查是否有等待的Transfer (coord=%s)", arrival.PinId)

	// 从新的PendingTeleport存储中查找
	pending, err := GetPendingTeleport(arrival.PinId)
	if err != nil {
		// 没有等待的 teleport，arrival 等待 transfer 到达
		log.Printf("[PendingPair] ℹ️  No pending transfer found, arrival is waiting for transfer to arrive")
		return
	}

	log.Printf("[PendingPair] ✅ Found pending transfer! type=%s, sourceTx=%s, createdAt=%d",
		pending.Type, pending.SourceTxId, pending.CreatedAt)

	// 验证是Transfer类型（Type="transfer"表示transfer先到达）
	if pending.Type != "transfer" {
		log.Printf("[PendingPair] ⚠️  Unexpected pending type: %s, skipping", pending.Type)
		return
	}

	// 反序列化pinNode
	var pinNode pin.PinInscription
	err = sonic.Unmarshal([]byte(pending.PinNodeJson), &pinNode)
	if err != nil {
		log.Printf("[PendingPair] ❌ Failed to unmarshal pinNode: %v", err)
		DeletePendingTeleport(pending.Coord)
		return
	}

	// ⚠️ 重要：重新从数据库加载最新的 pinNode
	// 因为缓存的 pinNode 可能是 mempool 阶段保存的（GenesisHeight=-1）
	// 现在 transfer 可能已经出块了，需要使用最新的高度信息
	latestPinNode, err := PebbleStore.GetPinById(pinNode.Id)
	if err != nil {
		log.Printf("[PendingPair] ⚠️  Failed to reload pinNode from DB: %v, using cached version", err)
		latestPinNode = pinNode // 降级：使用缓存的版本
	} else {
		log.Printf("[PendingPair] 🔄 Reloaded pinNode: pinId=%s, height changed from %d to %d",
			pinNode.Id, pinNode.GenesisHeight, latestPinNode.GenesisHeight)
	}

	// 重新触发teleport处理（此时arrival已存在）
	log.Printf("[PendingPair] 🔄 Retriggering teleport processing: pinId=%s, coord=%s", latestPinNode.Id, pending.Coord)

	// 调用V2处理逻辑
	isMempool := latestPinNode.GenesisHeight <= 0
	err = ProcessTeleportV2(&latestPinNode, pending.Data, isMempool)
	if err != nil {
		log.Printf("[PendingPair] ⚠️  Retry failed: %v, will retry later", err)
		// 更新重试计数
		pending.RetryCount++
		pending.LastCheckAt = time.Now().Unix()
		SavePendingTeleport(pending)
		return
	}

	// 处理成功，删除pending记录
	DeletePendingTeleport(pending.Coord)
	log.Printf("[PendingPair] ✅ Successfully paired and processed: transfer=%s + arrival=%s",
		pending.SourceTxId, arrival.TxId)
}

// saveCompletedArrival 保存已完成的 arrival 记录
func saveCompletedArrival(pinNode *pin.PinInscription, data mrc20.Mrc20ArrivalData, tickName, toAddress, sourceChain, msg string) error {
	log.Printf("[Arrival] ✅ Saving completed arrival: pinId=%s, assetOutpoint=%s, msg=%s", pinNode.Id, data.AssetOutpoint, msg)
	amount, _ := decimal.NewFromString(string(data.Amount))
	arrival := &mrc20.Mrc20Arrival{
		PinId:         pinNode.Id,
		TxId:          pinNode.GenesisTransaction,
		AssetOutpoint: data.AssetOutpoint,
		Amount:        amount,
		TickId:        data.TickId,
		Tick:          tickName,
		LocationIndex: data.LocationIndex,
		ToAddress:     toAddress,
		Chain:         pinNode.ChainName,
		SourceChain:   sourceChain,
		Status:        mrc20.ArrivalStatusCompleted,
		Msg:           msg,
		BlockHeight:   pinNode.GenesisHeight,
		Timestamp:     pinNode.Timestamp,
	}
	return PebbleStore.SaveMrc20Arrival(arrival)
}

// saveInvalidArrival 保存无效的 arrival 记录
func saveInvalidArrival(pinNode *pin.PinInscription, msg string) error {
	log.Printf("[Arrival] ❌ Saving invalid arrival: pinId=%s, msg=%s", pinNode.Id, msg)
	arrival := &mrc20.Mrc20Arrival{
		PinId:       pinNode.Id,
		TxId:        pinNode.GenesisTransaction,
		Chain:       pinNode.ChainName,
		Status:      mrc20.ArrivalStatusInvalid,
		Msg:         msg,
		BlockHeight: pinNode.GenesisHeight,
		Timestamp:   pinNode.Timestamp,
	}
	return PebbleStore.SaveMrc20Arrival(arrival)
}

// getAddressFromOutput 从交易的指定 output 获取地址
func getAddressFromOutput(chainName, txid string, outputIndex int) (string, error) {
	txb, err := GetTransactionWithCache(chainName, txid)
	if err != nil {
		return "", fmt.Errorf("get transaction error: %w", err)
	}

	if outputIndex < 0 || outputIndex >= len(txb.MsgTx().TxOut) {
		return "", fmt.Errorf("output index out of range: %d", outputIndex)
	}

	out := txb.MsgTx().TxOut[outputIndex]
	class, addresses, _, err := txscript.ExtractPkScriptAddrs(out.PkScript, getBtcNetParams(chainName))
	if err != nil {
		return "", fmt.Errorf("extract address error: %w", err)
	}
	if class.String() == "nulldata" || class.String() == "nonstandard" || len(addresses) == 0 {
		return "", fmt.Errorf("invalid output type: %s", class.String())
	}

	return addresses[0].String(), nil
}

// getAddressFromOutputWithHeight 从交易的指定 output 获取地址，支持通过区块高度获取交易
// 当节点没有 txindex 时，可以通过区块高度从区块中获取交易
func getAddressFromOutputWithHeight(chainName, txid string, outputIndex int, blockHeight int64) (string, error) {
	// 先尝试直接获取交易
	txb, err := GetTransactionWithCache(chainName, txid)
	if err == nil {
		if outputIndex < 0 || outputIndex >= len(txb.MsgTx().TxOut) {
			return "", fmt.Errorf("output index out of range: %d", outputIndex)
		}
		out := txb.MsgTx().TxOut[outputIndex]
		return extractAddressFromPkScript(out.PkScript, chainName)
	}

	// GetTransaction 失败，尝试从区块获取
	if blockHeight <= 0 {
		return "", fmt.Errorf("get transaction failed and no block height provided: %w", err)
	}

	//log.Printf("[MRC20] GetTransaction failed for %s, trying to get from block %d", txid, blockHeight)

	block, err := ChainAdapter[chainName].GetBlock(blockHeight)
	if err != nil {
		return "", fmt.Errorf("get block %d failed: %w", blockHeight, err)
	}

	// 从区块中查找交易 - 处理不同链的区块类型
	switch b := block.(type) {
	case *bsvwire.MsgBlock:
		// bsvd/wire.MsgBlock (用于 MVC)
		for _, blockTx := range b.Transactions {
			if blockTx.TxHash().String() == txid {
				if outputIndex < 0 || outputIndex >= len(blockTx.TxOut) {
					return "", fmt.Errorf("output index out of range: %d", outputIndex)
				}
				return extractAddressFromPkScript(blockTx.TxOut[outputIndex].PkScript, chainName)
			}
		}
	case *btcwire.MsgBlock:
		// btcsuite/btcd/wire.MsgBlock (用于 BTC, Doge)
		for _, blockTx := range b.Transactions {
			if blockTx.TxHash().String() == txid {
				if outputIndex < 0 || outputIndex >= len(blockTx.TxOut) {
					return "", fmt.Errorf("output index out of range: %d", outputIndex)
				}
				return extractAddressFromPkScript(blockTx.TxOut[outputIndex].PkScript, chainName)
			}
		}
	case *btcutil.Block:
		// btcutil.Block wrapper
		for _, blockTx := range b.MsgBlock().Transactions {
			if blockTx.TxHash().String() == txid {
				if outputIndex < 0 || outputIndex >= len(blockTx.TxOut) {
					return "", fmt.Errorf("output index out of range: %d", outputIndex)
				}
				return extractAddressFromPkScript(blockTx.TxOut[outputIndex].PkScript, chainName)
			}
		}
	default:
		return "", fmt.Errorf("unsupported block type: %T", block)
	}

	return "", fmt.Errorf("transaction %s not found in block %d", txid, blockHeight)
}

// extractAddressFromPkScript 从 PkScript 提取地址
func extractAddressFromPkScript(pkScript []byte, chainName string) (string, error) {
	class, addresses, _, err := txscript.ExtractPkScriptAddrs(pkScript, getBtcNetParams(chainName))
	if err != nil {
		return "", fmt.Errorf("extract address error: %w", err)
	}
	if class.String() == "nulldata" || class.String() == "nonstandard" || len(addresses) == 0 {
		return "", fmt.Errorf("invalid output type: %s", class.String())
	}
	return addresses[0].String(), nil
}

// 使用新的V2架构（状态机）
var UseTeleportV2 = true

// processTeleportTransfer 处理 teleport 类型的 transfer
// 返回 true 表示是 teleport 并且已处理，返回 false 表示不是 teleport 需要走普通 transfer 流程
func processTeleportTransfer(pinNode *pin.PinInscription, isMempool bool) (bool, []*mrc20.Mrc20Utxo, error) {
	log.Printf("[Teleport] 🔄 processTeleportTransfer called: pinId=%s, txId=%s, isMempool=%v", pinNode.Id, pinNode.GenesisTransaction, isMempool)

	// 尝试解析为 teleport 格式 - 支持对象或数组格式
	var teleportData []mrc20.Mrc20TeleportTransferData

	// 先尝试解析为数组
	err := json.Unmarshal(pinNode.ContentBody, &teleportData)
	if err != nil {
		// 数组解析失败，尝试解析为单个对象
		var singleData mrc20.Mrc20TeleportTransferData
		err = json.Unmarshal(pinNode.ContentBody, &singleData)
		if err != nil {
			log.Printf("[Teleport] ❌ JSON parse failed: %v", err)
			return false, nil, nil // 不是有效的 JSON，走普通 transfer
		}
		// 单个对象转为数组
		teleportData = []mrc20.Mrc20TeleportTransferData{singleData}
	}

	// 检查是否有 teleport 类型的项
	hasTeleport := false
	for _, item := range teleportData {
		if item.Type == "teleport" {
			hasTeleport = true
			break
		}
	}
	if !hasTeleport {
		return false, nil, nil // 没有 teleport 项，走普通 transfer
	}

	// 使用新的V2架构处理
	if UseTeleportV2 {
		for _, item := range teleportData {
			if item.Type != "teleport" {
				continue
			}

			// 调用新的V2处理逻辑
			err := ProcessTeleportV2(pinNode, item, isMempool)
			if err != nil {
				log.Printf("[TeleportV2] ❌ Processing failed: %v", err)
				// V2失败，回退到V1处理（可选）
				// 继续处理下一个teleport
			}
		}

		// V2架构不返回UTXO列表，由状态机内部管理
		return true, nil, nil
	}

	// === 以下是旧的V1逻辑（保留作为fallback）===

	// 处理 teleport transfer
	var mrc20UtxoList []*mrc20.Mrc20Utxo
	var failedTeleport bool
	var failedMsg string

	for _, item := range teleportData {
		if item.Type != "teleport" {
			// 非 teleport 项暂时跳过 (TODO: 可以支持混合 transfer)
			continue
		}

		// 验证 teleport 数据
		utxoList, err := validateAndProcessTeleport(pinNode, item, isMempool)
		if err != nil {
			log.Printf("[Teleport] ❌ validateAndProcessTeleport error for coord %s: %v", item.Coord, err)
			failedTeleport = true
			failedMsg = err.Error()
			// teleport 验证失败，继续检查其他项，但需要记录失败
			continue
		}

		mrc20UtxoList = append(mrc20UtxoList, utxoList...)
	}

	// 如果所有 teleport 都失败了，需要处理交易输入中的 MRC20 UTXO
	// 将它们转到第一个有效输出地址，防止 UTXO 状态不一致
	if failedTeleport && len(mrc20UtxoList) == 0 {
		log.Printf("[Teleport] All teleports failed for tx %s, handling input UTXOs: %s", pinNode.GenesisTransaction, failedMsg)
		fallbackUtxoList := handleFailedTeleportInputs(pinNode, failedMsg, isMempool)
		mrc20UtxoList = append(mrc20UtxoList, fallbackUtxoList...)
	}

	return true, mrc20UtxoList, nil
}

// validateAndProcessTeleport 验证并处理单个 teleport 项
// 核心逻辑：
// 1. 从交易输入获取匹配的 MRC20 UTXO
// 2. 验证 UTXO 状态必须是可用(0)
// 3. 如果 arrival 存在且 pending → 执行跃迁
// 4. 如果 arrival 不存在 → UTXO 状态设为 pending(1)，加入等待队列
// 注意：这是V1 teleport的逻辑，V2架构使用ProcessTeleportV2
func validateAndProcessTeleport(pinNode *pin.PinInscription, data mrc20.Mrc20TeleportTransferData, isMempool bool) ([]*mrc20.Mrc20Utxo, error) {
	// V1 Logic deprecated - Use V2 ProcessTeleportV2 instead
	return nil, fmt.Errorf("validateAndProcessTeleport is deprecated in V2, use ProcessTeleportV2")
}

// findTeleportSourceUtxo 从交易输入中查找符合 teleport 条件的 MRC20 UTXO
func findTeleportSourceUtxo(pinNode *pin.PinInscription, tickId string, amount decimal.Decimal) (*mrc20.Mrc20Utxo, error) {
	log.Printf("[TeleportV2] 🔍 Finding source UTXO: txId=%s, tickId=%s, amount=%s, sender=%s",
		pinNode.GenesisTransaction, tickId, amount.String(), pinNode.Address)

	// 获取交易
	txb, err := GetTransactionWithCache(pinNode.ChainName, pinNode.GenesisTransaction)
	if err != nil {
		return nil, fmt.Errorf("get transaction error: %w", err)
	}

	// 获取所有交易输入的 txpoint
	var inputList []string
	for i, in := range txb.MsgTx().TxIn {
		s := fmt.Sprintf("%s:%d", in.PreviousOutPoint.Hash.String(), in.PreviousOutPoint.Index)
		inputList = append(inputList, s)
		log.Printf("[TeleportV2] 📥 Input[%d]: %s", i, s)
	}

	log.Printf("[TeleportV2] 🔍 Searching for MRC20 UTXOs in %d inputs", len(inputList))

	// 查找输入中的 MRC20 UTXO（包括 pending 状态的，用于检查是否重复）
	var foundUtxos []*mrc20.Mrc20Utxo
	for _, txPoint := range inputList {
		utxo, err := PebbleStore.GetMrc20UtxoByTxPoint(txPoint, false) // 不检查状态
		if err != nil {
			log.Printf("[TeleportV2] ⚪ Input %s: not MRC20 UTXO (not found)", txPoint)
			continue // 不是 MRC20 UTXO，跳过
		}

		foundUtxos = append(foundUtxos, utxo)
		log.Printf("[TeleportV2] ✅ Found MRC20 UTXO: %s, tickId=%s, amount=%s, status=%d, owner=%s",
			txPoint, utxo.Mrc20Id, utxo.AmtChange.String(), utxo.Status, utxo.ToAddress)

		// 匹配 tickId 和金额
		if utxo.Mrc20Id == tickId && utxo.AmtChange.Equal(amount) {
			log.Printf("[TeleportV2] ✅ Found matching UTXO: %s", txPoint)

			// 验证 UTXO 所有者
			if utxo.ToAddress != pinNode.Address {
				log.Printf("[TeleportV2] ❌ Authorization failed: UTXO owner=%s, sender=%s", utxo.ToAddress, pinNode.Address)
				return nil, fmt.Errorf("not authorized to spend UTXO %s: owner is %s, sender is %s",
					txPoint, utxo.ToAddress, pinNode.Address)
			}

			log.Printf("[TeleportV2] 🎯 Selected source UTXO: %s (status=%d)", txPoint, utxo.Status)
			return utxo, nil
		} else {
			log.Printf("[TeleportV2] ❌ UTXO %s doesn't match: tickId match=%v, amount match=%v",
				txPoint, utxo.Mrc20Id == tickId, utxo.AmtChange.Equal(amount))
		}
	}

	log.Printf("[TeleportV2] ❌ No matching UTXO found. Summary: found %d MRC20 UTXOs, need tickId=%s amount=%s",
		len(foundUtxos), tickId, amount.String())
	for i, utxo := range foundUtxos {
		log.Printf("[TeleportV2] UTXO[%d]: %s, tickId=%s, amount=%s",
			i, utxo.TxPoint, utxo.Mrc20Id, utxo.AmtChange.String())
	}

	return nil, fmt.Errorf("no matching MRC20 UTXO found in transaction inputs for tickId %s, amount %s", tickId, amount.String())
}

// handleFailedTeleportInputs 处理 teleport 失败时的输入 UTXO
// 将交易输入中的所有 MRC20 UTXO 转到第一个有效输出地址
// 这确保了即使 teleport 验证失败，UTXO 状态仍然与链上一致
func handleFailedTeleportInputs(pinNode *pin.PinInscription, failedMsg string, isMempool bool) []*mrc20.Mrc20Utxo {
	var mrc20UtxoList []*mrc20.Mrc20Utxo

	// 获取交易
	txb, err := GetTransactionWithCache(pinNode.ChainName, pinNode.GenesisTransaction)
	if err != nil {
		log.Println("handleFailedTeleportInputs: GetTransactionWithCache error:", err)
		return nil
	}

	// 获取第一个有效输出地址
	toAddress := ""
	outputIdx := 0
	outputValue := int64(0)
	for i, out := range txb.MsgTx().TxOut {
		class, addresses, _, _ := txscript.ExtractPkScriptAddrs(out.PkScript, getBtcNetParams(pinNode.ChainName))
		if class.String() != "nulldata" && class.String() != "nonstandard" && len(addresses) > 0 {
			toAddress = addresses[0].String()
			outputIdx = i
			outputValue = out.Value
			break
		}
	}
	if toAddress == "" {
		log.Println("handleFailedTeleportInputs: no valid output address found")
		return nil
	}

	// 获取所有交易输入的 txpoint
	var inputList []string
	for _, in := range txb.MsgTx().TxIn {
		s := fmt.Sprintf("%s:%d", in.PreviousOutPoint.Hash.String(), in.PreviousOutPoint.Index)
		inputList = append(inputList, s)
	}

	// 查找并处理输入中的 MRC20 UTXO
	list, err := PebbleStore.GetMrc20UtxoByOutPutList(inputList, isMempool)
	if err != nil || len(list) == 0 {
		log.Println("handleFailedTeleportInputs: no MRC20 UTXOs in inputs")
		return nil
	}

	// 按 tickId 聚合金额
	utxoByTick := make(map[string]*mrc20.Mrc20Utxo)
	for _, item := range list {
		// 标记输入 UTXO 为已消耗
		spentUtxo := *item
		spentUtxo.Status = mrc20.UtxoStatusSpent
		spentUtxo.OperationTx = pinNode.GenesisTransaction
		spentUtxo.Msg = fmt.Sprintf("teleport failed: %s", failedMsg)
		mrc20UtxoList = append(mrc20UtxoList, &spentUtxo)

		// 聚合到新 UTXO
		if v, ok := utxoByTick[item.Mrc20Id]; ok {
			v.AmtChange = v.AmtChange.Add(item.AmtChange)
		} else {
			utxoByTick[item.Mrc20Id] = &mrc20.Mrc20Utxo{
				Mrc20Id:     item.Mrc20Id,
				Tick:        item.Tick,
				Verify:      true,
				PinId:       pinNode.Id,
				BlockHeight: pinNode.GenesisHeight,
				MrcOption:   mrc20.OptionTeleportTransfer,
				FromAddress: item.ToAddress,
				ToAddress:   toAddress,
				Chain:       pinNode.ChainName,
				Timestamp:   pinNode.Timestamp,
				TxPoint:     fmt.Sprintf("%s:%d", pinNode.GenesisTransaction, outputIdx),
				PointValue:  uint64(outputValue),
				PinContent:  string(pinNode.ContentBody),
				Index:       0,
				AmtChange:   item.AmtChange,
				Status:      mrc20.UtxoStatusAvailable,
				Msg:         fmt.Sprintf("teleport failed, fallback to address: %s", failedMsg),
				OperationTx: pinNode.GenesisTransaction,
			}
		}
	}

	// 添加新的 UTXO（转到第一个有效输出）
	for _, newUtxo := range utxoByTick {
		mrc20UtxoList = append(mrc20UtxoList, newUtxo)
	}

	log.Printf("[Teleport] Failed teleport handled: %d input UTXOs transferred to %s", len(list), toAddress)
	return mrc20UtxoList
}

// executeTeleportTransfer 实际执行 teleport 转账
// 此函数在 arrival 已存在且验证通过的情况下调用
func executeTeleportTransfer(pinNode *pin.PinInscription, data mrc20.Mrc20TeleportTransferData, sourceUtxo *mrc20.Mrc20Utxo, arrival *mrc20.Mrc20Arrival, isMempool bool) ([]*mrc20.Mrc20Utxo, error) {
	var mrc20UtxoList []*mrc20.Mrc20Utxo

	teleportAmount, _ := decimal.NewFromString(data.Amount)

	// ========== 执行跃迁 ==========

	// 标记源 UTXO 为已消耗 (teleported)
	spentUtxo := *sourceUtxo
	spentUtxo.Status = mrc20.UtxoStatusSpent
	// 注意：不修改 MrcOption，保留原始操作类型
	spentUtxo.OperationTx = pinNode.GenesisTransaction
	spentUtxo.Msg = fmt.Sprintf("teleported to %s via coord %s", data.Chain, data.Coord)
	mrc20UtxoList = append(mrc20UtxoList, &spentUtxo)

	// 在目标链创建新 UTXO
	newUtxo := mrc20.Mrc20Utxo{
		Tick:        sourceUtxo.Tick,
		Mrc20Id:     sourceUtxo.Mrc20Id,
		TxPoint:     fmt.Sprintf("%s:%d", arrival.TxId, arrival.LocationIndex),
		PointValue:  0, // 目标链的 output value 需要从交易获取
		PinId:       arrival.PinId,
		PinContent:  string(pinNode.ContentBody),
		Verify:      true,
		BlockHeight: arrival.BlockHeight,
		MrcOption:   mrc20.OptionTeleportTransfer,
		FromAddress: pinNode.Address, // 源链发送者
		ToAddress:   arrival.ToAddress,
		AmtChange:   teleportAmount,
		Status:      mrc20.UtxoStatusAvailable,
		Chain:       arrival.Chain,
		Index:       0,
		Timestamp:   arrival.Timestamp,
		OperationTx: pinNode.GenesisTransaction,
		Msg:         fmt.Sprintf("teleported from %s via coord %s", pinNode.ChainName, data.Coord),
	}
	mrc20UtxoList = append(mrc20UtxoList, &newUtxo)

	// ========== 新架构：删除源 UTXO，更新余额，写入流水 ==========

	// 1. 删除源链的 UTXO（从数据库中移除）
	err := PebbleStore.DeleteMrc20Utxo(spentUtxo.TxPoint, spentUtxo.ToAddress, spentUtxo.Mrc20Id)
	if err != nil {
		log.Printf("[ERROR] DeleteMrc20Utxo failed for teleport source %s: %v", spentUtxo.TxPoint, err)
	}

	// 2. 保存目标链的新 UTXO
	err = PebbleStore.SaveMrc20Pin([]mrc20.Mrc20Utxo{newUtxo})
	if err != nil {
		log.Printf("[ERROR] SaveMrc20Pin failed for teleport target %s: %v", newUtxo.TxPoint, err)
	}

	// 3. 更新源链余额
	// 根据 sourceUtxo 原始状态决定如何更新：
	// - 如果是 TeleportPending：Balance 已经在进入 pending 时减少了，现在只需 PendingOut -= amount
	// - 如果是 Available：arrival 先到达的情况，需要 Balance -= amount, UtxoCount--
	var sourceBalanceDelta, sourcePendingOutDelta decimal.Decimal
	var sourceUtxoCountDelta int
	if sourceUtxo.Status == mrc20.UtxoStatusTeleportPending {
		// 从 pending 状态完成：PendingOut -= amount，Balance 和 UtxoCount 不变（已经在进入 pending 时调整过了）
		sourceBalanceDelta = decimal.Zero
		sourcePendingOutDelta = teleportAmount.Neg()
		sourceUtxoCountDelta = 0
	} else {
		// 直接完成（arrival 已存在）：Balance -= amount, UtxoCount--，PendingOut 不变
		sourceBalanceDelta = teleportAmount.Neg()
		sourcePendingOutDelta = decimal.Zero
		sourceUtxoCountDelta = -1
	}

	err = PebbleStore.UpdateMrc20AccountBalance(
		pinNode.ChainName,
		pinNode.Address,
		sourceUtxo.Mrc20Id,
		sourceUtxo.Tick,
		sourceBalanceDelta,    // Balance 变化
		sourcePendingOutDelta, // PendingOut 变化
		decimal.Zero,          // PendingIn 不变
		sourceUtxoCountDelta,  // UtxoCount 变化
		pinNode.GenesisTransaction,
		pinNode.GenesisHeight,
		pinNode.Timestamp,
	)
	if err != nil {
		log.Printf("[ERROR] UpdateMrc20AccountBalance failed for source %s: %v", pinNode.Address, err)
	}

	// 4. 更新目标链余额
	// 检查是否有 PendingIn（通过 saveTeleportPendingIn 设置的）
	// - 如果有 PendingIn：PendingIn -= amount, Balance += amount
	// - 如果没有：直接 Balance += amount
	pendingIn, _ := PebbleStore.GetTeleportPendingInByCoord(arrival.PinId)
	var targetBalanceDelta, targetPendingInDelta decimal.Decimal
	if pendingIn != nil {
		// 有 PendingIn 记录：PendingIn -= amount, Balance += amount
		targetBalanceDelta = teleportAmount
		targetPendingInDelta = teleportAmount.Neg()
	} else {
		// 没有 PendingIn：直接 Balance += amount
		targetBalanceDelta = teleportAmount
		targetPendingInDelta = decimal.Zero
	}

	err = PebbleStore.UpdateMrc20AccountBalance(
		arrival.Chain,
		arrival.ToAddress,
		sourceUtxo.Mrc20Id,
		sourceUtxo.Tick,
		targetBalanceDelta,   // Balance 变化
		decimal.Zero,         // PendingOut 不变
		targetPendingInDelta, // PendingIn 变化
		1,                    // UTXO 数量 +1
		arrival.TxId,
		arrival.BlockHeight,
		arrival.Timestamp,
	)

	// 5. 删除 PendingIn 记录（如果存在）
	if pendingIn != nil {
		err = PebbleStore.DeleteTeleportPendingIn(arrival.PinId, arrival.ToAddress)
		if err != nil {
			log.Printf("[WARN] DeleteTeleportPendingIn failed: %v", err)
		}
	}

	// 6. 写入源链流水（teleport_out）- 发送方的支出记录
	sourceTx := &mrc20.Mrc20Transaction{
		Chain:        pinNode.ChainName,
		TxId:         pinNode.GenesisTransaction,
		TxPoint:      spentUtxo.TxPoint + "_out", // 使用花费的 UTXO 的 txpoint + _out 后缀
		TxIndex:      0,
		PinId:        pinNode.Id,
		TickId:       sourceUtxo.Mrc20Id,
		Tick:         sourceUtxo.Tick,
		TxType:       "teleport_out",
		Direction:    "out", // 支出
		Address:      pinNode.Address,
		FromAddress:  pinNode.Address,
		ToAddress:    arrival.ToAddress,
		Amount:       teleportAmount,
		IsChange:     false,
		SpentUtxos:   fmt.Sprintf("[\"%s\"]", spentUtxo.TxPoint),
		CreatedUtxos: "[]",
		BlockHeight:  pinNode.GenesisHeight,
		Timestamp:    pinNode.Timestamp,
		Msg:          fmt.Sprintf("teleport to %s", data.Chain),
		Status:       1,
		RelatedChain: data.Chain,
		RelatedTxId:  arrival.TxId,
		RelatedPinId: arrival.PinId,
	}
	err = PebbleStore.SaveMrc20Transaction(sourceTx)
	if err != nil {
		log.Printf("[ERROR] SaveMrc20Transaction failed for teleport_out: %v", err)
	}

	// 7. 写入目标链流水（teleport_in）- 接收方的收入记录
	targetTx := &mrc20.Mrc20Transaction{
		Chain:        arrival.Chain,
		TxId:         arrival.TxId,
		TxPoint:      newUtxo.TxPoint,
		TxIndex:      0,
		PinId:        arrival.PinId,
		TickId:       sourceUtxo.Mrc20Id,
		Tick:         sourceUtxo.Tick,
		TxType:       "teleport_in",
		Direction:    "in", // 收入
		Address:      arrival.ToAddress,
		FromAddress:  pinNode.Address,
		ToAddress:    arrival.ToAddress,
		Amount:       teleportAmount,
		IsChange:     false,
		SpentUtxos:   "[]",
		CreatedUtxos: fmt.Sprintf("[\"%s\"]", newUtxo.TxPoint),
		BlockHeight:  arrival.BlockHeight,
		Timestamp:    arrival.Timestamp,
		Msg:          fmt.Sprintf("teleport from %s", pinNode.ChainName),
		Status:       1,
		RelatedChain: pinNode.ChainName,
		RelatedTxId:  pinNode.GenesisTransaction,
		RelatedPinId: pinNode.Id,
	}
	err = PebbleStore.SaveMrc20Transaction(targetTx)
	if err != nil {
		log.Printf("[ERROR] SaveMrc20Transaction failed for teleport_in: %v", err)
	}

	// ========== 原有逻辑：更新 arrival 状态等 ==========

	// 更新 arrival 状态为已完成
	err = PebbleStore.UpdateMrc20ArrivalStatus(
		arrival.PinId,
		mrc20.ArrivalStatusCompleted,
		pinNode.Id,
		pinNode.ChainName,
		pinNode.GenesisTransaction,
		pinNode.Timestamp,
	)
	if err != nil {
		log.Println("UpdateMrc20ArrivalStatus error:", err)
	}

	// 删除 TeleportPendingIn 记录（跃迁完成，不再是 pending 状态）
	err = PebbleStore.DeleteTeleportPendingIn(arrival.PinId, arrival.ToAddress)
	if err != nil {
		log.Println("DeleteTeleportPendingIn error:", err)
	}

	// 保存 teleport 记录
	teleportRecord := &mrc20.Mrc20Teleport{
		PinId:          pinNode.Id,
		TxId:           pinNode.GenesisTransaction,
		TickId:         data.Id,
		Tick:           sourceUtxo.Tick,
		Amount:         teleportAmount,
		Coord:          data.Coord,
		FromAddress:    pinNode.Address,
		SourceChain:    pinNode.ChainName,
		TargetChain:    data.Chain,
		SpentUtxoPoint: sourceUtxo.TxPoint,
		Status:         1, // 完成
		BlockHeight:    pinNode.GenesisHeight,
		Timestamp:      pinNode.Timestamp,
	}
	err = PebbleStore.SaveMrc20Teleport(teleportRecord)
	if err != nil {
		log.Println("SaveMrc20Teleport error:", err)
	}

	log.Printf("Teleport completed: %s from %s to %s, amount: %s, coord: %s",
		sourceUtxo.Tick, pinNode.ChainName, data.Chain, teleportAmount.String(), data.Coord)

	return mrc20UtxoList, nil
}
