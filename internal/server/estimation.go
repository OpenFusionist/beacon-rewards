package server

import (
	"beacon-rewards/internal/dora"
	"beacon-rewards/internal/rewards"
	"beacon-rewards/internal/utils"
	"log/slog"
	"sort"
)

const (
	estimateWindowDays = 31
	secondsPerDay      = 24 * 60 * 60
	secondsPerYear     = 365 * secondsPerDay
	// Default fallback for validators missing effective balance data.
	defaultEffectiveBalanceGwei int64 = 32_000_000_000
	// maxHistoryDays is the maximum number of days to consider for average APR calculation.
	maxHistoryDays = 31
)

// 6975 epochs = 31 days
func estimateWindowEpochs() uint64 {
	return uint64(estimateWindowDays*secondsPerDay) / uint64(utils.SECONDS_PER_EPOCH)
}

func activeSecondsInWindow(lifecycle dora.ValidatorLifecycle, currentEpoch, epochsInWindow uint64) float64 {
	windowStart := uint64(0)
	if currentEpoch > epochsInWindow {
		windowStart = currentEpoch - epochsInWindow
	}

	start := max(lifecycle.ActivationEpoch, windowStart)

	end := min(lifecycle.ExitEpoch, currentEpoch)

	if end <= start {
		return 0
	}

	activeEpochs := end - start
	return float64(activeEpochs) * float64(utils.SECONDS_PER_EPOCH)
}

func estimateRecentRewardsForValidators(
	validatorIndices []uint64,
	aprPercent float64,
	currentEpoch uint64,
	epochsInWindow uint64,
	effectiveBalances map[uint64]int64,
	depositBalances map[uint64]int64,
	lifecycles map[uint64]dora.ValidatorLifecycle,
) float64 {
	if aprPercent <= 0 || len(validatorIndices) == 0 || epochsInWindow == 0 {
		return 0
	}

	apr := aprPercent / 100.0
	var estimated float64
	for _, idx := range validatorIndices {
		lifecycle, ok := lifecycles[idx]
		if !ok {
			continue
		}

		balance := effectiveBalances[idx]
		if balance <= 0 {
			if dep, ok := depositBalances[idx]; ok && dep > 0 {
				slog.Debug("using deposit balance for validator", "validator_index", idx, "deposit_balance", dep)
				balance = dep
			} else {
				balance = defaultEffectiveBalanceGwei
			}
		}

		activeSeconds := activeSecondsInWindow(lifecycle, currentEpoch, epochsInWindow)
		if activeSeconds == 0 {
			slog.Debug("validator is not active in the window", "validator_index", idx)
			continue
		}

		estimated += float64(balance) * apr * (activeSeconds / float64(secondsPerYear))
	}

	return estimated
}

// calculate31DayAverageAPR computes the average APR from historical snapshots
// with outlier removal using the IQR (Interquartile Range) method.
// It considers up to the last 31 days of history plus the current snapshot.
func calculate31DayAverageAPR(history []rewards.NetworkRewardSnapshot, currentSnapshot *rewards.NetworkRewardSnapshot) float64 {
	// Collect APR values from history (up to maxHistoryDays)
	aprValues := make([]float64, 0, maxHistoryDays+1)

	// Add historical values (most recent first, limited to maxHistoryDays)
	startIdx := 0
	if len(history) > maxHistoryDays {
		startIdx = len(history) - maxHistoryDays
	}
	for i := startIdx; i < len(history); i++ {
		if history[i].ProjectAprPercent > 0 {
			aprValues = append(aprValues, history[i].ProjectAprPercent)
		}
	}

	// Add current snapshot if valid
	if currentSnapshot != nil && currentSnapshot.ProjectAprPercent > 0 {
		aprValues = append(aprValues, currentSnapshot.ProjectAprPercent)
	}

	if len(aprValues) == 0 {
		slog.Warn("No valid APR values found for averaging")
		return 0
	}

	// If we only have one value, return it directly
	if len(aprValues) == 1 {
		return aprValues[0]
	}

	// Remove outliers using IQR method and calculate average
	filtered := removeOutliersIQR(aprValues)
	if len(filtered) == 0 {
		// Fallback to original values if all were filtered
		filtered = aprValues
	}

	var sum float64
	for _, v := range filtered {
		sum += v
	}
	avg := sum / float64(len(filtered))

	slog.Debug("Calculated 31-day average APR",
		"total_values", len(aprValues),
		"after_outlier_removal", len(filtered),
		"average_apr", avg)

	return avg
}

// removeOutliersIQR removes outliers using the Interquartile Range (IQR) method.
// Values outside [Q1 - 1.5*IQR, Q3 + 1.5*IQR] are considered outliers.
func removeOutliersIQR(values []float64) []float64 {
	if len(values) < 4 {
		// Not enough data points for IQR, return all values
		return values
	}

	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	n := len(sorted)
	q1Idx := n / 4
	q3Idx := (3 * n) / 4

	q1 := sorted[q1Idx]
	q3 := sorted[q3Idx]
	iqr := q3 - q1

	lowerBound := q1 - 1.5*iqr
	upperBound := q3 + 1.5*iqr

	// filter values within bounds
	filtered := make([]float64, 0, len(values))
	for _, v := range values {
		if v >= lowerBound && v <= upperBound {
			filtered = append(filtered, v)
		} else {
			slog.Info("Removed APR outlier", "value", v, "lower_bound", lowerBound, "upper_bound", upperBound)
		}
	}

	return filtered
}
