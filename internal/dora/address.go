package dora

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

var ErrInvalidAddress = errors.New("invalid address")

// NormalizeAddress ensures the provided address is a 0x-prefixed, lower-case, 20-byte hex string.
func NormalizeAddress(address string) (string, error) {
	trimmed := strings.TrimSpace(address)
	if trimmed == "" {
		return "", fmt.Errorf("%w: address is empty", ErrInvalidAddress)
	}

	if strings.HasPrefix(trimmed, "0x") || strings.HasPrefix(trimmed, "0X") {
		trimmed = trimmed[2:]
	}

	if len(trimmed) != 40 {
		return "", fmt.Errorf("%w: %s must have 40 hex characters", ErrInvalidAddress, address)
	}

	if _, err := hex.DecodeString(trimmed); err != nil {
		return "", fmt.Errorf("%w: %s", ErrInvalidAddress, err.Error())
	}

	return "0x" + strings.ToLower(trimmed), nil
}
