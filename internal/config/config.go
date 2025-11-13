package config

import (
	"time"
)

// Config holds the application configuration
type Config struct {
	// Server configuration
	ServerAddress string
	ServerPort    string

	// Ethereum configuration
	BeaconNodeURL    string
	ExecutionNodeURL string

	// Validator configuration
	ValidatorIndices []uint64

	// Cache configuration
	CacheResetInterval time.Duration

	// Epoch processing configuration
	EpochUpdateInterval time.Duration
	StartEpoch          uint64
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		ServerAddress:       "0.0.0.0",
		ServerPort:          "8080",
		BeaconNodeURL:       "http://localhost:5052",
		ExecutionNodeURL:    "http://localhost:8545",
		ValidatorIndices:    []uint64{},
		CacheResetInterval:  24 * time.Hour,
		EpochUpdateInterval: 12 * time.Second, // ~1 slot on mainnet
		StartEpoch:          1,
	}
}
