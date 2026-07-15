package stats

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"opencode-dashboard/internal/store"
)

var sensitiveKeys = []string{
	"apiKey",
	"api_key",
	"key",
	"token",
	"secret",
	"password",
	"credential",
	"auth",
}

var sensitivePrefixes = []string{
	"env",
	"header",
}

func Config(ctx context.Context, _ *store.Store) (ConfigView, error) {
	configPath := xdgConfigPath()

	view := ConfigView{
		Path:   configPath,
		Exists: false,
	}

	info, err := os.Stat(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return view, nil
		}
		return view, fmt.Errorf("failed to access config file: %w", err)
	}

	if info.IsDir() {
		return view, nil
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		return view, fmt.Errorf("failed to read config file: %w", err)
	}

	view.Exists = true
	view.Format = ConfigFormatJSON

	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		view.ParseError = sanitizeConfigParseError(err)
		return view, nil
	}

	redacted := redactSensitive(raw)

	view.Content = redacted
	if encoded, encodeErr := json.MarshalIndent(redacted, "", "  "); encodeErr == nil {
		view.Raw = string(encoded)
	}

	return view, nil
}

// sanitizeConfigParseError caps and scrubs a JSON parse error before it is
// exposed in a ConfigView (decoder errors may quote fragments of the file).
func sanitizeConfigParseError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ReplaceAll(err.Error(), "\n", " ")
	msg = longQuotedSpanPattern.ReplaceAllString(msg, `"…"`)
	if len(msg) > 300 {
		msg = msg[:300] + "…"
	}
	return msg
}

var longQuotedSpanPattern = regexp.MustCompile(`"[^"]{12,}"|'[^']{12,}'`)

func xdgConfigPath() string {
	xdgConfig := os.Getenv("XDG_CONFIG_HOME")
	if xdgConfig == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		xdgConfig = filepath.Join(home, ".config")
	}
	return filepath.Join(xdgConfig, "opencode", "opencode.json")
}

func redactSensitive(data map[string]any) map[string]any {
	result := make(map[string]any)
	for k, v := range data {
		result[k] = redactValue(k, v)
	}
	return result
}

func redactValue(key string, value any) any {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case map[string]any:
		return redactMap(v)
	case []any:
		return redactArray(key, v)
	case string:
		if isSensitiveKey(key) {
			return "[REDACTED]"
		}
		return v
	default:
		return v
	}
}

func redactMap(m map[string]any) map[string]any {
	result := make(map[string]any)
	for k, v := range m {
		result[k] = redactValue(k, v)
	}
	return result
}

func redactArray(parentKey string, arr []any) []any {
	if hasSensitivePrefix(parentKey) {
		result := make([]any, len(arr))
		for i := range arr {
			result[i] = "[REDACTED]"
		}
		return result
	}

	result := make([]any, len(arr))
	for i, v := range arr {
		switch item := v.(type) {
		case map[string]any:
			result[i] = redactMap(item)
		default:
			result[i] = item
		}
	}
	return result
}

func isSensitiveKey(key string) bool {
	lowerKey := strings.ToLower(key)
	for _, sensitive := range sensitiveKeys {
		if lowerKey == strings.ToLower(sensitive) {
			return true
		}
	}
	return false
}

func hasSensitivePrefix(key string) bool {
	lowerKey := strings.ToLower(key)
	for _, prefix := range sensitivePrefixes {
		if strings.HasPrefix(lowerKey, strings.ToLower(prefix)) {
			return true
		}
	}
	return false
}
