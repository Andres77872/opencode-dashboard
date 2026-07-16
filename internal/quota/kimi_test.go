package quota

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestKimiQuotaMapsManagedUsageAndExtraWallet(t *testing.T) {
	now := time.Date(2026, 7, 16, 18, 0, 0, 0, time.UTC)
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/coding/v1/usages" {
			t.Errorf("path = %q, want /coding/v1/usages", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fresh-access" {
			t.Errorf("Authorization = %q, want fresh token", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q, want application/json", got)
		}
		if got := r.Header.Get("User-Agent"); got != "" {
			t.Errorf("User-Agent = %q, want omitted to match Kimi Code", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
		  "usage": {
		    "name": "Weekly limit",
		    "used": 40,
		    "limit": 1000,
		    "resetAt": "2026-07-20T18:00:00Z"
		  },
		  "limits": [
		    {
		      "detail": {"used": 25, "limit": 100, "reset_in": 7200},
		      "window": {"duration": 300, "timeUnit": "MINUTE"}
		    },
		    {
		      "name": "Daily cap",
		      "detail": {"remaining": 75, "limit": 100},
		      "window": {"duration": 24, "timeUnit": "HOUR"}
		    }
		  ],
		  "boosterWallet": {
		    "balance": {
		      "type": "BOOSTER",
		      "amount": "20000000000",
		      "amountLeft": "10000000000"
		    },
		    "monthlyChargeLimitEnabled": true,
		    "monthlyChargeLimit": {"currency": "USD", "priceInCents": "20000"},
		    "monthlyUsed": {"currency": "USD", "priceInCents": "5000"}
		  }
		}`)
	}))
	defer server.Close()

	home := writeKimiQuotaHome(t, server.URL+"/coding/v1", server.URL, kimiOAuthToken{
		AccessToken:  "fresh-access",
		RefreshToken: "fresh-refresh",
		ExpiresAt:    now.Add(time.Hour).Unix(),
		ExpiresIn:    900,
		Scope:        "kimi-code",
		TokenType:    "Bearer",
	})
	provider := newKimiTestProvider(home, server.Client(), func() time.Time { return now })
	got := provider.quota(context.Background())

	if got.Status != StatusOK {
		t.Fatalf("status = %q (reason %q), want ok", got.Status, got.Reason)
	}
	if got.Provider != ProviderKimi || got.Label != "Kimi Code" {
		t.Errorf("provider identity = %q/%q, want kimi_code/Kimi Code", got.Provider, got.Label)
	}
	if got.AsOfMS != now.UnixMilli() {
		t.Errorf("as_of_ms = %d, want %d", got.AsOfMS, now.UnixMilli())
	}
	if len(got.Windows) != 3 {
		t.Fatalf("windows = %#v, want summary plus two limits", got.Windows)
	}
	assertKimiWindow(t, got.Windows[0], "weekly", "Weekly limit", 4, 10080, time.Date(2026, 7, 20, 18, 0, 0, 0, time.UTC).Unix())
	assertKimiWindow(t, got.Windows[1], "5h", "5h limit", 25, 300, now.Add(2*time.Hour).Unix())
	assertKimiWindow(t, got.Windows[2], "daily-cap", "Daily cap", 25, 1440, 0)

	if got.ExtraUsage == nil {
		t.Fatal("extra_usage = nil, want booster wallet")
	}
	if got.ExtraUsage.BalanceCents != 10000 || got.ExtraUsage.TotalCents != 20000 ||
		!got.ExtraUsage.MonthlyChargeLimitEnabled || got.ExtraUsage.MonthlyChargeLimitCents != 20000 ||
		got.ExtraUsage.MonthlyUsedCents != 5000 || got.ExtraUsage.Currency != "USD" {
		t.Errorf("extra_usage = %+v, want parsed booster wallet", *got.ExtraUsage)
	}

	// The provider-level TTL must avoid both another network call and exposing
	// mutable cached slices/pointers to callers.
	got.Windows[0].UsedPercent = 99
	got.ExtraUsage.BalanceCents = 0
	again := provider.quota(context.Background())
	if calls.Load() != 1 {
		t.Errorf("usage calls = %d, want one within TTL", calls.Load())
	}
	if !approxEqual(again.Windows[0].UsedPercent, 4) || again.ExtraUsage == nil || again.ExtraUsage.BalanceCents != 10000 {
		t.Errorf("cached quota was mutated through caller result: %+v", again)
	}
}

func TestKimiQuotaRefreshesExpiredOAuthCredentialUnderCompatibleLock(t *testing.T) {
	now := time.Date(2026, 7, 16, 18, 0, 0, 0, time.UTC)
	var refreshCalls atomic.Int64
	var usageCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/oauth/token":
			refreshCalls.Add(1)
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm: %v", err)
			}
			if r.Form.Get("client_id") != kimiOAuthClientID ||
				r.Form.Get("grant_type") != "refresh_token" ||
				r.Form.Get("refresh_token") != "old-refresh" {
				t.Errorf("refresh form = %#v", r.Form)
			}
			fmt.Fprint(w, `{
			  "access_token": "new-access",
			  "refresh_token": "new-refresh",
			  "expires_in": 900,
			  "scope": "kimi-code",
			  "token_type": "Bearer"
			}`)
		case "/coding/v1/usages":
			usageCalls.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer new-access" {
				t.Errorf("usage Authorization = %q, want refreshed access token", got)
			}
			fmt.Fprint(w, `{"usage":{"used":1,"limit":10,"name":"Weekly limit"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := writeKimiQuotaHome(t, server.URL+"/coding/v1", server.URL, kimiOAuthToken{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		ExpiresAt:    now.Add(-time.Minute).Unix(),
		ExpiresIn:    900,
		Scope:        "kimi-code",
		TokenType:    "Bearer",
	})
	provider := newKimiTestProvider(home, server.Client(), func() time.Time { return now })
	got := provider.quota(context.Background())
	if got.Status != StatusOK {
		t.Fatalf("status = %q (reason %q), want ok", got.Status, got.Reason)
	}
	if refreshCalls.Load() != 1 || usageCalls.Load() != 1 {
		t.Errorf("refresh/usage calls = %d/%d, want 1/1", refreshCalls.Load(), usageCalls.Load())
	}

	auth, err := resolveKimiRuntimeAuth(home)
	if err != nil {
		t.Fatalf("resolve auth: %v", err)
	}
	tokenPath, err := kimiCredentialPath(home, auth)
	if err != nil {
		t.Fatalf("credential path: %v", err)
	}
	saved, err := readKimiOAuthToken(tokenPath)
	if err != nil {
		t.Fatalf("read refreshed credential: %v", err)
	}
	if saved.AccessToken != "new-access" || saved.RefreshToken != "new-refresh" ||
		saved.ExpiresAt != now.Add(900*time.Second).Unix() {
		t.Errorf("saved token = %+v, want refreshed fields", saved)
	}
	if info, err := os.Stat(tokenPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("credential mode = %v, err = %v; want 0600", infoMode(info), err)
	}
	storageName, _ := kimiTokenStorageName(auth.OAuthKey)
	if _, err := os.Stat(filepath.Join(home, "oauth", storageName+".lock")); !os.IsNotExist(err) {
		t.Errorf("refresh lock remains after request (err = %v)", err)
	}
	if _, err := os.Stat(filepath.Join(home, "oauth", storageName)); err != nil {
		t.Errorf("Kimi-compatible lock sentinel is missing: %v", err)
	}
}

func TestKimiQuotaMissingCredentialHasSetupHelp(t *testing.T) {
	t.Setenv("KIMI_CODE_BASE_URL", "")
	t.Setenv("KIMI_CODE_OAUTH_HOST", "")
	t.Setenv("KIMI_OAUTH_HOST", "")
	provider := newKimiTestProvider(t.TempDir(), &http.Client{Timeout: time.Second}, time.Now)
	got := provider.quota(context.Background())
	if got.Status != StatusUnavailable {
		t.Errorf("status = %q, want unavailable", got.Status)
	}
	if got.Help == "" || !strings.Contains(got.Help, "kimi login") {
		t.Errorf("help = %q, want Kimi login guidance", got.Help)
	}
}

func TestKimiQuotaKeepsLastGoodOnTransientFailure(t *testing.T) {
	now := time.Date(2026, 7, 16, 18, 0, 0, 0, time.UTC)
	clock := now
	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			http.Error(w, "temporary outage", http.StatusBadGateway)
			return
		}
		fmt.Fprint(w, `{"usage":{"used":2,"limit":10,"name":"Weekly limit"}}`)
	}))
	defer server.Close()

	home := writeKimiQuotaHome(t, server.URL, server.URL, kimiOAuthToken{
		AccessToken:  "access",
		RefreshToken: "refresh",
		ExpiresAt:    now.Add(24 * time.Hour).Unix(),
		ExpiresIn:    900,
	})
	provider := newKimiTestProvider(home, server.Client(), func() time.Time { return clock })
	if got := provider.quota(context.Background()); got.Status != StatusOK {
		t.Fatalf("first status = %q (%q), want ok", got.Status, got.Reason)
	}
	fail.Store(true)
	clock = clock.Add(2 * kimiTTL)
	got := provider.quota(context.Background())
	if got.Status != StatusStale || len(got.Windows) != 1 || got.Reason == "" {
		t.Errorf("stale fallback = %+v, want last good quota plus reason", got)
	}
}

func TestKimiQuotaDoesNotForwardOAuthCredentialAcrossRedirect(t *testing.T) {
	now := time.Date(2026, 7, 16, 18, 0, 0, 0, time.UTC)
	var destinationCalls atomic.Int64
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destinationCalls.Add(1)
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("redirect destination received Authorization %q", got)
		}
		fmt.Fprint(w, `{"usage":{"used":1,"limit":10}}`)
	}))
	defer destination.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"/usages", http.StatusFound)
	}))
	defer redirector.Close()

	home := writeKimiQuotaHome(t, redirector.URL, redirector.URL, kimiOAuthToken{
		AccessToken:  "must-not-leak",
		RefreshToken: "refresh",
		ExpiresAt:    now.Add(time.Hour).Unix(),
		ExpiresIn:    900,
	})
	client := redirector.Client()
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return nil }
	provider := newKimiTestProvider(home, client, func() time.Time { return now })
	got := provider.quota(context.Background())
	if got.Status != StatusUnavailable {
		t.Errorf("status = %q, want unavailable after rejected redirect", got.Status)
	}
	if destinationCalls.Load() != 0 {
		t.Errorf("redirect destination calls = %d, want zero", destinationCalls.Load())
	}
	if strings.Contains(got.Reason, "must-not-leak") {
		t.Errorf("reason leaked OAuth access token: %q", got.Reason)
	}
}

func TestKimiQuotaRedactsCredentialReflectedByUsageAPI(t *testing.T) {
	now := time.Date(2026, 7, 16, 18, 0, 0, 0, time.UTC)
	const accessToken = "reflected-secret-access-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintf(w, `{"message":"rejected bearer %s"}`, accessToken)
	}))
	defer server.Close()

	home := writeKimiQuotaHome(t, server.URL, server.URL, kimiOAuthToken{
		AccessToken:  accessToken,
		RefreshToken: "refresh-token",
		ExpiresAt:    now.Add(time.Hour).Unix(),
		ExpiresIn:    900,
	})
	provider := newKimiTestProvider(home, server.Client(), func() time.Time { return now })
	got := provider.quota(context.Background())
	if got.Status != StatusUnavailable {
		t.Errorf("status = %q, want unavailable", got.Status)
	}
	if strings.Contains(got.Reason, accessToken) || !strings.Contains(got.Reason, "[redacted]") {
		t.Errorf("reason = %q, want reflected credential redacted", got.Reason)
	}
}

func TestKimiUsageParserToleratesCurrentFieldVariants(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	parsed := parseKimiUsagePayload(map[string]any{
		"usage": map[string]any{
			"remaining": "20",
			"limit":     json.Number("100"),
			"reset_at":  json.Number("1800003600000"),
		},
		"limits": []any{
			map[string]any{
				"detail": map[string]any{"used": json.Number("1"), "limit": "4"},
				"window": map[string]any{"duration": json.Number("2"), "timeUnit": "DAY"},
			},
			map[string]any{
				"duration": json.Number("90"),
				"timeUnit": "MINUTE",
				"detail":   map[string]any{"used": json.Number("1"), "limit": "2"},
			},
		},
	}, now)
	if len(parsed.Windows) != 3 {
		t.Fatalf("windows = %#v, want three", parsed.Windows)
	}
	assertKimiWindow(t, parsed.Windows[0], "weekly", "Weekly limit", 80, 10080, 1_800_003_600)
	assertKimiWindow(t, parsed.Windows[1], "2d-limit", "2d limit", 25, 2880, 0)
	assertKimiWindow(t, parsed.Windows[2], "90m-limit", "90m limit", 50, 90, 0)
}

func TestKimiOAuthRuntimeResolutionMatchesOfficialScopedKey(t *testing.T) {
	got := kimiOAuthKey(
		"https://auth.dev.example.test/",
		"https://api.dev.example.test/coding/v1/",
	)
	if got != "oauth/kimi-code-env-51d35a57390d1c7e" {
		t.Errorf("scoped OAuth key = %q, want official JSON/SHA-256 derivation", got)
	}
	if got := kimiOAuthKey(kimiDefaultOAuthHost, kimiDefaultBaseURL); got != kimiDefaultOAuthKey {
		t.Errorf("default OAuth key = %q, want %q", got, kimiDefaultOAuthKey)
	}
}

func TestKimiRuntimeEnvironmentOverrideSelectsScopedCredential(t *testing.T) {
	home := t.TempDir()
	config := `[providers."managed:kimi-code"]
type = "kimi"
base_url = "https://api.kimi.com/coding/v1"

[providers."managed:kimi-code".oauth]
storage = "file"
key = "oauth/kimi-code"
`
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KIMI_CODE_BASE_URL", "https://api.dev.example.test/coding/v1/")
	t.Setenv("KIMI_CODE_OAUTH_HOST", "https://auth.dev.example.test/")
	t.Setenv("KIMI_OAUTH_HOST", "")

	auth, err := resolveKimiRuntimeAuth(home)
	if err != nil {
		t.Fatalf("resolve runtime auth: %v", err)
	}
	if auth.BaseURL != "https://api.dev.example.test/coding/v1" ||
		auth.OAuthHost != "https://auth.dev.example.test" ||
		auth.OAuthKey != "oauth/kimi-code-env-51d35a57390d1c7e" ||
		auth.Storage != "file" {
		t.Errorf("runtime auth = %+v, want environment-scoped credential", auth)
	}
}

func TestKimiRefreshLockUsesOfficialPathAndRecoversStaleLock(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "oauth", "kimi-code")
	lockPath := target + ".lock"
	if err := os.MkdirAll(lockPath, 0o700); err != nil {
		t.Fatalf("create stale lock: %v", err)
	}
	stale := time.Now().Add(-2 * kimiRefreshLockStaleAfter)
	if err := os.Chtimes(lockPath, stale, stale); err != nil {
		t.Fatalf("age stale lock: %v", err)
	}

	lock, err := acquireKimiRefreshLock(context.Background(), home, "kimi-code")
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("official sentinel %s missing: %v", target, err)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("official lock directory %s missing: %v", lockPath, err)
	}
	lock.release()
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("lock directory exists after release (err = %v)", err)
	}
}

func newKimiTestProvider(home string, client *http.Client, now func() time.Time) *kimiProvider {
	return &kimiProvider{home: home, client: client, now: now}
}

func writeKimiQuotaHome(t *testing.T, baseURL, oauthHost string, token kimiOAuthToken) string {
	t.Helper()
	t.Setenv("KIMI_CODE_BASE_URL", "")
	t.Setenv("KIMI_CODE_OAUTH_HOST", "")
	t.Setenv("KIMI_OAUTH_HOST", "")

	home := t.TempDir()
	key := kimiOAuthKey(oauthHost, baseURL)
	config := fmt.Sprintf(`[providers."managed:kimi-code"]
type = "kimi"
base_url = %q

[providers."managed:kimi-code".oauth]
storage = "file"
key = %q
oauth_host = %q
`, baseURL, key, oauthHost)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	auth, err := resolveKimiRuntimeAuth(home)
	if err != nil {
		t.Fatalf("resolve auth fixture: %v", err)
	}
	tokenPath, err := kimiCredentialPath(home, auth)
	if err != nil {
		t.Fatalf("credential path fixture: %v", err)
	}
	if token.raw == nil {
		token.raw = map[string]any{}
	}
	if err := writeKimiOAuthToken(tokenPath, token); err != nil {
		t.Fatalf("write OAuth fixture: %v", err)
	}
	return home
}

func assertKimiWindow(
	t *testing.T,
	got Window,
	wantID, wantLabel string,
	wantPercent float64,
	wantMinutes, wantReset int64,
) {
	t.Helper()
	if got.ID != wantID || got.Label != wantLabel || !approxEqual(got.UsedPercent, wantPercent) ||
		got.WindowMinutes != wantMinutes || got.ResetsAt != wantReset {
		t.Errorf("window = %+v, want id=%q label=%q used=%.2f minutes=%d reset=%d",
			got, wantID, wantLabel, wantPercent, wantMinutes, wantReset)
	}
}

func infoMode(info os.FileInfo) os.FileMode {
	if info == nil {
		return 0
	}
	return info.Mode().Perm()
}
