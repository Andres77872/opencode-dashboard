package quota

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	if info, err := os.Stat(filepath.Dir(tokenPath)); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("credential directory mode = %v, err = %v; want 0700", infoMode(info), err)
	}
	if entries, err := os.ReadDir(filepath.Dir(tokenPath)); err != nil {
		t.Errorf("read credential directory: %v", err)
	} else {
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".kimi-code-token-") {
				t.Errorf("atomic credential temp file remains after refresh: %s", entry.Name())
			}
		}
	}
	storageName, _ := kimiTokenStorageName(auth.OAuthKey)
	if _, err := os.Stat(filepath.Join(home, "oauth", storageName+".lock")); !os.IsNotExist(err) {
		t.Errorf("refresh lock remains after request (err = %v)", err)
	}
	if _, err := os.Stat(filepath.Join(home, "oauth", storageName)); err != nil {
		t.Errorf("Kimi-compatible lock sentinel is missing: %v", err)
	}
}

func TestKimiQuotaInvalidGrantOnBadRequestTombstonesCredential(t *testing.T) {
	now := time.Date(2026, 7, 16, 18, 0, 0, 0, time.UTC)
	const accessToken = "revoked-secret-access"
	const refreshToken = "revoked-secret-refresh"
	var refreshCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/oauth/token" {
			t.Errorf("path = %q, want /api/oauth/token", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		refreshCalls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":"invalid_grant","error_description":"rejected %s and %s"}`,
			accessToken, refreshToken)
	}))
	defer server.Close()

	home := writeKimiQuotaHome(t, server.URL+"/coding/v1", server.URL, kimiOAuthToken{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    now.Add(-time.Minute).Unix(),
		ExpiresIn:    900,
		Scope:        "kimi-code",
		TokenType:    "Bearer",
		raw:          map[string]any{"preserved": "metadata"},
	})
	provider := newKimiTestProvider(home, server.Client(), func() time.Time { return now })
	provider.sleep = noKimiTestWait
	got := provider.quota(context.Background())

	if got.Status != StatusUnavailable || got.Help == "" {
		t.Fatalf("quota = %+v, want unavailable with login help", got)
	}
	if refreshCalls.Load() != 1 {
		t.Errorf("refresh calls = %d, want invalid_grant to stop after one attempt", refreshCalls.Load())
	}
	if strings.Contains(got.Reason, accessToken) || strings.Contains(got.Reason, refreshToken) ||
		!strings.Contains(got.Reason, "[redacted]") {
		t.Errorf("reason = %q, want both OAuth secrets redacted", got.Reason)
	}

	tokenPath := kimiTestTokenPath(t, home)
	if _, err := readKimiOAuthToken(tokenPath); !errors.Is(err, errKimiAuthRequired) {
		t.Fatalf("read tombstone error = %v, want errKimiAuthRequired", err)
	}
	body, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("read tombstone: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("parse tombstone: %v", err)
	}
	_, preserved := raw["preserved"]
	if raw["access_token"] != "" || raw["refresh_token"] != "" ||
		raw["expires_at"] != float64(0) || raw["expires_in"] != float64(0) ||
		raw["scope"] != "kimi-code" || raw["token_type"] != "Bearer" || preserved {
		t.Errorf("tombstone = %#v, want only empty token fields plus scope and type", raw)
	}
	if info, err := os.Stat(tokenPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("tombstone mode = %v, err = %v; want 0600", infoMode(info), err)
	}
	if entries, err := os.ReadDir(filepath.Dir(tokenPath)); err != nil {
		t.Errorf("read credential directory: %v", err)
	} else {
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".kimi-code-token-") {
				t.Errorf("atomic tombstone temp file remains: %s", entry.Name())
			}
		}
	}
}

func TestKimiQuotaGenericUnauthorizedRefreshPreservesCredential(t *testing.T) {
	now := time.Date(2026, 7, 16, 18, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/oauth/token" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"access_denied","error_description":"account policy requires attention"}`)
	}))
	defer server.Close()

	home := writeKimiQuotaHome(t, server.URL+"/coding/v1", server.URL, kimiOAuthToken{
		AccessToken:  "preserved-access",
		RefreshToken: "preserved-refresh",
		ExpiresAt:    now.Add(-time.Minute).Unix(),
		ExpiresIn:    900,
		Scope:        "kimi-code",
		TokenType:    "Bearer",
	})
	provider := newKimiTestProvider(home, server.Client(), func() time.Time { return now })
	provider.sleep = noKimiTestWait
	got := provider.quota(context.Background())
	if got.Status != StatusUnavailable || got.Help == "" {
		t.Fatalf("quota = %+v, want unavailable with login help", got)
	}

	saved, err := readKimiOAuthToken(kimiTestTokenPath(t, home))
	if err != nil {
		t.Fatalf("read preserved credential: %v", err)
	}
	if saved.AccessToken != "preserved-access" || saved.RefreshToken != "preserved-refresh" {
		t.Errorf("credential was destructively changed after generic 401: %+v", saved)
	}
}

func TestKimiQuotaUsesPeerRefreshRotationAfterRejectedRequest(t *testing.T) {
	now := time.Date(2026, 7, 16, 18, 0, 0, 0, time.UTC)
	var tokenPath string
	var refreshCalls atomic.Int64
	var usageCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/oauth/token":
			refreshCalls.Add(1)
			peer := kimiOAuthToken{
				AccessToken:  "peer-access",
				RefreshToken: "peer-refresh",
				ExpiresAt:    now.Add(time.Hour).Unix(),
				ExpiresIn:    900,
				Scope:        "kimi-code",
				TokenType:    "Bearer",
				raw:          map[string]any{},
			}
			if err := writeKimiOAuthToken(tokenPath, peer); err != nil {
				t.Errorf("write peer-rotated token: %v", err)
			}
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":"invalid_grant","error_description":"old refresh token"}`)
		case "/coding/v1/usages":
			usageCalls.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer peer-access" {
				t.Errorf("usage Authorization = %q, want peer access token", got)
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
	tokenPath = kimiTestTokenPath(t, home)
	provider := newKimiTestProvider(home, server.Client(), func() time.Time { return now })
	provider.sleep = noKimiTestWait
	got := provider.quota(context.Background())

	if got.Status != StatusOK {
		t.Fatalf("quota = %+v, want peer token recovery", got)
	}
	if refreshCalls.Load() != 1 || usageCalls.Load() != 1 {
		t.Errorf("refresh/usage calls = %d/%d, want 1/1", refreshCalls.Load(), usageCalls.Load())
	}
	saved, err := readKimiOAuthToken(tokenPath)
	if err != nil {
		t.Fatalf("read peer token: %v", err)
	}
	if saved.AccessToken != "peer-access" || saved.RefreshToken != "peer-refresh" {
		t.Errorf("saved peer token = %+v, want peer rotation left intact", saved)
	}
}

func TestKimiOAuthRefreshRetriesTransientFailures(t *testing.T) {
	now := time.Date(2026, 7, 16, 18, 0, 0, 0, time.UTC)
	current := kimiOAuthToken{
		AccessToken:  "secret-access",
		RefreshToken: "secret-refresh",
		Scope:        "kimi-code",
		TokenType:    "Bearer",
	}

	t.Run("retryable statuses then success", func(t *testing.T) {
		var calls atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			switch calls.Add(1) {
			case 1:
				http.Error(w, "rate limited", http.StatusTooManyRequests)
			case 2:
				http.Error(w, "gateway timeout", http.StatusGatewayTimeout)
			default:
				fmt.Fprint(w, `{"access_token":"new-access","refresh_token":"new-refresh","expires_in":900}`)
			}
		}))
		defer server.Close()

		var backoffs []time.Duration
		provider := newKimiTestProvider("", server.Client(), func() time.Time { return now })
		provider.sleep = func(_ context.Context, delay time.Duration) error {
			backoffs = append(backoffs, delay)
			return nil
		}
		got, err := provider.refreshKimiOAuthToken(context.Background(), server.URL, current)
		if err != nil {
			t.Fatalf("refresh: %v", err)
		}
		if got.AccessToken != "new-access" || got.RefreshToken != "new-refresh" {
			t.Errorf("refreshed token = %+v", got)
		}
		assertKimiBackoffs(t, backoffs)
		if calls.Load() != kimiRefreshMaxAttempts {
			t.Errorf("refresh calls = %d, want %d", calls.Load(), kimiRefreshMaxAttempts)
		}
	})

	t.Run("transport failures exhaust attempts safely", func(t *testing.T) {
		var calls atomic.Int64
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return nil, fmt.Errorf("transport reflected %s and %s", current.AccessToken, current.RefreshToken)
		})}
		var backoffs []time.Duration
		provider := newKimiTestProvider("", client, func() time.Time { return now })
		provider.sleep = func(_ context.Context, delay time.Duration) error {
			backoffs = append(backoffs, delay)
			return nil
		}
		_, err := provider.refreshKimiOAuthToken(context.Background(), "https://auth.example.test", current)
		if err == nil {
			t.Fatal("refresh error = nil, want exhausted transport failure")
		}
		if calls.Load() != kimiRefreshMaxAttempts {
			t.Errorf("refresh calls = %d, want %d", calls.Load(), kimiRefreshMaxAttempts)
		}
		assertKimiBackoffs(t, backoffs)
		if strings.Contains(err.Error(), current.AccessToken) || strings.Contains(err.Error(), current.RefreshToken) ||
			!strings.Contains(err.Error(), "[redacted]") {
			t.Errorf("refresh error = %q, want secrets redacted", err)
		}
	})

	for _, status := range []int{429, 500, 502, 503, 504} {
		if !kimiRefreshStatusRetryable(status) {
			t.Errorf("status %d should be retryable", status)
		}
	}
	for _, status := range []int{400, 401, 403, 404} {
		if kimiRefreshStatusRetryable(status) {
			t.Errorf("status %d should not be retryable", status)
		}
	}
}

func TestKimiQuotaUsesRefreshAndUsageTimeoutBudgets(t *testing.T) {
	now := time.Date(2026, 7, 16, 18, 0, 0, 0, time.UTC)
	var refreshSeen atomic.Bool
	var usageSeen atomic.Bool
	client := &http.Client{
		// The Kimi operation contexts, rather than this generic inherited
		// timeout, must control both requests.
		Timeout: time.Nanosecond,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			deadline, ok := req.Context().Deadline()
			if !ok {
				t.Errorf("%s request has no context deadline", req.URL.Path)
			}
			remaining := time.Until(deadline)
			switch req.URL.Path {
			case "/api/oauth/token":
				refreshSeen.Store(true)
				if remaining < 29*time.Second || remaining > kimiRefreshBudget {
					t.Errorf("refresh deadline remaining = %v, want about %v", remaining, kimiRefreshBudget)
				}
				return kimiTestHTTPResponse(req, http.StatusOK,
					`{"access_token":"new-access","refresh_token":"new-refresh","expires_in":900}`), nil
			case "/coding/v1/usages":
				usageSeen.Store(true)
				if remaining < 7*time.Second || remaining > kimiUsageTimeout {
					t.Errorf("usage deadline remaining = %v, want about %v", remaining, kimiUsageTimeout)
				}
				return kimiTestHTTPResponse(req, http.StatusOK,
					`{"usage":{"used":1,"limit":10,"name":"Weekly limit"}}`), nil
			default:
				return kimiTestHTTPResponse(req, http.StatusNotFound, `{}`), nil
			}
		}),
	}

	home := writeKimiQuotaHome(t,
		"https://api.example.test/coding/v1",
		"https://auth.example.test",
		kimiOAuthToken{
			AccessToken:  "old-access",
			RefreshToken: "old-refresh",
			ExpiresAt:    now.Add(-time.Minute).Unix(),
			ExpiresIn:    900,
		},
	)
	provider := newKimiTestProvider(home, client, func() time.Time { return now })
	got := provider.quota(context.Background())
	if got.Status != StatusOK {
		t.Fatalf("quota = %+v, want ok", got)
	}
	if !refreshSeen.Load() || !usageSeen.Load() {
		t.Errorf("refresh/usage observed = %v/%v, want both", refreshSeen.Load(), usageSeen.Load())
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
	if info, err := os.Stat(filepath.Dir(target)); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("OAuth lock directory mode = %v, err = %v; want 0700", infoMode(info), err)
	}
	if info, err := os.Stat(target); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("OAuth lock sentinel mode = %v, err = %v; want 0600", infoMode(info), err)
	}
	lock.release()
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("lock directory exists after release (err = %v)", err)
	}
}

func TestKimiRefreshLockHonorsContextTimeout(t *testing.T) {
	home := t.TempDir()
	first, err := acquireKimiRefreshLock(context.Background(), home, "kimi-code")
	if err != nil {
		t.Fatalf("acquire first lock: %v", err)
	}
	defer first.release()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	second, err := acquireKimiRefreshLock(ctx, home, "kimi-code")
	if second != nil {
		second.release()
		t.Fatal("second lock unexpectedly acquired")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second lock error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Errorf("lock timeout took %v, want prompt context cancellation", elapsed)
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

func kimiTestTokenPath(t *testing.T, home string) string {
	t.Helper()
	auth, err := resolveKimiRuntimeAuth(home)
	if err != nil {
		t.Fatalf("resolve Kimi auth: %v", err)
	}
	path, err := kimiCredentialPath(home, auth)
	if err != nil {
		t.Fatalf("resolve Kimi credential path: %v", err)
	}
	return path
}

func noKimiTestWait(context.Context, time.Duration) error {
	return nil
}

func assertKimiBackoffs(t *testing.T, got []time.Duration) {
	t.Helper()
	want := []time.Duration{time.Second, 2 * time.Second}
	if len(got) != len(want) {
		t.Fatalf("backoffs = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("backoffs[%d] = %v, want %v", index, got[index], want[index])
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func kimiTestHTTPResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
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
