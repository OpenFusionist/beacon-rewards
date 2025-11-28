package utils

import (
	"sync/atomic"
	"time"
)

const (
	SECONDS_PER_SLOT  = 12
	SLOTS_PER_EPOCH   = 32
	SECONDS_PER_EPOCH = SECONDS_PER_SLOT * SLOTS_PER_EPOCH
	// DefaultGenesisTimestamp is the default beacon chain genesis time for Endurance Network (2024-03-04 06:00:00 +0000 UTC).
	DefaultGenesisTimestamp int64 = 1709532000
)

var genesisTimestamp atomic.Int64

func init() {
	genesisTimestamp.Store(DefaultGenesisTimestamp)
}

// SetGenesisTimestamp overrides the global genesis timestamp used for epoch/time calculations.
func SetGenesisTimestamp(ts int64) {
	if ts > 0 {
		genesisTimestamp.Store(ts)
	}
}

// GenesisTimestamp returns the currently configured genesis timestamp.
func GenesisTimestamp() int64 {
	return genesisTimestamp.Load()
}

// TimeToEpoch returns the epoch of the given time.
func TimeToEpoch(ts time.Time) uint64 {
	genesis := genesisTimestamp.Load()
	if genesis > ts.Unix() {
		return 0
	}
	return uint64((ts.Unix() - genesis) / int64(SECONDS_PER_SLOT) / int64(SLOTS_PER_EPOCH))
}

// EpochToTime returns the time of the given epoch.
func EpochToTime(epoch uint64) time.Time {
	genesis := genesisTimestamp.Load()
	return time.Unix(genesis+(int64(epoch)+1)*int64(SECONDS_PER_EPOCH), 0).UTC()
}
