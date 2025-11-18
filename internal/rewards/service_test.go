package rewards

import (
	"math"
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"endurance-rewards/internal/config"
	"github.com/gobitfly/eth-rewards/types"
)

func TestTotalNetworkRewards(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.RewardsHistoryFile = filepath.Join(t.TempDir(), "history.jsonl")
	svc := NewService(cfg)
	t.Cleanup(svc.Stop)

	windowStart := time.Now().Add(-2 * time.Hour)
	svc.setCacheWindowStart(windowStart)

	svc.cacheMux.Lock()
	svc.cache[1] = &types.ValidatorEpochIncome{
		AttestationSourceReward: 64,
	}
	svc.cache[1].TxFeeRewardWei = new(big.Int).Mul(big.NewInt(5), gweiScalar).Bytes()
	svc.cacheMux.Unlock()

	snapshot := svc.TotalNetworkRewards()
	if snapshot.ValidatorCount != 1 {
		t.Fatalf("expected 1 validator, got %d", snapshot.ValidatorCount)
	}
	if snapshot.ClRewardsGwei != 64 {
		t.Fatalf("unexpected CL rewards: %d", snapshot.ClRewardsGwei)
	}
	if snapshot.ElRewardsGwei != 5 {
		t.Fatalf("unexpected EL rewards: %d", snapshot.ElRewardsGwei)
	}
	if snapshot.TotalRewardsGwei != 69 {
		t.Fatalf("unexpected total rewards: %d", snapshot.TotalRewardsGwei)
	}
	if snapshot.TotalEffectiveBalanceGwei != defaultEffectiveBalanceGwei {
		t.Fatalf("unexpected effective balance: %d", snapshot.TotalEffectiveBalanceGwei)
	}
	if math.Abs(snapshot.WindowDurationSeconds-2*3600) > 2 {
		t.Fatalf("duration not close to 2h: %f", snapshot.WindowDurationSeconds)
	}
	expectedAPR := float64(snapshot.TotalRewardsGwei) / float64(snapshot.TotalEffectiveBalanceGwei)
	expectedAPR *= cfg.CacheResetInterval.Seconds() / snapshot.WindowDurationSeconds
	expectedAPR *= 100.0 * 365.0
	if math.Abs(snapshot.DailyAprPercent-expectedAPR) > 1e-9 {
		t.Fatalf("unexpected apr: got %f want %f", snapshot.DailyAprPercent, expectedAPR)
	}
}
