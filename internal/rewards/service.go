package rewards

import (
	"context"
	"endurance-rewards/internal/config"
	"fmt"
	"log/slog"
	"math/big"
	"sync"
	"time"

	"github.com/gobitfly/eth-rewards/beacon"
	"github.com/gobitfly/eth-rewards/elrewards"
	"github.com/gobitfly/eth-rewards/types"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

const (
	SECONDS_PER_SLOT  = 12
	SLOTS_PER_EPOCH   = 32
	SECONDS_PER_EPOCH = SECONDS_PER_SLOT * SLOTS_PER_EPOCH
	// 2024-03-04 06:00:00 +0000 UTC
	GENESIS_TIMESTAMP = 1709532000
)

var gweiScalar = big.NewInt(1_000_000_000)

// Service manages validator reward statistics
type Service struct {
	config   *config.Config
	beaconCL *beacon.Client
	elClient *string
	cache    map[uint64]*types.ValidatorEpochIncome
	cacheMux sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewService creates a new rewards service
func NewService(cfg *config.Config) *Service {
	ctx, cancel := context.WithCancel(context.Background())

	beaconClient := beacon.NewClient(cfg.BeaconNodeURL, time.Minute*5)

	return &Service{
		config:   cfg,
		beaconCL: beaconClient,
		elClient: &cfg.ExecutionNodeURL,
		cache:    make(map[uint64]*types.ValidatorEpochIncome),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start begins the reward tracking service
func (s *Service) Start() error {
	slog.Info("Starting rewards service")

	// Trigger backfill to current UTC+8 00:00 epoch (non-blocking)
	go s.backfillToUTCMidnight()

	midnightEpoch := s.currentMidnightEpoch()
	startFrom := s.determineLiveStartEpoch(midnightEpoch)

	// Start the epoch processor
	go s.epochProcessor(startFrom)

	// Start the cache reset timer
	go s.cacheResetTimer()

	return nil
}

// Stop gracefully stops the service
func (s *Service) Stop() {
	slog.Info("Stopping rewards service")
	s.cancel()
}

// epochProcessor continuously processes epochs and updates the cache
func (s *Service) epochProcessor(startFrom uint64) {
	ticker := time.NewTicker(s.config.EpochUpdateInterval)
	defer ticker.Stop()

	currentEpoch := startFrom
	slog.Info("Live epoch processor starting", "start_epoch", currentEpoch, "interval", s.config.EpochUpdateInterval)

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			// Only process up to the latest completed epoch (N-1). If current epoch is not yet completed, wait.
			latest := s.getCurrentEpoch(time.Now())
			if latest == 0 || currentEpoch > (latest-1) {
				slog.Info("No completed epoch to process yet", "current_epoch", currentEpoch, "latest_now", latest)
				continue
			}

			// Retry processing the epoch on transient failures; only advance when successful.
			if ok := s.processEpochWithRetry(currentEpoch); ok {
				currentEpoch++
			}
		}
	}
}

// backfillToUTCMidnight backfills epochs using a worker pool.
// Default behavior (StartEpoch == 0): backfill from today's UTC+8 midnight epoch up to the current latest epoch.
func (s *Service) backfillToUTCMidnight() {
	startTime := time.Now()
	midnightEpoch := s.currentMidnightEpoch()
	from, to, ok := s.backfillRange(midnightEpoch)
	if !ok {
		slog.Info("Backfill skipped", "start_epoch", s.config.StartEpoch, "midnight_epoch", midnightEpoch)
		return
	}

	workerCount := s.workerCount()

	total := to - from + 1
	slog.Info("Starting backfill", "from_epoch", from, "to_epoch", to, "count", total, "concurrency", workerCount)

	jobs := make(chan uint64, workerCount*16)
	var wg sync.WaitGroup

	// Workers
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-s.ctx.Done():
					return
				case epoch, ok := <-jobs:
					if !ok {
						return
					}
					_ = s.processEpochWithRetry(epoch)
				}
			}
		}()
	}

	// Producer
produce:
	for epoch := from; epoch <= to; epoch++ {
		select {
		case <-s.ctx.Done():
			break produce
		case jobs <- epoch:
		}
	}
	close(jobs)
	wg.Wait()

	if s.ctx.Err() != nil {
		slog.Info("Backfill cancelled", "from_epoch", from, "to_epoch", to, "duration", time.Since(startTime))
		return
	}

	slog.Info("Backfill completed", "from_epoch", from, "to_epoch", to, "duration", time.Since(startTime))
}

// processEpoch fetches rewards for a specific epoch and updates the cache
func (s *Service) processEpoch(epoch uint64) error {
	startTime := time.Now()
	slog.Debug("Processing epoch", "epoch", epoch)

	// Get rewards for the epoch
	rewardsStart := time.Now()
	rewards, err := GetRewardsForEpoch(epoch, s.beaconCL, *s.elClient)
	rewardsDuration := time.Since(rewardsStart)
	if err != nil {
		slog.Error("Failed to get rewards for epoch", "epoch", epoch, "error", err, "get_rewards_duration", rewardsDuration)
		return err
	}

	// Update cache with rewards for all validators (using accumulation)
	accumulateStart := time.Now()
	s.cacheMux.Lock()

	for validatorIndex, income := range rewards {
		s.accumulateRewards(validatorIndex, income)
	}
	s.cacheMux.Unlock()
	accumulateDuration := time.Since(accumulateStart)

	totalDuration := time.Since(startTime)
	slog.Info("Updated cache with all validators", "epoch", epoch, "validators", len(rewards), "get_rewards_duration", rewardsDuration, "accumulate_duration", accumulateDuration, "total_duration", totalDuration)
	return nil
}

// accumulateRewards adds the new rewards to existing cached rewards
// Note: This method assumes the caller holds the cacheMux lock
func (s *Service) accumulateRewards(validatorIndex uint64, income *types.ValidatorEpochIncome) {
	if income == nil {
		return
	}

	existing, exists := s.cache[validatorIndex]
	if !exists {
		s.cache[validatorIndex] = income
		return
	}

	// Accumulate rewards by adding to existing values
	existing.AttestationSourceReward += income.AttestationSourceReward
	existing.AttestationSourcePenalty += income.AttestationSourcePenalty
	existing.AttestationTargetReward += income.AttestationTargetReward
	existing.AttestationTargetPenalty += income.AttestationTargetPenalty
	existing.AttestationHeadReward += income.AttestationHeadReward
	existing.FinalityDelayPenalty += income.FinalityDelayPenalty
	existing.ProposerSlashingInclusionReward += income.ProposerSlashingInclusionReward
	existing.ProposerAttestationInclusionReward += income.ProposerAttestationInclusionReward
	existing.ProposerSyncInclusionReward += income.ProposerSyncInclusionReward
	existing.SyncCommitteeReward += income.SyncCommitteeReward
	existing.SyncCommitteePenalty += income.SyncCommitteePenalty
	existing.SlashingReward += income.SlashingReward
	existing.SlashingPenalty += income.SlashingPenalty
	existing.ProposalsMissed += income.ProposalsMissed

	existing.TxFeeRewardWei = addWei(existing.TxFeeRewardWei, income.TxFeeRewardWei)
}

// cacheResetTimer periodically clears the cache
func (s *Service) cacheResetTimer() {
	ticker := time.NewTicker(s.config.CacheResetInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			cleared := s.resetCache()
			slog.Info("Cache reset completed", "cleared", cleared)
		}
	}
}

// GetRewards returns cached rewards for the specified validator indices
func (s *Service) GetRewards(validatorIndices []uint64) map[uint64]*types.ValidatorEpochIncome {
	s.cacheMux.RLock()
	defer s.cacheMux.RUnlock()

	result := make(map[uint64]*types.ValidatorEpochIncome)

	for _, index := range validatorIndices {
		if income, exists := s.cache[index]; exists {
			result[index] = income
		}
	}

	return result
}

// GetAllRewards returns all cached rewards
func (s *Service) GetAllRewards() map[uint64]*types.ValidatorEpochIncome {
	s.cacheMux.RLock()
	defer s.cacheMux.RUnlock()

	result := make(map[uint64]*types.ValidatorEpochIncome)
	for k, v := range s.cache {
		result[k] = v
	}

	return result
}

// ValidatorReward represents the total reward (EL + CL) for a single validator.
type ValidatorReward struct {
	ValidatorIndex       uint64  `json:"validator_index"`
	ClRewardsGwei        int64   `json:"cl_rewards_gwei"`
	ElRewardsGwei        int64   `json:"el_rewards_gwei"`
	TotalRewardsGwei     int64   `json:"total_rewards_gwei"`
	EffectiveBalanceGwei int64   `json:"effective_balance_gwei"`
	APY1D                float64 `json:"1d_apy"`
}

// GetTotalRewards returns the sum of EL+CL rewards for each validator and derives simple APY estimates.
func (s *Service) GetTotalRewards(validatorIndices []uint64, effectiveBalances map[uint64]int64) map[uint64]*ValidatorReward {
	s.cacheMux.RLock()
	defer s.cacheMux.RUnlock()

	result := make(map[uint64]*ValidatorReward, len(validatorIndices))

	for _, index := range validatorIndices {
		income, exists := s.cache[index]
		if !exists {
			continue
		}

		reward := &ValidatorReward{ValidatorIndex: index}
		reward.ClRewardsGwei = income.TotalClRewards()

		elRewardsWei := weiBytesToBigInt(income.TxFeeRewardWei)
		reward.ElRewardsGwei = new(big.Int).Div(new(big.Int).Set(elRewardsWei), gweiScalar).Int64()

		clRewardsWei := new(big.Int).Mul(big.NewInt(reward.ClRewardsGwei), gweiScalar)
		totalRewardsWei := new(big.Int).Add(clRewardsWei, elRewardsWei)
		reward.TotalRewardsGwei = new(big.Int).Div(totalRewardsWei, gweiScalar).Int64()
		reward.EffectiveBalanceGwei = effectiveBalances[index]
		// calculate duration since current UTC+8 midnight
		utcPlusEight := time.FixedZone("UTC+8", 8*60*60)
		now := time.Now().In(utcPlusEight)
		midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, utcPlusEight)
		duration := now.Sub(midnight)
		durationSeconds := duration.Seconds()
		reward.APY1D = float64(reward.TotalRewardsGwei) / float64(reward.EffectiveBalanceGwei) * durationSeconds / s.config.CacheResetInterval.Seconds() * 100.0

		result[index] = reward
	}

	return result
}

func (s *Service) currentMidnightEpoch() uint64 {
	loc := time.FixedZone("UTC+8", 8*60*60)
	now := time.Now().In(loc)
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	return s.getCurrentEpoch(midnight)
}

func (s *Service) determineLiveStartEpoch(midnightEpoch uint64) uint64 {
	switch {
	case s.config.StartEpoch == 0:
		// Start live processing from the next epoch after the latest completed epoch
		latest := s.getCurrentEpoch(time.Now())
		if latest == 0 {
			return 0
		}
		return (latest - 1) + 1 // i.e., latest
	case s.config.StartEpoch <= midnightEpoch:
		return midnightEpoch + 1
	default:
		return s.config.StartEpoch + 1
	}
}

func (s *Service) backfillRange(midnightEpoch uint64) (uint64, uint64, bool) {
	start := s.config.StartEpoch
	if start == 0 {
		// Default: backfill from midnight to the current latest epoch
		latest := s.getCurrentEpoch(time.Now())
		// Only backfill up to the latest completed epoch (N-1)
		if latest == 0 {
			return 0, 0, false
		}
		latest = latest - 1
		if midnightEpoch > latest {
			return 0, 0, false
		}
		return midnightEpoch, latest, true
	}

	if start <= midnightEpoch {
		return start, midnightEpoch, start <= midnightEpoch
	}

	from := midnightEpoch + 1
	if from > start {
		return 0, 0, false
	}

	return from, start, true
}

func (s *Service) workerCount() int {
	if s.config.BackfillConcurrency <= 0 {
		return 1
	}
	return s.config.BackfillConcurrency
}

func (s *Service) resetCache() int {
	s.cacheMux.Lock()
	defer s.cacheMux.Unlock()

	size := len(s.cache)
	s.cache = make(map[uint64]*types.ValidatorEpochIncome)
	return size
}

// getCurrentEpoch fetches the current epoch from the beacon chain
func (s *Service) getCurrentEpoch(ts time.Time) uint64 {
	if int64(GENESIS_TIMESTAMP) > ts.Unix() {
		return 0
	}
	return uint64((ts.Unix() - int64(GENESIS_TIMESTAMP)) / int64(SECONDS_PER_SLOT) / int64(SLOTS_PER_EPOCH))

}

func addWei(base, delta []byte) []byte {
	if len(delta) == 0 {
		return base
	}

	baseInt := new(big.Int).SetBytes(base)
	deltaInt := new(big.Int).SetBytes(delta)
	baseInt.Add(baseInt, deltaInt)
	return baseInt.Bytes()
}

func weiBytesToBigInt(data []byte) *big.Int {
	if len(data) == 0 {
		return big.NewInt(0)
	}
	return new(big.Int).SetBytes(data)
}

// processEpochWithRetry wraps processEpoch with a simple bounded backoff retry.
func (s *Service) processEpochWithRetry(epoch uint64) bool {
	maxRetries := s.config.EpochProcessMaxRetries
	if maxRetries <= 0 {
		maxRetries = 5
	}
	baseBackoff := s.config.EpochProcessBaseBackoff
	if baseBackoff <= 0 {
		baseBackoff = 2 * time.Second
	}
	maxBackoff := s.config.EpochProcessMaxBackoff
	if maxBackoff <= 0 {
		maxBackoff = 30 * time.Second
	}

	backoff := baseBackoff
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if s.ctx.Err() != nil {
			return false
		}
		err := s.processEpoch(epoch)
		if err == nil {
			return true
		}
		slog.Warn("Retrying epoch processing", "epoch", epoch, "attempt", attempt, "max_attempts", maxRetries, "backoff", backoff, "error", err)
		select {
		case <-s.ctx.Done():
			return false
		case <-time.After(backoff):
		}
		// Exponential backoff with cap
		backoff = backoff * 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
	slog.Error("Exhausted retries for epoch", "epoch", epoch, "attempts", maxRetries)
	return false
}

// GetRewardsForEpoch fetches rewards for a specific epoch from the beacon chain and execution layer
func GetRewardsForEpoch(epoch uint64, client *beacon.Client, elEndpoint string) (map[uint64]*types.ValidatorEpochIncome, error) {
	proposerAssignments, err := client.ProposerAssignments(epoch)
	if err != nil {
		return nil, err
	}

	slotsPerEpoch := uint64(len(proposerAssignments.Data))

	startSlot := epoch * slotsPerEpoch
	endSlot := startSlot + slotsPerEpoch - 1

	g := new(errgroup.Group)
	// g.SetLimit(32)

	slotsToProposerIndex := make(map[uint64]uint64)
	for _, pa := range proposerAssignments.Data {
		slotsToProposerIndex[uint64(pa.Slot)] = uint64(pa.ValidatorIndex)
	}

	rewardsMux := &sync.Mutex{}

	rewards := make(map[uint64]*types.ValidatorEpochIncome)

	for i := startSlot; i <= endSlot; i++ {
		i := i

		g.Go(func() error {
			proposer, found := slotsToProposerIndex[i]
			if !found {
				return fmt.Errorf("assigned proposer for slot %v not found", i)
			}

			// Run per-slot RPCs in parallel:
			// 1) Execution block number (+ ELRewardForBlock if exists)
			// 2) SyncCommitteeRewards
			// 3) BlockRewards
			slotGroup := new(errgroup.Group)

			// 1) Execution Layer path
			slotGroup.Go(func() error {
				execStart := time.Now()
				execBlockNumber, err := client.ExecutionBlockNumber(i)
				slog.Debug("Fetched ExecutionBlockNumber", "slot", i, "duration", time.Since(execStart))

				// Ensure proposer entry exists (needed for missed proposal increment below)
				rewardsMux.Lock()
				if rewards[proposer] == nil {
					rewards[proposer] = &types.ValidatorEpochIncome{}
				}
				rewardsMux.Unlock()

				if err != nil {
					if err == types.ErrBlockNotFound {
						rewardsMux.Lock()
						rewards[proposer].ProposalsMissed += 1
						rewardsMux.Unlock()
						return nil
					} else if err != types.ErrSlotPreMerge { // ignore pre-merge
						logrus.Errorf("error retrieving execution block number for slot %v: %v", i, err)
						return err
					}
					return nil
				}

				txFeeIncomeStart := time.Now()
				txFeeIncome, err := elrewards.GetELRewardForBlock(execBlockNumber, elEndpoint)
				slog.Debug("Fetched ELRewardForBlock", "slot", i, "duration", time.Since(txFeeIncomeStart))
				if err != nil {
					return err
				}

				rewardsMux.Lock()
				// proposer entry already ensured above
				rewards[proposer].TxFeeRewardWei = txFeeIncome.Bytes()
				rewardsMux.Unlock()
				return nil
			})

			// 2) SyncCommitteeRewards
			slotGroup.Go(func() error {
				syncRewardsStart := time.Now()
				syncRewards, err := client.SyncCommitteeRewards(i)
				slog.Debug("Fetched SyncCommitteeRewards", "slot", i, "duration", time.Since(syncRewardsStart))
				if err != nil {
					if err != types.ErrSlotPreSyncCommittees {
						return err
					}
					return nil
				}

				if syncRewards != nil {
					rewardsMux.Lock()
					for _, sr := range syncRewards.Data {
						if rewards[sr.ValidatorIndex] == nil {
							rewards[sr.ValidatorIndex] = &types.ValidatorEpochIncome{}
						}

						if sr.Reward > 0 {
							rewards[sr.ValidatorIndex].SyncCommitteeReward += uint64(sr.Reward)
						} else {
							rewards[sr.ValidatorIndex].SyncCommitteePenalty += uint64(sr.Reward * -1)
						}
					}
					rewardsMux.Unlock()
				}
				return nil
			})

			// 3) BlockRewards
			slotGroup.Go(func() error {
				blockRewardsStart := time.Now()
				blockRewards, err := client.BlockRewards(i)
				slog.Debug("Fetched BlockRewards", "slot", i, "duration", time.Since(blockRewardsStart))
				if err != nil {
					return err
				}

				rewardsMux.Lock()
				if rewards[blockRewards.Data.ProposerIndex] == nil {
					rewards[blockRewards.Data.ProposerIndex] = &types.ValidatorEpochIncome{}
				}
				rewards[blockRewards.Data.ProposerIndex].ProposerAttestationInclusionReward += blockRewards.Data.Attestations
				rewards[blockRewards.Data.ProposerIndex].ProposerSlashingInclusionReward += blockRewards.Data.AttesterSlashings + blockRewards.Data.ProposerSlashings
				rewards[blockRewards.Data.ProposerIndex].ProposerSyncInclusionReward += blockRewards.Data.SyncAggregate
				rewardsMux.Unlock()
				return nil
			})

			// Wait for all per-slot calls
			return slotGroup.Wait()
		})
	}

	g.Go(func() error {
		attestationRewardsStart := time.Now()
		ar, err := client.AttestationRewards(epoch)
		slog.Debug("Fetched AttestationRewards", "epoch", epoch, "duration", time.Since(attestationRewardsStart))
		if err != nil {
			return err
		}
		rewardsMux.Lock()
		defer rewardsMux.Unlock()
		for _, ar := range ar.Data.TotalRewards {
			if rewards[ar.ValidatorIndex] == nil {
				rewards[ar.ValidatorIndex] = &types.ValidatorEpochIncome{}
			}

			if ar.Head >= 0 {
				rewards[ar.ValidatorIndex].AttestationHeadReward = uint64(ar.Head)
			} else {
				return fmt.Errorf("retrieved negative attestation head reward for validator %v: %v", ar.ValidatorIndex, ar.Head)
			}

			if ar.Source > 0 {
				rewards[ar.ValidatorIndex].AttestationSourceReward = uint64(ar.Source)
			} else {
				rewards[ar.ValidatorIndex].AttestationSourcePenalty = uint64(ar.Source * -1)
			}

			if ar.Target > 0 {
				rewards[ar.ValidatorIndex].AttestationTargetReward = uint64(ar.Target)
			} else {
				rewards[ar.ValidatorIndex].AttestationTargetPenalty = uint64(ar.Target * -1)
			}

			if ar.InclusionDelay <= 0 {
				rewards[ar.ValidatorIndex].FinalityDelayPenalty = uint64(ar.InclusionDelay * -1)
			} else {
				return fmt.Errorf("retrieved positive inclusion delay penalty for validator %v: %v", ar.ValidatorIndex, ar.InclusionDelay)
			}
		}

		return nil
	})

	err = g.Wait()
	if err != nil {
		return nil, err
	}

	return rewards, nil
}
