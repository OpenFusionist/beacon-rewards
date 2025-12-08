package rewards

import (
	"bufio"
	"bytes"
	"encoding/json"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"beacon-rewards/internal/config"
	"beacon-rewards/internal/utils"

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
	currentEpoch := utils.TimeToEpoch(time.Now())
	svc.latestSyncEpoch = currentEpoch
	svc.cacheMux.Unlock()

	snapshot := svc.TotalNetworkRewards()
	if snapshot.ActiveValidatorCount != 1 {
		t.Fatalf("expected 1 validator, got %d", snapshot.ActiveValidatorCount)
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

	expectedEnd := utils.EpochToTime(currentEpoch)
	expectedDuration := expectedEnd.Sub(windowStart).Seconds()
	if math.Abs(snapshot.WindowDurationSeconds-expectedDuration) > 2 {
		t.Fatalf("duration mismatch: got %f want %f", snapshot.WindowDurationSeconds, expectedDuration)
	}
	expectedAPR := float64(snapshot.TotalRewardsGwei) / float64(snapshot.TotalEffectiveBalanceGwei)
	expectedAPR *= cfg.CacheResetInterval.Seconds() / snapshot.WindowDurationSeconds
	expectedAPR *= 100.0 * 365.0
	if math.Abs(snapshot.ProjectAprPercent-expectedAPR) > 1e-9 {
		t.Fatalf("unexpected apr: got %f want %f", snapshot.ProjectAprPercent, expectedAPR)
	}
}

func TestCacheResetTimer(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.RewardsHistoryFile = filepath.Join(t.TempDir(), "history.jsonl")
	svc := NewService(cfg)

	// Freeze time near midnight UTC+8 so the reset triggers quickly, then advance on subsequent calls
	loc := time.FixedZone("UTC+8", 8*60*60)
	base := time.Date(2024, 1, 1, 23, 59, 59, int(900*time.Millisecond), loc)
	callCount := 0
	clock := func() time.Time {
		if callCount == 0 {
			callCount++
			return base
		}
		return base.Add(time.Second)
	}

	svc.cacheMux.Lock()
	svc.cache[42] = &types.ValidatorEpochIncome{
		AttestationSourceReward: 10,
	}
	svc.latestSyncEpoch = utils.TimeToEpoch(base)
	svc.cacheMux.Unlock()

	done := make(chan struct{})
	t.Cleanup(func() {
		svc.Stop()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	})

	go func() {
		svc.cacheResetTimerWithClock(clock)
		close(done)
	}()

	// Wait until the reset clears the cache
	waitDeadline := time.After(2 * time.Second)
	for {
		svc.cacheMux.RLock()
		cacheEmpty := len(svc.cache) == 0
		svc.cacheMux.RUnlock()

		if cacheEmpty {
			break
		}

		select {
		case <-waitDeadline:
			t.Fatal("cache was not reset before deadline")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	data, err := os.ReadFile(cfg.RewardsHistoryFile)
	if err != nil {
		t.Fatalf("failed to read history file: %v", err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	if !scanner.Scan() {
		t.Fatalf("expected snapshot entry after reset")
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}

	var snap NetworkRewardSnapshot
	if err := json.Unmarshal(scanner.Bytes(), &snap); err != nil {
		t.Fatalf("failed to unmarshal snapshot: %v", err)
	}
	if snap.ActiveValidatorCount != 1 {
		t.Fatalf("unexpected active validator count: %d", snap.ActiveValidatorCount)
	}
	if snap.TotalRewardsGwei == 0 {
		t.Fatalf("expected rewards to be recorded in snapshot")
	}

	svc.cacheMux.RLock()
	latestEpoch := svc.latestSyncEpoch
	svc.cacheMux.RUnlock()

	expectedEpoch := utils.TimeToEpoch(base)
	if latestEpoch != expectedEpoch {
		t.Fatalf("latestSyncEpoch should be preserved, got %d want %d", latestEpoch, expectedEpoch)
	}

	windowStart := svc.cacheWindowStartTime()
	expectedWindowStart := base.Add(time.Second)
	if windowStart.Before(expectedWindowStart.Add(-time.Second)) || windowStart.After(expectedWindowStart.Add(time.Second)) {
		t.Fatalf("cache window start not updated near reset time: got %s want around %s", windowStart, expectedWindowStart)
	}
}

func TestStartEpochUsesLookback(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.RewardsHistoryFile = filepath.Join(t.TempDir(), "history.jsonl")
	cfg.BackfillLookback = 6 * time.Hour
	svc := NewService(cfg)

	now := time.Unix(utils.GenesisTimestamp(), 0).Add(time.Duration(100*utils.SECONDS_PER_EPOCH) * time.Second)
	expected := utils.TimeToEpoch(now.Add(-cfg.BackfillLookback))

	if got := svc.startEpoch(now); got != expected {
		t.Fatalf("start epoch from lookback mismatch: got %d want %d", got, expected)
	}
}

func TestStartEpochDefaultsToCacheWindow(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.RewardsHistoryFile = filepath.Join(t.TempDir(), "history.jsonl")
	cfg.BackfillLookback = 0
	svc := NewService(cfg)

	windowStart := time.Unix(utils.GenesisTimestamp(), 0).Add(time.Duration(20*utils.SECONDS_PER_EPOCH) * time.Second)
	svc.setCacheWindowStart(windowStart)

	now := windowStart.Add(time.Hour)
	expected := utils.TimeToEpoch(windowStart)

	if got := svc.startEpoch(now); got != expected {
		t.Fatalf("default start epoch mismatch: got %d want %d", got, expected)
	}
}

func TestNetworkRewardHistoryScannerError(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.RewardsHistoryFile = filepath.Join(t.TempDir(), "history.jsonl")
	svc := NewService(cfg)

	oversizedLine := strings.Repeat("x", bufio.MaxScanTokenSize+16)
	if err := os.WriteFile(cfg.RewardsHistoryFile, []byte(oversizedLine+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write history file: %v", err)
	}

	if _, err := svc.NetworkRewardHistory(); err == nil {
		t.Fatalf("expected scanner error for oversized history line")
	}
}
