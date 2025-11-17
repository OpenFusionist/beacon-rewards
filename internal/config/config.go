package config

import (
	"time"
)

// Config holds the application configuration.
type Config struct {
	// Server configuration.
	ServerAddress   string
	ServerPort      string
	RequestTimeout  time.Duration
	DefaultAPILimit int

	// Database configuration.
	DoraPGURL string

	// Ethereum configuration.
	BeaconNodeURL    string
	ExecutionNodeURL string

	// Cache configuration.
	CacheResetInterval time.Duration

	// Epoch processing configuration.
	EpochUpdateInterval time.Duration
	StartEpoch          uint64

	// Backfill configuration.
	BackfillConcurrency int
}

// DefaultConfig returns a default configuration.
func DefaultConfig() *Config {
	return &Config{
		ServerAddress:       "0.0.0.0",
		ServerPort:          "8080",
		RequestTimeout:      10 * time.Second,
		DefaultAPILimit:     100,
		DoraPGURL:           "postgres://postgres:postgres@127.0.0.1:5432/dora?sslmode=disable",
		BeaconNodeURL:       "http://localhost:5052",
		ExecutionNodeURL:    "http://localhost:8545",
		CacheResetInterval:  24 * time.Hour,
		EpochUpdateInterval: 384 * time.Second, // ~32 slots 
		StartEpoch:          0,
		BackfillConcurrency: 16,
	}
}

// ListenAddress returns the HTTP listen address derived from the server config.
func (c *Config) ListenAddress() string {
	return c.ServerAddress + ":" + c.ServerPort
}
