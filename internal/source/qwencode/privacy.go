package qwencode

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"opencode-dashboard/internal/stats"
)

const (
	messageTextMaxBytes = 2000
	toolTextMaxBytes    = 2000
	redactedValue       = "[REDACTED]"
)

var (
	absolutePathPattern = regexp.MustCompile(`(?i)(/[A-Za-z0-9._@%+\-=]+){2,}`)
	inlineSecretPattern = regexp.MustCompile(`(?i)\b(api[_-]?key|access[_-]?token|auth[_-]?token|password|secret)\b(\s*[:=]\s*)([^\s,;"']+)`)
	bearerPattern       = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]{12,}`)
)

func redactText(text string) (string, bool) {
	if text == "" {
		return "", false
	}
	redacted := text
	changed := false
	if strings.Contains(redacted, "MUST_NOT_LEAK") || strings.Contains(redacted, "SYNTHETIC_") && !strings.Contains(redacted, "[REDACTED_") {
		redacted = redactedValue
		changed = true
	}
	next := inlineSecretPattern.ReplaceAllString(redacted, `$1$2[REDACTED]`)
	if next != redacted {
		redacted = next
		changed = true
	}
	next = bearerPattern.ReplaceAllString(redacted, "Bearer [REDACTED]")
	if next != redacted {
		redacted = next
		changed = true
	}
	next = absolutePathPattern.ReplaceAllStringFunc(redacted, func(path string) string {
		base := filepath.Base(path)
		if base == "." || base == string(filepath.Separator) || base == "" {
			return "[REDACTED_PATH]"
		}
		return "[REDACTED_PATH]/" + base
	})
	if next != redacted {
		redacted = next
		changed = true
	}
	return redacted, changed
}

func redactDisplayPath(path string) string {
	redacted, _ := redactText(path)
	return redacted
}

func redactAndTruncateMessagePart(kind, text string) stats.MessagePart {
	redactedText, redacted := redactText(text)
	truncated, truncation := truncateText(redactedText, messageTextMaxBytes)
	return stats.MessagePart{Type: kind, Text: truncated, Truncation: truncation, Redacted: redacted || truncation != nil}
}

func redactToolText(text string) (string, *stats.TruncationInfo, bool) {
	redactedText, redacted := redactText(text)
	truncated, truncation := truncateText(redactedText, toolTextMaxBytes)
	return truncated, truncation, redacted || truncation != nil
}

func redactToolInput(value any) (map[string]any, *stats.TruncationInfo, bool) {
	if value == nil {
		return nil, nil, false
	}
	encoded := valueToText(value)
	if encoded == "" {
		return nil, nil, false
	}
	redacted, truncation, changed := redactToolText(encoded)
	return map[string]any{"redacted": redacted}, truncation, changed
}

func truncateText(content string, maxBytes int) (string, *stats.TruncationInfo) {
	if maxBytes <= 0 || len(content) <= maxBytes {
		return content, nil
	}
	end := maxBytes
	for end > 0 && !utf8.ValidString(content[:end]) {
		end--
	}
	truncated := content[:end] + "..."
	return truncated, &stats.TruncationInfo{
		Truncated: true, OriginalBytes: int64(len(content)), DisplayBytes: int64(len(truncated)),
	}
}

func mergeTruncation(current, next *stats.TruncationInfo) *stats.TruncationInfo {
	if next == nil {
		return current
	}
	if current == nil {
		copy := *next
		return &copy
	}
	current.Truncated = current.Truncated || next.Truncated
	current.OriginalBytes += next.OriginalBytes
	current.DisplayBytes += next.DisplayBytes
	return current
}

func redactConfigMap(input map[string]any) (map[string]any, bool) {
	return redactConfigMapWithParent(input, "")
}

func redactConfigMapWithParent(input map[string]any, parent string) (map[string]any, bool) {
	out := make(map[string]any, len(input))
	changed := false
	for key, value := range input {
		if shouldRedactConfigKey(key) || sensitiveConfigContainer(parent) {
			out[key] = redactWholeConfigValue(value)
			changed = true
			continue
		}
		redacted, itemChanged := redactConfigValue(key, value)
		out[key] = redacted
		changed = changed || itemChanged
	}
	return out, changed
}

func redactConfigValue(key string, value any) (any, bool) {
	if shouldRedactConfigKey(key) || sensitiveConfigContainer(key) {
		return redactWholeConfigValue(value), true
	}
	switch v := value.(type) {
	case map[string]any:
		return redactConfigMapWithParent(v, key)
	case []any:
		out := make([]any, len(v))
		changed := false
		for i, item := range v {
			redacted, itemChanged := redactConfigValue(key, item)
			out[i] = redacted
			changed = changed || itemChanged
		}
		return out, changed
	case string:
		return redactText(v)
	default:
		return value, false
	}
}

func redactWholeConfigValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key := range v {
			out[key] = redactedValue
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = redactedValue
		}
		return out
	case nil:
		return nil
	default:
		return redactedValue
	}
}

func shouldRedactConfigKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", "_"), ".", "_"))
	for _, fragment := range []string{
		"api_key", "apikey", "access_token", "refresh_token", "auth_token",
		"password", "passwd", "secret", "credential", "cookie", "authorization",
		"private_key", "client_secret",
	} {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}

func sensitiveConfigContainer(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	return strings.HasPrefix(lower, "env") || strings.Contains(lower, "header")
}

func encodeRedactedJSON(content map[string]any) (string, error) {
	encoded, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func sanitizeParseError(err error) string {
	if err == nil {
		return ""
	}
	text, _ := redactText(err.Error())
	if strings.TrimSpace(text) == "" {
		return "invalid JSON"
	}
	return fmt.Sprintf("invalid JSON: %s", text)
}
