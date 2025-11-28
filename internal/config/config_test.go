package config

import (
	"testing"
	"time"
)

func TestEnableFrontendFlag(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		expected bool
		wantErr  bool
	}{
		{
			name:     "default true",
			env:      map[string]string{},
			expected: true,
		},
		{
			name: "explicit false",
			env: map[string]string{
				"ENABLE_FRONTEND": "false",
			},
			expected: false,
		},
		{
			name: "explicit true",
			env: map[string]string{
				"ENABLE_FRONTEND": "true",
			},
			expected: true,
		},
		{
			name: "invalid value",
			env: map[string]string{
				"ENABLE_FRONTEND": "nope",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			lookup := func(key string) string {
				return tt.env[key]
			}

			cfg, err := LoadFromEnv(lookup)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.EnableFrontend != tt.expected {
				t.Fatalf("EnableFrontend = %v, want %v", cfg.EnableFrontend, tt.expected)
			}
		})
	}
}

func TestListenAddress(t *testing.T) {
	cfg := &Config{
		ServerAddress: "127.0.0.1",
		ServerPort:    "9090",
	}

	if got, want := cfg.ListenAddress(), "127.0.0.1:9090"; got != want {
		t.Fatalf("ListenAddress = %s, want %s", got, want)
	}
}

func TestLoadOverridesAndErrors(t *testing.T) {
	t.Run("overrides defaults via env", func(t *testing.T) {
		t.Setenv("SERVER_ADDRESS", "1.2.3.4")
		t.Setenv("SERVER_PORT", "9999")
		t.Setenv("REQUEST_TIMEOUT", "15s")
		t.Setenv("DEFAULT_API_LIMIT", "250")
		t.Setenv("ENABLE_FRONTEND", "false")
		t.Setenv("REWARDS_HISTORY_FILE", "/tmp/history.jsonl")
		t.Setenv("GENESIS_TIMESTAMP", "1710000000")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load returned error: %v", err)
		}

		if cfg.ServerAddress != "1.2.3.4" || cfg.ServerPort != "9999" {
			t.Fatalf("server address/port not applied: %+v", cfg)
		}
		if cfg.RequestTimeout != 15*time.Second {
			t.Fatalf("RequestTimeout = %v, want %v", cfg.RequestTimeout, 15*time.Second)
		}
		if cfg.DefaultAPILimit != 250 {
			t.Fatalf("DefaultAPILimit = %d, want 250", cfg.DefaultAPILimit)
		}
		if cfg.EnableFrontend {
			t.Fatalf("EnableFrontend expected false after env override")
		}
		if cfg.RewardsHistoryFile != "/tmp/history.jsonl" {
			t.Fatalf("RewardsHistoryFile = %s, want /tmp/history.jsonl", cfg.RewardsHistoryFile)
		}
		if cfg.GenesisTimestamp != 1710000000 {
			t.Fatalf("GenesisTimestamp = %d, want 1710000000", cfg.GenesisTimestamp)
		}
	})

	t.Run("invalid duration yields error", func(t *testing.T) {
		t.Setenv("REQUEST_TIMEOUT", "not-a-duration")
		if _, err := Load(); err == nil {
			t.Fatalf("expected error for invalid REQUEST_TIMEOUT")
		}
	})

	t.Run("invalid genesis timestamp yields error", func(t *testing.T) {
		t.Setenv("GENESIS_TIMESTAMP", "-1")
		if _, err := Load(); err == nil {
			t.Fatalf("expected error for invalid GENESIS_TIMESTAMP")
		}
	})
}
