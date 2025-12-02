package beacon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const defaultRequestTimeout = 10 * time.Second

// FetchGenesisTimestamp retrieves the genesis timestamp (Unix seconds) from the beacon node.
// It calls the /eth/v1/beacon/genesis endpoint and returns an error if the value is missing or invalid.
func FetchGenesisTimestamp(ctx context.Context, baseURL string, timeout time.Duration) (int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	trimmedURL := strings.TrimSpace(baseURL)
	if trimmedURL == "" {
		return 0, errors.New("beacon node URL is empty")
	}

	var endpoints []string
	for _, part := range strings.Split(trimmedURL, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			endpoints = append(endpoints, part)
		}
	}
	if len(endpoints) == 0 {
		return 0, errors.New("beacon node URL is empty")
	}

	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var errs []error
	for _, endpointBase := range endpoints {
		endpoint := strings.TrimSuffix(endpointBase, "/") + "/eth/v1/beacon/genesis"

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: create request: %w", endpointBase, err))
			continue
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: request beacon genesis: %w", endpointBase, err))
			continue
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			_ = resp.Body.Close()

			msg := strings.TrimSpace(string(body))
			if msg != "" {
				errs = append(errs, fmt.Errorf("%s: beacon genesis request failed: %s: %s", endpointBase, resp.Status, msg))
			} else {
				errs = append(errs, fmt.Errorf("%s: beacon genesis request failed: %s", endpointBase, resp.Status))
			}
			continue
		}

		var payload struct {
			Data struct {
				GenesisTime string `json:"genesis_time"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			_ = resp.Body.Close()
			errs = append(errs, fmt.Errorf("%s: decode response: %w", endpointBase, err))
			continue
		}
		_ = resp.Body.Close()

		if payload.Data.GenesisTime == "" {
			errs = append(errs, fmt.Errorf("%s: genesis_time missing in response", endpointBase))
			continue
		}

		ts, err := strconv.ParseInt(payload.Data.GenesisTime, 10, 64)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: parse genesis_time %q: %w", endpointBase, payload.Data.GenesisTime, err))
			continue
		}
		if ts <= 0 {
			errs = append(errs, fmt.Errorf("%s: genesis_time must be positive, got %d", endpointBase, ts))
			continue
		}

		return ts, nil
	}

	if len(errs) == 0 {
		return 0, errors.New("beacon node URL is empty")
	}
	return 0, errors.Join(errs...)
}
