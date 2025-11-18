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

// NormalizeAddresses normalizes and de-duplicates addresses while keeping the first occurrence order.
func NormalizeAddresses(addresses []string) ([]string, error) {
	if len(addresses) == 0 {
		return []string{}, nil
	}

	seen := make(map[string]struct{}, len(addresses))
	normalized := make([]string, 0, len(addresses))

	for _, addr := range addresses {
		normalizedAddr, err := NormalizeAddress(addr)
		if err != nil {
			return nil, err
		}

		if _, exists := seen[normalizedAddr]; exists {
			continue
		}
		seen[normalizedAddr] = struct{}{}
		normalized = append(normalized, normalizedAddr)
	}

	return normalized, nil
}
