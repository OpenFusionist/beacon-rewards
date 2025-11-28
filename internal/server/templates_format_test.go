package server

import "testing"

func TestFormatInt(t *testing.T) {
	tests := []struct {
		name     string
		input    int64
		expected string
	}{
		{name: "zero", input: 0, expected: "0"},
		{name: "positive", input: 1234567, expected: "1,234,567"},
		{name: "negative", input: -1234, expected: "-1,234"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatInt(tt.input); got != tt.expected {
				t.Fatalf("formatInt(%d) = %s, want %s", tt.input, got, tt.expected)
			}
		})
	}
}

func TestFormatFloat(t *testing.T) {
	tests := []struct {
		name      string
		input     float64
		precision int
		expected  string
	}{
		{name: "trim trailing zeros", input: 1234.5000, precision: 4, expected: "1,234.5"},
		{name: "small number clamps to zero", input: 0.0000001, precision: 6, expected: "0"},
		{name: "respects precision", input: 0.1234, precision: 2, expected: "0.12"},
		{name: "integer formatting", input: 1000.0, precision: 2, expected: "1,000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatFloat(tt.input, tt.precision); got != tt.expected {
				t.Fatalf("formatFloat(%f) = %s, want %s", tt.input, got, tt.expected)
			}
		})
	}
}
