package codex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"

	"opencode-dashboard/internal/stats"
)

// configCandidates lists the config files Codex may use, in priority order.
// config.toml is the current Rust CLI standard; json/yaml come from older
// TypeScript CLI releases.
var configCandidates = []struct {
	name   string
	format string
}{
	{"config.toml", stats.ConfigFormatTOML},
	{"config.json", stats.ConfigFormatJSON},
	{"config.yaml", stats.ConfigFormatYAML},
	{"config.yml", stats.ConfigFormatYAML},
}

// detectConfigFile returns the first existing regular config file under
// codexHome. When none exists it returns the default config.toml path with
// exists=false. Directories matching a candidate name are skipped.
func detectConfigFile(codexHome string) (path, format string, exists bool, err error) {
	for _, candidate := range configCandidates {
		candidatePath := filepath.Join(codexHome, candidate.name)
		info, statErr := os.Stat(candidatePath)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				continue
			}
			return candidatePath, candidate.format, false, statErr
		}
		if info.IsDir() {
			continue
		}
		return candidatePath, candidate.format, true, nil
	}
	return filepath.Join(codexHome, configCandidates[0].name), configCandidates[0].format, false, nil
}

// parseConfigDocument parses content per format into a JSON-safe map.
func parseConfigDocument(format string, content []byte) (map[string]any, error) {
	switch format {
	case stats.ConfigFormatTOML:
		var parsed map[string]any
		if err := toml.Unmarshal(content, &parsed); err != nil {
			return nil, err
		}
		return normalizeJSONSafeMap(parsed), nil
	case stats.ConfigFormatJSON:
		decoder := json.NewDecoder(bytes.NewReader(content))
		decoder.UseNumber()
		var parsed map[string]any
		if err := decoder.Decode(&parsed); err != nil {
			return nil, err
		}
		return normalizeJSONSafeMap(parsed), nil
	case stats.ConfigFormatYAML:
		var parsed map[string]any
		if err := yaml.Unmarshal(content, &parsed); err != nil {
			return nil, err
		}
		return normalizeJSONSafeMap(parsed), nil
	default:
		return nil, fmt.Errorf("unsupported config format %q", format)
	}
}

func normalizeJSONSafeMap(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = normalizeJSONSafe(v)
	}
	return result
}

// normalizeJSONSafe converts parser-specific types into JSON-safe values:
// time.Time -> RFC3339 string; []map[string]any (BurntSushi array-of-tables)
// and typed slices -> []any; map[any]any (yaml non-string keys) ->
// map[string]any via fmt.Sprint(key).
func normalizeJSONSafe(v any) any {
	switch value := v.(type) {
	case nil, string, bool, float64, float32, int, int64, int32, uint, uint64, uint32, json.Number:
		return value
	case time.Time:
		return value.Format(time.RFC3339)
	case map[string]any:
		return normalizeJSONSafeMap(value)
	case map[any]any:
		result := make(map[string]any, len(value))
		for k, item := range value {
			result[fmt.Sprint(k)] = normalizeJSONSafe(item)
		}
		return result
	case []any:
		result := make([]any, len(value))
		for i, item := range value {
			result[i] = normalizeJSONSafe(item)
		}
		return result
	case []map[string]any:
		result := make([]any, len(value))
		for i, item := range value {
			result[i] = normalizeJSONSafeMap(item)
		}
		return result
	case []string:
		result := make([]any, len(value))
		for i, item := range value {
			result[i] = item
		}
		return result
	case []int64:
		result := make([]any, len(value))
		for i, item := range value {
			result[i] = item
		}
		return result
	default:
		return fmt.Sprint(value)
	}
}
