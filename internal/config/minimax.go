package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	EnvMiniMaxBaseURL    = "OPENCODE_DASHBOARD_MINIMAX_BASE_URL"
	EnvMiniMaxRunTimeout = "OPENCODE_DASHBOARD_MINIMAX_TIMEOUT"
)

// ResolveMiniMaxAPIKey prefers the dashboard-specific environment variable and
// falls back to OpenCode's MiniMax coding-plan credential. It returns the key
// only to server-side callers; web responses must never expose it.
func ResolveMiniMaxAPIKey(authPath string) (string, error) {
	if key := strings.TrimSpace(os.Getenv(EnvMiniMaxAPIKey)); key != "" {
		return key, nil
	}
	if strings.TrimSpace(authPath) == "" {
		return "", nil
	}
	body, err := os.ReadFile(authPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read OpenCode auth store: %w", err)
	}
	var store map[string]struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(body, &store); err != nil {
		return "", fmt.Errorf("parse OpenCode auth store: %w", err)
	}
	return strings.TrimSpace(store["minimax-coding-plan"].Key), nil
}

// MiniMaxBaseURLOverride is intentionally server-environment-only. The
// browser chat contract has no base URL field, preventing credential-bearing
// requests from being redirected by untrusted input.
func MiniMaxBaseURLOverride() string {
	return strings.TrimSpace(os.Getenv(EnvMiniMaxBaseURL))
}

// MiniMaxRunTimeoutOverride bounds one complete agent run, including model
// discovery, every tool round, and any delegated specialist run. Zero means the
// service default.
func MiniMaxRunTimeoutOverride() (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(EnvMiniMaxRunTimeout))
	if raw == "" {
		return 0, nil
	}
	timeout, err := time.ParseDuration(raw)
	if err != nil || timeout < 10*time.Second || timeout > 5*time.Minute {
		return 0, fmt.Errorf("%s must be a duration from 10s through 5m", EnvMiniMaxRunTimeout)
	}
	return timeout, nil
}
