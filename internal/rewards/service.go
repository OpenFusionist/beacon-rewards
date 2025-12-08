package rewards

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"beacon-rewards/internal/config"
	"beacon-rewards/internal/dora"
	"beacon-rewards/internal/utils"

	"github.com/gobitfly/eth-rewards/elrewards"
	"github.com/gobitfly/eth-rewards/types"
	"golang.org/x/sync/errgroup"
)

const (
	defaultEffectiveBalanceGwei int64 = 32_000_000_000
)

var gweiScalar = big.NewInt(1_000_000_000)

// NetworkRewardSnapshot captures aggregated reward totals for all validators within a cache window.
type NetworkRewardSnapshot struct {
	WindowStart               time.Time `json:"window_start"`
	WindowEnd                 time.Time `json:"window_end"`
	WindowDurationSeconds     float64   `json:"window_duration_seconds"`
	ActiveValidatorCount      int       `json:"active_validator_count"`
	ClRewardsGwei             int64     `json:"cl_rewards_gwei"`
	ElRewardsGwei             int64     `json:"el_rewards_gwei"`
	TotalRewardsGwei          int64     `json:"total_rewards_gwei"`
	TotalEffectiveBalanceGwei int64     `json:"total_effective_balance_gwei"`
	ProjectAprPercent         float64   `json:"project_apr_percent"`
}

// ValidatorReward represents the total reward (EL + CL) for a single validator.
type ValidatorReward struct {
	ValidatorIndex       uint64  `json:"validator_index"`
	ClRewardsGwei        int64   `json:"cl_rewards_gwei"`
	ElRewardsGwei        int64   `json:"el_rewards_gwei"`
	TotalRewardsGwei     int64   `json:"total_rewards_gwei"`
	EffectiveBalanceGwei int64   `json:"effective_balance_gwei"`
	ProjectAPRPercent    float64 `json:"project_apr_percent"`
}

// Service manages validator reward statistics
type Service struct {
	config   *config.Config
	beaconCL *NodePool
	elClient *string
	doraDB   *dora.DB
	ctx      context.Context
	cancel   context.CancelFunc

	// Cache state
	cache            map[uint64]*types.ValidatorEpochIncome
	cacheMux         sync.RWMutex
	latestSyncEpoch  uint64
	cacheWindowStart time.Time
	cacheWindowMu    sync.RWMutex

	// History state
	historyPath string
	historyMu   sync.Mutex
}

// NewService creates a new rewards service
func NewService(cfg *config.Config) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	nodePool := NewNodePool(cfg.BeaconNodeURL, time.Minute*5)

	s := &Service{
		config:      cfg,
		beaconCL:    nodePool,
		elClient:    &cfg.ExecutionNodeURL,
		cache:       make(map[uint64]*types.ValidatorEpochIncome),
		historyPath: strings.TrimSpace(cfg.RewardsHistoryFile),
		ctx:         ctx,
		cancel:      cancel,
	}

	// Default cache window start to today 00:00 UTC+8
	loc := time.FixedZone("UTC+8", 8*60*60)
	now := time.Now().In(loc)
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	s.setCacheWindowStart(midnight)

	return s
}

// SetDoraDB attaches a Dora DB handle for effective balance lookups (optional).
func (s *Service) SetDoraDB(db *dora.DB) {
	s.doraDB = db
}

// Start begins the reward tracking service
func (s *Service) Start() error {
	slog.Info("Starting rewards service")

	startEpoch := s.startEpoch(time.Now())

	go s.syncRoutine(startEpoch)
	go s.cacheResetTimerWithClock(time.Now)

	return nil
}

func (s *Service) startEpoch(now time.Time) uint64 {
	if s.config.BackfillLookback > 0 {
		startTime := now.Add(-s.config.BackfillLookback)
		return utils.TimeToEpoch(startTime)
	}

	// Default: Start from cache window start (00:00 UTC+8)
	return utils.TimeToEpoch(s.cacheWindowStartTime())
}

// Stop gracefully stops the service
func (s *Service) Stop() {
	slog.Info("Stopping rewards service")
	s.cancel()
}

// ---------------------------------------------------------------------
// Sync Logic (Backfill + Live)
// ---------------------------------------------------------------------

func (s *Service) syncRoutine(startEpoch uint64) {
	// 1. Backfill Phase
	// Process from startEpoch up to (latest_completed - 2)
	latestEpoch := utils.TimeToEpoch(time.Now())
	if latestEpoch > 2 {
		latestEpoch -= 2
	} else {
		latestEpoch = 0
	}

	if startEpoch <= latestEpoch {
		slog.Info("Starting backfill", "from", startEpoch, "to", latestEpoch)
		s.runBackfill(startEpoch, latestEpoch)
		slog.Info("Backfill completed")
	} else {
		slog.Warn("Backfill skipped", "startEpoch", startEpoch, "latestEpoch", latestEpoch)
	}

	// 2. Live Sync Phase
	// Continues strictly from latestSyncEpoch + 1
	s.runLiveSync()
}

func (s *Service) runBackfill(from, to uint64) {
	g, ctx := errgroup.WithContext(s.ctx)
	g.SetLimit(s.config.BackfillConcurrency)

	// Create a channel to feed epochs to workers
	epochs := make(chan uint64)

	// Producer
	go func() {
		defer close(epochs)
		for e := from; e <= to; e++ {
			select {
			case epochs <- e:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Consumers
	for i := 0; i < s.config.BackfillConcurrency; i++ {
		g.Go(func() error {
			for epoch := range epochs {
				if err := s.processEpochWithRetry(epoch); err != nil {
					// In backfill, we log error but don't abort the whole group unless critical
					slog.Error("Backfill epoch failed after retries", "epoch", epoch, "error", err)
				}
			}
			return nil
		})
	}
	_ = g.Wait()
}

func (s *Service) runLiveSync() {
	ticker := time.NewTicker(s.config.EpochCheckInterval)
	defer ticker.Stop()

	slog.Info("Live sync starting")

	for {
		// Always proceed from the last successfully synced epoch
		s.cacheMux.RLock()
		nextEpoch := s.latestSyncEpoch + 1
		s.cacheMux.RUnlock()

		// Check if we can process nextEpoch (now - 2)
		chainHead := utils.TimeToEpoch(time.Now())
		safeHead := uint64(0)
		if chainHead > 2 {
			safeHead = chainHead - 2
		}
		// sync from cached nextEpoch to safeHead
		for epoch := nextEpoch; epoch <= safeHead; epoch++ {
			if err := s.processEpochWithRetry(epoch); err != nil {
				slog.Error("Live sync epoch failed after retries", "epoch", epoch, "error", err)
			}
		}

		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) processEpochWithRetry(epoch uint64) error {
	var err error
	backoff := time.Second
	maxRetries := s.config.EpochProcessMaxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}

	for i := 0; i < maxRetries; i++ {
		if s.ctx.Err() != nil {
			return s.ctx.Err()
		}
		if err = s.processEpoch(epoch); err == nil {
			return nil
		}
		slog.Warn("Epoch processing failed", "epoch", epoch, "attempt", i+1, "error", err)
		time.Sleep(backoff)
		backoff *= 2
	}
	return err
}

func (s *Service) processEpoch(epoch uint64) error {
	startTime := time.Now()
	rewards, err := s.getRewardsForEpoch(epoch)
	if err != nil {
		return err
	}

	s.cacheMux.Lock()
	for validatorIndex, income := range rewards {
		s.accumulateRewards(validatorIndex, income)
	}
	if epoch > s.latestSyncEpoch {
		s.latestSyncEpoch = epoch
	}
	s.cacheMux.Unlock()

	slog.Info("Processed epoch", "epoch", epoch, "validators", len(rewards), "duration", time.Since(startTime))
	return nil
}

// ---------------------------------------------------------------------
// Cache & History
// ---------------------------------------------------------------------

func (s *Service) accumulateRewards(validatorIndex uint64, income *types.ValidatorEpochIncome) {
	if income == nil {
		return
	}
	existing, exists := s.cache[validatorIndex]
	if !exists {
		s.cache[validatorIndex] = income
		return
	}
	// In-place accumulation to avoid copying struct
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

func (s *Service) cacheResetTimerWithClock(now func() time.Time) {
	loc := time.FixedZone("UTC+8", 8*60*60)
	for {
		current := now().In(loc)
		// Calculate next 00:00 UTC+8
		nextRun := time.Date(current.Year(), current.Month(), current.Day()+1, 0, 0, 0, 0, loc)
		duration := nextRun.Sub(current)

		slog.Info("Scheduled next cache reset", "next_run", nextRun, "wait_duration", duration)

		timer := time.NewTimer(duration)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			s.resetCacheAt(now())
		}
	}
}

func (s *Service) resetCacheAt(currentTime time.Time) {
	s.cacheMux.Lock()
	defer s.cacheMux.Unlock()

	if len(s.cache) > 0 {
		snapshot := s.computeNetworkSnapshotLocked(currentTime)
		s.persistSnapshot(snapshot)
	}

	s.cache = make(map[uint64]*types.ValidatorEpochIncome)
	// NOTE: We do NOT reset latestSyncEpoch here. It serves as the high-water mark for synchronization.
	s.setCacheWindowStart(currentTime)
	slog.Info("Cache reset")
}

// ---------------------------------------------------------------------
// Data Access (Read)
// ---------------------------------------------------------------------

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

func (s *Service) TotalNetworkRewards() *NetworkRewardSnapshot {
	s.cacheMux.RLock()
	defer s.cacheMux.RUnlock()
	return s.computeNetworkSnapshotLocked(time.Now())
}

func (s *Service) NetworkRewardHistory() ([]NetworkRewardSnapshot, error) {
	if s.historyPath == "" {
		return nil, nil
	}
	s.historyMu.Lock()
	defer s.historyMu.Unlock()

	f, err := os.Open(s.historyPath)
	if os.IsNotExist(err) {
		return []NetworkRewardSnapshot{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []NetworkRewardSnapshot
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if b := bytes.TrimSpace(scanner.Bytes()); len(b) > 0 {
			var e NetworkRewardSnapshot
			_ = json.Unmarshal(b, &e)
			entries = append(entries, e)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan rewards history: %w", err)
	}
	return entries, nil
}

func (s *Service) GetTotalRewards(validatorIndices []uint64, effectiveBalances map[uint64]int64) map[uint64]*ValidatorReward {
	s.cacheMux.RLock()
	defer s.cacheMux.RUnlock()

	start, end := s.GetRewardWindow()
	duration := end.Sub(start).Seconds()
	if duration <= 0 {
		duration = s.config.CacheResetInterval.Seconds()
	}
	// use network snapshot for project APR calculation
	snapshot := s.computeNetworkSnapshotLocked(time.Now())

	result := make(map[uint64]*ValidatorReward, len(validatorIndices))
	for _, index := range validatorIndices {

		income, exists := s.cache[index]
		if !exists {
			continue
		}

		cl := income.TotalClRewards()
		elWei := weiBytesToBigInt(income.TxFeeRewardWei)
		elGwei := new(big.Int).Div(elWei, gweiScalar).Int64()

		// Calc Total
		totalGwei := cl + elGwei
		bal := effectiveBalances[index]

		r := &ValidatorReward{
			ValidatorIndex:       index,
			ClRewardsGwei:        cl,
			ElRewardsGwei:        elGwei,
			TotalRewardsGwei:     totalGwei,
			EffectiveBalanceGwei: bal,
		}

		r.ProjectAPRPercent = snapshot.ProjectAprPercent

		result[index] = r
	}
	return result
}

func (s *Service) GetRewardWindow() (time.Time, time.Time) {

	s.cacheMux.RLock()
	latestEpoch := s.latestSyncEpoch
	s.cacheMux.RUnlock()
	start := s.cacheWindowStartTime().UTC()
	end := utils.EpochToTime(latestEpoch)
	if end.Before(start) {
		return start, start
	}
	return start, end
}

// computeNetworkSnapshotLocked aggregates rewards; caller must hold cacheMux.
func (s *Service) computeNetworkSnapshotLocked(now time.Time) *NetworkRewardSnapshot {
	start := s.cacheWindowStartTime().UTC()
	end := utils.EpochToTime(s.latestSyncEpoch)
	if end.Before(start) {
		end = start
	}
	duration := end.Sub(start)
	if duration <= 0 {
		duration = s.config.CacheResetInterval
		start = end.Add(-duration)
	}

	var clTotal int64
	elWei := big.NewInt(0)
	for _, inc := range s.cache {
		clTotal += inc.TotalClRewards()
		elWei.Add(elWei, weiBytesToBigInt(inc.TxFeeRewardWei))
	}
	elTotal := new(big.Int).Div(elWei, gweiScalar).Int64()

	snap := &NetworkRewardSnapshot{
		WindowStart:           start,
		WindowEnd:             end,
		WindowDurationSeconds: duration.Seconds(),
		ActiveValidatorCount:  len(s.cache),
		ClRewardsGwei:         clTotal,
		ElRewardsGwei:         elTotal,
		TotalRewardsGwei:      clTotal + elTotal,
	}

	// Effective balance
	if s.doraDB != nil {
		// This db call can take time, potentially blocking the lock?
		// Ideally we shouldn't hold lock over DB calls.
		// But for simplicity in this refactor we keep it, as this only happens on cache reset/stats.
		ctx, cancel := context.WithTimeout(s.ctx, s.config.RequestTimeout)
		if count, err := s.doraDB.ActiveValidatorCount(ctx, utils.TimeToEpoch(now)); err == nil && count > 0 {
			snap.ActiveValidatorCount = int(count)
		}
		if eff, err := s.doraDB.TotalEffectiveBalance(ctx, utils.TimeToEpoch(now)); err == nil {
			snap.TotalEffectiveBalanceGwei = eff
		}
		cancel()
	}

	if snap.TotalEffectiveBalanceGwei == 0 {
		snap.TotalEffectiveBalanceGwei = int64(len(s.cache)) * defaultEffectiveBalanceGwei
	}

	if snap.TotalEffectiveBalanceGwei > 0 && snap.WindowDurationSeconds > 0 {
		apr := float64(snap.TotalRewardsGwei) / float64(snap.TotalEffectiveBalanceGwei)
		apr *= s.config.CacheResetInterval.Seconds() / snap.WindowDurationSeconds
		apr *= 100.0 * 365.0
		snap.ProjectAprPercent = apr
	}

	return snap
}

// getRewardsForEpoch fetches rewards (Beacon + EL)
func (s *Service) getRewardsForEpoch(epoch uint64) (map[uint64]*types.ValidatorEpochIncome, error) {
	assigns, err := s.beaconCL.ProposerAssignments(epoch)
	if err != nil {
		return nil, err
	}

	proposers := make(map[uint64]uint64, len(assigns.Data))
	for _, pa := range assigns.Data {
		proposers[uint64(pa.Slot)] = uint64(pa.ValidatorIndex)
	}

	rewards := make(map[uint64]*types.ValidatorEpochIncome)
	var mu sync.Mutex

	g, _ := errgroup.WithContext(s.ctx)

	// Fetch Slot Rewards (EL, Sync, Block)
	slots := uint64(len(assigns.Data))
	startSlot := epoch * slots
	for i := uint64(0); i < slots; i++ {
		slot := startSlot + i
		g.Go(func() error {
			return s.processSlot(slot, proposers, rewards, &mu)
		})
	}

	// Fetch Attestations
	g.Go(func() error {
		ar, err := s.beaconCL.AttestationRewards(epoch)
		if err != nil {
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		for _, r := range ar.Data.TotalRewards {
			e := s.getEntry(rewards, r.ValidatorIndex)
			e.AttestationHeadReward = uint64(r.Head)
			e.AttestationSourceReward = uint64(r.Source)
			e.AttestationTargetReward = uint64(r.Target)
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}
	return rewards, nil
}

func (s *Service) processSlot(slot uint64, proposers map[uint64]uint64, rewards map[uint64]*types.ValidatorEpochIncome, mu *sync.Mutex) error {
	proposer, ok := proposers[slot]
	if !ok {
		return fmt.Errorf("no proposer for slot %d", slot)
	}

	// EL Rewards
	blkNum, err := s.beaconCL.ExecutionBlockNumber(slot)
	if err == nil {
		if el, err := elrewards.GetELRewardForBlock(blkNum, *s.elClient); err == nil {
			mu.Lock()
			s.getEntry(rewards, proposer).TxFeeRewardWei = el.Bytes()
			mu.Unlock()
		}
	} else if err == types.ErrBlockNotFound {
		mu.Lock()
		s.getEntry(rewards, proposer).ProposalsMissed++
		mu.Unlock()
	}

	// Sync Committee
	if syncRew, err := s.beaconCL.SyncCommitteeRewards(slot); err == nil && syncRew != nil {
		mu.Lock()
		for _, r := range syncRew.Data {
			e := s.getEntry(rewards, r.ValidatorIndex)
			if r.Reward > 0 {
				e.SyncCommitteeReward += uint64(r.Reward)
			} else {
				e.SyncCommitteePenalty += uint64(-r.Reward)
			}
		}
		mu.Unlock()
	}

	// Block Inclusion
	if blkRew, err := s.beaconCL.BlockRewards(slot); err == nil {
		mu.Lock()
		e := s.getEntry(rewards, blkRew.Data.ProposerIndex)
		e.ProposerAttestationInclusionReward += blkRew.Data.Attestations
		e.ProposerSlashingInclusionReward += blkRew.Data.AttesterSlashings + blkRew.Data.ProposerSlashings
		e.ProposerSyncInclusionReward += blkRew.Data.SyncAggregate
		mu.Unlock()
	}

	return nil
}

func (s *Service) getEntry(m map[uint64]*types.ValidatorEpochIncome, idx uint64) *types.ValidatorEpochIncome {
	if m[idx] == nil {
		m[idx] = &types.ValidatorEpochIncome{}
	}
	return m[idx]
}

func (s *Service) persistSnapshot(snap *NetworkRewardSnapshot) {
	if s.historyPath == "" || snap == nil {
		return
	}
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	_ = os.MkdirAll(filepath.Dir(s.historyPath), 0o755)
	f, err := os.OpenFile(s.historyPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		slog.Error("Failed to open rewards history file", "path", s.historyPath, "error", err)
		return
	}
	_ = json.NewEncoder(f).Encode(snap)
	_ = f.Close()
}

func (s *Service) setCacheWindowStart(t time.Time) {
	s.cacheWindowMu.Lock()
	s.cacheWindowStart = t
	s.cacheWindowMu.Unlock()
}

func (s *Service) cacheWindowStartTime() time.Time {
	s.cacheWindowMu.RLock()
	defer s.cacheWindowMu.RUnlock()
	if s.cacheWindowStart.IsZero() {
		return time.Now().Add(-s.config.CacheResetInterval)
	}
	return s.cacheWindowStart
}

func addWei(a, b []byte) []byte {
	if len(b) == 0 {
		return a
	}
	return new(big.Int).Add(new(big.Int).SetBytes(a), new(big.Int).SetBytes(b)).Bytes()
}

func weiBytesToBigInt(b []byte) *big.Int {
	if len(b) == 0 {
		return big.NewInt(0)
	}
	return new(big.Int).SetBytes(b)
}
