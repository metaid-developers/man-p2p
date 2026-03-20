package api

import (
	"sort"
	"strings"

	"man-p2p/mrc20"
	"man-p2p/pin"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type sortParams struct {
	By   string
	Desc bool
}

func parseSortParams(ctx *gin.Context, defaultBy string) sortParams {
	sortBy := strings.TrimSpace(ctx.Query("sortBy"))
	if sortBy == "" {
		sortBy = defaultBy
	}
	order := strings.TrimSpace(ctx.Query("order"))
	if order == "" {
		order = strings.TrimSpace(ctx.Query("sortOrder"))
	}
	desc := true
	if order != "" && strings.EqualFold(order, "asc") {
		desc = false
	}
	return sortParams{By: strings.ToLower(strings.TrimSpace(sortBy)), Desc: desc}
}

func lessInt64(a, b int64, desc bool) bool {
	if desc {
		return a > b
	}
	return a < b
}

func lessString(a, b string, desc bool) bool {
	if desc {
		return a > b
	}
	return a < b
}

func lessDecimal(a, b decimal.Decimal, desc bool) bool {
	cmp := a.Cmp(b)
	if desc {
		return cmp > 0
	}
	return cmp < 0
}

func sortPinMsgList(list []*pin.PinMsg, params sortParams) {
	if len(list) < 2 {
		return
	}
	by := params.By
	if by == "" {
		by = "timestamp"
	}
	switch by {
	case "time", "timestamp":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b int64
			if list[i] != nil {
				a = list[i].Timestamp
			}
			if list[j] != nil {
				b = list[j].Timestamp
			}
			return lessInt64(a, b, params.Desc)
		})
	case "number":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b int64
			if list[i] != nil {
				a = list[i].Number
			}
			if list[j] != nil {
				b = list[j].Number
			}
			return lessInt64(a, b, params.Desc)
		})
	case "height", "blockheight":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b int64
			if list[i] != nil {
				a = list[i].Height
			}
			if list[j] != nil {
				b = list[j].Height
			}
			return lessInt64(a, b, params.Desc)
		})
	case "id":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b string
			if list[i] != nil {
				a = list[i].Id
			}
			if list[j] != nil {
				b = list[j].Id
			}
			return lessString(a, b, params.Desc)
		})
	case "metaid":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b string
			if list[i] != nil {
				a = list[i].MetaId
			}
			if list[j] != nil {
				b = list[j].MetaId
			}
			return lessString(a, b, params.Desc)
		})
	case "path":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b string
			if list[i] != nil {
				a = list[i].Path
			}
			if list[j] != nil {
				b = list[j].Path
			}
			return lessString(a, b, params.Desc)
		})
	default:
		sort.SliceStable(list, func(i, j int) bool {
			var a, b int64
			if list[i] != nil {
				a = list[i].Timestamp
			}
			if list[j] != nil {
				b = list[j].Timestamp
			}
			return lessInt64(a, b, params.Desc)
		})
	}
}

func sortPinInscriptionList(list []*pin.PinInscription, params sortParams) {
	if len(list) < 2 {
		return
	}
	by := params.By
	if by == "" {
		by = "timestamp"
	}
	switch by {
	case "time", "timestamp":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b int64
			if list[i] != nil {
				a = list[i].Timestamp
			}
			if list[j] != nil {
				b = list[j].Timestamp
			}
			return lessInt64(a, b, params.Desc)
		})
	case "height", "blockheight", "genesisheight":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b int64
			if list[i] != nil {
				a = list[i].GenesisHeight
			}
			if list[j] != nil {
				b = list[j].GenesisHeight
			}
			return lessInt64(a, b, params.Desc)
		})
	case "number":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b int64
			if list[i] != nil {
				a = list[i].Number
			}
			if list[j] != nil {
				b = list[j].Number
			}
			return lessInt64(a, b, params.Desc)
		})
	case "id":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b string
			if list[i] != nil {
				a = list[i].Id
			}
			if list[j] != nil {
				b = list[j].Id
			}
			return lessString(a, b, params.Desc)
		})
	case "metaid":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b string
			if list[i] != nil {
				a = list[i].MetaId
			}
			if list[j] != nil {
				b = list[j].MetaId
			}
			return lessString(a, b, params.Desc)
		})
	case "path":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b string
			if list[i] != nil {
				a = list[i].Path
			}
			if list[j] != nil {
				b = list[j].Path
			}
			return lessString(a, b, params.Desc)
		})
	default:
		sort.SliceStable(list, func(i, j int) bool {
			var a, b int64
			if list[i] != nil {
				a = list[i].Timestamp
			}
			if list[j] != nil {
				b = list[j].Timestamp
			}
			return lessInt64(a, b, params.Desc)
		})
	}
}

func sortMetaIdInfoList(list []pin.MetaIdInfo, params sortParams) {
	if len(list) < 2 {
		return
	}
	by := params.By
	if by == "" {
		by = "number"
	}
	switch by {
	case "number":
		sort.SliceStable(list, func(i, j int) bool {
			return lessInt64(list[i].Number, list[j].Number, params.Desc)
		})
	case "pdv":
		sort.SliceStable(list, func(i, j int) bool {
			return lessInt64(list[i].Pdv, list[j].Pdv, params.Desc)
		})
	case "fdv":
		sort.SliceStable(list, func(i, j int) bool {
			return lessInt64(list[i].Fdv, list[j].Fdv, params.Desc)
		})
	case "followcount":
		sort.SliceStable(list, func(i, j int) bool {
			return lessInt64(list[i].FollowCount, list[j].FollowCount, params.Desc)
		})
	case "name":
		sort.SliceStable(list, func(i, j int) bool {
			return lessString(list[i].Name, list[j].Name, params.Desc)
		})
	case "metaid":
		sort.SliceStable(list, func(i, j int) bool {
			return lessString(list[i].MetaId, list[j].MetaId, params.Desc)
		})
	case "address":
		sort.SliceStable(list, func(i, j int) bool {
			return lessString(list[i].Address, list[j].Address, params.Desc)
		})
	case "pinid", "id":
		sort.SliceStable(list, func(i, j int) bool {
			return lessString(list[i].PinId, list[j].PinId, params.Desc)
		})
	default:
		sort.SliceStable(list, func(i, j int) bool {
			return lessInt64(list[i].Number, list[j].Number, params.Desc)
		})
	}
}

func sortNotifcationList(list []pin.NotifcationData, params sortParams) {
	if len(list) < 2 {
		return
	}
	by := params.By
	if by == "" {
		by = "notifcationtime"
	}
	switch by {
	case "time", "timestamp", "notifcationtime":
		sort.SliceStable(list, func(i, j int) bool {
			return lessInt64(list[i].NotifcationTime, list[j].NotifcationTime, params.Desc)
		})
	case "id", "notifcationid":
		sort.SliceStable(list, func(i, j int) bool {
			return lessInt64(list[i].NotifcationId, list[j].NotifcationId, params.Desc)
		})
	case "frompinid":
		sort.SliceStable(list, func(i, j int) bool {
			return lessString(list[i].FromPinId, list[j].FromPinId, params.Desc)
		})
	default:
		sort.SliceStable(list, func(i, j int) bool {
			return lessInt64(list[i].NotifcationTime, list[j].NotifcationTime, params.Desc)
		})
	}
}

func sortMrc20DeployInfoList(list []mrc20.Mrc20DeployInfo, params sortParams) {
	if len(list) < 2 {
		return
	}
	by := params.By
	if by == "" {
		by = "deploytime"
	}
	switch by {
	case "time", "timestamp", "deploytime":
		sort.SliceStable(list, func(i, j int) bool {
			return lessInt64(list[i].DeployTime, list[j].DeployTime, params.Desc)
		})
	case "number", "pinnumber":
		sort.SliceStable(list, func(i, j int) bool {
			return lessInt64(list[i].PinNumber, list[j].PinNumber, params.Desc)
		})
	case "holders":
		sort.SliceStable(list, func(i, j int) bool {
			return lessInt64(int64(list[i].Holders), int64(list[j].Holders), params.Desc)
		})
	case "txcount":
		sort.SliceStable(list, func(i, j int) bool {
			return lessInt64(int64(list[i].TxCount), int64(list[j].TxCount), params.Desc)
		})
	case "tick":
		sort.SliceStable(list, func(i, j int) bool {
			return lessString(list[i].Tick, list[j].Tick, params.Desc)
		})
	case "tickid", "id", "mrc20id":
		sort.SliceStable(list, func(i, j int) bool {
			return lessString(list[i].Mrc20Id, list[j].Mrc20Id, params.Desc)
		})
	default:
		sort.SliceStable(list, func(i, j int) bool {
			return lessInt64(list[i].DeployTime, list[j].DeployTime, params.Desc)
		})
	}
}

func sortMrc20UtxoList(list []*mrc20.Mrc20Utxo, params sortParams) {
	if len(list) < 2 {
		return
	}
	by := params.By
	if by == "" {
		by = "timestamp"
	}
	switch by {
	case "time", "timestamp":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b int64
			if list[i] != nil {
				a = list[i].Timestamp
			}
			if list[j] != nil {
				b = list[j].Timestamp
			}
			return lessInt64(a, b, params.Desc)
		})
	case "height", "blockheight":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b int64
			if list[i] != nil {
				a = list[i].BlockHeight
			}
			if list[j] != nil {
				b = list[j].BlockHeight
			}
			return lessInt64(a, b, params.Desc)
		})
	case "amount", "amtchange":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b decimal.Decimal
			if list[i] != nil {
				a = list[i].AmtChange
			}
			if list[j] != nil {
				b = list[j].AmtChange
			}
			return lessDecimal(a, b, params.Desc)
		})
	case "status":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b int64
			if list[i] != nil {
				a = int64(list[i].Status)
			}
			if list[j] != nil {
				b = int64(list[j].Status)
			}
			return lessInt64(a, b, params.Desc)
		})
	case "txpoint":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b string
			if list[i] != nil {
				a = list[i].TxPoint
			}
			if list[j] != nil {
				b = list[j].TxPoint
			}
			return lessString(a, b, params.Desc)
		})
	default:
		sort.SliceStable(list, func(i, j int) bool {
			var a, b int64
			if list[i] != nil {
				a = list[i].Timestamp
			}
			if list[j] != nil {
				b = list[j].Timestamp
			}
			return lessInt64(a, b, params.Desc)
		})
	}
}

func sortMrc20TransactionList(list []*mrc20.Mrc20Transaction, params sortParams) {
	if len(list) < 2 {
		return
	}
	by := params.By
	if by == "" {
		by = "timestamp"
	}
	switch by {
	case "time", "timestamp":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b int64
			if list[i] != nil {
				a = list[i].Timestamp
			}
			if list[j] != nil {
				b = list[j].Timestamp
			}
			return lessInt64(a, b, params.Desc)
		})
	case "height", "blockheight":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b int64
			if list[i] != nil {
				a = list[i].BlockHeight
			}
			if list[j] != nil {
				b = list[j].BlockHeight
			}
			return lessInt64(a, b, params.Desc)
		})
	case "txindex":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b int64
			if list[i] != nil {
				a = list[i].TxIndex
			}
			if list[j] != nil {
				b = list[j].TxIndex
			}
			return lessInt64(a, b, params.Desc)
		})
	case "amount":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b decimal.Decimal
			if list[i] != nil {
				a = list[i].Amount
			}
			if list[j] != nil {
				b = list[j].Amount
			}
			return lessDecimal(a, b, params.Desc)
		})
	case "tick", "tickid":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b string
			if list[i] != nil {
				a = list[i].TickId
			}
			if list[j] != nil {
				b = list[j].TickId
			}
			return lessString(a, b, params.Desc)
		})
	case "txid":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b string
			if list[i] != nil {
				a = list[i].TxId
			}
			if list[j] != nil {
				b = list[j].TxId
			}
			return lessString(a, b, params.Desc)
		})
	default:
		sort.SliceStable(list, func(i, j int) bool {
			var a, b int64
			if list[i] != nil {
				a = list[i].Timestamp
			}
			if list[j] != nil {
				b = list[j].Timestamp
			}
			return lessInt64(a, b, params.Desc)
		})
	}
}

func sortMrc20BalanceList(list []*mrc20.Mrc20Balance, params sortParams) {
	if len(list) < 2 {
		return
	}
	by := params.By
	if by == "" {
		by = "id"
	}
	switch by {
	case "balance":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b decimal.Decimal
			if list[i] != nil {
				a = list[i].Balance
			}
			if list[j] != nil {
				b = list[j].Balance
			}
			return lessDecimal(a, b, params.Desc)
		})
	case "pendingin":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b decimal.Decimal
			if list[i] != nil {
				a = list[i].PendingInBalance
			}
			if list[j] != nil {
				b = list[j].PendingInBalance
			}
			return lessDecimal(a, b, params.Desc)
		})
	case "pendingout":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b decimal.Decimal
			if list[i] != nil {
				a = list[i].PendingOutBalance
			}
			if list[j] != nil {
				b = list[j].PendingOutBalance
			}
			return lessDecimal(a, b, params.Desc)
		})
	case "tick", "name":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b string
			if list[i] != nil {
				a = list[i].Name
			}
			if list[j] != nil {
				b = list[j].Name
			}
			return lessString(a, b, params.Desc)
		})
	case "id", "tickid":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b string
			if list[i] != nil {
				a = list[i].Id
			}
			if list[j] != nil {
				b = list[j].Id
			}
			return lessString(a, b, params.Desc)
		})
	default:
		sort.SliceStable(list, func(i, j int) bool {
			var a, b string
			if list[i] != nil {
				a = list[i].Id
			}
			if list[j] != nil {
				b = list[j].Id
			}
			return lessString(a, b, params.Desc)
		})
	}
}

func sortTeleportTransactionList(list []*mrc20.TeleportTransaction, params sortParams) {
	if len(list) < 2 {
		return
	}
	by := params.By
	if by == "" {
		by = "createdat"
	}
	switch by {
	case "time", "timestamp", "createdat":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b int64
			if list[i] != nil {
				a = list[i].CreatedAt
			}
			if list[j] != nil {
				b = list[j].CreatedAt
			}
			return lessInt64(a, b, params.Desc)
		})
	case "updatedat":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b int64
			if list[i] != nil {
				a = list[i].UpdatedAt
			}
			if list[j] != nil {
				b = list[j].UpdatedAt
			}
			return lessInt64(a, b, params.Desc)
		})
	case "completedat":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b int64
			if list[i] != nil {
				a = list[i].CompletedAt
			}
			if list[j] != nil {
				b = list[j].CompletedAt
			}
			return lessInt64(a, b, params.Desc)
		})
	case "sourceheight", "sourceblockheight":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b int64
			if list[i] != nil {
				a = list[i].SourceBlockHeight
			}
			if list[j] != nil {
				b = list[j].SourceBlockHeight
			}
			return lessInt64(a, b, params.Desc)
		})
	case "targetheight", "targetblockheight":
		sort.SliceStable(list, func(i, j int) bool {
			var a, b int64
			if list[i] != nil {
				a = list[i].TargetBlockHeight
			}
			if list[j] != nil {
				b = list[j].TargetBlockHeight
			}
			return lessInt64(a, b, params.Desc)
		})
	default:
		sort.SliceStable(list, func(i, j int) bool {
			var a, b int64
			if list[i] != nil {
				a = list[i].CreatedAt
			}
			if list[j] != nil {
				b = list[j].CreatedAt
			}
			return lessInt64(a, b, params.Desc)
		})
	}
}
