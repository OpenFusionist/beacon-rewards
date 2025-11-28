package config

import (
	"beacon-rewards/internal/utils"
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds the application configuration.
type Config struct {
	// Server configuration.
	ServerAddress       string
	ServerPort          string
	RequestTimeout      time.Duration
	DefaultAPILimit     int
	EnableFrontend      bool
	DepositorLabelsFile string
	GenesisTimestamp    int64

	// Database configuration.
	DoraPGURL string

	// Ethereum configuration.
	BeaconNodeURL    string
	ExecutionNodeURL string

	// Cache configuration.
	CacheResetInterval time.Duration
	RewardsHistoryFile string

	// Epoch processing configuration.
	EpochCheckInterval      time.Duration
	StartEpoch              uint64
	EpochProcessMaxRetries  int
	EpochProcessBaseBackoff time.Duration
	EpochProcessMaxBackoff  time.Duration

	// Backfill configuration.
	BackfillConcurrency int
}

// DefaultConfig returns a default configuration.
func DefaultConfig() *Config {
	return &Config{
		ServerAddress:           "0.0.0.0",
		ServerPort:              "8080",
		RequestTimeout:          10 * time.Second,
		DefaultAPILimit:         100,
		EnableFrontend:          true,
		DepositorLabelsFile:     "depositor-name.yaml",
		GenesisTimestamp:        utils.DefaultGenesisTimestamp,
		DoraPGURL:               "postgres://postgres:postgres@127.0.0.1:5432/dora?sslmode=disable",
		BeaconNodeURL:           "http://localhost:5052",
		ExecutionNodeURL:        "http://localhost:8545",
		CacheResetInterval:      24 * time.Hour,
		RewardsHistoryFile:      "data/reward_history.jsonl",
		EpochCheckInterval:      12 * time.Second,
		StartEpoch:              0,
		EpochProcessMaxRetries:  5,
		EpochProcessBaseBackoff: 2 * time.Second,
		EpochProcessMaxBackoff:  30 * time.Second,
		BackfillConcurrency:     16,
	}
}

// ListenAddress returns the HTTP listen address derived from the server config.
func (c *Config) ListenAddress() string {
	return c.ServerAddress + ":" + c.ServerPort
}

// Load returns a Config populated from defaults and environment variables.
func Load() (*Config, error) {
	return LoadFromEnv(os.Getenv)
}

// LoadFromEnv loads configuration using a lookup function (e.g., os.Getenv).
func LoadFromEnv(lookup func(string) string) (*Config, error) {
	cfg := DefaultConfig()

	if v := lookup("SERVER_ADDRESS"); v != "" {
		cfg.ServerAddress = v
	}
	if v := lookup("SERVER_PORT"); v != "" {
		cfg.ServerPort = v
	}
	if v := lookup("REQUEST_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("REQUEST_TIMEOUT: %w", err)
		}
		cfg.RequestTimeout = d
	}
	if v := lookup("DEFAULT_API_LIMIT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("DEFAULT_API_LIMIT: %w", err)
		}
		cfg.DefaultAPILimit = n
	}
	if v := lookup("ENABLE_FRONTEND"); v != "" {
		enabled, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("ENABLE_FRONTEND: %w", err)
		}
		cfg.EnableFrontend = enabled
	}
	if v := lookup("DEPOSITOR_LABELS_FILE"); v != "" {
		cfg.DepositorLabelsFile = v
	}
	if v := lookup("GENESIS_TIMESTAMP"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("GENESIS_TIMESTAMP: %w", err)
		}
		if n <= 0 {
			return nil, fmt.Errorf("GENESIS_TIMESTAMP: must be positive")
		}
		cfg.GenesisTimestamp = n
	}
	if v := lookup("DORA_PG_URL"); v != "" {
		cfg.DoraPGURL = v
	}
	if v := lookup("BEACON_NODE_URL"); v != "" {
		cfg.BeaconNodeURL = v
	}
	if v := lookup("EXECUTION_NODE_URL"); v != "" {
		cfg.ExecutionNodeURL = v
	}
	if v := lookup("CACHE_RESET_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("CACHE_RESET_INTERVAL: %w", err)
		}
		cfg.CacheResetInterval = d
	}
	if v := lookup("REWARDS_HISTORY_FILE"); v != "" {
		cfg.RewardsHistoryFile = v
	}
	if v := lookup("EPOCH_CHECK_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("EPOCH_CHECK_INTERVAL: %w", err)
		}
		cfg.EpochCheckInterval = d
	}
	if v := lookup("START_EPOCH"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("START_EPOCH: %w", err)
		}
		cfg.StartEpoch = n
	}
	if v := lookup("BACKFILL_CONCURRENCY"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("BACKFILL_CONCURRENCY: %w", err)
		}
		cfg.BackfillConcurrency = n
	}
	if v := lookup("EPOCH_PROCESS_MAX_RETRIES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("EPOCH_PROCESS_MAX_RETRIES: %w", err)
		}
		cfg.EpochProcessMaxRetries = n
	}
	if v := lookup("EPOCH_PROCESS_BASE_BACKOFF"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("EPOCH_PROCESS_BASE_BACKOFF: %w", err)
		}
		cfg.EpochProcessBaseBackoff = d
	}
	if v := lookup("EPOCH_PROCESS_MAX_BACKOFF"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("EPOCH_PROCESS_MAX_BACKOFF: %w", err)
		}
		cfg.EpochProcessMaxBackoff = d
	}

	return cfg, nil
}
