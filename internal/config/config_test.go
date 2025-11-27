package config

import "testing"

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
