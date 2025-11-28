package config

import (
	"testing"
	"time"
)

func TestLoadBackfillLookback(t *testing.T) {
	env := map[string]string{
		"BACKFILL_LOOKBACK": "6h",
	}

	cfg, err := LoadFromEnv(func(key string) string {
		return env[key]
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BackfillLookback != 6*time.Hour {
		t.Fatalf("expected backfill lookback to be 6h, got %s", cfg.BackfillLookback)
	}
}

func TestLoadBackfillLookbackNegative(t *testing.T) {
	env := map[string]string{
		"BACKFILL_LOOKBACK": "-1h",
	}

	_, err := LoadFromEnv(func(key string) string {
		return env[key]
	})
	if err == nil {
		t.Fatalf("expected error for negative backfill lookback")
	}
}
