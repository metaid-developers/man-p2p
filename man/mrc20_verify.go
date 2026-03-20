package man

import (
	"fmt"
	"log"
	"man-p2p/mrc20"
	"strconv"

	"github.com/bytedance/sonic"
	"github.com/cockroachdb/pebble"
	"github.com/shopspring/decimal"
)

// VerifyMRC20TotalSupply 验证某个MRC20代币的总供应量是否正确
// 通过比较 UTXO总和、Balance总和、Deploy定义的供应量
func VerifyMRC20TotalSupply(tickId string) (*MRC20VerifyReport, error) {
	log.Printf("[Verify] 🔍 Verifying total supply for tickId: %s", tickId)

	report := &MRC20VerifyReport{
		TickId: tickId,
	}

	// 1. 获取Deploy信息
	deploy, err := PebbleStore.GetMrc20TickInfo(tickId, "")
	if err != nil {
		return nil, fmt.Errorf("get deploy info failed: %w", err)
	}
	report.TickName = deploy.Tick

	// 计算总供应量：mintCount * amtPerMint
	mintCount := int64(deploy.MintCount)
	amtPerMint, _ := strconv.ParseInt(deploy.AmtPerMint, 10, 64)
	report.ExpectedTotal = decimal.NewFromInt(mintCount * amtPerMint)

	// 2. 计算所有链的 UTXO 总和
	chains := []string{"btc", "doge", "mvc"}
	utxoByChain := make(map[string]decimal.Decimal)
	utxoCountByChain := make(map[string]int)

	for _, chain := range chains {
		totalUtxo, count, err := calculateChainUTXOTotal(chain, tickId)
		if err != nil {
			log.Printf("[Verify] ⚠️  Failed to calculate UTXO total for %s: %v", chain, err)
			continue
		}
		utxoByChain[chain] = totalUtxo
		utxoCountByChain[chain] = count
		report.TotalUTXO = report.TotalUTXO.Add(totalUtxo)
		report.UTXOCount += count
	}
	report.UTXOByChain = utxoByChain

	// 3. 计算所有链的 Balance 总和
	balanceByChain := make(map[string]decimal.Decimal)
	pendingOutByChain := make(map[string]decimal.Decimal)
	pendingInByChain := make(map[string]decimal.Decimal)

	for _, chain := range chains {
		balance, pendingOut, pendingIn, err := calculateChainBalanceTotal(chain, tickId)
		if err != nil {
			log.Printf("[Verify] ⚠️  Failed to calculate balance total for %s: %v", chain, err)
			continue
		}
		balanceByChain[chain] = balance
		pendingOutByChain[chain] = pendingOut
		pendingInByChain[chain] = pendingIn
		report.TotalBalance = report.TotalBalance.Add(balance)
		report.TotalPendingOut = report.TotalPendingOut.Add(pendingOut)
		report.TotalPendingIn = report.TotalPendingIn.Add(pendingIn)
	}
	report.BalanceByChain = balanceByChain
	report.PendingOutByChain = pendingOutByChain
	report.PendingInByChain = pendingInByChain

	// 4. 验证一致性
	// UTXO总和应该等于 Deploy的总供应量
	if !report.TotalUTXO.Equal(report.ExpectedTotal) {
		report.Errors = append(report.Errors, fmt.Sprintf(
			"❌ UTXO total mismatch: got %s, expected %s, diff=%s",
			report.TotalUTXO, report.ExpectedTotal, report.TotalUTXO.Sub(report.ExpectedTotal)))
	}

	// Balance总和 + PendingOut应该等于 Deploy的总供应量
	balancePlusPending := report.TotalBalance.Add(report.TotalPendingOut)
	if !balancePlusPending.Equal(report.ExpectedTotal) {
		report.Errors = append(report.Errors, fmt.Sprintf(
			"❌ Balance+PendingOut mismatch: got %s, expected %s, diff=%s",
			balancePlusPending, report.ExpectedTotal, balancePlusPending.Sub(report.ExpectedTotal)))
	}

	// PendingOut应该等于PendingIn（跨链pending应该平衡）
	if !report.TotalPendingOut.Equal(report.TotalPendingIn) {
		report.Errors = append(report.Errors, fmt.Sprintf(
			"⚠️  PendingOut != PendingIn: out=%s, in=%s, diff=%s",
			report.TotalPendingOut, report.TotalPendingIn, report.TotalPendingOut.Sub(report.TotalPendingIn)))
	}

	// 5. 检查每条链的内部一致性
	for _, chain := range chains {
		utxo := utxoByChain[chain]
		balance := balanceByChain[chain]
		pendingOut := pendingOutByChain[chain]
		// pendingIn := pendingInByChain[chain] // 暂不使用

		// 每条链的 Balance + PendingOut 应该等于 UTXO总和
		balancePlusPending := balance.Add(pendingOut)
		if !balancePlusPending.Equal(utxo) {
			report.Warnings = append(report.Warnings, fmt.Sprintf(
				"⚠️  [%s] Balance+PendingOut != UTXO: balance+pending=%s, utxo=%s, diff=%s",
				chain, balancePlusPending, utxo, balancePlusPending.Sub(utxo)))
		}
	}

	// 6. 总结
	if len(report.Errors) == 0 {
		report.Status = "✅ PASSED"
		log.Printf("[Verify] ✅ Verification passed for %s (%s)", report.TickName, tickId)
	} else {
		report.Status = "❌ FAILED"
		log.Printf("[Verify] ❌ Verification failed for %s (%s): %d errors", report.TickName, tickId, len(report.Errors))
	}

	return report, nil
}

// MRC20VerifyReport 验证报告
type MRC20VerifyReport struct {
	TickId         string                     `json:"tickId"`
	TickName       string                     `json:"tickName"`
	Status         string                     `json:"status"` // ✅ PASSED / ❌ FAILED
	ExpectedTotal  decimal.Decimal            `json:"expectedTotal"`
	TotalUTXO      decimal.Decimal            `json:"totalUtxo"`
	TotalBalance   decimal.Decimal            `json:"totalBalance"`
	TotalPendingOut decimal.Decimal           `json:"totalPendingOut"`
	TotalPendingIn  decimal.Decimal           `json:"totalPendingIn"`
	UTXOCount      int                        `json:"utxoCount"`
	UTXOByChain    map[string]decimal.Decimal `json:"utxoByChain"`
	BalanceByChain map[string]decimal.Decimal `json:"balanceByChain"`
	PendingOutByChain map[string]decimal.Decimal `json:"pendingOutByChain"`
	PendingInByChain  map[string]decimal.Decimal `json:"pendingInByChain"`
	Errors         []string                   `json:"errors"`
	Warnings       []string                   `json:"warnings"`
}

// calculateChainUTXOTotal 计算某条链上某个代币的UTXO总和
func calculateChainUTXOTotal(chain, tickId string) (decimal.Decimal, int, error) {
	total := decimal.Zero
	count := 0

	// 遍历该链上的所有UTXO
	prefix := []byte(fmt.Sprintf("mrc20_utxo_"))
	iter, err := PebbleStore.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return total, 0, err
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		var utxo mrc20.Mrc20Utxo
		if err := sonic.Unmarshal(iter.Value(), &utxo); err != nil {
			continue
		}

		// 过滤条件：匹配chain和tickId，状态为Available或TeleportPending
		if utxo.Chain != chain || utxo.Mrc20Id != tickId {
			continue
		}

		if utxo.Status == mrc20.UtxoStatusAvailable ||
			utxo.Status == mrc20.UtxoStatusTeleportPending {
			total = total.Add(utxo.AmtChange)
			count++
		}
	}

	return total, count, nil
}

// calculateChainBalanceTotal 计算某条链上某个代币的Balance总和
func calculateChainBalanceTotal(chain, tickId string) (decimal.Decimal, decimal.Decimal, decimal.Decimal, error) {
	totalBalance := decimal.Zero
	totalPendingOut := decimal.Zero
	totalPendingIn := decimal.Zero

	// 遍历该链上的所有AccountBalance
	prefix := []byte(fmt.Sprintf("balance_%s_", chain))
	iter, err := PebbleStore.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return totalBalance, totalPendingOut, totalPendingIn, err
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		var accountBalance mrc20.Mrc20AccountBalance
		if err := sonic.Unmarshal(iter.Value(), &accountBalance); err != nil {
			continue
		}

		// 过滤条件：匹配tickId
		if accountBalance.TickId != tickId {
			continue
		}

		totalBalance = totalBalance.Add(accountBalance.Balance)
		totalPendingOut = totalPendingOut.Add(accountBalance.PendingOut)
		totalPendingIn = totalPendingIn.Add(accountBalance.PendingIn)
	}

	return totalBalance, totalPendingOut, totalPendingIn, nil
}

// VerifyAllMRC20 验证所有MRC20代币
func VerifyAllMRC20() ([]*MRC20VerifyReport, error) {
	log.Printf("[Verify] 🔍 Verifying all MRC20 tokens")

	// 获取所有代币列表
	ticks, err := getAllMRC20Ticks()
	if err != nil {
		return nil, err
	}

	var reports []*MRC20VerifyReport
	for _, tickId := range ticks {
		report, err := VerifyMRC20TotalSupply(tickId)
		if err != nil {
			log.Printf("[Verify] ⚠️  Failed to verify %s: %v", tickId, err)
			continue
		}
		reports = append(reports, report)
	}

	// 统计
	passed := 0
	failed := 0
	for _, report := range reports {
		if len(report.Errors) == 0 {
			passed++
		} else {
			failed++
		}
	}

	log.Printf("[Verify] 📊 Summary: %d ticks verified, %d passed, %d failed", len(reports), passed, failed)

	return reports, nil
}

// getAllMRC20Ticks 获取所有MRC20代币的tickId列表
func getAllMRC20Ticks() ([]string, error) {
	var ticks []string

	prefix := []byte("mrc20_tick_")
	iter, err := PebbleStore.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		var deploy mrc20.Mrc20DeployInfo
		if err := sonic.Unmarshal(iter.Value(), &deploy); err != nil {
			continue
		}
		ticks = append(ticks, deploy.Mrc20Id)
	}

	return ticks, nil
}
