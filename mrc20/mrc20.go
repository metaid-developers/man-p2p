package mrc20

import (
	"encoding/json"
	"fmt"

	"github.com/shopspring/decimal"
)

const (
	ErrDeployContent        = "deploy content format error, it needs to be a JSON string"
	ErrDeployTickLength     = "the length must be between 2 and 24"
	ErrDeployTickNameLength = "the length must be between 1 and 48"
	ErrDeployNum            = "incorrect deployment parameters"
	ErrDeployTxGet          = "failed to retrieve transaction information"
	ErrDeployTickExists     = "tick already exists"
	ErrCrossChain           = "cross-chain operations are currently not allowed"
	ErrMintTickNotExists    = "tick not exists"
	ErrMintLimit            = "minting limit reached"
	ErrMintHeight           = "current block height is too low"
	ErrMintPopNull          = "shovel is none"
	ErrMintPopDiff          = "pop level check failed"
	ErrMintCreator          = "creator check failed"
	ErrMintVout             = "vout value error"
	ErrMintPayCheck         = "payCheck validation failed"
	ErrMintPathCheck        = "shovel path check failed"
	ErrMintCountCheck       = "shovel count check failed"
	ErrMintTickIdNull       = "tickId is null"
	ErrMintDecimals         = "decimals error"
	ErrMintPinIdNull        = "pin is null"
	ErrMintPinOwner         = "not have the right to use this PIN"
	ErrTranferReqData       = "transfer data error"
	ErrTranferBalnceErr     = "transfer balance error"
	ErrTranferBalnceLess    = "insufficient balance for transfer"
	ErrTranferAmt           = "the amount should be greater than 0"
)

// UTXO Status 常量定义
const (
	UtxoStatusAvailable       = 0  // 可用
	UtxoStatusTeleportPending = 1  // 等待跃迁中 (teleport transfer mempool)
	UtxoStatusTransferPending = 2  // 等待转账确认中 (普通/native transfer mempool)
	UtxoStatusMintPending     = 3  // 等待mint确认中 (mint mempool)
	UtxoStatusSpent           = -1 // 已消耗
)

// MrcOption 操作类型常量定义
const (
	OptionDeploy           = "deploy"            // 部署代币
	OptionMint             = "mint"              // 铸造代币
	OptionPreMint          = "pre-mint"          // 预挖代币
	OptionNativeTransfer   = "native-transfer"   // 原生转账 (直接花费 UTXO，无 PIN)
	OptionDataTransfer     = "data-transfer"     // 数据转账 (通过 /ft/mrc20/transfer PIN)
	OptionTeleportTransfer = "teleport-transfer" // 跃迁转账 (跨链)
)

type Mrc20Utxo struct {
	Tick          string          `json:"tick"`
	Mrc20Id       string          `json:"mrc20Id"`
	TxPoint       string          `json:"txPoint"`
	PointValue    uint64          `json:"pointValue"`
	PinId         string          `json:"pinId"`
	PinContent    string          `json:"pinContent"`
	Verify        bool            `json:"verify"`
	BlockHeight   int64           `json:"blockHeight"`
	MrcOption     string          `json:"mrcOption"` // 操作类型: deploy, mint, pre-mint, transfer, teleport
	FromAddress   string          `json:"fromAddress"`
	ToAddress     string          `json:"toAddress"`
	Msg           string          `json:"msg"`
	AmtChange     decimal.Decimal `json:"amtChange"`
	Status        int             `json:"status"`
	Chain         string          `json:"chain"`
	Index         int             `json:"index"`
	Timestamp     int64           `json:"timestamp"`
	OperationTx   string          `json:"operationTx"`
	SpentAtHeight int64           `json:"spentAtHeight"` // 被消费时的区块高度，0表示未消费
}
type Mrc20DeployQual struct {
	Creator string `json:"creator"`
	Lv      string `json:"lvl"`
	Path    string `json:"path"`
	Count   string `json:"count"`
}
type Mrc20DeployPayCheck struct {
	PayTo     string `json:"payTo"`
	PayAmount string `json:"payAmount"`
}
type Mrc20DeployPayCheckLower struct {
	PayTo     string `json:"payto"`
	PayAmount string `json:"payamount"`
}
type Mrc20Deploy struct {
	Tick         string              `json:"tick"`
	TokenName    string              `json:"tokenName"`
	Decimals     string              `json:"decimals"`
	AmtPerMint   string              `json:"amtPerMint"`
	MintCount    string              `json:"mintCount"`
	BeginHeight  string              `json:"beginHeight"`
	EndHeight    string              `json:"endHeight"`
	Metadata     string              `json:"metadata"`
	DeployType   string              `json:"type"`
	PremineCount string              `json:"premineCount"`
	PinCheck     Mrc20DeployQual     `json:"pinCheck"`
	PayCheck     Mrc20DeployPayCheck `json:"payCheck"`
}
type Mrc20DeployLow struct {
	Tick         string                   `json:"tick"`
	TokenName    string                   `json:"tokenname"`
	Decimals     string                   `json:"decimals"`
	AmtPerMint   string                   `json:"amtpermint"`
	MintCount    string                   `json:"mintcount"`
	BeginHeight  string                   `json:"beginheight"`
	EndHeight    string                   `json:"endheight"`
	Metadata     string                   `json:"metadata"`
	DeployType   string                   `json:"type"`
	PremineCount string                   `json:"preminecount"`
	PinCheck     Mrc20DeployQual          `json:"pincheck"`
	PayCheck     Mrc20DeployPayCheckLower `json:"paycheck"`
}
type Mrc20DeployInfo struct {
	Tick         string              `json:"tick"`
	TokenName    string              `json:"tokenName"`
	Decimals     string              `json:"decimals"`
	AmtPerMint   string              `json:"amtPerMint"`
	MintCount    uint64              `json:"mintCount"`
	BeginHeight  string              `json:"beginHeight"`
	EndHeight    string              `json:"endHeight"`
	Metadata     string              `json:"metadata"`
	DeployType   string              `json:"type"`
	PremineCount uint64              `json:"premineCount"`
	PinCheck     Mrc20DeployQual     `json:"pinCheck"`
	PayCheck     Mrc20DeployPayCheck `json:"payCheck"`
	TotalMinted  uint64              `json:"totalMinted"`
	Mrc20Id      string              `json:"mrc20Id"`
	PinNumber    int64               `json:"pinNumber"`
	Chain        string              `json:"chain"`
	Holders      uint64              `json:"holders"`
	TxCount      uint64              `json:"txCount"`
	MetaId       string              `json:"metaId"`
	Address      string              `json:"address"`
	DeployTime   int64               `json:"deployTime"`
}

type Mrc20Shovel struct {
	Id           string `json:"id"`
	Mrc20MintPin string `json:"mrc20MintPin"`
}
type Mrc20MintData struct {
	Id   string `json:"id"`
	Vout string `json:"vout"`
	//Pin string `json:"pin"`
}
type Mrc20TranferData struct {
	Amount string `json:"amount"`
	Vout   int    `json:"vout"`
	Id     string `json:"id"`
}
type Mrc20Balance struct {
	Id                string          `json:"id"`
	Name              string          `json:"name"`
	Balance           decimal.Decimal `json:"balance"`           // 已确认的可用余额
	PendingInBalance  decimal.Decimal `json:"pendingInBalance"`  // 待转入余额（所有类型 transfer 接收方 mempool 阶段）
	PendingOutBalance decimal.Decimal `json:"pendingOutBalance"` // 待转出余额（所有类型 transfer 发送方 mempool 阶段）
	Chain             string          `json:"chain"`
}
type Mrc20MempoolBalance struct {
	Id        string          `json:"id"`
	Name      string          `json:"name"`
	SpendUtxo []string        `json:"send"`
	Recive    decimal.Decimal `json:"recive"`
}

// Teleport 跃迁相关数据结构

// ArrivalStatus 表示 arrival 的状态
type ArrivalStatus int

const (
	ArrivalStatusPending   ArrivalStatus = 0 // 等待中 - 等待源链的 teleport
	ArrivalStatusCompleted ArrivalStatus = 1 // 已完成 - 跃迁成功
	ArrivalStatusInvalid   ArrivalStatus = 2 // 无效 - 验证失败
)

// FlexibleString 是一个可以同时解析 JSON 字符串和数字的类型
type FlexibleString string

func (f *FlexibleString) UnmarshalJSON(data []byte) error {
	// 尝试作为字符串解析
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*f = FlexibleString(s)
		return nil
	}
	// 尝试作为数字解析
	var n json.Number
	if err := json.Unmarshal(data, &n); err == nil {
		*f = FlexibleString(n.String())
		return nil
	}
	return fmt.Errorf("cannot unmarshal %s into FlexibleString", string(data))
}

// Mrc20ArrivalData 是 /ft/mrc20/arrival PIN 的内容结构
type Mrc20ArrivalData struct {
	AssetOutpoint string         `json:"assetOutpoint"` // 源链 MRC20 UTXO 的 outpoint (txid:vout)
	Amount        FlexibleString `json:"amount"`        // 跃迁的金额 (可以是字符串或数字)
	TickId        string         `json:"tickId"`        // MRC20 ID (部署 PIN 的 ID)
	LocationIndex int            `json:"locationIndex"` // 目标链接收资产的 output 索引
	Metadata      string         `json:"metadata"`      // 可选的元数据
}

// Mrc20Arrival 表示一个跨链到达记录
type Mrc20Arrival struct {
	PinId         string          `json:"pinId"`         // arrival PIN 的 ID (用作 coord)
	TxId          string          `json:"txId"`          // arrival 交易的 txid
	AssetOutpoint string          `json:"assetOutpoint"` // 源链 UTXO 的 outpoint
	Amount        decimal.Decimal `json:"amount"`        // 跃迁金额
	TickId        string          `json:"tickId"`        // MRC20 ID
	Tick          string          `json:"tick"`          // MRC20 名称
	LocationIndex int             `json:"locationIndex"` // 目标链接收 output 索引
	ToAddress     string          `json:"toAddress"`     // 接收地址 (output[locationIndex]的地址)
	Chain         string          `json:"chain"`         // 目标链名称 (arrival 所在的链)
	SourceChain   string          `json:"sourceChain"`   // 源链名称 (从 UTXO 推断)
	Status        ArrivalStatus   `json:"status"`        // 状态: pending/completed/invalid
	Msg           string          `json:"msg"`           // 错误消息或备注
	BlockHeight   int64           `json:"blockHeight"`   // 区块高度
	Timestamp     int64           `json:"timestamp"`     // 时间戳
	TeleportPinId string          `json:"teleportPinId"` // 完成跃迁的 teleport PIN ID
	TeleportChain string          `json:"teleportChain"` // teleport 所在的链
	TeleportTxId  string          `json:"teleportTxId"`  // teleport 交易的 txid
	CompletedAt   int64           `json:"completedAt"`   // 跃迁完成时间
}

// Mrc20TeleportTransferData 是 teleport 类型 transfer 的扩展字段
type Mrc20TeleportTransferData struct {
	Amount string `json:"amount"` // 跃迁金额
	Vout   int    `json:"vout"`   // output 索引 (对于 teleport 通常不使用)
	Id     string `json:"id"`     // MRC20 ID
	Coord  string `json:"coord"`  // 目标链 arrival 的 PINID
	Chain  string `json:"chain"`  // 目标链名称
	Type   string `json:"type"`   // "teleport"
}

// Mrc20Teleport 表示一个跃迁传输记录
type Mrc20Teleport struct {
	PinId          string          `json:"pinId"`          // teleport transfer PIN 的 ID
	TxId           string          `json:"txId"`           // teleport 交易的 txid
	TickId         string          `json:"tickId"`         // MRC20 ID
	Tick           string          `json:"tick"`           // MRC20 名称
	Amount         decimal.Decimal `json:"amount"`         // 跃迁金额
	Coord          string          `json:"coord"`          // 目标链 arrival 的 PINID
	FromAddress    string          `json:"fromAddress"`    // 源地址
	SourceChain    string          `json:"sourceChain"`    // 源链名称 (teleport 所在的链)
	TargetChain    string          `json:"targetChain"`    // 目标链名称
	SpentUtxoPoint string          `json:"spentUtxoPoint"` // 消耗的 UTXO outpoint
	Status         int             `json:"status"`         // 0=进行中, 1=已完成, -1=失败
	Msg            string          `json:"msg"`            // 错误消息
	BlockHeight    int64           `json:"blockHeight"`    // 区块高度
	Timestamp      int64           `json:"timestamp"`      // 时间戳
}

// TeleportPendingIn 表示 teleport 接收方的待转入余额记录
// 当 arrival 和 teleport transfer 都在 mempool/出块后，但跃迁未最终完成时创建
type TeleportPendingIn struct {
	Coord       string          `json:"coord"`       // arrival PIN ID (唯一标识)
	ToAddress   string          `json:"toAddress"`   // 接收地址 (B 地址)
	TickId      string          `json:"tickId"`      // MRC20 ID
	Tick        string          `json:"tick"`        // MRC20 名称
	Amount      decimal.Decimal `json:"amount"`      // 跃迁金额
	Chain       string          `json:"chain"`       // 目标链 (接收方所在链)
	SourceChain string          `json:"sourceChain"` // 源链
	FromAddress string          `json:"fromAddress"` // 发送方地址
	TeleportTx  string          `json:"teleportTx"`  // teleport 交易 ID
	ArrivalTx   string          `json:"arrivalTx"`   // arrival 交易 ID
	BlockHeight int64           `json:"blockHeight"` // 记录创建时的区块高度 (-1 表示 mempool)
	Timestamp   int64           `json:"timestamp"`   // 时间戳
}

// TransferPendingIn 表示普通 transfer/native_transfer 接收方的待转入余额记录
// 当转账在 mempool 中时，记录接收方的 pending in 余额
type TransferPendingIn struct {
	TxPoint     string          `json:"txPoint"`     // 新 UTXO 的 outpoint (txid:vout)，唯一标识
	TxId        string          `json:"txId"`        // 交易 ID
	ToAddress   string          `json:"toAddress"`   // 接收地址
	TickId      string          `json:"tickId"`      // MRC20 ID
	Tick        string          `json:"tick"`        // MRC20 名称
	Amount      decimal.Decimal `json:"amount"`      // 转账金额
	Chain       string          `json:"chain"`       // 链名称
	FromAddress string          `json:"fromAddress"` // 发送方地址
	TxType      string          `json:"txType"`      // 类型: transfer/native_transfer
	BlockHeight int64           `json:"blockHeight"` // -1 表示 mempool
	Timestamp   int64           `json:"timestamp"`   // 时间戳
}

// Mrc20Transaction 表示 MRC20 交易流水记录
type Mrc20Transaction struct {
	TxId         string          `json:"txId"`         // 交易 ID (主交易，对于 teleport 是 teleport tx)
	TxPoint      string          `json:"txPoint"`      // 交易输出点 (txid:vout)，唯一标识此条记录
	TxIndex      int64           `json:"txIndex"`      // 交易序号 (全局递增，用于排序)
	PinId        string          `json:"pinId"`        // PIN ID
	TickId       string          `json:"tickId"`       // tick ID
	Tick         string          `json:"tick"`         // tick 名称
	TxType       string          `json:"txType"`       // 交易类型: mint/transfer/teleport_out/teleport_in
	Direction    string          `json:"direction"`    // 流水方向: "in" (收入) / "out" (支出)
	Address      string          `json:"address"`      // 关联地址（从谁的视角记录这条流水）
	FromAddress  string          `json:"fromAddress"`  // 发送方地址 (mint 时为空)
	ToAddress    string          `json:"toAddress"`    // 接收方地址
	Amount       decimal.Decimal `json:"amount"`       // 交易金额 (始终为正数)
	IsChange     bool            `json:"isChange"`     // 是否是找零
	Chain        string          `json:"chain"`        // 交易所在链
	BlockHeight  int64           `json:"blockHeight"`  // 区块高度
	Timestamp    int64           `json:"timestamp"`    // 时间戳
	RelatedTxId  string          `json:"relatedTxId"`  // 关联交易 ID (teleport 的 arrival tx)
	RelatedChain string          `json:"relatedChain"` // 关联链 (teleport 的目标/源链)
	RelatedPinId string          `json:"relatedPinId"` // 关联 PIN ID (arrival/teleport PIN)
	SpentUtxos   string          `json:"spentUtxos"`   // 消耗的 UTXO (JSON 数组字符串)
	CreatedUtxos string          `json:"createdUtxos"` // 创建的 UTXO (JSON 数组字符串)
	Msg          string          `json:"msg"`          // 验证失败原因
	Status       int             `json:"status"`       // 验证状态: 1=成功(verify=true), -1=失败(verify=false)
}

// Mrc20AccountBalance 表示账户余额记录
type Mrc20AccountBalance struct {
	Address          string          `json:"address"`          // 地址
	TickId           string          `json:"tickId"`           // tick ID
	Tick             string          `json:"tick"`             // tick 名称
	Balance          decimal.Decimal `json:"balance"`          // 已确认的可用余额
	PendingOut       decimal.Decimal `json:"pendingOut"`       // 待转出余额 (teleport pending 源链)
	PendingIn        decimal.Decimal `json:"pendingIn"`        // 待转入余额 (teleport pending 目标链)
	Chain            string          `json:"chain"`            // 链名称
	LastUpdateTx     string          `json:"lastUpdateTx"`     // 最后更新的交易 ID
	LastUpdateHeight int64           `json:"lastUpdateHeight"` // 最后更新的区块高度
	LastUpdateTime   int64           `json:"lastUpdateTime"`   // 最后更新时间戳
	UtxoCount        int             `json:"utxoCount"`        // UTXO 数量 (可用状态)
}
