package quota

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	kimiDefaultOAuthHost       = "https://auth.kimi.com"
	kimiOAuthClientID          = "17e5f671-d194-4dfb-9706-5516cb48c098"
	kimiDefaultOAuthKey        = "oauth/kimi-code"
	kimiCredentialFileMaxBytes = 1 << 20
	kimiRefreshBudget          = 30 * time.Second
	kimiRefreshMaxAttempts     = 3
	kimiRefreshRetryBase       = time.Second
	kimiPeerRotationDelay      = 100 * time.Millisecond
	kimiRefreshLockStaleAfter  = 5 * time.Second
	kimiRefreshLockHeartbeat   = time.Second
	kimiRefreshLockRetry       = 100 * time.Millisecond
)

var (
	errKimiTokenMissing     = errors.New("Kimi Code OAuth credential is missing")
	errKimiAuthRequired     = errors.New("Kimi Code OAuth login is required")
	errKimiConfirmedRevoked = errors.New("Kimi Code OAuth refresh token was rejected as invalid_grant")
)

// ResolveKimiAssistantCredential reuses Kimi Code's managed OAuth storage and
// refresh protocol for the analytics assistant. The access token is returned
// only to the server-side provider registry and must never be logged or stored
// in dashboard-settings.sqlite.
func ResolveKimiAssistantCredential(ctx context.Context, home string) (string, string, error) {
	home = strings.TrimSpace(home)
	if home == "" {
		return "", "", errKimiTokenMissing
	}
	auth, err := resolveKimiRuntimeAuth(home)
	if err != nil {
		return "", "", err
	}
	path, err := kimiCredentialPath(home, auth)
	if err != nil {
		return "", "", err
	}
	token, err := readKimiOAuthToken(path)
	if err != nil {
		return "", "", err
	}
	provider := &kimiProvider{home: home, now: time.Now}
	accessToken, err := provider.ensureFreshAccessToken(ctx, auth, path, token, time.Now())
	if err != nil {
		return "", "", err
	}
	return auth.BaseURL, accessToken, nil
}

type kimiRuntimeAuth struct {
	BaseURL   string
	OAuthHost string
	OAuthKey  string
	Storage   string
}

type kimiConfigFile struct {
	Providers map[string]kimiProviderConfig `toml:"providers"`
}

type kimiProviderConfig struct {
	BaseURL string              `toml:"base_url"`
	OAuth   *kimiOAuthReference `toml:"oauth"`
}

type kimiOAuthReference struct {
	Storage   string `toml:"storage"`
	Key       string `toml:"key"`
	OAuthHost string `toml:"oauth_host"`
}

func resolveKimiRuntimeAuth(home string) (kimiRuntimeAuth, error) {
	configuredBaseURL := ""
	var configuredOAuth *kimiOAuthReference
	configPath := filepath.Join(home, "config.toml")
	if _, err := os.Stat(configPath); err == nil {
		var config kimiConfigFile
		if _, err := toml.DecodeFile(configPath, &config); err != nil {
			return kimiRuntimeAuth{}, fmt.Errorf("parse Kimi Code config: %w", err)
		}
		if provider, ok := config.Providers["managed:kimi-code"]; ok {
			configuredBaseURL = strings.TrimSpace(provider.BaseURL)
			configuredOAuth = provider.OAuth
		}
	} else if !os.IsNotExist(err) {
		return kimiRuntimeAuth{}, fmt.Errorf("inspect Kimi Code config: %w", err)
	}

	envBaseURL, hasEnvBaseURL := nonEmptyEnv("KIMI_CODE_BASE_URL")
	envOAuthHost, hasEnvOAuthHost := nonEmptyEnv("KIMI_CODE_OAUTH_HOST")
	if !hasEnvOAuthHost {
		envOAuthHost, hasEnvOAuthHost = nonEmptyEnv("KIMI_OAUTH_HOST")
	}
	hasEnvironmentOverride := hasEnvBaseURL || hasEnvOAuthHost

	baseURL := configuredBaseURL
	if hasEnvBaseURL {
		baseURL = envBaseURL
	}
	if baseURL == "" {
		baseURL = kimiDefaultBaseURL
	}
	baseURL = normalizeEndpoint(baseURL)
	if err := validateKimiEndpoint(baseURL); err != nil {
		return kimiRuntimeAuth{}, fmt.Errorf("invalid Kimi Code managed base URL %q", baseURL)
	}

	configuredHost := ""
	if configuredOAuth != nil {
		configuredHost = configuredOAuth.OAuthHost
	}
	oauthHost := configuredHost
	if hasEnvOAuthHost {
		oauthHost = envOAuthHost
	}
	if oauthHost == "" {
		oauthHost = kimiDefaultOAuthHost
	}
	oauthHost = normalizeEndpoint(oauthHost)
	if err := validateKimiEndpoint(oauthHost); err != nil {
		return kimiRuntimeAuth{}, fmt.Errorf("invalid Kimi Code OAuth host %q", oauthHost)
	}

	expectedKey := kimiOAuthKey(oauthHost, baseURL)
	auth := kimiRuntimeAuth{
		BaseURL:   baseURL,
		OAuthHost: oauthHost,
		OAuthKey:  expectedKey,
		Storage:   "file",
	}
	if configuredOAuth == nil || strings.TrimSpace(configuredOAuth.Key) == "" || hasEnvironmentOverride {
		return auth, nil
	}
	configuredKey := strings.TrimSpace(configuredOAuth.Key)
	if configuredKey != expectedKey {
		return auth, nil
	}
	auth.OAuthKey = configuredKey
	if storage := strings.TrimSpace(configuredOAuth.Storage); storage != "" {
		auth.Storage = storage
	}
	return auth, nil
}

func nonEmptyEnv(name string) (string, bool) {
	value, ok := os.LookupEnv(name)
	value = strings.TrimSpace(value)
	return value, ok && value != ""
}

func normalizeEndpoint(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}

func validateKimiEndpoint(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return err
	}
	if (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("endpoint must be an absolute HTTP(S) URL without credentials, query, or fragment")
	}
	return nil
}

func kimiOAuthKey(oauthHost, baseURL string) string {
	oauthHost = normalizeEndpoint(oauthHost)
	baseURL = normalizeEndpoint(baseURL)
	if oauthHost == kimiDefaultOAuthHost && baseURL == kimiDefaultBaseURL {
		return kimiDefaultOAuthKey
	}
	// Match Kimi Code's JSON.stringify({ oauthHost, baseUrl }) key derivation.
	payload := struct {
		OAuthHost string `json:"oauthHost"`
		BaseURL   string `json:"baseUrl"`
	}{OAuthHost: oauthHost, BaseURL: baseURL}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return "oauth/kimi-code-env-" + hex.EncodeToString(sum[:])[:16]
}

func kimiCredentialPath(home string, auth kimiRuntimeAuth) (string, error) {
	if auth.Storage != "" && auth.Storage != "file" {
		return "", fmt.Errorf("Kimi Code OAuth storage %q is not readable by the dashboard", auth.Storage)
	}
	name, err := kimiTokenStorageName(auth.OAuthKey)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "credentials", name+".json"), nil
}

func kimiTokenStorageName(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" || key == "kimi-code" || key == kimiDefaultOAuthKey {
		return "kimi-code", nil
	}
	if strings.HasPrefix(key, "oauth/") {
		key = strings.TrimPrefix(key, "oauth/")
	}
	if key == "" || key == "." || key == ".." || strings.ContainsAny(key, `/\`) || strings.HasPrefix(key, ".") {
		return "", fmt.Errorf("invalid Kimi Code OAuth credential key %q", key)
	}
	return key, nil
}

type kimiOAuthToken struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    int64
	Scope        string
	TokenType    string
	ExpiresIn    int64
	raw          map[string]any
}

func readKimiOAuthToken(path string) (kimiOAuthToken, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return kimiOAuthToken{}, errKimiTokenMissing
		}
		return kimiOAuthToken{}, fmt.Errorf("read Kimi Code OAuth credential: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(io.LimitReader(file, kimiCredentialFileMaxBytes))
	decoder.UseNumber()
	raw := make(map[string]any)
	if err := decoder.Decode(&raw); err != nil {
		return kimiOAuthToken{}, fmt.Errorf("parse Kimi Code OAuth credential: %w", err)
	}
	token := kimiOAuthToken{
		AccessToken:  firstNonEmptyString(raw["access_token"]),
		RefreshToken: firstNonEmptyString(raw["refresh_token"]),
		Scope:        firstNonEmptyString(raw["scope"]),
		TokenType:    firstNonEmptyString(raw["token_type"]),
		raw:          raw,
	}
	if value, ok := numericValue(raw["expires_at"]); ok {
		token.ExpiresAt = int64(value)
	}
	if value, ok := numericValue(raw["expires_in"]); ok {
		token.ExpiresIn = int64(value)
	}
	if token.AccessToken == "" {
		return kimiOAuthToken{}, fmt.Errorf("%w: stored credential was revoked or is empty", errKimiAuthRequired)
	}
	return token, nil
}

func (p *kimiProvider) ensureFreshAccessToken(
	ctx context.Context,
	auth kimiRuntimeAuth,
	tokenPath string,
	token kimiOAuthToken,
	now time.Time,
) (string, error) {
	if !kimiTokenNeedsRefresh(token, now) {
		return token.AccessToken, nil
	}
	if token.RefreshToken == "" {
		return "", fmt.Errorf("%w: stored credential has no refresh token", errKimiAuthRequired)
	}

	storageName, err := kimiTokenStorageName(auth.OAuthKey)
	if err != nil {
		return "", err
	}
	refreshCtx, cancel := context.WithTimeout(ctx, kimiRefreshBudget)
	defer cancel()

	lock, err := acquireKimiRefreshLock(refreshCtx, p.home, storageName)
	if err != nil {
		return "", fmt.Errorf("coordinate Kimi Code OAuth refresh: %w", err)
	}
	defer lock.release()

	// A running Kimi process may have refreshed while this process waited.
	latest, err := readKimiOAuthToken(tokenPath)
	if err == nil {
		if !kimiTokenNeedsRefresh(latest, now) {
			return latest.AccessToken, nil
		}
		token = latest
	} else if !errors.Is(err, errKimiTokenMissing) {
		return "", err
	}

	refreshed, err := p.refreshKimiOAuthToken(refreshCtx, auth.OAuthHost, token)
	if err != nil {
		if errors.Is(err, errKimiAuthRequired) {
			// A peer can rotate the refresh token between our request and the
			// rejection. Re-read once before declaring this credential revoked.
			if waitErr := p.waitKimiOAuth(refreshCtx, kimiPeerRotationDelay); waitErr != nil {
				return "", waitErr
			}
			recovery, readErr := readKimiOAuthToken(tokenPath)
			if readErr == nil && recovery.RefreshToken != "" && recovery.RefreshToken != token.RefreshToken {
				return recovery.AccessToken, nil
			}
			if !errors.Is(err, errKimiConfirmedRevoked) {
				// A generic 401/403 can represent policy, client, or service
				// rejection; it requires login attention but does not prove that
				// the refresh credential itself was revoked. Preserve it intact.
				return "", err
			}

			// No peer rotation was persisted, so retain an atomic on-disk marker
			// that distinguishes a rejected login from a never-configured one.
			if writeErr := writeKimiOAuthToken(tokenPath, revokedKimiOAuthToken(token)); writeErr != nil {
				return "", fmt.Errorf("%w: OAuth refresh was rejected and the revoked credential could not be recorded: %v",
					errKimiAuthRequired, writeErr)
			}
		}
		return "", err
	}
	if err := writeKimiOAuthToken(tokenPath, refreshed); err != nil {
		return "", err
	}
	return refreshed.AccessToken, nil
}

func (p *kimiProvider) waitKimiOAuth(ctx context.Context, delay time.Duration) error {
	if p.sleep != nil {
		return p.sleep(ctx, delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func revokedKimiOAuthToken(prior kimiOAuthToken) kimiOAuthToken {
	return kimiOAuthToken{
		Scope:     prior.Scope,
		TokenType: prior.TokenType,
	}
}

func kimiTokenNeedsRefresh(token kimiOAuthToken, now time.Time) bool {
	if token.ExpiresAt == 0 {
		return false
	}
	threshold := int64(300)
	if halfLife := token.ExpiresIn / 2; halfLife > threshold {
		threshold = halfLife
	}
	return token.ExpiresAt-now.Unix() < threshold
}

func (p *kimiProvider) refreshKimiOAuthToken(
	ctx context.Context,
	oauthHost string,
	current kimiOAuthToken,
) (kimiOAuthToken, error) {
	form := url.Values{
		"client_id":     {kimiOAuthClientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {current.RefreshToken},
	}
	endpoint := strings.TrimRight(oauthHost, "/") + "/api/oauth/token"
	encodedForm := form.Encode()
	client := credentialSafeClient(p.client)
	var lastErr error

	for attempt := 0; attempt < kimiRefreshMaxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(encodedForm))
		if err != nil {
			return kimiOAuthToken{}, fmt.Errorf("build Kimi Code OAuth refresh request: %w", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "")

		resp, err := client.Do(req)
		if err != nil {
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			lastErr = fmt.Errorf("refresh Kimi Code OAuth credential: %s",
				redactKimiSecrets(err.Error(), current.AccessToken, current.RefreshToken))
			if attempt == kimiRefreshMaxAttempts-1 {
				return kimiOAuthToken{}, lastErr
			}
			if waitErr := p.waitKimiOAuth(ctx, kimiRefreshRetryBase<<attempt); waitErr != nil {
				return kimiOAuthToken{}, fmt.Errorf("wait to retry Kimi Code OAuth refresh: %w", waitErr)
			}
			continue
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, kimiCredentialFileMaxBytes))
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("read Kimi Code OAuth refresh response: %s",
				redactKimiSecrets(readErr.Error(), current.AccessToken, current.RefreshToken))
			if attempt == kimiRefreshMaxAttempts-1 {
				return kimiOAuthToken{}, lastErr
			}
			if waitErr := p.waitKimiOAuth(ctx, kimiRefreshRetryBase<<attempt); waitErr != nil {
				return kimiOAuthToken{}, fmt.Errorf("wait to retry Kimi Code OAuth refresh: %w", waitErr)
			}
			continue
		}

		if resp.StatusCode != http.StatusOK {
			message := kimiAPIErrorMessage(body, current.AccessToken, current.RefreshToken)
			if message == "" {
				message = http.StatusText(resp.StatusCode)
			}
			if strings.EqualFold(kimiOAuthErrorCode(body), "invalid_grant") {
				return kimiOAuthToken{}, fmt.Errorf("%w: %w: %s (status %d)",
					errKimiAuthRequired, errKimiConfirmedRevoked, message, resp.StatusCode)
			}
			if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
				return kimiOAuthToken{}, fmt.Errorf("%w: %s (status %d)",
					errKimiAuthRequired, message, resp.StatusCode)
			}

			lastErr = fmt.Errorf("Kimi Code OAuth refresh failed: %s (status %d)", message, resp.StatusCode)
			if !kimiRefreshStatusRetryable(resp.StatusCode) || attempt == kimiRefreshMaxAttempts-1 {
				return kimiOAuthToken{}, lastErr
			}
			if waitErr := p.waitKimiOAuth(ctx, kimiRefreshRetryBase<<attempt); waitErr != nil {
				return kimiOAuthToken{}, fmt.Errorf("wait to retry Kimi Code OAuth refresh: %w", waitErr)
			}
			continue
		}

		decoder := json.NewDecoder(strings.NewReader(string(body)))
		decoder.UseNumber()
		raw := make(map[string]any)
		if err := decoder.Decode(&raw); err != nil {
			return kimiOAuthToken{}, fmt.Errorf("parse Kimi Code OAuth refresh response: %w", err)
		}
		accessToken := firstNonEmptyString(raw["access_token"])
		refreshToken := firstNonEmptyString(raw["refresh_token"])
		expiresIn, hasExpiresIn := numericValue(raw["expires_in"])
		if accessToken == "" || refreshToken == "" || !hasExpiresIn || expiresIn <= 0 {
			return kimiOAuthToken{}, errors.New("Kimi Code OAuth refresh response is missing required token fields")
		}
		scope := firstNonEmptyString(raw["scope"])
		if scope == "" {
			scope = current.Scope
		}
		tokenType := firstNonEmptyString(raw["token_type"])
		if tokenType == "" {
			tokenType = "Bearer"
		}
		return kimiOAuthToken{
			AccessToken:  accessToken,
			RefreshToken: refreshToken,
			ExpiresAt:    p.now().Unix() + int64(expiresIn),
			Scope:        scope,
			TokenType:    tokenType,
			ExpiresIn:    int64(expiresIn),
			raw:          current.raw,
		}, nil
	}

	return kimiOAuthToken{}, lastErr
}

func kimiRefreshStatusRetryable(status int) bool {
	switch status {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func kimiOAuthErrorCode(body []byte) string {
	var payload map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	if err := decoder.Decode(&payload); err != nil {
		return ""
	}
	if code, ok := payload["error"].(string); ok {
		return strings.TrimSpace(code)
	}
	if nested, ok := payload["error"].(map[string]any); ok {
		return firstNonEmptyString(nested["code"], nested["type"], nested["error"])
	}
	return ""
}

func writeKimiOAuthToken(path string, token kimiOAuthToken) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create Kimi Code credential directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("secure Kimi Code credential directory: %w", err)
	}

	raw := make(map[string]any, len(token.raw)+6)
	for key, value := range token.raw {
		raw[key] = value
	}
	raw["access_token"] = token.AccessToken
	raw["refresh_token"] = token.RefreshToken
	raw["expires_at"] = token.ExpiresAt
	raw["scope"] = token.Scope
	raw["token_type"] = token.TokenType
	raw["expires_in"] = token.ExpiresIn

	body, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Kimi Code OAuth credential: %w", err)
	}
	body = append(body, '\n')
	tmp, err := os.CreateTemp(dir, ".kimi-code-token-*.tmp")
	if err != nil {
		return fmt.Errorf("create Kimi Code OAuth temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return fmt.Errorf("secure Kimi Code OAuth temp file: %w", err)
	}
	if _, err := tmp.Write(body); err != nil {
		cleanup()
		return fmt.Errorf("write Kimi Code OAuth credential: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync Kimi Code OAuth credential: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close Kimi Code OAuth credential: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace Kimi Code OAuth credential: %w", err)
	}
	return nil
}

type kimiRefreshLock struct {
	path     string
	stopOnce sync.Once
	stop     chan struct{}
	done     chan struct{}
}

func acquireKimiRefreshLock(ctx context.Context, home, storageName string) (*kimiRefreshLock, error) {
	dir := filepath.Join(home, "oauth")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	target := filepath.Join(dir, storageName)
	file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	if err := os.Chmod(target, 0o600); err != nil {
		return nil, err
	}
	lockPath := target + ".lock"

	for {
		err := os.Mkdir(lockPath, 0o700)
		if err == nil {
			now := time.Now()
			_ = os.Chtimes(lockPath, now, now)
			lock := &kimiRefreshLock{
				path: lockPath,
				stop: make(chan struct{}),
				done: make(chan struct{}),
			}
			go lock.heartbeat()
			return lock, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		info, statErr := os.Stat(lockPath)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				continue
			}
			return nil, statErr
		}
		if time.Since(info.ModTime()) > kimiRefreshLockStaleAfter {
			removeErr := os.Remove(lockPath)
			if removeErr == nil || os.IsNotExist(removeErr) {
				continue
			}
			return nil, fmt.Errorf("remove stale Kimi OAuth lock: %w", removeErr)
		}

		timer := time.NewTimer(kimiRefreshLockRetry)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (l *kimiRefreshLock) heartbeat() {
	ticker := time.NewTicker(kimiRefreshLockHeartbeat)
	defer ticker.Stop()
	defer close(l.done)
	for {
		select {
		case <-l.stop:
			return
		case timestamp := <-ticker.C:
			_ = os.Chtimes(l.path, timestamp, timestamp)
		}
	}
}

func (l *kimiRefreshLock) release() {
	if l == nil {
		return
	}
	l.stopOnce.Do(func() {
		close(l.stop)
		<-l.done
		_ = os.Remove(l.path)
	})
}
