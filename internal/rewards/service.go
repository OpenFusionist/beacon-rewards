package rewards

import (
	"context"
	"endurance-rewards/internal/config"
	"log/slog"
	"math/big"
	"sync"
	"time"

	eth_rewards "github.com/gobitfly/eth-rewards"
	"github.com/gobitfly/eth-rewards/beacon"
	"github.com/gobitfly/eth-rewards/types"
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

	// Start the epoch processor
	go s.epochProcessor()

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
func (s *Service) epochProcessor() {
	ticker := time.NewTicker(s.config.EpochUpdateInterval)
	defer ticker.Stop()

	currentEpoch := s.config.StartEpoch
	if currentEpoch == 0 {
		// Get current epoch from beacon chain
		currentEpoch = s.getCurrentEpoch(time.Now())
		slog.Info("Starting from current epoch", "epoch", currentEpoch)
	}

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

// processEpoch fetches rewards for a specific epoch and updates the cache
func (s *Service) processEpoch(epoch uint64) {
	slog.Debug("Processing epoch", "epoch", epoch)

	// Get rewards for the epoch
	rewards, err := eth_rewards.GetRewardsForEpoch(epoch, s.beaconCL, *s.elClient)
	if err != nil {
		slog.Error("Failed to get rewards for epoch", "epoch", epoch, "error", err)
		return
	}

	// Update cache with rewards for all validators (using accumulation)
	s.cacheMux.Lock()
	defer s.cacheMux.Unlock()

	for validatorIndex, income := range rewards {
		s.accumulateRewards(validatorIndex, income)
	}
	slog.Info("Updated cache with all validators", "epoch", epoch, "validators", len(rewards))
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
	ClRewards        int64  `json:"cl_rewards"`         // CL rewards in gwei
	ElRewards        string `json:"el_rewards"`         // EL rewards in wei as string
	ElRewardsGwei    int64  `json:"el_rewards_gwei"`    // EL rewards in gwei
	TotalRewards     string `json:"total_rewards"`      // Total (CL + EL) in wei as string
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
			reward.ClRewards = clRewardsGwei

			// Get EL rewards (in wei)
			elRewardsWei := big.NewInt(0)
			if len(income.TxFeeRewardWei) > 0 {
				elRewardsWei.SetBytes(income.TxFeeRewardWei)
			}
			reward.ElRewards = elRewardsWei.String()
			reward.ElRewardsGwei = new(big.Int).Div(elRewardsWei, gweiDivisor).Int64()

			// Calculate total rewards (CL + EL) in wei
			clRewardsWei := new(big.Int).Mul(big.NewInt(clRewardsGwei), gweiToWei)
			totalRewardsWei := new(big.Int).Add(clRewardsWei, elRewardsWei)
			reward.TotalRewards = totalRewardsWei.String()
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
