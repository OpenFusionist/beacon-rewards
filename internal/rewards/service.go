package rewards

import (
	"context"
	"endurance-rewards/internal/config"
	"log/slog"
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
	GENESIS_TIMESTAMP = 1606824000
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

	// Update cache with rewards for monitored validators
	s.cacheMux.Lock()
	defer s.cacheMux.Unlock()

	// If ValidatorIndices is empty, cache all validators
	if len(s.config.ValidatorIndices) == 0 {
		for validatorIndex, income := range rewards {
			s.cache[validatorIndex] = income
		}
		slog.Info("Updated cache with all validators", "epoch", epoch, "validators", len(rewards))
	} else {
		// Only cache monitored validators
		updated := 0
		for _, validatorIndex := range s.config.ValidatorIndices {
			if income, exists := rewards[validatorIndex]; exists {
				s.cache[validatorIndex] = income
				updated++
			}
		}
		slog.Info("Updated cache with monitored validators", "epoch", epoch, "updated", updated, "total", len(s.config.ValidatorIndices))
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

// getCurrentEpoch fetches the current epoch from the beacon chain
func (s *Service) getCurrentEpoch(ts time.Time) uint64 {
	if int64(GENESIS_TIMESTAMP) > ts.Unix() {
		return 0
	}
	return uint64((ts.Unix() - int64(GENESIS_TIMESTAMP)) / int64(SECONDS_PER_SLOT) / int64(SLOTS_PER_EPOCH))

}
