package codex

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"opencode-dashboard/internal/stats"
)

const (
	messageTextMaxBytes = 2000
	toolTextMaxBytes    = 2000
	redactedValue       = "[REDACTED]"
)

var absolutePathPattern = regexp.MustCompile(`(?i)(/[A-Za-z0-9._@%+\-=]+){2,}`)

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
	pathRedacted := absolutePathPattern.ReplaceAllStringFunc(redacted, func(path string) string {
		base := filepath.Base(path)
		if base == "." || base == string(filepath.Separator) || base == "" {
			return "[REDACTED_PATH]"
		}
		return "[REDACTED_PATH]/" + base
	})
	if pathRedacted != redacted {
		redacted = pathRedacted
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

func redactToolInput(text string) (map[string]any, *stats.TruncationInfo, bool) {
	if text == "" {
		return nil, nil, false
	}
	redactedText, truncation, redacted := redactToolText(text)
	return map[string]any{"redacted": redactedText}, truncation, redacted
}

func truncateText(content string, maxBytes int) (string, *stats.TruncationInfo) {
	if maxBytes <= 0 || len(content) <= maxBytes {
		return content, nil
	}
	truncated := content[:maxBytes] + "..."
	return truncated, &stats.TruncationInfo{Truncated: true, OriginalBytes: int64(len(content)), DisplayBytes: int64(len(truncated))}
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

func redactConfigTOML(content []byte) (map[string]any, bool) {
	lines := strings.Split(string(content), "\n")
	redactedLines := make([]string, 0, len(lines))
	changed := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			redactedLines = append(redactedLines, line)
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			redactedLines = append(redactedLines, line)
			continue
		}
		key := trimmed
		if idx := strings.Index(trimmed, "="); idx >= 0 {
			key = strings.TrimSpace(trimmed[:idx])
		}
		if shouldRedactKey(key) || strings.Contains(trimmed, "MUST_NOT_LEAK") || strings.Contains(trimmed, "[REDACTED_PATH]") {
			redactedLines = append(redactedLines, key+" = \""+redactedValue+"\"")
			changed = true
			continue
		}
		redacted, textChanged := redactText(line)
		redactedLines = append(redactedLines, redacted)
		changed = changed || textChanged
	}
	return map[string]any{"lines": redactedLines}, changed
}

func shouldRedactKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
	if normalized == "key" || normalized == "apikey" || normalized == "password" || normalized == "secret" || normalized == "token" || normalized == "credential" || normalized == "auth" || normalized == "authorization" {
		return true
	}
	for _, needle := range []string{"apikey", "password", "secret", "token", "credential", "header", "projectroot"} {
		if strings.Contains(normalized, needle) {
			return true
		}
	}
	return false
}

func stringifySafe(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any, map[string]any:
		encoded, err := json.Marshal(v)
		if err == nil {
			return string(encoded)
		}
	}
	return fmt.Sprint(value)
}
