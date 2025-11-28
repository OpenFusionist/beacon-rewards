package utils

import (
	"testing"
	"time"
)

func TestSetGenesisTimestampOverridesCalculations(t *testing.T) {
	original := GenesisTimestamp()
	t.Cleanup(func() {
		SetGenesisTimestamp(original)
	})

	newGenesis := int64(1_800_000_000)
	SetGenesisTimestamp(newGenesis)

	if got := GenesisTimestamp(); got != newGenesis {
		t.Fatalf("GenesisTimestamp = %d, want %d", got, newGenesis)
	}

	epoch := TimeToEpoch(time.Unix(newGenesis+int64(SECONDS_PER_EPOCH), 0))
	if epoch != 1 {
		t.Fatalf("TimeToEpoch with overridden genesis = %d, want 1", epoch)
	}

	epochTime := EpochToTime(0)
	expected := time.Unix(newGenesis+int64(SECONDS_PER_EPOCH), 0).UTC()
	if !epochTime.Equal(expected) {
		t.Fatalf("EpochToTime = %s, want %s", epochTime, expected)
	}
}

func TestSetGenesisTimestampIgnoresNonPositive(t *testing.T) {
	original := GenesisTimestamp()
	SetGenesisTimestamp(0)
	if got := GenesisTimestamp(); got != original {
		t.Fatalf("expected genesis timestamp to remain %d when setting 0, got %d", original, got)
	}
}
