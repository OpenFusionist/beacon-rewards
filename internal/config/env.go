package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

const (
	envServerAddress           = "SERVER_ADDRESS"
	envServerPort              = "SERVER_PORT"
	envRequestTimeout          = "REQUEST_TIMEOUT"
	envDefaultLimit            = "DEFAULT_API_LIMIT"
	envDepositorLabelsFile     = "DEPOSITOR_LABELS_FILE"
	envDoraPostgresURL         = "DORA_PG_URL"
	envBeaconNodeURL           = "BEACON_NODE_URL"
	envExecutionNodeURL        = "EXECUTION_NODE_URL"
	envCacheResetInterval      = "CACHE_RESET_INTERVAL"
	envEpochUpdateInterval     = "EPOCH_UPDATE_INTERVAL"
	envStartEpoch              = "START_EPOCH"
	envBackfillConcurrency     = "BACKFILL_CONCURRENCY"
	envEpochProcessMaxRetries  = "EPOCH_PROCESS_MAX_RETRIES"
	envEpochProcessBaseBackoff = "EPOCH_PROCESS_BASE_BACKOFF"
	envEpochProcessMaxBackoff  = "EPOCH_PROCESS_MAX_BACKOFF"
)

type envLookup func(string) string

// Load returns a Config populated from defaults and environment variables.
func Load() (*Config, error) {
	return loadFromEnv(DefaultConfig(), os.Getenv)
}

// LoadWithLookup mirrors Load but allows injecting a custom env lookup (useful in tests).
func LoadWithLookup(lookup envLookup) (*Config, error) {
	return loadFromEnv(DefaultConfig(), lookup)
}

func loadFromEnv(cfg *Config, lookup envLookup) (*Config, error) {
	for _, binding := range envBindings {
		value := lookup(binding.key)
		if value == "" {
			continue
		}

		if err := binding.apply(cfg, value); err != nil {
			return nil, fmt.Errorf("load %s: %w", binding.key, err)
		}
	}

	return cfg, nil
}

type envBinding struct {
	key   string
	apply func(*Config, string) error
}

var envBindings = []envBinding{
	{envServerAddress, func(cfg *Config, value string) error {
		cfg.ServerAddress = value
		return nil
	}},
	{envServerPort, func(cfg *Config, value string) error {
		cfg.ServerPort = value
		return nil
	}},
	{envRequestTimeout, func(cfg *Config, value string) error {
		dur, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		if dur <= 0 {
			return fmt.Errorf("duration must be > 0")
		}
		cfg.RequestTimeout = dur
		return nil
	}},
	{envDefaultLimit, func(cfg *Config, value string) error {
		limit, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		if limit <= 0 {
			return fmt.Errorf("default limit must be > 0")
		}
		cfg.DefaultAPILimit = limit
		return nil
	}},
	{envDepositorLabelsFile, func(cfg *Config, value string) error {
		cfg.DepositorLabelsFile = value
		return nil
	}},
	{envDoraPostgresURL, func(cfg *Config, value string) error {
		cfg.DoraPGURL = value
		return nil
	}},
	{envBeaconNodeURL, func(cfg *Config, value string) error {
		cfg.BeaconNodeURL = value
		return nil
	}},
	{envExecutionNodeURL, func(cfg *Config, value string) error {
		cfg.ExecutionNodeURL = value
		return nil
	}},
	{envCacheResetInterval, func(cfg *Config, value string) error {
		dur, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		if dur <= 0 {
			return fmt.Errorf("duration must be > 0")
		}
		cfg.CacheResetInterval = dur
		return nil
	}},
	{envEpochUpdateInterval, func(cfg *Config, value string) error {
		dur, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		if dur <= 0 {
			return fmt.Errorf("duration must be > 0")
		}
		cfg.EpochUpdateInterval = dur
		return nil
	}},
	{envStartEpoch, func(cfg *Config, value string) error {
		start, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return err
		}
		cfg.StartEpoch = start
		return nil
	}},
	{envBackfillConcurrency, func(cfg *Config, value string) error {
		concurrency, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		if concurrency <= 0 {
			return fmt.Errorf("concurrency must be > 0")
		}
		cfg.BackfillConcurrency = concurrency
		return nil
	}},
	{envEpochProcessMaxRetries, func(cfg *Config, value string) error {
		retries, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		if retries <= 0 {
			return fmt.Errorf("max retries must be > 0")
		}
		cfg.EpochProcessMaxRetries = retries
		return nil
	}},
	{envEpochProcessBaseBackoff, func(cfg *Config, value string) error {
		dur, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		if dur <= 0 {
			return fmt.Errorf("base backoff must be > 0")
		}
		cfg.EpochProcessBaseBackoff = dur
		return nil
	}},
	{envEpochProcessMaxBackoff, func(cfg *Config, value string) error {
		dur, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		if dur <= 0 {
			return fmt.Errorf("max backoff must be > 0")
		}
		cfg.EpochProcessMaxBackoff = dur
		return nil
	}},
}
