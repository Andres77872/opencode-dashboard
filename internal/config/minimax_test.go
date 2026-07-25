package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveMiniMaxAPIKeyPrecedenceAndFallback(t *testing.T) {
	authPath := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(authPath, []byte(`{"other":{"key":"ignore"},"minimax-coding-plan":{"key":"store-key"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvMiniMaxAPIKey, " env-key ")
	key, err := ResolveMiniMaxAPIKey(authPath)
	if err != nil || key != "env-key" {
		t.Fatalf("env resolution key=%q err=%v", key, err)
	}
	t.Setenv(EnvMiniMaxAPIKey, "")
	key, err = ResolveMiniMaxAPIKey(authPath)
	if err != nil || key != "store-key" {
		t.Fatalf("store resolution key=%q err=%v", key, err)
	}
}

func TestResolveMiniMaxAPIKeyMissingAndMalformedStore(t *testing.T) {
	t.Setenv(EnvMiniMaxAPIKey, "")
	key, err := ResolveMiniMaxAPIKey(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil || key != "" {
		t.Fatalf("missing store key=%q err=%v", key, err)
	}
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte(`not-json`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveMiniMaxAPIKey(path); err == nil {
		t.Fatal("malformed auth store returned nil error")
	}
}

func TestMiniMaxBaseURLOverrideUsesServerEnvironment(t *testing.T) {
	t.Setenv(EnvMiniMaxBaseURL, " https://api.minimaxi.com/v1 ")
	if got := MiniMaxBaseURLOverride(); got != "https://api.minimaxi.com/v1" {
		t.Fatalf("base URL = %q", got)
	}
}

func TestMiniMaxRunTimeoutOverride(t *testing.T) {
	t.Setenv(EnvMiniMaxRunTimeout, "45s")
	if got, err := MiniMaxRunTimeoutOverride(); err != nil || got != 45*time.Second {
		t.Fatalf("timeout = %v, err=%v", got, err)
	}
	// Delegated specialist runs share the turn budget, so longer limits are
	// legitimate; the upper bound still keeps one question bounded.
	t.Setenv(EnvMiniMaxRunTimeout, "3m")
	if got, err := MiniMaxRunTimeoutOverride(); err != nil || got != 3*time.Minute {
		t.Fatalf("timeout = %v, err=%v", got, err)
	}
	for _, value := range []string{"broken", "9s", "6m"} {
		t.Setenv(EnvMiniMaxRunTimeout, value)
		if _, err := MiniMaxRunTimeoutOverride(); err == nil {
			t.Errorf("timeout %q unexpectedly accepted", value)
		}
	}
}
