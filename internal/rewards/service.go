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

	// Trigger backfill to current UTC 00:00 epoch (non-blocking)
	go s.backfillToUTCMidnight()

	// Determine starting epoch for live processing, avoiding overlap with backfill
	now := time.Now().UTC()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	midnightEpoch := s.getCurrentEpoch(midnight)

	var startFrom uint64
	switch {
	case s.config.StartEpoch == 0:
		// Start from the current epoch
		startFrom = s.getCurrentEpoch(time.Now())
	case s.config.StartEpoch <= midnightEpoch:
		// If backfilling [StartEpoch..midnightEpoch], start live processing from midnightEpoch+1
		startFrom = midnightEpoch + 1
	default:
		// If backfilling (midnightEpoch+1..StartEpoch), start from StartEpoch+1
		startFrom = s.config.StartEpoch + 1
	}

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
			s.processEpoch(currentEpoch)
			currentEpoch++
		}
	}
}

// backfillToUTCMidnight backfills epochs from StartEpoch up to current UTC 00:00 epoch using a worker pool
func (s *Service) backfillToUTCMidnight() {
	startTime := time.Now()
	now := time.Now().UTC()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	midnightEpoch := s.getCurrentEpoch(midnight)

	start := s.config.StartEpoch
	if start == 0 {
		slog.Info("Backfill skipped", "start_epoch", start, "midnight_epoch", midnightEpoch)
		return
	}

	var from, to uint64
	if start <= midnightEpoch {
		from = start
		to = midnightEpoch
	} else {
		// typical usage: start_epoch > midnight_epoch → backfill (midnight_epoch+1 .. start_epoch)
		from = midnightEpoch + 1
		to = start
	}
	if from > to {
		slog.Info("Backfill skipped (empty range)", "from_epoch", from, "to_epoch", to)
		return
	}

	workerCount := s.config.BackfillConcurrency
	if workerCount <= 0 {
		workerCount = 1
	}

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
					s.processEpoch(epoch)
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
func (s *Service) processEpoch(epoch uint64) {
	startTime := time.Now()
	slog.Debug("Processing epoch", "epoch", epoch)

	// Get rewards for the epoch
	rewardsStart := time.Now()
	rewards, err := GetRewardsForEpoch(epoch, s.beaconCL, *s.elClient)
	rewardsDuration := time.Since(rewardsStart)
	if err != nil {
		slog.Error("Failed to get rewards for epoch", "epoch", epoch, "error", err, "get_rewards_duration", rewardsDuration)
		return
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
}

// accumulateRewards adds the new rewards to existing cached rewards
// Note: This method assumes the caller holds the cacheMux lock
func (s *Service) accumulateRewards(validatorIndex uint64, income *types.ValidatorEpochIncome) {
	existing, exists := s.cache[validatorIndex]
	if !exists {
		// First time seeing this validator, create a copy to store
		newIncome := &types.ValidatorEpochIncome{
			AttestationSourceReward:            income.AttestationSourceReward,
			AttestationSourcePenalty:           income.AttestationSourcePenalty,
			AttestationTargetReward:            income.AttestationTargetReward,
			AttestationTargetPenalty:           income.AttestationTargetPenalty,
			AttestationHeadReward:              income.AttestationHeadReward,
			FinalityDelayPenalty:               income.FinalityDelayPenalty,
			ProposerSlashingInclusionReward:    income.ProposerSlashingInclusionReward,
			ProposerAttestationInclusionReward: income.ProposerAttestationInclusionReward,
			ProposerSyncInclusionReward:        income.ProposerSyncInclusionReward,
			SyncCommitteeReward:                income.SyncCommitteeReward,
			SyncCommitteePenalty:               income.SyncCommitteePenalty,
			SlashingReward:                     income.SlashingReward,
			SlashingPenalty:                    income.SlashingPenalty,
			TxFeeRewardWei:                     income.TxFeeRewardWei,
			ProposalsMissed:                    income.ProposalsMissed,
		}
		s.cache[validatorIndex] = newIncome
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

	// Accumulate TxFeeRewardWei (EL rewards)
	if len(income.TxFeeRewardWei) > 0 {
		existingWei := new(big.Int).SetBytes(existing.TxFeeRewardWei)
		incomeWei := new(big.Int).SetBytes(income.TxFeeRewardWei)
		totalWei := new(big.Int).Add(existingWei, incomeWei)
		existing.TxFeeRewardWei = totalWei.Bytes()
	}
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
			s.cacheMux.Lock()
			oldSize := len(s.cache)
			s.cache = make(map[uint64]*types.ValidatorEpochIncome)
			s.cacheMux.Unlock()

			slog.Info("Cache reset completed", "cleared", oldSize)
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

// ValidatorReward represents the total reward (EL + CL) for a single validator
type ValidatorReward struct {
	ValidatorIndex   uint64 `json:"validator_index"`
	ClRewardsGwei    int64  `json:"cl_rewards_gwei"`    // CL rewards in gwei
	ElRewardsGwei    int64  `json:"el_rewards_gwei"`    // EL rewards in gwei
	TotalRewardsGwei int64  `json:"total_rewards_gwei"` // Total (CL + EL) in gwei
}

// GetTotalRewards returns the sum of EL+CL rewards for each validator
func (s *Service) GetTotalRewards(validatorIndices []uint64) map[uint64]*ValidatorReward {
	s.cacheMux.RLock()
	defer s.cacheMux.RUnlock()

	result := make(map[uint64]*ValidatorReward)
	gweiToWei := big.NewInt(1000000000) // 1 gwei = 10^9 wei
	gweiDivisor := big.NewInt(1000000000)

	for _, index := range validatorIndices {
		if income, exists := s.cache[index]; exists {
			reward := &ValidatorReward{
				ValidatorIndex: index,
			}

			// Calculate CL rewards (in gwei)
			clRewardsGwei := income.TotalClRewards()
			reward.ClRewardsGwei = clRewardsGwei

			// Get EL rewards (in wei)
			elRewardsWei := big.NewInt(0)
			if len(income.TxFeeRewardWei) > 0 {
				elRewardsWei.SetBytes(income.TxFeeRewardWei)
			}
			reward.ElRewardsGwei = new(big.Int).Div(elRewardsWei, gweiDivisor).Int64()

			// Calculate total rewards (CL + EL) in wei
			clRewardsWei := new(big.Int).Mul(big.NewInt(clRewardsGwei), gweiToWei)
			totalRewardsWei := new(big.Int).Add(clRewardsWei, elRewardsWei)
			reward.TotalRewardsGwei = new(big.Int).Div(totalRewardsWei, gweiDivisor).Int64()

			result[index] = reward
		}
	}

	return result
}

// getCurrentEpoch fetches the current epoch from the beacon chain
func (s *Service) getCurrentEpoch(ts time.Time) uint64 {
	if int64(GENESIS_TIMESTAMP) > ts.Unix() {
		return 0
	}
	return uint64((ts.Unix() - int64(GENESIS_TIMESTAMP)) / int64(SECONDS_PER_SLOT) / int64(SLOTS_PER_EPOCH))

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
					// Pre-merge: nothing to fetch from EL
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
