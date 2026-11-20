package man

import (
	"fmt"
	"testing"
	"time"

	"man-p2p/mrc20"
)

// TestMrc20ReadPathScaleLatency R2 出口标准：全库 UTXO 放大后，
// 地址定向读路径时延恒定（与库存量无关）。
// 10k 无关 UTXO（500 地址 × 100 tick）+ 10 条目标地址 UTXO：
// 旧实现每次余额请求读全表、holders 为 O(持有者×全库) 平方级；
// 新实现只碰地址前缀。阈值给 CI 留足余量。
func TestMrc20ReadPathScaleLatency(t *testing.T) {
	if testing.Short() {
		t.Skip("scale test skipped in -short mode")
	}
	pd := setupMrc20ReadPathStore(t)
	const (
		tick     = "tickScale"
		addr     = "ScaleAddrTarget"
		stockNum = 10000
	)

	// 目标地址的 10 条 UTXO
	for i := 0; i < 10; i++ {
		utxo := fixtureUtxo(fmt.Sprintf("scale_t_%04d:0", i), tick, "OtherScale", addr, 100+int64(i), mrc20.UtxoStatusAvailable, "1")
		mustSaveUtxo(t, pd, utxo)
	}
	// 全库噪声：不同地址/不同 tick 的 10k 条
	for i := 0; i < stockNum; i++ {
		utxo := fixtureUtxo(fmt.Sprintf("scale_n_%06d:0", i), fmt.Sprintf("tickNoise%03d", i%100),
			"NoiseFrom", fmt.Sprintf("NoiseAddr%04d", i%500), int64(200+i%1000), mrc20.UtxoStatusAvailable, "1")
		mustSaveUtxo(t, pd, utxo)
	}

	start := time.Now()
	bal, err := CalculateBalanceFromUTXO("mvc", addr, tick)
	if err != nil {
		t.Fatalf("CalculateBalanceFromUTXO: %v", err)
	}
	balanceElapsed := time.Since(start)
	if bal.Balance.String() != "10" {
		t.Fatalf("balance = %s, want 10", bal.Balance)
	}
	if balanceElapsed > 100*time.Millisecond {
		t.Fatalf("balance query took %s at %d-row stock — read path is not address-scoped", balanceElapsed, stockNum)
	}

	start = time.Now()
	balances, err := GetAddressBalances("mvc", addr)
	if err != nil {
		t.Fatalf("GetAddressBalances: %v", err)
	}
	balancesElapsed := time.Since(start)
	if len(balances) != 1 || balances[0].Balance.String() != "10" {
		t.Fatalf("unexpected balances: %+v", balances)
	}
	if balancesElapsed > 100*time.Millisecond {
		t.Fatalf("GetAddressBalances took %s — not address-scoped", balancesElapsed)
	}

	start = time.Now()
	holders, err := pd.GetMrc20Holders(tick, 0, 50, "")
	if err != nil {
		t.Fatalf("GetMrc20Holders: %v", err)
	}
	holdersElapsed := time.Since(start)
	if len(holders) != 1 || holders[0].Balance.String() != "10" {
		t.Fatalf("unexpected holders: %+v", holders)
	}

	t.Logf("balance=%s balances=%s holders=%s at stock=%d", balanceElapsed, balancesElapsed, holdersElapsed, stockNum)
}
