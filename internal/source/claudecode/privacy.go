package claudecode

import (
	"encoding/json"
	"fmt"
	"strings"

	"opencode-dashboard/internal/stats"
)

const (
	messageTextMaxBytes = 2000
	toolTextMaxBytes    = 2000
	redactedValue       = "[REDACTED]"
)

func redactJSONDocument(content []byte) (map[string]any, bool, error) {
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		return nil, false, fmt.Errorf("parse Claude settings JSON: %w", err)
	}
	redacted, changed := redactMap(raw, "")
	return redacted, changed, nil
}

func redactMap(input map[string]any, parentKey string) (map[string]any, bool) {
	result := make(map[string]any, len(input))
	changed := false
	for key, value := range input {
		if shouldRedactKey(key) || hasSensitivePrefix(parentKey) {
			result[key] = redactWholeValue(value)
			changed = true
			continue
		}
		redacted, redactedChanged := redactAny(key, value)
		result[key] = redacted
		changed = changed || redactedChanged
	}
	return result, changed
}

func redactAny(key string, value any) (any, bool) {
	if shouldRedactKey(key) || hasSensitivePrefix(key) {
		return redactWholeValue(value), true
	}
	switch v := value.(type) {
	case map[string]any:
		return redactMap(v, key)
	case []any:
		out := make([]any, len(v))
		changed := false
		for i, item := range v {
			redacted, itemChanged := redactAny(key, item)
			out[i] = redacted
			changed = changed || itemChanged
		}
		return out, changed
	default:
		return value, false
	}
}

func redactWholeValue(value any) any {
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

func shouldRedactKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
	if normalized == "key" || normalized == "apikey" || normalized == "password" || normalized == "secret" || normalized == "token" || normalized == "credential" || normalized == "auth" || normalized == "authorization" {
		return true
	}
	for _, needle := range []string{"apikey", "password", "secret", "token", "credential"} {
		if strings.Contains(normalized, needle) {
			return true
		}
	}
	return false
}

func hasSensitivePrefix(key string) bool {
	lower := strings.ToLower(key)
	return strings.HasPrefix(lower, "env") || strings.HasPrefix(lower, "header")
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

func redactAndTruncateToolInput(input map[string]any) (map[string]any, *stats.TruncationInfo, bool) {
	redacted, changed := redactMap(input, "")
	truncated, info := truncateMapStrings(redacted, toolTextMaxBytes)
	return truncated, info, changed || info != nil
}

func truncateMapStrings(input map[string]any, maxBytes int) (map[string]any, *stats.TruncationInfo) {
	result := make(map[string]any, len(input))
	var aggregate *stats.TruncationInfo
	for key, value := range input {
		switch v := value.(type) {
		case string:
			truncated, info := truncateText(v, maxBytes)
			result[key] = truncated
			aggregate = mergeTruncation(aggregate, info)
		case map[string]any:
			nested, info := truncateMapStrings(v, maxBytes)
			result[key] = nested
			aggregate = mergeTruncation(aggregate, info)
		case []any:
			array, info := truncateArrayStrings(v, maxBytes)
			result[key] = array
			aggregate = mergeTruncation(aggregate, info)
		default:
			result[key] = value
		}
	}
	return result, aggregate
}

func truncateArrayStrings(input []any, maxBytes int) ([]any, *stats.TruncationInfo) {
	result := make([]any, len(input))
	var aggregate *stats.TruncationInfo
	for i, value := range input {
		switch v := value.(type) {
		case string:
			truncated, info := truncateText(v, maxBytes)
			result[i] = truncated
			aggregate = mergeTruncation(aggregate, info)
		case map[string]any:
			nested, info := truncateMapStrings(v, maxBytes)
			result[i] = nested
			aggregate = mergeTruncation(aggregate, info)
		default:
			result[i] = value
		}
	}
	return result, aggregate
}

func stringifyToolContent(content any) (string, bool) {
	switch v := content.(type) {
	case nil:
		return "", false
	case string:
		trimmed := strings.TrimSpace(v)
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			var raw any
			decoder := json.NewDecoder(strings.NewReader(trimmed))
			decoder.UseNumber()
			if err := decoder.Decode(&raw); err == nil {
				redacted, changed := redactAny("", raw)
				encoded, err := json.Marshal(redacted)
				if err == nil {
					return string(encoded), changed
				}
			}
		}
		return v, false
	case []any, map[string]any:
		redacted, changed := redactAny("", v)
		encoded, err := json.Marshal(redacted)
		if err != nil {
			return fmt.Sprint(v), changed
		}
		return string(encoded), changed
	default:
		return fmt.Sprint(v), false
	}
}
