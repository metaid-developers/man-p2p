package man

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"man-p2p/mrc20"
	"man-p2p/pin"
	"os"
	"strings"
	"time"

	"github.com/bytedance/sonic"
	"github.com/shopspring/decimal"
)

// ProcessID 当前进程ID（用于分布式锁）
var ProcessID string

func init() {
	ProcessID = fmt.Sprintf("%d_%d", os.Getpid(), time.Now().Unix())
}

// GenerateTeleportID 生成唯一的 Teleport 事务ID
// 使用 coord + sourceTxId 确保幂等性
func GenerateTeleportID(coord, sourceTxId string) string {
	h := sha256.New()
	h.Write([]byte(coord + "|" + sourceTxId))
	return hex.EncodeToString(h.Sum(nil))
}

// ProcessTeleportV2 新架构的 Teleport 处理入口
// 支持 mempool 和 block 两种模式
func ProcessTeleportV2(pinNode *pin.PinInscription, data mrc20.Mrc20TeleportTransferData, isMempool bool) error {
	// 1. 生成唯一ID
	teleportID := GenerateTeleportID(data.Coord, pinNode.GenesisTransaction)

	log.Printf("[TeleportV2] 🚀 Processing: id=%s, coord=%s, txId=%s, isMempool=%v",
		teleportID, data.Coord, pinNode.GenesisTransaction, isMempool)

	// 2. 加载或创建 TeleportTransaction
	tx, err := LoadTeleportTransaction(teleportID)
	if err != nil {
		// 首次处理，创建新的事务记录
		tx = &mrc20.TeleportTransaction{
			ID:                teleportID,
			Coord:             data.Coord,
			SourceChain:       pinNode.ChainName,
			TargetChain:       data.Chain,
			SourceTxId:        pinNode.GenesisTransaction,
			SourcePinId:       pinNode.Id,
			TickId:            data.Id,
			FromAddress:       pinNode.Address,
			State:             mrc20.TeleportStateCreated,
			CreatedAt:         time.Now().Unix(),
			UpdatedAt:         time.Now().Unix(),
			SourceBlockHeight: pinNode.GenesisHeight,
		}

		// 解析金额
		amount, err := decimal.NewFromString(data.Amount)
		if err != nil {
			return fmt.Errorf("invalid amount: %w", err)
		}
		tx.Amount = amount

		log.Printf("[TeleportV2] ✨ Created new transaction: %s", teleportID)
	} else {
		log.Printf("[TeleportV2] 📂 Loaded existing transaction: id=%s, state=%s",
			teleportID, mrc20.GetStateName(tx.State))
	}

	// 3. 检查是否已完成（幂等性）
	if tx.State == mrc20.TeleportStateCompleted {
		log.Printf("[TeleportV2] ✅ Already completed: %s", teleportID)
		return nil
	}

	// 4. 检查是否失败（需要手动处理）
	if tx.State == mrc20.TeleportStateFailed {
		log.Printf("[TeleportV2] ❌ Failed previously: %s, reason: %s", teleportID, tx.FailureReason)
		return fmt.Errorf("teleport failed: %s", tx.FailureReason)
	}

	// 5. ✅ Block 阶段：更新 TeleportTransaction 的区块高度（mempool → block 转换）
	// 🔧 关键：流水记录在 Step 4 才创建，此时只需更新 tx.SourceBlockHeight
	// Step 4 创建流水时会使用更新后的 SourceBlockHeight
	if !isMempool && pinNode.GenesisHeight > 0 && tx.SourceBlockHeight != pinNode.GenesisHeight {
		log.Printf("[TeleportV2] 🔄 Updating SourceBlockHeight: %d -> %d", tx.SourceBlockHeight, pinNode.GenesisHeight)
		tx.SourceBlockHeight = pinNode.GenesisHeight
	}

	// 6. 尝试获取锁（防止并发处理）
	if !tx.AcquireLock(ProcessID, 5*time.Minute) {
		log.Printf("[TeleportV2] 🔒 Locked by another process: %s (lockedBy=%s, expires=%d)",
			teleportID, tx.LockedBy, tx.LockExpiresAt)
		return nil // 等待锁释放或过期
	}

	var execErr error
	defer func() {
		tx.ReleaseLock(ProcessID)
		// 如果是 "waiting for arrival" 错误，TeleportTransaction 已被删除，不需要保存
		if execErr != nil && strings.Contains(execErr.Error(), "waiting for arrival") {
			log.Printf("[TeleportV2] ⏸️  Skipping save (waiting for arrival)")
			return
		}
		SaveTeleportTransaction(tx) // 释放锁时保存状态
	}()

	// 6. 执行状态机
	operator := "mempool"
	if !isMempool {
		operator = "block"
	}

	execErr = ExecuteTeleportStateMachine(tx, pinNode, data, operator)
	return execErr
}

// ExecuteTeleportStateMachine 执行状态机
// 根据当前状态执行对应的操作，支持断点续传
func ExecuteTeleportStateMachine(tx *mrc20.TeleportTransaction, pinNode *pin.PinInscription, data mrc20.Mrc20TeleportTransferData, operator string) error {
	log.Printf("[TeleportV2] 🔄 State machine: currentState=%s", mrc20.GetStateName(tx.State))

	for !mrc20.IsTerminalState(tx.State) {
		var err error

		switch tx.State {
		case mrc20.TeleportStateCreated:
			err = stepLockSourceUTXO(tx, pinNode, data)

		case mrc20.TeleportStateSourceLocked:
			err = stepVerifyArrival(tx, pinNode, data)

		case mrc20.TeleportStateArrivalVerified:
			// Mempool 阶段到此为止，等待区块确认
			if pinNode.GenesisHeight <= 0 {
				log.Printf("[TeleportV2] ⏸️  Mempool stage completed, waiting for block confirmation")
				return SaveTeleportTransaction(tx)
			}
			err = stepMarkSourceSpent(tx, pinNode, data)

		case mrc20.TeleportStateSourceSpent:
			err = stepCreateTargetUTXO(tx, pinNode, data)

		case mrc20.TeleportStateTargetCreated:
			err = stepUpdateBalances(tx, pinNode, data)

		case mrc20.TeleportStateBalanceUpdated:
			err = stepFinalizeTeleport(tx, pinNode, data)

		default:
			err = fmt.Errorf("unknown state: %d", tx.State)
		}

		if err != nil {
			log.Printf("[TeleportV2] ❌ Step failed: state=%s, error=%v",
				mrc20.GetStateName(tx.State), err)

			// 🔧 特殊处理：如果是 "waiting for arrival" 错误，直接返回，不标记为失败
			if strings.Contains(err.Error(), "waiting for arrival") {
				log.Printf("[TeleportV2] ⏸️  Waiting for arrival, skipping failure marking")
				return err
			}

			// 根据错误类型决定是否回滚
			if shouldRollback(tx.State) {
				return rollbackTeleport(tx, err.Error())
			}

			// 标记失败
			tx.AddStateChange(mrc20.TeleportStateFailed, false, err.Error(), operator)
			SaveTeleportTransaction(tx)
			return err
		}

		// 保存每一步的状态
		if err := SaveTeleportTransaction(tx); err != nil {
			log.Printf("[TeleportV2] ⚠️  Failed to save state: %v", err)
		}
	}

	log.Printf("[TeleportV2] ✅ State machine completed: finalState=%s", mrc20.GetStateName(tx.State))
	return nil
}

// stepLockSourceUTXO 步骤1: 锁定源UTXO
func stepLockSourceUTXO(tx *mrc20.TeleportTransaction, pinNode *pin.PinInscription, data mrc20.Mrc20TeleportTransferData) error {
	log.Printf("[TeleportV2] Step 1: Locking source UTXO")

	// ⚠️ 双向等待机制：在锁定资产前，先验证Arrival存在
	// 如果Arrival不存在，保存到Pending队列等待Arrival到达后配对
	log.Printf("[TeleportV2] Pre-check: Verifying arrival exists before locking")
	arrival, err := PebbleStore.GetMrc20ArrivalByPinId(tx.Coord)
	if err != nil {
		// 🔧 防止重复保存：检查是否已经保存过 PendingTeleport
		existingPending, _ := GetPendingTeleport(tx.Coord)
		if existingPending != nil && existingPending.SourceTxId == pinNode.GenesisTransaction {
			log.Printf("[TeleportV2] ⏸️  Already in pending queue, coord=%s. Skipping duplicate save.", tx.Coord)
			// 删除 TeleportTransaction，避免重复处理
			DeleteTeleportTransaction(tx.ID)
			return fmt.Errorf("waiting for arrival (already in pending queue)")
		}

		// Arrival不存在：保存到Pending队列，等待Arrival到达
		log.Printf("[TeleportV2] ⏸️  Arrival not found yet, coord=%s. Saving to pending queue...", tx.Coord)

		// 序列化pinNode供后续重新处理
		pinNodeData, err := sonic.Marshal(pinNode)
		if err != nil {
			return fmt.Errorf("serialize pinNode failed: %w", err)
		}

		pending := &mrc20.PendingTeleport{
			Coord:       tx.Coord,
			Type:        "transfer", // 标识是Transfer先到达
			SourceTxId:  pinNode.GenesisTransaction,
			SourcePinId: pinNode.Id,
			SourceChain: tx.SourceChain,
			TargetChain: tx.TargetChain,
			Data:        data,
			PinNodeJson: string(pinNodeData),
			CreatedAt:   time.Now().Unix(),
			ExpireAt:    time.Now().Add(24 * time.Hour).Unix(), // 24小时过期
			RetryCount:  0,
		}

		if err := SavePendingTeleport(pending); err != nil {
			return fmt.Errorf("save pending teleport failed: %w", err)
		}

		log.Printf("[TeleportV2] ✅ Saved to pending queue, waiting for arrival: coord=%s", tx.Coord)

		// 删除 TeleportTransaction，避免重复处理
		DeleteTeleportTransaction(tx.ID)

		// 返回特殊错误，表示正在等待 Arrival
		return fmt.Errorf("waiting for arrival")
	}

	if arrival.Status != mrc20.ArrivalStatusPending {
		return fmt.Errorf("arrival not pending (pre-check failed): status=%d", arrival.Status)
	}

	// 验证Arrival数据匹配
	if arrival.TickId != tx.TickId {
		return fmt.Errorf("tickId mismatch: arrival=%s, teleport=%s", arrival.TickId, tx.TickId)
	}
	if !arrival.Amount.Equal(tx.Amount) {
		return fmt.Errorf("amount mismatch: arrival=%s, teleport=%s", arrival.Amount, tx.Amount)
	}
	if arrival.Chain != tx.TargetChain {
		return fmt.Errorf("chain mismatch: arrival=%s, teleport=%s", arrival.Chain, tx.TargetChain)
	}

	// 记录目标链信息（提前获取）
	tx.ToAddress = arrival.ToAddress
	tx.TargetBlockHeight = arrival.BlockHeight

	log.Printf("[TeleportV2] ✅ Pre-check passed: arrival=%s, toAddress=%s", tx.Coord, tx.ToAddress)

	// 1. 查找源UTXO
	sourceUtxo, err := findTeleportSourceUtxo(pinNode, tx.TickId, tx.Amount)
	if err != nil {
		return fmt.Errorf("find source UTXO failed: %w", err)
	}

	// 2. 验证UTXO状态
	if sourceUtxo.Status != mrc20.UtxoStatusAvailable {
		return fmt.Errorf("source UTXO not available: status=%d", sourceUtxo.Status)
	}

	// 3. 记录源UTXO信息
	tx.SourceOutpoint = sourceUtxo.TxPoint
	tx.AssetOutpoint = sourceUtxo.TxPoint
	tx.Tick = sourceUtxo.Tick

	// 4. 标记为 TeleportPending（锁定）
	sourceUtxo.Status = mrc20.UtxoStatusTeleportPending
	sourceUtxo.OperationTx = pinNode.GenesisTransaction
	sourceUtxo.Msg = fmt.Sprintf("teleport locked, coord=%s", tx.Coord)

	// 5. 保存UTXO状态（状态变更为TeleportPending，余额通过UTXO状态实时计算）
	if err := PebbleStore.UpdateMrc20Utxo([]*mrc20.Mrc20Utxo{sourceUtxo}, pinNode.GenesisHeight <= 0); err != nil {
		return fmt.Errorf("update UTXO failed: %w", err)
	}

	log.Printf("[TeleportV2] 💡 余额通过UTXO状态实时计算:")
	log.Printf("   - Balance = sum(Available + TeleportPending) [资金还在链上]")
	log.Printf("   - PendingOut = sum(TeleportPending) [显示正在跃迁的金额]")

	// 💡 新架构：不在 Step 1 创建 pending 流水
	// 原因：UTXO状态已经是TeleportPending，余额接口会自动计算PendingOut
	// 只在 Step 4 完成时创建最终的 out/in 流水记录

	// 8. 状态转换
	tx.AddStateChange(mrc20.TeleportStateSourceLocked, true, "", "lock")
	log.Printf("[TeleportV2] ✅ Source UTXO locked: %s", tx.SourceOutpoint)

	return nil
}

// stepVerifyArrival 步骤2: 验证Arrival（创建目标链PendingIn）
func stepVerifyArrival(tx *mrc20.TeleportTransaction, pinNode *pin.PinInscription, data mrc20.Mrc20TeleportTransferData) error {
	log.Printf("[TeleportV2] Step 2: Setting up target chain PendingIn")

	// 1. 获取Arrival（应该在step1已验证过，这里再次确认）
	arrival, err := PebbleStore.GetMrc20ArrivalByPinId(tx.Coord)
	if err != nil {
		return fmt.Errorf("arrival not found: %w", err)
	}

	// 2. 再次验证状态（防御性编程）
	if arrival.Status != mrc20.ArrivalStatusPending {
		return fmt.Errorf("arrival not pending: status=%d", arrival.Status)
	}

	// 3. 验证assetOutpoint匹配（这个在step1无法验证，因为当时还没有sourceOutpoint）
	log.Printf("[TeleportV2] 🔍 Verifying assetOutpoint: arrival=%s, source=%s", arrival.AssetOutpoint, tx.SourceOutpoint)
	if arrival.AssetOutpoint != tx.SourceOutpoint {
		log.Printf("[TeleportV2] ❌ AssetOutpoint mismatch! This will prevent PendingIn creation!")
		log.Printf("[TeleportV2]    Arrival declared: %s", arrival.AssetOutpoint)
		log.Printf("[TeleportV2]    Transfer using:   %s", tx.SourceOutpoint)
		return fmt.Errorf("assetOutpoint mismatch: arrival=%s, source=%s", arrival.AssetOutpoint, tx.SourceOutpoint)
	}
	log.Printf("[TeleportV2] ✅ AssetOutpoint matched")

	// 5. 创建目标链的 PendingIn 记录（目标UTXO还未创建，所以用单独的PendingIn记录）
	pendingIn := &mrc20.TeleportPendingIn{
		Coord:       tx.Coord,
		Chain:       tx.TargetChain,
		ToAddress:   arrival.ToAddress,
		TickId:      tx.TickId,
		Tick:        tx.Tick,
		Amount:      tx.Amount,
		SourceChain: tx.SourceChain,
		FromAddress: tx.FromAddress,
		TeleportTx:  tx.SourceTxId,
		ArrivalTx:   arrival.TxId,
		BlockHeight: arrival.BlockHeight,
		Timestamp:   arrival.Timestamp,
	}
	if err := PebbleStore.SaveTeleportPendingIn(pendingIn); err != nil {
		return fmt.Errorf("failed to save PendingIn: %w", err)
	}

	log.Printf("[TeleportV2] 💡 目标链PendingIn通过TeleportPendingIn记录，查询时实时计算")
	log.Printf("[TeleportV2] 💡 该记录将在Step4创建目标UTXO后删除，避免重复计算")
	log.Printf("[TeleportV2] ✅ Created PendingIn: chain=%s, toAddress=%s, amount=%s", tx.TargetChain, arrival.ToAddress, tx.Amount)

	// 7. 状态转换
	tx.AddStateChange(mrc20.TeleportStateArrivalVerified, true, "", "verify")
	log.Printf("[TeleportV2] ✅ Arrival verified: coord=%s, toAddress=%s", tx.Coord, tx.ToAddress)

	return nil
}

// stepMarkSourceSpent 步骤3: 标记源UTXO为spent（区块确认后执行）
func stepMarkSourceSpent(tx *mrc20.TeleportTransaction, pinNode *pin.PinInscription, data mrc20.Mrc20TeleportTransferData) error {
	log.Printf("[TeleportV2] Step 3: Marking source UTXO as spent")

	// 1. 获取源UTXO
	sourceUtxo, err := PebbleStore.GetMrc20UtxoByTxPoint(tx.SourceOutpoint, false)
	if err != nil {
		return fmt.Errorf("source UTXO not found: %w", err)
	}

	// 2. 验证状态（必须是TeleportPending）
	if sourceUtxo.Status != mrc20.UtxoStatusTeleportPending {
		return fmt.Errorf("source UTXO not in pending state: status=%d", sourceUtxo.Status)
	}

	// 3. 标记为Spent（不删除，保留审计记录）
	sourceUtxo.Status = mrc20.UtxoStatusSpent
	sourceUtxo.OperationTx = tx.SourceTxId
	sourceUtxo.Msg = fmt.Sprintf("teleported to %s via coord %s", tx.TargetChain, tx.Coord)

	// 4. 保存UTXO状态
	if err := PebbleStore.UpdateMrc20Utxo([]*mrc20.Mrc20Utxo{sourceUtxo}, false); err != nil {
		return fmt.Errorf("mark UTXO spent failed: %w", err)
	}

	// 5. 状态转换
	tx.AddStateChange(mrc20.TeleportStateSourceSpent, true, "", "spend")
	log.Printf("[TeleportV2] ✅ Source UTXO marked as spent: %s", tx.SourceOutpoint)

	return nil
}

// stepCreateTargetUTXO 步骤4: 创建目标UTXO
func stepCreateTargetUTXO(tx *mrc20.TeleportTransaction, pinNode *pin.PinInscription, data mrc20.Mrc20TeleportTransferData) error {
	log.Printf("[TeleportV2] Step 4: Creating target UTXO")

	// 1. 获取Arrival信息
	arrival, err := PebbleStore.GetMrc20ArrivalByPinId(tx.Coord)
	if err != nil {
		return fmt.Errorf("arrival not found: %w", err)
	}

	// 🔧 重要：重新加载 arrival 的最新 pinNode，获取最新的 BlockHeight
	// arrival 记录可能是 mempool 阶段保存的（BlockHeight=-1），需要更新为出块后的高度
	if arrival.PinId != "" {
		arrivalPinNode, err := PebbleStore.GetPinById(arrival.PinId)
		if err != nil {
			log.Printf("[TeleportV2] ⚠️  Failed to reload arrival pinNode: %v, using cached version", err)
		} else if arrivalPinNode.GenesisHeight > 0 && arrivalPinNode.GenesisHeight != arrival.BlockHeight {
			log.Printf("[TeleportV2] 🔄 Updated arrival height: %d -> %d", arrival.BlockHeight, arrivalPinNode.GenesisHeight)
			arrival.BlockHeight = arrivalPinNode.GenesisHeight
			arrival.Timestamp = arrivalPinNode.Timestamp
		}
	}

	// 2. 生成目标UTXO的txpoint
	targetOutpoint := fmt.Sprintf("%s:%d", arrival.TxId, arrival.LocationIndex)
	tx.TargetOutpoint = targetOutpoint

	// 3. 检查目标UTXO是否已存在（幂等性检查）
	existingUtxo, err := PebbleStore.GetMrc20UtxoByTxPoint(targetOutpoint, false)
	if err == nil && existingUtxo != nil {
		// UTXO已存在，验证是否匹配
		if existingUtxo.Mrc20Id == tx.TickId &&
			existingUtxo.AmtChange.Equal(tx.Amount) &&
			existingUtxo.ToAddress == tx.ToAddress {
			log.Printf("[TeleportV2] ℹ️  Target UTXO already exists (idempotent): %s", targetOutpoint)
			tx.AddStateChange(mrc20.TeleportStateTargetCreated, true, "", "create")
			return nil
		}
		return fmt.Errorf("target UTXO exists but mismatch: %s", targetOutpoint)
	}

	// 4. 创建新的目标UTXO
	newUtxo := mrc20.Mrc20Utxo{
		Tick:        tx.Tick,
		Mrc20Id:     tx.TickId,
		TxPoint:     targetOutpoint,
		PointValue:  0, // 可以从arrival交易中获取
		PinId:       arrival.PinId,
		PinContent:  string(pinNode.ContentBody),
		Verify:      true,
		BlockHeight: arrival.BlockHeight,
		MrcOption:   mrc20.OptionTeleportTransfer,
		FromAddress: tx.FromAddress,
		ToAddress:   tx.ToAddress,
		AmtChange:   tx.Amount,
		Status:      mrc20.UtxoStatusAvailable,
		Chain:       tx.TargetChain,
		Index:       0,
		Timestamp:   arrival.Timestamp,
		OperationTx: tx.SourceTxId,
		Msg:         fmt.Sprintf("teleported from %s via coord %s", tx.SourceChain, tx.Coord),
	}

	// 5. 保存目标UTXO
	if err := PebbleStore.SaveMrc20Pin([]mrc20.Mrc20Utxo{newUtxo}); err != nil {
		return fmt.Errorf("save target UTXO failed: %w", err)
	}

	// 6. 🔧 创建流水记录（在创建 UTXO 的同时创建，确保不会遗漏）
	// 源链流水（teleport_out）
	sourceTx := &mrc20.Mrc20Transaction{
		Chain:        tx.SourceChain,
		TxId:         tx.SourceTxId,
		TxPoint:      tx.SourceOutpoint + "_out",
		TxIndex:      0,
		PinId:        pinNode.Id,
		TickId:       tx.TickId,
		Tick:         tx.Tick,
		TxType:       "teleport_out",
		Direction:    "out",
		Address:      tx.FromAddress,
		FromAddress:  tx.FromAddress,
		ToAddress:    tx.ToAddress,
		Amount:       tx.Amount,
		IsChange:     false,
		SpentUtxos:   fmt.Sprintf("[\"%s\"]", tx.SourceOutpoint),
		CreatedUtxos: "[]",
		BlockHeight:  tx.SourceBlockHeight,
		Timestamp:    pinNode.Timestamp,
		Msg:          fmt.Sprintf("teleport to %s", tx.TargetChain),
		Status:       1,
		RelatedChain: tx.TargetChain,
		RelatedTxId:  arrival.TxId,
		RelatedPinId: arrival.PinId,
	}
	if err := PebbleStore.SaveMrc20Transaction(sourceTx); err != nil {
		log.Printf("[TeleportV2] ⚠️  Failed to save source transaction: %v", err)
	}

	// 目标链流水（teleport_in）
	targetTx := &mrc20.Mrc20Transaction{
		Chain:        tx.TargetChain,
		TxId:         arrival.TxId,
		TxPoint:      targetOutpoint,
		TxIndex:      0,
		PinId:        arrival.PinId,
		TickId:       tx.TickId,
		Tick:         tx.Tick,
		TxType:       "teleport_in",
		Direction:    "in",
		Address:      tx.ToAddress,
		FromAddress:  tx.FromAddress,
		ToAddress:    tx.ToAddress,
		Amount:       tx.Amount,
		IsChange:     false,
		SpentUtxos:   "[]",
		CreatedUtxos: fmt.Sprintf("[\"%s\"]", targetOutpoint),
		BlockHeight:  arrival.BlockHeight,
		Timestamp:    arrival.Timestamp,
		Msg:          fmt.Sprintf("teleport from %s", tx.SourceChain),
		Status:       1,
		RelatedChain: tx.SourceChain,
		RelatedTxId:  tx.SourceTxId,
		RelatedPinId: pinNode.Id,
	}
	if err := PebbleStore.SaveMrc20Transaction(targetTx); err != nil {
		log.Printf("[TeleportV2] ⚠️  Failed to save target transaction: %v", err)
	}

	// 7. 删除TeleportPendingIn记录（避免重复计算余额）
	// 目标UTXO已创建（Available状态），不再需要PendingIn记录
	if err := PebbleStore.DeleteTeleportPendingIn(tx.Coord, tx.ToAddress); err != nil {
		log.Printf("[TeleportV2] ⚠️  Failed to delete PendingIn: %v", err)
	}
	log.Printf("[TeleportV2] ✅ Deleted TeleportPendingIn to avoid double counting")

	// 8. 状态转换
	tx.AddStateChange(mrc20.TeleportStateTargetCreated, true, "", "create")
	log.Printf("[TeleportV2] ✅ Target UTXO created: %s", targetOutpoint)

	return nil
}

// stepUpdateBalances 步骤5: 验证UTXO状态（不再需要更新余额表）
func stepUpdateBalances(tx *mrc20.TeleportTransaction, pinNode *pin.PinInscription, data mrc20.Mrc20TeleportTransferData) error {
	log.Printf("[TeleportV2] Step 5: Verifying UTXO states (no balance table update needed)")

	// 💡 新架构：余额通过UTXO状态实时计算
	// - 源UTXO: Spent (step3已标记)
	// - 目标UTXO: Available (step4已创建)
	// - PendingOut: sum(UTXO where status=TeleportPending) - 源UTXO已经不是pending了
	// - PendingIn: TeleportPendingIn记录 - 将在step6删除

	// 验证源UTXO状态
	sourceUtxo, err := PebbleStore.GetMrc20UtxoByTxPoint(tx.SourceOutpoint, false)
	if err != nil {
		return fmt.Errorf("source UTXO not found: %w", err)
	}
	if sourceUtxo.Status != mrc20.UtxoStatusSpent {
		return fmt.Errorf("source UTXO not spent: status=%d", sourceUtxo.Status)
	}

	// 验证目标UTXO状态
	targetUtxo, err := PebbleStore.GetMrc20UtxoByTxPoint(tx.TargetOutpoint, false)
	if err != nil {
		return fmt.Errorf("target UTXO not found: %w", err)
	}
	if targetUtxo.Status != mrc20.UtxoStatusAvailable {
		return fmt.Errorf("target UTXO not available: status=%d", targetUtxo.Status)
	}

	// 验证金额匹配
	if !sourceUtxo.AmtChange.Equal(targetUtxo.AmtChange) {
		return fmt.Errorf("amount mismatch: source=%s, target=%s",
			sourceUtxo.AmtChange, targetUtxo.AmtChange)
	}

	log.Printf("[TeleportV2] ✅ UTXO states verified: source=Spent, target=Available")
	log.Printf("[TeleportV2] 💡 余额计算说明:")
	log.Printf("   - 源链Balance: sum(UTXO where status=Available) [源UTXO已Spent，不再计入]")
	log.Printf("   - 源链PendingOut: sum(UTXO where status=TeleportPending) [源UTXO已Spent，不再计入]")
	log.Printf("   - 目标链Balance: sum(UTXO where status=Available) [包含Step4创建的目标UTXO]")
	log.Printf("   - 目标链PendingIn: TeleportPendingIn记录 [已在Step4删除，避免重复计算]")

	// 状态转换
	tx.AddStateChange(mrc20.TeleportStateBalanceUpdated, true, "", "verify")

	return nil
}

// stepFinalizeTeleport 步骤6: 完成Teleport（更新Arrival状态）
// 注意：流水记录已在 stepCreateTargetUTXO 中创建，这里只更新状态
func stepFinalizeTeleport(tx *mrc20.TeleportTransaction, pinNode *pin.PinInscription, data mrc20.Mrc20TeleportTransferData) error {
	log.Printf("[TeleportV2] Step 6: Finalizing teleport")

	arrival, err := PebbleStore.GetMrc20ArrivalByPinId(tx.Coord)
	if err != nil {
		return fmt.Errorf("arrival not found: %w", err)
	}

	// 更新Arrival状态为已完成
	err = PebbleStore.UpdateMrc20ArrivalStatus(
		arrival.PinId,
		mrc20.ArrivalStatusCompleted,
		pinNode.Id,
		tx.SourceChain,
		tx.SourceTxId,
		pinNode.Timestamp,
	)
	if err != nil {
		log.Printf("[TeleportV2] ⚠️  Failed to update arrival status: %v", err)
	}

	// 4. TeleportPendingIn已在Step4删除，这里不需要重复删除
	// （保留注释以便理解）

	// 5. 保存完成的Teleport记录（用于CheckTeleportExists）
	teleportRecord := &mrc20.Mrc20Teleport{
		PinId:          pinNode.Id,
		TxId:           tx.SourceTxId,
		TickId:         tx.TickId,
		Tick:           tx.Tick,
		Amount:         tx.Amount,
		Coord:          tx.Coord,
		FromAddress:    tx.FromAddress,
		SourceChain:    tx.SourceChain,
		TargetChain:    tx.TargetChain,
		SpentUtxoPoint: tx.SourceOutpoint,
		Status:         1, // 完成
		BlockHeight:    tx.SourceBlockHeight,
		Timestamp:      pinNode.Timestamp,
	}
	if err := PebbleStore.SaveMrc20Teleport(teleportRecord); err != nil {
		log.Printf("[TeleportV2] ⚠️  Failed to save teleport record: %v", err)
	}

	// 6. 状态转换到Completed
	tx.AddStateChange(mrc20.TeleportStateCompleted, true, "", "finalize")
	log.Printf("[TeleportV2] 🎉 Teleport completed: id=%s, from=%s to=%s, amount=%s",
		tx.ID, tx.FromAddress, tx.ToAddress, tx.Amount)

	return nil
}

// shouldRollback 判断当前状态是否应该回滚
func shouldRollback(state int) bool {
	// 只有在创建了目标UTXO之后才需要回滚
	return state >= mrc20.TeleportStateTargetCreated && state < mrc20.TeleportStateCompleted
}

// rollbackTeleport 回滚Teleport
func rollbackTeleport(tx *mrc20.TeleportTransaction, reason string) error {
	log.Printf("[TeleportV2] 🔄 Rolling back: id=%s, reason=%s", tx.ID, reason)

	// TODO: 实现回滚逻辑
	// 1. 删除目标UTXO（如果已创建）
	// 2. 恢复源UTXO状态（从TeleportPending -> Available）
	// 3. 回滚余额变更
	// 4. 删除相关流水记录

	tx.AddStateChange(mrc20.TeleportStateRolledBack, true, reason, "rollback")
	return SaveTeleportTransaction(tx)
}

// updateAccountBalanceInBatch 在batch中更新账户余额
// updateAccountBalanceInBatch - 已移除
// 新架构：余额通过UTXO状态实时计算，不再维护AccountBalance表
