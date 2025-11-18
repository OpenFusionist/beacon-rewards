package rewards

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// NetworkRewardSnapshot captures aggregated reward totals for all validators within a cache window.
type NetworkRewardSnapshot struct {
	WindowStart               time.Time `json:"window_start"`
	WindowEnd                 time.Time `json:"window_end"`
	WindowDurationSeconds     float64   `json:"window_duration_seconds"`
	ValidatorCount            int       `json:"validator_count"`
	ClRewardsGwei             int64     `json:"cl_rewards_gwei"`
	ElRewardsGwei             int64     `json:"el_rewards_gwei"`
	TotalRewardsGwei          int64     `json:"total_rewards_gwei"`
	TotalEffectiveBalanceGwei int64     `json:"total_effective_balance_gwei"`
	DailyAprPercent           float64   `json:"daily_apr_percent"`
}

// HistoryStore appends daily snapshots to a local jsonl file.
type HistoryStore struct {
	path string
	mu   sync.Mutex
}

// NewHistoryStore creates a store for the provided path. Empty paths disable persistence.
func NewHistoryStore(path string) *HistoryStore {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil
	}
	return &HistoryStore{path: trimmed}
}

// Path returns the backing file path.
func (s *HistoryStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Append creates the directory (if needed) and appends the snapshot as JSON.
func (s *HistoryStore) Append(entry *NetworkRewardSnapshot) error {
	if s == nil || entry == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	return enc.Encode(entry)
}

// ReadAll returns every stored snapshot. Missing files return an empty slice.
func (s *HistoryStore) ReadAll() ([]NetworkRewardSnapshot, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return []NetworkRewardSnapshot{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	entries := make([]NetworkRewardSnapshot, 0)
	for scanner.Scan() {
		trimmed := bytes.TrimSpace(scanner.Bytes())
		if len(trimmed) == 0 {
			continue
		}
		var entry NetworkRewardSnapshot
		if err := json.Unmarshal(trimmed, &entry); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}
