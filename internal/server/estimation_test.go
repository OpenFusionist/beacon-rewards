package server

import (
	"math"
	"testing"

	"beacon-rewards/internal/dora"
	"beacon-rewards/internal/utils"
)

func TestActiveSecondsInWindow(t *testing.T) {
	seconds := activeSecondsInWindow(dora.ValidatorLifecycle{
		ActivationEpoch: 10,
		ExitEpoch:       200,
	}, 100, 50)

	expected := float64(50 * utils.SECONDS_PER_EPOCH)
	if seconds != expected {
		t.Fatalf("unexpected active seconds: got %f want %f", seconds, expected)
	}

	seconds = activeSecondsInWindow(dora.ValidatorLifecycle{
		ActivationEpoch: 10,
		ExitEpoch:       40,
	}, 100, 50)

	if seconds != 0 {
		t.Fatalf("expected zero active seconds when validator exited before window, got %f", seconds)
	}
}

func TestEstimateRecentRewardsForValidators(t *testing.T) {
	validatorIndices := []uint64{1, 2}
	lifecycles := map[uint64]dora.ValidatorLifecycle{
		1: {ActivationEpoch: 0, ExitEpoch: 200},
		2: {ActivationEpoch: 10, ExitEpoch: 40},
	}
	effectiveBalances := map[uint64]int64{
		1: 32_000_000_000,
		2: 32_000_000_000,
	}

	aprPercent := 10.0
	currentEpoch := uint64(100)
	epochsInWindow := uint64(50)

	estimated := estimateRecentRewardsForValidators(
		validatorIndices,
		aprPercent,
		currentEpoch,
		epochsInWindow,
		effectiveBalances,
		map[uint64]int64{},
		lifecycles,
	)

	activeSeconds := float64(50 * utils.SECONDS_PER_EPOCH) // validator 1 is active for the full 50-epoch window
	expected := float64(effectiveBalances[1]) * (aprPercent / 100.0) * (activeSeconds / float64(secondsPerYear))

	if math.Abs(estimated-expected) > expected*1e-9 {
		t.Fatalf("unexpected estimated rewards: got %f want %f", estimated, expected)
	}
}

func TestEstimateRecentRewardsForValidatorsUsesDefaultBalance(t *testing.T) {
	validatorIndices := []uint64{3}
	lifecycles := map[uint64]dora.ValidatorLifecycle{
		3: {ActivationEpoch: 50, ExitEpoch: 150},
	}
	effectiveBalances := map[uint64]int64{
		3: 0, // simulate missing or zeroed balance (e.g., exited validator)
	}

	aprPercent := 10.0
	currentEpoch := uint64(100)
	epochsInWindow := uint64(50)

	estimated := estimateRecentRewardsForValidators(
		validatorIndices,
		aprPercent,
		currentEpoch,
		epochsInWindow,
		effectiveBalances,
		map[uint64]int64{},
		lifecycles,
	)

	activeSeconds := float64(50 * utils.SECONDS_PER_EPOCH)
	expected := float64(defaultEffectiveBalanceGwei) * (aprPercent / 100.0) * (activeSeconds / float64(secondsPerYear))

	if math.Abs(estimated-expected) > expected*1e-9 {
		t.Fatalf("unexpected estimated rewards with default balance: got %f want %f", estimated, expected)
	}
}

func TestEstimateRecentRewardsForValidatorsUsesDepositFallback(t *testing.T) {
	validatorIndices := []uint64{4}
	lifecycles := map[uint64]dora.ValidatorLifecycle{
		4: {ActivationEpoch: 50, ExitEpoch: 150},
	}
	effectiveBalances := map[uint64]int64{
		4: 0,
	}
	depositBalances := map[uint64]int64{
		4: 16_000_000_000,
	}

	aprPercent := 10.0
	currentEpoch := uint64(100)
	epochsInWindow := uint64(50)

	estimated := estimateRecentRewardsForValidators(
		validatorIndices,
		aprPercent,
		currentEpoch,
		epochsInWindow,
		effectiveBalances,
		depositBalances,
		lifecycles,
	)

	activeSeconds := float64(50 * utils.SECONDS_PER_EPOCH)
	expected := float64(depositBalances[4]) * (aprPercent / 100.0) * (activeSeconds / float64(secondsPerYear))

	if math.Abs(estimated-expected) > expected*1e-9 {
		t.Fatalf("unexpected estimated rewards with deposit fallback: got %f want %f", estimated, expected)
	}
}
