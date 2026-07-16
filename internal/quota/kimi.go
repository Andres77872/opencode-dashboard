package quota

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	kimiDefaultBaseURL = "https://api.kimi.com/coding/v1"
	kimiTTL            = 60 * time.Second
	kimiSetupHelp      = `Run "kimi login" (or /login inside Kimi Code), then refresh quotas. Membership quota is available for the managed Kimi Code OAuth account.`
)

var quotaDurationPattern = regexp.MustCompile(`(?i)(\d+)\s*(month|minute|min|week|hour|day|m|h|d|w)\b`)

type kimiProvider struct {
	home   string
	client *http.Client
	now    func() time.Time

	mu        sync.Mutex
	cached    *ProviderQuota
	fetchedAt time.Time
	lastGood  *ProviderQuota
}

func (p *kimiProvider) quota(ctx context.Context) ProviderQuota {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := p.now()
	if p.cached != nil && now.Sub(p.fetchedAt) < kimiTTL {
		return cloneProviderQuota(*p.cached)
	}

	result := p.fetch(ctx, now)
	p.cached = &result
	p.fetchedAt = now
	return cloneProviderQuota(result)
}

func (p *kimiProvider) fetch(ctx context.Context, now time.Time) ProviderQuota {
	result := ProviderQuota{Provider: ProviderKimi, Label: "Kimi Code"}
	if strings.TrimSpace(p.home) == "" {
		result.Status = StatusUnavailable
		result.Reason = "Kimi Code home is not configured"
		result.Help = kimiSetupHelp
		return result
	}

	auth, err := resolveKimiRuntimeAuth(p.home)
	if err != nil {
		return p.failed(result, err)
	}
	tokenPath, err := kimiCredentialPath(p.home, auth)
	if err != nil {
		return p.failed(result, err)
	}
	token, err := readKimiOAuthToken(tokenPath)
	if err != nil {
		if errors.Is(err, errKimiTokenMissing) {
			result.Status = StatusUnavailable
			result.Reason = "no Kimi Code OAuth credential found"
			result.Help = kimiSetupHelp
			return result
		}
		return p.failed(result, err)
	}

	accessToken, err := p.ensureFreshAccessToken(ctx, auth, tokenPath, token, now)
	if err != nil {
		return p.failed(result, err)
	}
	payload, err := p.fetchUsage(ctx, auth.BaseURL, accessToken)
	if err != nil {
		return p.failed(result, err)
	}

	parsed := parseKimiUsagePayload(payload, now)
	result.Windows = parsed.Windows
	result.ExtraUsage = parsed.ExtraUsage
	if len(result.Windows) == 0 && result.ExtraUsage == nil {
		result.Status = StatusUnavailable
		result.Reason = "Kimi Code usage response contained no quota data"
		return result
	}

	result.AsOfMS = now.UnixMilli()
	result.Status = StatusOK
	good := cloneProviderQuota(result)
	p.lastGood = &good
	return result
}

func (p *kimiProvider) failed(empty ProviderQuota, err error) ProviderQuota {
	if p.lastGood != nil {
		stale := cloneProviderQuota(*p.lastGood)
		stale.Status = StatusStale
		stale.Reason = err.Error()
		return stale
	}
	empty.Status = StatusUnavailable
	empty.Reason = err.Error()
	if errors.Is(err, errKimiAuthRequired) || errors.Is(err, errKimiTokenMissing) {
		empty.Help = kimiSetupHelp
	}
	return empty
}

func (p *kimiProvider) fetchUsage(ctx context.Context, baseURL, accessToken string) (map[string]any, error) {
	url := strings.TrimRight(baseURL, "/") + "/usages"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build Kimi Code usage request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	// Kimi Code's own managed-usage client deliberately sends only the
	// authorization and accept headers.
	req.Header.Set("User-Agent", "")

	resp, err := credentialSafeClient(p.client).Do(req)
	if err != nil {
		return nil, fmt.Errorf("contact Kimi Code usage service: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read Kimi Code usage response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		message := kimiAPIErrorMessage(body, accessToken)
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			if message == "" {
				message = "OAuth credential was rejected"
			}
			return nil, fmt.Errorf("%w: %s (status %d)", errKimiAuthRequired, message, resp.StatusCode)
		case http.StatusNotFound:
			if message == "" {
				message = "usage endpoint is not available for this Kimi provider"
			}
		default:
			if message == "" {
				message = http.StatusText(resp.StatusCode)
			}
		}
		return nil, fmt.Errorf("Kimi Code usage API: %s (status %d)", message, resp.StatusCode)
	}

	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("parse Kimi Code usage response: %w", err)
	}
	return payload, nil
}

func credentialSafeClient(client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{Timeout: providerTimeout}
	}
	cloned := *client
	cloned.Jar = nil
	cloned.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return errors.New("redirects are disabled for credential-bearing quota requests")
	}
	return &cloned
}

type parsedKimiUsage struct {
	Windows    []Window
	ExtraUsage *ExtraUsage
}

type kimiUsageRow struct {
	Label         string
	Used          float64
	Limit         float64
	ResetsAt      int64
	WindowMinutes int64
}

func parseKimiUsagePayload(payload map[string]any, now time.Time) parsedKimiUsage {
	rows := make([]kimiUsageRow, 0, 4)
	if summary := kimiUsageRowFrom(payload["usage"], "Weekly limit", now); summary != nil {
		rows = append(rows, *summary)
	}

	if rawLimits, ok := payload["limits"].([]any); ok {
		for index, raw := range rawLimits {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			detail, ok := item["detail"].(map[string]any)
			if !ok {
				detail = item
			}
			window, _ := item["window"].(map[string]any)
			label := kimiLimitLabel(item, detail, window, index)
			if row := kimiUsageRowFrom(detail, label, now, window, item, detail); row != nil {
				rows = append(rows, *row)
			}
		}
	}

	windows := make([]Window, 0, len(rows))
	seenIDs := make(map[string]int)
	for index, row := range rows {
		id := kimiWindowID(row, index)
		seenIDs[id]++
		if seenIDs[id] > 1 {
			id = fmt.Sprintf("%s-%d", id, seenIDs[id])
		}
		usedPercent := 0.0
		if row.Limit > 0 {
			usedPercent = row.Used / row.Limit * 100
		}
		windows = append(windows, Window{
			ID:            id,
			Label:         row.Label,
			UsedPercent:   finiteOrZero(usedPercent),
			ResetsAt:      row.ResetsAt,
			WindowMinutes: row.WindowMinutes,
		})
	}

	return parsedKimiUsage{
		Windows:    windows,
		ExtraUsage: parseKimiExtraUsage(payload["boosterWallet"]),
	}
}

func kimiUsageRowFrom(
	raw any,
	defaultLabel string,
	now time.Time,
	windowSources ...map[string]any,
) *kimiUsageRow {
	record, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	limit, hasLimit := numericValue(record["limit"])
	used, hasUsed := numericValue(record["used"])
	if !hasUsed {
		if remaining, ok := numericValue(record["remaining"]); ok && hasLimit {
			used = limit - remaining
			hasUsed = true
		}
	}
	if !hasUsed && !hasLimit {
		return nil
	}

	label := firstNonEmptyString(record["name"], record["title"])
	if label == "" {
		label = defaultLabel
	}
	return &kimiUsageRow{
		Label:         label,
		Used:          valueOrZero(used, hasUsed),
		Limit:         valueOrZero(limit, hasLimit),
		ResetsAt:      kimiResetAt(record, now),
		WindowMinutes: kimiWindowMinutes(label, windowSources...),
	}
}

func kimiLimitLabel(item, detail, window map[string]any, index int) string {
	for _, key := range []string{"name", "title", "scope"} {
		if value := firstNonEmptyString(item[key], detail[key]); value != "" {
			return value
		}
	}
	if minutes := kimiWindowMinutes("", window, item, detail); minutes > 0 {
		switch {
		case minutes%10080 == 0:
			return fmt.Sprintf("%dw limit", minutes/10080)
		case minutes%1440 == 0:
			return fmt.Sprintf("%dd limit", minutes/1440)
		case minutes%60 == 0:
			return fmt.Sprintf("%dh limit", minutes/60)
		default:
			return fmt.Sprintf("%dm limit", minutes)
		}
	}
	return fmt.Sprintf("Limit #%d", index+1)
}

func kimiWindowMinutes(label string, sources ...map[string]any) int64 {
	duration, hasDuration := firstNumericField(sources, "duration")
	unit := strings.ToUpper(firstStringField(sources, "timeUnit", "time_unit"))
	if hasDuration && duration > 0 {
		switch {
		case strings.Contains(unit, "MINUTE"):
			return int64(duration)
		case strings.Contains(unit, "HOUR"):
			return int64(duration * 60)
		case strings.Contains(unit, "DAY"):
			return int64(duration * 1440)
		case strings.Contains(unit, "WEEK"):
			return int64(duration * 10080)
		case strings.Contains(unit, "SECOND"), unit == "":
			return int64(math.Ceil(duration / 60))
		}
	}

	lower := strings.ToLower(label)
	switch {
	case strings.Contains(lower, "weekly"), strings.Contains(lower, "week"):
		return 10080
	case strings.Contains(lower, "monthly"), strings.Contains(lower, "month"):
		return 30 * 1440
	}
	match := quotaDurationPattern.FindStringSubmatch(lower)
	if len(match) != 3 {
		return 0
	}
	value, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil || value <= 0 {
		return 0
	}
	switch match[2] {
	case "m", "min", "minute":
		return value
	case "h", "hour":
		return value * 60
	case "d", "day":
		return value * 1440
	case "w", "week":
		return value * 10080
	case "month":
		return value * 30 * 1440
	}
	return 0
}

func firstNumericField(sources []map[string]any, key string) (float64, bool) {
	for _, source := range sources {
		if source == nil {
			continue
		}
		if value, ok := numericValue(source[key]); ok {
			return value, true
		}
	}
	return 0, false
}

func firstStringField(sources []map[string]any, keys ...string) string {
	for _, source := range sources {
		if source == nil {
			continue
		}
		for _, key := range keys {
			if value := firstNonEmptyString(source[key]); value != "" {
				return value
			}
		}
	}
	return ""
}

func kimiResetAt(record map[string]any, now time.Time) int64 {
	for _, key := range []string{"reset_at", "resetAt", "reset_time", "resetTime"} {
		if seconds, ok := epochSeconds(record[key]); ok {
			return seconds
		}
	}
	for _, key := range []string{"reset_in", "resetIn", "ttl", "window"} {
		if seconds, ok := numericValue(record[key]); ok && seconds > 0 {
			return now.Unix() + int64(seconds)
		}
	}
	return 0
}

func kimiWindowID(row kimiUsageRow, index int) string {
	lower := strings.ToLower(row.Label)
	switch {
	case row.WindowMinutes == 300 || strings.Contains(lower, "5h"):
		return "5h"
	case row.WindowMinutes == 10080 || strings.Contains(lower, "weekly") || strings.Contains(lower, "week"):
		return "weekly"
	case strings.Contains(lower, "monthly") || strings.Contains(lower, "month"):
		return "monthly"
	}
	slug := strings.Trim(strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '-'
		}
	}, row.Label), "-")
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	if slug == "" {
		return fmt.Sprintf("limit-%d", index+1)
	}
	return slug
}

func parseKimiExtraUsage(raw any) *ExtraUsage {
	wallet, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	balance, ok := wallet["balance"].(map[string]any)
	if !ok || firstNonEmptyString(balance["type"]) != "BOOSTER" {
		return nil
	}
	amount, ok := numericValue(balance["amount"])
	if !ok || amount <= 0 {
		return nil
	}
	amountLeft, _ := numericValue(balance["amountLeft"])
	monthlyLimitCents, monthlyLimitCurrency := kimiMoney(wallet["monthlyChargeLimit"])
	monthlyUsedCents, monthlyUsedCurrency := kimiMoney(wallet["monthlyUsed"])
	currency := monthlyLimitCurrency
	if currency == "" {
		currency = monthlyUsedCurrency
	}
	if currency == "" {
		currency = "USD"
	}
	return &ExtraUsage{
		BalanceCents:              kimiFixedPointCents(amountLeft),
		TotalCents:                kimiFixedPointCents(amount),
		MonthlyChargeLimitEnabled: wallet["monthlyChargeLimitEnabled"] == true,
		MonthlyChargeLimitCents:   monthlyLimitCents,
		MonthlyUsedCents:          monthlyUsedCents,
		Currency:                  currency,
	}
}

func kimiMoney(raw any) (int64, string) {
	record, ok := raw.(map[string]any)
	if !ok {
		return 0, ""
	}
	cents, ok := numericValue(record["priceInCents"])
	if !ok {
		return 0, ""
	}
	return int64(cents), firstNonEmptyString(record["currency"])
}

func kimiFixedPointCents(value float64) int64 {
	cents := value / 1_000_000
	if cents > 0 && cents < 1 {
		return 1
	}
	return int64(math.Round(cents))
}

func kimiAPIErrorMessage(body []byte, secrets ...string) string {
	if len(body) == 0 {
		return ""
	}
	var payload map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return ""
	}
	if message := firstNonEmptyString(payload["message"], payload["detail"], payload["error_description"]); message != "" {
		return compactKimiErrorMessage(redactKimiSecrets(message, secrets...))
	}
	if nested, ok := payload["error"].(map[string]any); ok {
		message := firstNonEmptyString(nested["message"], nested["detail"], nested["description"])
		return compactKimiErrorMessage(redactKimiSecrets(message, secrets...))
	}
	if text, ok := payload["error"].(string); ok {
		return compactKimiErrorMessage(redactKimiSecrets(text, secrets...))
	}
	return ""
}

func redactKimiSecrets(message string, secrets ...string) string {
	for _, secret := range secrets {
		if secret = strings.TrimSpace(secret); secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	return message
}

func compactKimiErrorMessage(message string) string {
	message = strings.Join(strings.Fields(message), " ")
	const maxRunes = 512
	runes := []rune(message)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes]) + "…"
	}
	return message
}

func numericValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil && !math.IsNaN(number) && !math.IsInf(number, 0)
	case float64:
		return typed, !math.IsNaN(typed) && !math.IsInf(typed, 0)
	case float32:
		number := float64(typed)
		return number, !math.IsNaN(number) && !math.IsInf(number, 0)
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case string:
		number, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return number, err == nil && !math.IsNaN(number) && !math.IsInf(number, 0)
	default:
		return 0, false
	}
}

func epochSeconds(value any) (int64, bool) {
	if text, ok := value.(string); ok {
		text = strings.TrimSpace(text)
		if text == "" {
			return 0, false
		}
		if timestamp, err := time.Parse(time.RFC3339Nano, text); err == nil {
			return timestamp.Unix(), true
		}
	}
	number, ok := numericValue(value)
	if !ok || number <= 0 {
		return 0, false
	}
	seconds := int64(number)
	if seconds > 100_000_000_000 {
		seconds /= 1000
	}
	return seconds, true
}

func firstNonEmptyString(values ...any) string {
	for _, value := range values {
		if text, ok := value.(string); ok {
			if trimmed := strings.TrimSpace(text); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func valueOrZero(value float64, ok bool) float64 {
	if !ok {
		return 0
	}
	return value
}

func finiteOrZero(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
}

func cloneProviderQuota(in ProviderQuota) ProviderQuota {
	out := in
	out.Windows = append([]Window(nil), in.Windows...)
	if in.ExtraUsage != nil {
		extra := *in.ExtraUsage
		out.ExtraUsage = &extra
	}
	return out
}
