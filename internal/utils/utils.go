package utils

import (
	"math/big"
	"time"
)

const (
	SECONDS_PER_SLOT  = 12
	SLOTS_PER_EPOCH   = 32
	SECONDS_PER_EPOCH = SECONDS_PER_SLOT * SLOTS_PER_EPOCH
	// 2024-03-04 06:00:00 +0000 UTC
	GENESIS_TIMESTAMP = 1709532000
)

// TimeToEpoch returns the epoch of the given time.
func TimeToEpoch(ts time.Time) uint64 {
	if int64(GENESIS_TIMESTAMP) > ts.Unix() {
		return 0
	}
	return uint64((ts.Unix() - int64(GENESIS_TIMESTAMP)) / int64(SECONDS_PER_SLOT) / int64(SLOTS_PER_EPOCH))
}

// EpochToTime returns the time of the given epoch.
func EpochToTime(epoch uint64) time.Time {
	return time.Unix(int64(GENESIS_TIMESTAMP)+(int64(epoch)+1)*int64(SECONDS_PER_EPOCH), 0).UTC()
}

func AddWei(base, delta []byte) []byte {
	if len(delta) == 0 {
		return base
	}

	baseInt := new(big.Int).SetBytes(base)
	deltaInt := new(big.Int).SetBytes(delta)
	baseInt.Add(baseInt, deltaInt)
	return baseInt.Bytes()
}

func WeiBytesToBigInt(data []byte) *big.Int {
	if len(data) == 0 {
		return big.NewInt(0)
	}
	return new(big.Int).SetBytes(data)
}

