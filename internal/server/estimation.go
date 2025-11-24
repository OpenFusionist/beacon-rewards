package server

import (
	"endurance-rewards/internal/dora"
	"endurance-rewards/internal/utils"
	"log/slog"
)

const (
	estimateWindowDays = 31
	secondsPerDay      = 24 * 60 * 60
	secondsPerYear     = 365 * secondsPerDay
	// Default fallback for validators missing effective balance data.
	defaultEffectiveBalanceGwei int64 = 32_000_000_000
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
				slog.Info("using deposit balance for validator", "validator_index", idx, "deposit_balance", dep)
				balance = dep
			} else {
				balance = defaultEffectiveBalanceGwei
			}
		}

		activeSeconds := activeSecondsInWindow(lifecycle, currentEpoch, epochsInWindow)
		if activeSeconds == 0 {
			continue
		}

		estimated += float64(balance) * apr * (activeSeconds / float64(secondsPerYear))
	}

	return estimated
}
