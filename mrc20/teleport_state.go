package mrc20

import (
	"time"

	"github.com/shopspring/decimal"
)

// TeleportState 状态定义
const (
	TeleportStateCreated         = 0  // 初始创建
	TeleportStateSourceLocked    = 1  // 源UTXO已锁定
	TeleportStateArrivalVerified = 2  // Arrival已验证
	TeleportStateSourceSpent     = 3  // 源UTXO已标记spent
	TeleportStateTargetCreated   = 4  // 目标UTXO已创建
	TeleportStateBalanceUpdated  = 5  // 余额已更新
	TeleportStateCompleted       = 6  // 完成
	TeleportStateFailed          = -1 // 失败
	TeleportStateRolledBack      = -2 // 已回滚
)

// TeleportTransaction 事务记录 - 新架构的核心数据结构
// 使用状态机管理整个teleport生命周期，确保原子性和幂等性
type TeleportTransaction struct {
	// 基本信息
	ID             string          `json:"id"`             // 唯一ID: hash(coord + sourceTxId)
	Coord          string          `json:"coord"`          // Arrival coord (pinId)
	SourceChain    string          `json:"sourceChain"`    // 源链（btc/doge/mvc）
	TargetChain    string          `json:"targetChain"`    // 目标链
	SourceTxId     string          `json:"sourceTxId"`     // 源链transfer交易ID
	SourcePinId    string          `json:"sourcePinId"`    // 源链transfer PIN ID
	SourceOutpoint string          `json:"sourceOutpoint"` // 源UTXO txpoint
	TargetOutpoint string          `json:"targetOutpoint"` // 目标UTXO txpoint（创建后填充）
	Amount         decimal.Decimal `json:"amount"`         // 转移金额
	TickId         string          `json:"tickId"`         // 代币ID
	Tick           string          `json:"tick"`           // 代币名称
	FromAddress    string          `json:"fromAddress"`    // 发送方地址
	ToAddress      string          `json:"toAddress"`      // 接收方地址
	AssetOutpoint  string          `json:"assetOutpoint"`  // Arrival声明的资产outpoint（用于验证）

	// 状态管理
	State         int                   `json:"state"`         // 当前状态
	StateHistory  []TeleportStateChange `json:"stateHistory"`  // 状态变更历史
	FailureReason string                `json:"failureReason"` // 失败原因
	RetryCount    int                   `json:"retryCount"`    // 重试次数
	LastRetryAt   int64                 `json:"lastRetryAt"`   // 最后重试时间

	// 时间戳
	CreatedAt         int64 `json:"createdAt"`         // 创建时间
	UpdatedAt         int64 `json:"updatedAt"`         // 更新时间
	CompletedAt       int64 `json:"completedAt"`       // 完成时间
	SourceBlockHeight int64 `json:"sourceBlockHeight"` // 源链区块高度
	TargetBlockHeight int64 `json:"targetBlockHeight"` // 目标链区块高度

	// 锁定信息
	LockedBy      string `json:"lockedBy"`      // 锁定者（进程ID或节点ID）
	LockExpiresAt int64  `json:"lockExpiresAt"` // 锁过期时间
}

// TeleportStateChange 状态变更记录
type TeleportStateChange struct {
	FromState   int    `json:"fromState"`   // 源状态
	ToState     int    `json:"toState"`     // 目标状态
	Timestamp   int64  `json:"timestamp"`   // 变更时间
	BlockHeight int64  `json:"blockHeight"` // 区块高度
	Success     bool   `json:"success"`     // 是否成功
	Error       string `json:"error"`       // 错误信息
	Operator    string `json:"operator"`    // 操作者（mempool/block/retry）
}

// TeleportLock 分布式锁
type TeleportLock struct {
	TeleportID string `json:"teleportId"` // Teleport事务ID
	ProcessID  string `json:"processId"`  // 持有锁的进程ID
	AcquiredAt int64  `json:"acquiredAt"` // 获取时间
	ExpiresAt  int64  `json:"expiresAt"`  // 过期时间
}

// GetStateName 获取状态名称（用于日志）
func GetStateName(state int) string {
	switch state {
	case TeleportStateCreated:
		return "Created"
	case TeleportStateSourceLocked:
		return "SourceLocked"
	case TeleportStateArrivalVerified:
		return "ArrivalVerified"
	case TeleportStateSourceSpent:
		return "SourceSpent"
	case TeleportStateTargetCreated:
		return "TargetCreated"
	case TeleportStateBalanceUpdated:
		return "BalanceUpdated"
	case TeleportStateCompleted:
		return "Completed"
	case TeleportStateFailed:
		return "Failed"
	case TeleportStateRolledBack:
		return "RolledBack"
	default:
		return "Unknown"
	}
}

// IsValidTransition 验证状态转换是否合法
func IsValidTransition(from, to int) bool {
	validTransitions := map[int][]int{
		TeleportStateCreated:         {TeleportStateSourceLocked, TeleportStateFailed},
		TeleportStateSourceLocked:    {TeleportStateArrivalVerified, TeleportStateFailed, TeleportStateRolledBack},
		TeleportStateArrivalVerified: {TeleportStateSourceSpent, TeleportStateFailed, TeleportStateRolledBack},
		TeleportStateSourceSpent:     {TeleportStateTargetCreated, TeleportStateRolledBack},
		TeleportStateTargetCreated:   {TeleportStateBalanceUpdated, TeleportStateRolledBack},
		TeleportStateBalanceUpdated:  {TeleportStateCompleted},
		// 终态不允许转换
		TeleportStateCompleted:  {},
		TeleportStateFailed:     {},
		TeleportStateRolledBack: {},
	}

	allowed, ok := validTransitions[from]
	if !ok {
		return false
	}

	for _, state := range allowed {
		if state == to {
			return true
		}
	}

	return false
}

// IsTerminalState 判断是否为终态
func IsTerminalState(state int) bool {
	return state == TeleportStateCompleted ||
		state == TeleportStateFailed ||
		state == TeleportStateRolledBack
}

// ShouldRetry 判断是否应该重试
func (tx *TeleportTransaction) ShouldRetry() bool {
	// 终态不重试
	if IsTerminalState(tx.State) {
		return false
	}

	// 失败状态不重试（需要手动处理）
	if tx.State == TeleportStateFailed {
		return false
	}

	// 重试次数限制
	maxRetries := 10
	if tx.RetryCount >= maxRetries {
		return false
	}

	// 重试间隔（指数退避）
	minInterval := int64(60) // 60秒
	if tx.LastRetryAt > 0 {
		interval := minInterval * int64(1<<uint(tx.RetryCount))
		if time.Now().Unix()-tx.LastRetryAt < interval {
			return false
		}
	}

	return true
}

// AddStateChange 添加状态变更记录
func (tx *TeleportTransaction) AddStateChange(toState int, success bool, err string, operator string) {
	change := TeleportStateChange{
		FromState:   tx.State,
		ToState:     toState,
		Timestamp:   time.Now().Unix(),
		BlockHeight: tx.SourceBlockHeight,
		Success:     success,
		Error:       err,
		Operator:    operator,
	}

	if tx.StateHistory == nil {
		tx.StateHistory = []TeleportStateChange{}
	}
	tx.StateHistory = append(tx.StateHistory, change)

	if success {
		tx.State = toState
		tx.UpdatedAt = time.Now().Unix()

		if IsTerminalState(toState) {
			tx.CompletedAt = time.Now().Unix()
		}
	} else {
		tx.FailureReason = err
	}
}

// IsLocked 判断是否被锁定
func (tx *TeleportTransaction) IsLocked() bool {
	if tx.LockedBy == "" {
		return false
	}

	// 检查锁是否过期
	if tx.LockExpiresAt > 0 && time.Now().Unix() > tx.LockExpiresAt {
		return false
	}

	return true
}

// AcquireLock 获取锁
func (tx *TeleportTransaction) AcquireLock(processID string, duration time.Duration) bool {
	if tx.IsLocked() {
		return false
	}

	tx.LockedBy = processID
	tx.LockExpiresAt = time.Now().Add(duration).Unix()
	return true
}

// ReleaseLock 释放锁
func (tx *TeleportTransaction) ReleaseLock(processID string) {
	if tx.LockedBy == processID {
		tx.LockedBy = ""
		tx.LockExpiresAt = 0
	}
}

// PendingTeleport 等待配对的 Teleport Transfer
// 当 Transfer 或 Arrival 任意一方先到达时，保存到 Pending 队列等待配对
type PendingTeleport struct {
	Coord       string                    `json:"coord"`       // Arrival coord (pinId)
	Type        string                    `json:"type"`        // "transfer" or "arrival" - 标识是哪一方先到达
	SourceTxId  string                    `json:"sourceTxId"`  // 源链transfer交易ID
	SourcePinId string                    `json:"sourcePinId"` // 源链transfer PIN ID
	SourceChain string                    `json:"sourceChain"` // 源链
	TargetChain string                    `json:"targetChain"` // 目标链
	Data        Mrc20TeleportTransferData `json:"data"`        // Transfer数据
	PinNodeJson string                    `json:"pinNodeJson"` // PinInscription序列化JSON（用于重新处理）
	CreatedAt   int64                     `json:"createdAt"`   // 创建时间
	ExpireAt    int64                     `json:"expireAt"`    // 过期时间（超时后清理）
	RetryCount  int                       `json:"retryCount"`  // 重试次数
	LastCheckAt int64                     `json:"lastCheckAt"` // 最后检查时间
}
