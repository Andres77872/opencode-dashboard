package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"opencode-dashboard/internal/stats"
)

func writeConfigFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestDetectConfigFilePriority(t *testing.T) {
	cases := []struct {
		name       string
		files      []string
		wantName   string
		wantFormat string
		wantExists bool
	}{
		{name: "toml wins over json", files: []string{"config.toml", "config.json"}, wantName: "config.toml", wantFormat: stats.ConfigFormatTOML, wantExists: true},
		{name: "json wins over yaml", files: []string{"config.json", "config.yaml"}, wantName: "config.json", wantFormat: stats.ConfigFormatJSON, wantExists: true},
		{name: "yaml wins over yml", files: []string{"config.yaml", "config.yml"}, wantName: "config.yaml", wantFormat: stats.ConfigFormatYAML, wantExists: true},
		{name: "yml alone", files: []string{"config.yml"}, wantName: "config.yml", wantFormat: stats.ConfigFormatYAML, wantExists: true},
		{name: "none defaults to toml path", files: nil, wantName: "config.toml", wantFormat: stats.ConfigFormatTOML, wantExists: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range tc.files {
				writeConfigFile(t, dir, name, "")
			}
			path, format, exists, err := detectConfigFile(dir)
			if err != nil {
				t.Fatalf("detectConfigFile: %v", err)
			}
			if filepath.Base(path) != tc.wantName {
				t.Errorf("path = %q, want basename %q", path, tc.wantName)
			}
			if format != tc.wantFormat {
				t.Errorf("format = %q, want %q", format, tc.wantFormat)
			}
			if exists != tc.wantExists {
				t.Errorf("exists = %t, want %t", exists, tc.wantExists)
			}
		})
	}
}

func TestDetectConfigFileSkipsDirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "config.toml"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeConfigFile(t, dir, "config.json", "{}")
	path, format, exists, err := detectConfigFile(dir)
	if err != nil {
		t.Fatalf("detectConfigFile: %v", err)
	}
	if filepath.Base(path) != "config.json" || format != stats.ConfigFormatJSON || !exists {
		t.Errorf("got (%q, %q, %t), want config.json/json/true", path, format, exists)
	}
}

func TestParseConfigDocumentAndNormalize(t *testing.T) {
	cases := []struct {
		name    string
		format  string
		content string
		check   func(t *testing.T, parsed map[string]any)
	}{
		{
			name:   "toml nested tables dotted keys inline tables arrays datetime",
			format: stats.ConfigFormatTOML,
			content: `model = "gpt-5.5"
created = 1979-05-27T07:32:00Z
notify = ["echo", "done"]

[model_providers.openai]
name = "OpenAI"
query_params = { api-version = "2025-04-01" }
retries = 3

[[servers]]
host = "a"

[[servers]]
host = "b"
`,
			check: func(t *testing.T, parsed map[string]any) {
				if parsed["model"] != "gpt-5.5" {
					t.Errorf("model = %#v", parsed["model"])
				}
				if parsed["created"] != "1979-05-27T07:32:00Z" {
					t.Errorf("created = %#v, want RFC3339 string", parsed["created"])
				}
				providers, ok := parsed["model_providers"].(map[string]any)
				if !ok {
					t.Fatalf("model_providers = %#v, want map", parsed["model_providers"])
				}
				openai, ok := providers["openai"].(map[string]any)
				if !ok {
					t.Fatalf("openai = %#v, want map", providers["openai"])
				}
				inline, ok := openai["query_params"].(map[string]any)
				if !ok || inline["api-version"] != "2025-04-01" {
					t.Errorf("query_params = %#v", openai["query_params"])
				}
				servers, ok := parsed["servers"].([]any)
				if !ok || len(servers) != 2 {
					t.Fatalf("servers = %#v, want []any of 2 (array-of-tables)", parsed["servers"])
				}
			},
		},
		{
			name:    "json with big integers",
			format:  stats.ConfigFormatJSON,
			content: `{"max_tokens": 9007199254740993, "nested": {"flag": true}}`,
			check: func(t *testing.T, parsed map[string]any) {
				num, ok := parsed["max_tokens"].(json.Number)
				if !ok || num.String() != "9007199254740993" {
					t.Errorf("max_tokens = %#v, want json.Number 9007199254740993", parsed["max_tokens"])
				}
			},
		},
		{
			name:   "yaml nested maps timestamp non-string key",
			format: stats.ConfigFormatYAML,
			content: `model: gpt-5.5
created: 1979-05-27T07:32:00Z
nested:
  1: one
  deep:
    flag: true
items:
  - a
  - b
`,
			check: func(t *testing.T, parsed map[string]any) {
				if parsed["model"] != "gpt-5.5" {
					t.Errorf("model = %#v", parsed["model"])
				}
				if _, ok := parsed["created"].(string); !ok {
					t.Errorf("created = %#v (%T), want string", parsed["created"], parsed["created"])
				}
				nested, ok := parsed["nested"].(map[string]any)
				if !ok {
					t.Fatalf("nested = %#v, want map[string]any", parsed["nested"])
				}
				if nested["1"] != "one" {
					t.Errorf("nested[1] = %#v, want stringified key", nested["1"])
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := parseConfigDocument(tc.format, []byte(tc.content))
			if err != nil {
				t.Fatalf("parseConfigDocument: %v", err)
			}
			if _, err := json.Marshal(parsed); err != nil {
				t.Errorf("normalized document is not JSON-safe: %v", err)
			}
			tc.check(t, parsed)
		})
	}
}

func TestParseConfigDocumentErrors(t *testing.T) {
	cases := []struct {
		name    string
		format  string
		content string
	}{
		{name: "invalid toml", format: stats.ConfigFormatTOML, content: "model = \"unterminated\nx = 1"},
		{name: "invalid json", format: stats.ConfigFormatJSON, content: "{"},
		{name: "empty json", format: stats.ConfigFormatJSON, content: ""},
		{name: "yaml top-level list", format: stats.ConfigFormatYAML, content: "- a\n- b\n"},
		{name: "unsupported format", format: "ini", content: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseConfigDocument(tc.format, []byte(tc.content)); err == nil {
				t.Errorf("parseConfigDocument succeeded, want error")
			}
		})
	}
}

func TestParseConfigDocumentEmptyTOML(t *testing.T) {
	parsed, err := parseConfigDocument(stats.ConfigFormatTOML, nil)
	if err != nil {
		t.Fatalf("empty TOML should parse: %v", err)
	}
	if len(parsed) != 0 {
		t.Errorf("parsed = %#v, want empty map", parsed)
	}
}
