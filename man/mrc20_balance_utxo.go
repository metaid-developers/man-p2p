package man

import (
	"fmt"
	"log"
	"man-p2p/mrc20"

	"github.com/bytedance/sonic"
	"github.com/cockroachdb/pebble"
	"github.com/shopspring/decimal"
)

// CalculateBalanceFromUTXO 通过UTXO实时计算余额
// 这是新架构的核心：不维护余额表，余额=UTXO状态的函数
func CalculateBalanceFromUTXO(chain, address, tickId string) (*MRC20Balance, error) {
	balance := &MRC20Balance{
		Chain:          chain,
		Address:        address,
		TickId:         tickId,
		Balance:        decimal.Zero,
		PendingOut:     decimal.Zero,
		PendingIn:      decimal.Zero,
		AvailableUtxos: 0,
		PendingUtxos:   0,
	}

	// 1. 扫描该地址的所有UTXO
	prefix := []byte(fmt.Sprintf("mrc20_utxo_"))
	iter, err := PebbleStore.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		var utxo mrc20.Mrc20Utxo
		if err := sonic.Unmarshal(iter.Value(), &utxo); err != nil {
			continue
		}

		// 过滤条件：匹配chain, address, tickId
		if utxo.Chain != chain || utxo.ToAddress != address || utxo.Mrc20Id != tickId {
			continue
		}

		// 根据UTXO状态计算余额
		switch utxo.Status {
		case mrc20.UtxoStatusAvailable:
			// 可用余额
			balance.Balance = balance.Balance.Add(utxo.AmtChange)
			balance.AvailableUtxos++
			log.Printf("[Balance] Available UTXO: %s, amt=%s, balance=%s", utxo.TxPoint, utxo.AmtChange, balance.Balance)

		case mrc20.UtxoStatusMintPending:
			// Mint pending in (mempool中等待确认的mint)
			balance.PendingIn = balance.PendingIn.Add(utxo.AmtChange)
			balance.PendingUtxos++
			log.Printf("[Balance] MintPending UTXO: %s, amt=%s, pendingIn=%s", utxo.TxPoint, utxo.AmtChange, balance.PendingIn)

		case mrc20.UtxoStatusTeleportPending:
			// Teleport pending: 资金还在链上等待跃迁，同时计入balance和pendingOut
			// balance保持不变（实际还未离开链），pendingOut显示正在跃迁的金额
			balance.Balance = balance.Balance.Add(utxo.AmtChange)
			balance.PendingOut = balance.PendingOut.Add(utxo.AmtChange)
			balance.PendingUtxos++
			log.Printf("[Balance] TeleportPending UTXO: %s, amt=%s, balance=%s, pendingOut=%s", utxo.TxPoint, utxo.AmtChange, balance.Balance, balance.PendingOut)

		case mrc20.UtxoStatusTransferPending:
			// Transfer pending: 通过AmtChange正负区分输入/输出UTXO
			// 负数 = 输入UTXO（被花费）→ 计入balance（因为还未真正花费）+ pendingOut
			// 正数 = 输出UTXO（新创建）→ 通过TransferPendingIn显示pendingIn
			if utxo.AmtChange.IsNegative() {
				// 输入UTXO（被花费的），需要同时计入balance和pendingOut
				// balance保持不变（因为转账还未真正完成）
				// pendingOut显示正在转出的金额
				absAmount := utxo.AmtChange.Abs()
				balance.Balance = balance.Balance.Add(absAmount)
				balance.PendingOut = balance.PendingOut.Add(absAmount)
				balance.PendingUtxos++
				log.Printf("[Balance] TransferPending Input UTXO: %s, amt=%s, balance=%s, pendingOut=%s", utxo.TxPoint, utxo.AmtChange, balance.Balance, balance.PendingOut)
			} else {
				log.Printf("[Balance] TransferPending Output UTXO (skipped): %s, amt=%s", utxo.TxPoint, utxo.AmtChange)
			}
			// 输出UTXO（AmtChange > 0）不计入余额，通过TransferPendingIn处理

		case mrc20.UtxoStatusSpent:
			// 已花费，不计入余额
			log.Printf("[Balance] Spent UTXO (skipped): %s, amt=%s", utxo.TxPoint, utxo.AmtChange)
			continue

		default:
			log.Printf("[Balance] Unknown UTXO status: %d, txPoint=%s", utxo.Status, utxo.TxPoint)
		}

		// 记录Tick名称（第一次获取）
		if balance.Tick == "" {
			balance.Tick = utxo.Tick
		}
	}

	// 2. 计算 PendingIn (从 TeleportPendingIn 和 TransferPendingIn)

	// 2.1 Teleport PendingIn
	teleportPendingIns, err := PebbleStore.GetTeleportPendingInByAddress(address)
	if err == nil {
		for _, pendingIn := range teleportPendingIns {
			if pendingIn.Chain == chain && pendingIn.TickId == tickId {
				balance.PendingIn = balance.PendingIn.Add(pendingIn.Amount)
				// 如果没有UTXO，从PendingIn获取Tick名称
				if balance.Tick == "" {
					balance.Tick = pendingIn.Tick
				}
			}
		}
	}

	// 2.2 Transfer PendingIn
	transferPendingIns, err := PebbleStore.GetTransferPendingInByAddress(address)
	if err == nil {
		for _, pendingIn := range transferPendingIns {
			if pendingIn.Chain == chain && pendingIn.TickId == tickId {
				balance.PendingIn = balance.PendingIn.Add(pendingIn.Amount)
				// 如果没有UTXO，从PendingIn获取Tick名称
				if balance.Tick == "" {
					balance.Tick = pendingIn.Tick
				}
			}
		}
	}

	// 3. 如果Tick名称仍然为空（没有UTXO也没有PendingIn中的Tick），从deploy信息获取
	if balance.Tick == "" {
		tickInfo, err := PebbleStore.GetMrc20TickInfo(tickId, "")
		if err == nil {
			balance.Tick = tickInfo.Tick
		} else {
			log.Printf("[Balance] Warning: unable to get tick name for tickId=%s: %v", tickId, err)
		}
	}

	// 4. 计算总额
	balance.Total = balance.Balance.Add(balance.PendingOut).Add(balance.PendingIn)

	log.Printf("[Balance] Calculated from UTXO: chain=%s, address=%s, tick=%s, balance=%s, pendingOut=%s, pendingIn=%s, utxos=%d",
		chain, address, balance.Tick, balance.Balance, balance.PendingOut, balance.PendingIn, balance.AvailableUtxos)

	return balance, nil
}

// MRC20Balance 余额结构（基于UTXO计算）
type MRC20Balance struct {
	Chain          string          `json:"chain"`
	Address        string          `json:"address"`
	TickId         string          `json:"tickId"`
	Tick           string          `json:"tick"`
	Balance        decimal.Decimal `json:"balance"`        // sum(UTXO where status=Available)
	PendingOut     decimal.Decimal `json:"pendingOut"`     // sum(UTXO where status in [TeleportPending, TransferPending])
	PendingIn      decimal.Decimal `json:"pendingIn"`      // TeleportPendingIn + TransferPendingIn
	Total          decimal.Decimal `json:"total"`          // Balance + PendingOut + PendingIn
	AvailableUtxos int             `json:"availableUtxos"` // 可用UTXO数量
	PendingUtxos   int             `json:"pendingUtxos"`   // Pending UTXO数量
}

// GetAddressBalances 获取某个地址的所有代币余额（基于UTXO实时计算）
func GetAddressBalances(chain, address string) ([]*MRC20Balance, error) {
	// 先获取该地址持有的所有tickId（通过扫描UTXO + PendingIn）
	tickMap := make(map[string]bool)

	// 1. 扫描UTXO
	prefix := []byte(fmt.Sprintf("mrc20_utxo_"))
	iter, err := PebbleStore.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		var utxo mrc20.Mrc20Utxo
		if err := sonic.Unmarshal(iter.Value(), &utxo); err != nil {
			continue
		}

		if utxo.Chain == chain && utxo.ToAddress == address {
			// 只统计非Spent的UTXO
			if utxo.Status != mrc20.UtxoStatusSpent {
				tickMap[utxo.Mrc20Id] = true
			}
		}
	}

	// 2. 扫描 TeleportPendingIn（接收方可能没有UTXO，只有PendingIn）
	teleportPendingIns, err := PebbleStore.GetTeleportPendingInByAddress(address)
	if err == nil {
		for _, pendingIn := range teleportPendingIns {
			if pendingIn.Chain == chain {
				tickMap[pendingIn.TickId] = true
			}
		}
	}

	// 3. 扫描 TransferPendingIn（同理）
	transferPendingIns, err := PebbleStore.GetTransferPendingInByAddress(address)
	if err == nil {
		for _, pendingIn := range transferPendingIns {
			if pendingIn.Chain == chain {
				tickMap[pendingIn.TickId] = true
			}
		}
	}

	// 为每个tickId计算余额
	var balances []*MRC20Balance
	for tickId := range tickMap {
		balance, err := CalculateBalanceFromUTXO(chain, address, tickId)
		if err != nil {
			log.Printf("[Balance] Failed to calculate for tickId=%s: %v", tickId, err)
			continue
		}

		// 只返回有余额的
		if balance.Total.GreaterThan(decimal.Zero) {
			balances = append(balances, balance)
		}
	}

	return balances, nil
}

// GetAllChainsBalances 获取某个地址在所有链上的余额
func GetAllChainsBalances(address string) (map[string][]*MRC20Balance, error) {
	chains := []string{"btc", "doge", "mvc"}
	result := make(map[string][]*MRC20Balance)

	for _, chain := range chains {
		balances, err := GetAddressBalances(chain, address)
		if err != nil {
			log.Printf("[Balance] Failed to get balances for chain=%s: %v", chain, err)
			continue
		}

		if len(balances) > 0 {
			result[chain] = balances
		}
	}

	return result, nil
}
