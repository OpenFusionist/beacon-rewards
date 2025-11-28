package server

import (
	"math"
	"testing"
	"time"

	"beacon-rewards/internal/dora"
	"beacon-rewards/internal/rewards"
	"beacon-rewards/internal/utils"
)

func TestEstimateWindowEpochs(t *testing.T) {
	expected := uint64(estimateWindowDays*secondsPerDay) / uint64(utils.SECONDS_PER_EPOCH)
	if got := estimateWindowEpochs(); got != expected {
		t.Fatalf("estimateWindowEpochs = %d, want %d", got, expected)
	}
}

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

func TestRemoveOutliersIQR(t *testing.T) {
	tests := []struct {
		name     string
		values   []float64
		expected []float64
	}{
		{
			name:     "too few values returns all",
			values:   []float64{10.0, 11.0, 12.0},
			expected: []float64{10.0, 11.0, 12.0},
		},
		{
			name:     "no outliers",
			values:   []float64{10.0, 10.5, 11.0, 11.5, 12.0},
			expected: []float64{10.0, 10.5, 11.0, 11.5, 12.0},
		},
		{
			name:     "removes high outlier",
			values:   []float64{10.0, 10.5, 11.0, 11.5, 100.0},
			expected: []float64{10.0, 10.5, 11.0, 11.5},
		},
		{
			name:     "removes low outlier",
			values:   []float64{0.5, 10.0, 10.5, 11.0, 11.5},
			expected: []float64{10.0, 10.5, 11.0, 11.5},
		},
		{
			name:     "removes both outliers",
			values:   []float64{0.5, 10.0, 10.5, 11.0, 11.5, 100.0},
			expected: []float64{10.0, 10.5, 11.0, 11.5},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := removeOutliersIQR(tc.values)
			if len(result) != len(tc.expected) {
				t.Fatalf("unexpected length: got %d want %d", len(result), len(tc.expected))
			}
			// Check that expected values are present (order may differ due to filtering)
			for _, exp := range tc.expected {
				found := false
				for _, got := range result {
					if math.Abs(got-exp) < 1e-9 {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected value %f not found in result %v", exp, result)
				}
			}
		})
	}
}

func TestCalculate31DayAverageAPR(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name            string
		history         []rewards.NetworkRewardSnapshot
		currentSnapshot *rewards.NetworkRewardSnapshot
		expectedMin     float64
		expectedMax     float64
	}{
		{
			name:            "empty history with current snapshot",
			history:         nil,
			currentSnapshot: &rewards.NetworkRewardSnapshot{ProjectAprPercent: 10.0},
			expectedMin:     10.0,
			expectedMax:     10.0,
		},
		{
			name: "single history entry with current snapshot",
			history: []rewards.NetworkRewardSnapshot{
				{WindowStart: now.Add(-24 * time.Hour), ProjectAprPercent: 10.0},
			},
			currentSnapshot: &rewards.NetworkRewardSnapshot{ProjectAprPercent: 11.0},
			expectedMin:     10.5,
			expectedMax:     10.5,
		},
		{
			name: "multiple history entries averages correctly",
			history: []rewards.NetworkRewardSnapshot{
				{WindowStart: now.Add(-48 * time.Hour), ProjectAprPercent: 10.0},
				{WindowStart: now.Add(-24 * time.Hour), ProjectAprPercent: 11.0},
			},
			currentSnapshot: &rewards.NetworkRewardSnapshot{ProjectAprPercent: 12.0},
			expectedMin:     11.0,
			expectedMax:     11.0,
		},
		{
			name: "outlier is removed",
			history: []rewards.NetworkRewardSnapshot{
				{WindowStart: now.Add(-96 * time.Hour), ProjectAprPercent: 10.0},
				{WindowStart: now.Add(-72 * time.Hour), ProjectAprPercent: 10.5},
				{WindowStart: now.Add(-48 * time.Hour), ProjectAprPercent: 11.0},
				{WindowStart: now.Add(-24 * time.Hour), ProjectAprPercent: 11.5},
			},
			currentSnapshot: &rewards.NetworkRewardSnapshot{ProjectAprPercent: 100.0}, // outlier
			expectedMin:     10.5,                                                     // average of 10.0, 10.5, 11.0, 11.5 = 10.75
			expectedMax:     11.0,
		},
		{
			name:            "nil current snapshot uses history only",
			history:         []rewards.NetworkRewardSnapshot{{ProjectAprPercent: 10.0}},
			currentSnapshot: nil,
			expectedMin:     10.0,
			expectedMax:     10.0,
		},
		{
			name:            "zero APR values are skipped",
			history:         []rewards.NetworkRewardSnapshot{{ProjectAprPercent: 0.0}},
			currentSnapshot: &rewards.NetworkRewardSnapshot{ProjectAprPercent: 10.0},
			expectedMin:     10.0,
			expectedMax:     10.0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := calculate31DayAverageAPR(tc.history, tc.currentSnapshot)
			if result < tc.expectedMin || result > tc.expectedMax {
				t.Fatalf("unexpected average APR: got %f, want between %f and %f", result, tc.expectedMin, tc.expectedMax)
			}
		})
	}
}

func TestCalculate31DayAverageAPRLimitsTo31Days(t *testing.T) {
	now := time.Now()

	// Create 40 days of history
	history := make([]rewards.NetworkRewardSnapshot, 40)
	for i := 0; i < 40; i++ {
		// Older entries have lower APR
		history[i] = rewards.NetworkRewardSnapshot{
			WindowStart:       now.Add(time.Duration(-40+i) * 24 * time.Hour),
			ProjectAprPercent: float64(i + 1), // 1.0 to 40.0
		}
	}

	// Only the last 31 entries (10-40) should be used
	// Average of 10, 11, 12, ..., 40 = 25
	result := calculate31DayAverageAPR(history, nil)

	// The function uses the last maxHistoryDays (31) entries
	// So it uses entries 9-39 (indices), which have APR 10.0 to 40.0
	// Average = (10+11+...+40)/31 = 775/31 = 25
	expectedAvg := 25.0

	if math.Abs(result-expectedAvg) > 0.1 {
		t.Fatalf("expected average close to %f, got %f", expectedAvg, result)
	}
}
