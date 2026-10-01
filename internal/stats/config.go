package stats

import (
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

// globalConfigNames are the files OpenCode reads from its global config
// directory, lowest precedence first. Both OpenCode 1.x (loadGlobal in
// packages/opencode/src/config/config.ts) and 2.x (ConfigDiscovery.names in
// packages/core/src/config/discovery.ts) load every one that exists, parse each
// as JSONC, and let opencode.jsonc override opencode.json.
var globalConfigNames = []string{"opencode.json", "opencode.jsonc"}

func Config(ctx context.Context, _ *store.Store) (ConfigView, error) {
	return loadGlobalConfig(xdgConfigDir())
}

// loadGlobalConfig builds the redacted view of OpenCode's global config in dir.
// When several config files exist they are merged in precedence order, Path
// names the highest-precedence one and MergedPaths lists all of them.
func loadGlobalConfig(dir string) (ConfigView, error) {
	view := ConfigView{
		Path:   filepath.Join(dir, globalConfigNames[0]),
		Exists: false,
	}

	var paths []string
	for _, name := range globalConfigNames {
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return view, fmt.Errorf("failed to access config file: %w", err)
		}
		if info.IsDir() {
			continue
		}
		paths = append(paths, candidate)
	}
	if len(paths) == 0 {
		return view, nil
	}

	view.Path = paths[len(paths)-1]
	view.Exists = true
	view.Format = ConfigFormatJSON
	if len(paths) > 1 {
		view.MergedPaths = paths
	}

	merged := map[string]any{}
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			return view, fmt.Errorf("failed to read config file: %w", err)
		}
		raw, err := parseJSONCObject(content)
		if err != nil {
			view.ParseError = sanitizeConfigParseError(err)
			if len(paths) > 1 {
				view.ParseError = filepath.Base(path) + ": " + view.ParseError
			}
			return view, nil
		}
		merged = mergeConfigDeep(merged, raw)
	}

	redacted := redactSensitive(merged)

	view.Content = redacted
	if encoded, encodeErr := json.MarshalIndent(redacted, "", "  "); encodeErr == nil {
		view.Raw = string(encoded)
	}

	return view, nil
}

// mergeConfigDeep layers src over dst the way OpenCode 1.x merges its global
// config files (remeda mergeDeep): when both sides hold an object the two are
// merged key by key; any other value in src, arrays included, replaces dst's.
func mergeConfigDeep(dst, src map[string]any) map[string]any {
	result := make(map[string]any, len(dst)+len(src))
	for k, v := range dst {
		result[k] = v
	}
	for k, v := range src {
		if srcMap, ok := v.(map[string]any); ok {
			if dstMap, ok := result[k].(map[string]any); ok {
				result[k] = mergeConfigDeep(dstMap, srcMap)
				continue
			}
		}
		result[k] = v
	}
	return result
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

func xdgConfigDir() string {
	xdgConfig := os.Getenv("XDG_CONFIG_HOME")
	if xdgConfig == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		xdgConfig = filepath.Join(home, ".config")
	}
	return filepath.Join(xdgConfig, "opencode")
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
