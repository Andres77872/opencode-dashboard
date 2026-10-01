package stats

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestIsSensitiveKey(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected bool
	}{
		// Exact matches (case-insensitive)
		{name: "apiKey exact", key: "apiKey", expected: true},
		{name: "api_key snake_case", key: "api_key", expected: true},
		{name: "key", key: "key", expected: true},
		{name: "token", key: "token", expected: true},
		{name: "secret", key: "secret", expected: true},
		{name: "password", key: "password", expected: true},
		{name: "credential", key: "credential", expected: true},
		{name: "auth", key: "auth", expected: true},

		// Case insensitivity
		{name: "APIKEY uppercase", key: "APIKEY", expected: true},
		{name: "ApiKey mixed case", key: "ApiKey", expected: true},
		{name: "TOKEN uppercase", key: "TOKEN", expected: true},
		{name: "SECRET uppercase", key: "SECRET", expected: true},
		{name: "PASSWORD uppercase", key: "PASSWORD", expected: true},

		// Non-sensitive keys
		{name: "name", key: "name", expected: false},
		{name: "url", key: "url", expected: false},
		{name: "model", key: "model", expected: false},
		{name: "projectId", key: "projectId", expected: false},
		{name: "created_at", key: "created_at", expected: false},
		{name: "empty string", key: "", expected: false},
		{name: "random string", key: "randomKey", expected: false},

		// Keys containing sensitive words but not exact matches
		{name: "apiKeyPrefix should not match", key: "apiKeyPrefix", expected: false},
		{name: "myToken should not match", key: "myToken", expected: false},
		{name: "authToken should not match", key: "authToken", expected: false},
		{name: "secretKey should not match", key: "secretKey", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSensitiveKey(tt.key)
			if result != tt.expected {
				t.Errorf("isSensitiveKey(%q) = %v, want %v", tt.key, result, tt.expected)
			}
		})
	}
}

func TestHasSensitivePrefix(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected bool
	}{
		// Exact prefix matches
		{name: "env prefix", key: "env", expected: true},
		{name: "header prefix", key: "header", expected: true},

		// Prefix with suffix
		{name: "env[] array", key: "env[]", expected: true},
		{name: "env1", key: "env1", expected: true},
		{name: "envVar", key: "envVar", expected: true},
		{name: "headerAuth", key: "headerAuth", expected: true},
		{name: "headers", key: "headers", expected: true},

		// Case insensitivity
		{name: "ENV uppercase", key: "ENV", expected: true},
		{name: "Env mixed case", key: "Env", expected: true},
		{name: "HEADER uppercase", key: "HEADER", expected: true},
		{name: "Header mixed case", key: "Header", expected: true},

		// Keys that start with env/header (prefix match)
		{name: "environment matches env prefix", key: "environment", expected: true},
		{name: "envVar matches env prefix", key: "envVar", expected: true},
		{name: "envConfig matches env prefix", key: "envConfig", expected: true},

		// Keys that do NOT match env/header prefix
		{name: "heading does not match header", key: "heading", expected: false},
		{name: "head does not match header", key: "head", expected: false},
		{name: "random key", key: "random", expected: false},
		{name: "empty string", key: "", expected: false},
		{name: "key", key: "key", expected: false},
		{name: "token", key: "token", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasSensitivePrefix(tt.key)
			if result != tt.expected {
				t.Errorf("hasSensitivePrefix(%q) = %v, want %v", tt.key, result, tt.expected)
			}
		})
	}
}

func TestRedactValue(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    any
		expected any
	}{
		// Sensitive string values
		{name: "apiKey value redacted", key: "apiKey", value: "sk-12345", expected: "[REDACTED]"},
		{name: "token value redacted", key: "token", value: "abc123", expected: "[REDACTED]"},
		{name: "password value redacted", key: "password", value: "secret123", expected: "[REDACTED]"},
		{name: "secret value redacted", key: "secret", value: "my-secret", expected: "[REDACTED]"},

		// Non-sensitive string values preserved
		{name: "name value preserved", key: "name", value: "my-project", expected: "my-project"},
		{name: "model value preserved", key: "model", value: "gpt-4", expected: "gpt-4"},
		{name: "url value preserved", key: "url", value: "https://example.com", expected: "https://example.com"},

		// nil value
		{name: "nil value", key: "anything", value: nil, expected: nil},

		// Other types preserved
		{name: "int value", key: "count", value: 42, expected: 42},
		{name: "float value", key: "ratio", value: 3.14, expected: 3.14},
		{name: "bool value", key: "enabled", value: true, expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := redactValue(tt.key, tt.value)
			if result != tt.expected {
				t.Errorf("redactValue(%q, %v) = %v, want %v", tt.key, tt.value, result, tt.expected)
			}
		})
	}
}

func TestRedactValue_Map(t *testing.T) {
	// Maps are recursively redacted
	input := map[string]any{
		"name":   "test",
		"apiKey": "secret-key",
	}

	result := redactValue("config", input)
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("redactValue with map returned %T, want map[string]any", result)
	}

	if resultMap["name"] != "test" {
		t.Errorf("name = %v, want test", resultMap["name"])
	}
	if resultMap["apiKey"] != "[REDACTED]" {
		t.Errorf("apiKey = %v, want [REDACTED]", resultMap["apiKey"])
	}
}

func TestRedactValue_Array(t *testing.T) {
	t.Run("non-sensitive array preserved", func(t *testing.T) {
		input := []any{"item1", "item2", 3}
		result := redactValue("items", input)
		resultArr, ok := result.([]any)
		if !ok {
			t.Fatalf("redactValue with array returned %T, want []any", result)
		}
		if len(resultArr) != 3 {
			t.Errorf("len = %d, want 3", len(resultArr))
		}
		if resultArr[0] != "item1" || resultArr[1] != "item2" || resultArr[2] != 3 {
			t.Errorf("array values not preserved: %v", resultArr)
		}
	})

	t.Run("sensitive prefix array redacted", func(t *testing.T) {
		input := []any{"SECRET_KEY=abc123", "API_TOKEN=xyz789"}
		result := redactValue("env", input)
		resultArr, ok := result.([]any)
		if !ok {
			t.Fatalf("redactValue with env array returned %T, want []any", result)
		}
		for i, v := range resultArr {
			if v != "[REDACTED]" {
				t.Errorf("env[%d] = %v, want [REDACTED]", i, v)
			}
		}
	})

	t.Run("header array redacted", func(t *testing.T) {
		input := []any{"Authorization: Bearer token"}
		result := redactValue("header", input)
		resultArr, ok := result.([]any)
		if !ok {
			t.Fatalf("redactValue with header array returned %T, want []any", result)
		}
		if resultArr[0] != "[REDACTED]" {
			t.Errorf("header[0] = %v, want [REDACTED]", resultArr[0])
		}
	})
}

func TestRedactMap(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		expected map[string]any
	}{
		{
			name:     "empty map",
			input:    map[string]any{},
			expected: map[string]any{},
		},
		{
			name: "simple non-sensitive map",
			input: map[string]any{
				"name":  "test",
				"count": 10,
			},
			expected: map[string]any{
				"name":  "test",
				"count": 10,
			},
		},
		{
			name: "sensitive values redacted",
			input: map[string]any{
				"name":     "test",
				"apiKey":   "sk-123",
				"token":    "abc",
				"password": "secret",
			},
			expected: map[string]any{
				"name":     "test",
				"apiKey":   "[REDACTED]",
				"token":    "[REDACTED]",
				"password": "[REDACTED]",
			},
		},
		{
			name: "nested map redaction",
			input: map[string]any{
				"config": map[string]any{
					"apiKey": "nested-secret",
					"name":   "nested-name",
				},
			},
			expected: map[string]any{
				"config": map[string]any{
					"apiKey": "[REDACTED]",
					"name":   "nested-name",
				},
			},
		},
		{
			name: "deeply nested map",
			input: map[string]any{
				"level1": map[string]any{
					"level2": map[string]any{
						"secret": "deep-secret",
						"name":   "deep-name",
					},
				},
			},
			expected: map[string]any{
				"level1": map[string]any{
					"level2": map[string]any{
						"secret": "[REDACTED]",
						"name":   "deep-name",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := redactMap(tt.input)

			// Compare maps
			for k, expectedV := range tt.expected {
				resultV, exists := result[k]
				if !exists {
					t.Errorf("missing key %q in result", k)
					continue
				}

				// Handle nested maps
				if expectedMap, ok := expectedV.(map[string]any); ok {
					resultMap, ok := resultV.(map[string]any)
					if !ok {
						t.Errorf("key %q: expected map, got %T", k, resultV)
						continue
					}
					compareMaps(t, k, resultMap, expectedMap)
				} else if resultV != expectedV {
					t.Errorf("key %q = %v, want %v", k, resultV, expectedV)
				}
			}

			// Check for extra keys
			for k := range result {
				if _, exists := tt.expected[k]; !exists {
					t.Errorf("unexpected key %q in result", k)
				}
			}
		})
	}
}

func compareMaps(t *testing.T, prefix string, result, expected map[string]any) {
	for k, expectedV := range expected {
		fullKey := prefix + "." + k
		resultV, exists := result[k]
		if !exists {
			t.Errorf("missing key %q in result", fullKey)
			continue
		}

		if expectedMap, ok := expectedV.(map[string]any); ok {
			resultMap, ok := resultV.(map[string]any)
			if !ok {
				t.Errorf("key %q: expected map, got %T", fullKey, resultV)
				continue
			}
			compareMaps(t, fullKey, resultMap, expectedMap)
		} else if resultV != expectedV {
			t.Errorf("key %q = %v, want %v", fullKey, resultV, expectedV)
		}
	}
}

func TestRedactArray(t *testing.T) {
	t.Run("sensitive prefix redacts all elements", func(t *testing.T) {
		input := []any{"SECRET=abc", "TOKEN=xyz", "KEY=123"}
		result := redactArray("env", input)

		if len(result) != len(input) {
			t.Fatalf("len = %d, want %d", len(result), len(input))
		}

		for i, v := range result {
			if v != "[REDACTED]" {
				t.Errorf("env[%d] = %v, want [REDACTED]", i, v)
			}
		}
	})

	t.Run("non-sensitive prefix preserves elements", func(t *testing.T) {
		input := []any{"item1", "item2", "item3"}
		result := redactArray("items", input)

		if len(result) != len(input) {
			t.Fatalf("len = %d, want %d", len(result), len(input))
		}

		for i, v := range result {
			if v != input[i] {
				t.Errorf("items[%d] = %v, want %v", i, v, input[i])
			}
		}
	})

	t.Run("array of maps gets redacted", func(t *testing.T) {
		input := []any{
			map[string]any{"name": "obj1", "token": "secret1"},
			map[string]any{"name": "obj2", "apiKey": "secret2"},
		}
		result := redactArray("data", input)

		if len(result) != 2 {
			t.Fatalf("len = %d, want 2", len(result))
		}

		// First object
		obj1, ok := result[0].(map[string]any)
		if !ok {
			t.Fatalf("result[0] is %T, want map[string]any", result[0])
		}
		if obj1["name"] != "obj1" {
			t.Errorf("obj1.name = %v, want obj1", obj1["name"])
		}
		if obj1["token"] != "[REDACTED]" {
			t.Errorf("obj1.token = %v, want [REDACTED]", obj1["token"])
		}

		// Second object
		obj2, ok := result[1].(map[string]any)
		if !ok {
			t.Fatalf("result[1] is %T, want map[string]any", result[1])
		}
		if obj2["name"] != "obj2" {
			t.Errorf("obj2.name = %v, want obj2", obj2["name"])
		}
		if obj2["apiKey"] != "[REDACTED]" {
			t.Errorf("obj2.apiKey = %v, want [REDACTED]", obj2["apiKey"])
		}
	})

	t.Run("case insensitive prefix", func(t *testing.T) {
		input := []any{"secret"}
		result := redactArray("ENV", input)
		if result[0] != "[REDACTED]" {
			t.Errorf("ENV array not redacted: %v", result[0])
		}

		result = redactArray("Header", input)
		if result[0] != "[REDACTED]" {
			t.Errorf("Header array not redacted: %v", result[0])
		}
	})
}

func TestRedactSensitive(t *testing.T) {
	t.Run("complex nested structure", func(t *testing.T) {
		input := map[string]any{
			"project": "my-project",
			"config": map[string]any{
				"apiKey": "sk-12345",
				"model":  "gpt-4",
				"nested": map[string]any{
					"password": "nested-secret",
					"name":     "nested-name",
				},
			},
			"env": []any{
				"API_KEY=abc",
				"DATABASE_URL=postgres://...",
			},
			"credentials": []any{
				map[string]any{
					"token": "cred-token",
					"type":  "bearer",
				},
			},
			"metadata": map[string]any{
				"count":   42,
				"enabled": true,
			},
		}

		result := redactSensitive(input)

		// Top-level non-sensitive
		if result["project"] != "my-project" {
			t.Errorf("project = %v, want my-project", result["project"])
		}

		// Nested config
		config, ok := result["config"].(map[string]any)
		if !ok {
			t.Fatalf("config is %T, want map[string]any", result["config"])
		}
		if config["apiKey"] != "[REDACTED]" {
			t.Errorf("config.apiKey = %v, want [REDACTED]", config["apiKey"])
		}
		if config["model"] != "gpt-4" {
			t.Errorf("config.model = %v, want gpt-4", config["model"])
		}

		// Deeply nested
		nested, ok := config["nested"].(map[string]any)
		if !ok {
			t.Fatalf("config.nested is %T, want map[string]any", config["nested"])
		}
		if nested["password"] != "[REDACTED]" {
			t.Errorf("config.nested.password = %v, want [REDACTED]", nested["password"])
		}

		// env array - sensitive prefix
		env, ok := result["env"].([]any)
		if !ok {
			t.Fatalf("env is %T, want []any", result["env"])
		}
		for i, v := range env {
			if v != "[REDACTED]" {
				t.Errorf("env[%d] = %v, want [REDACTED]", i, v)
			}
		}

		// credentials array with map elements
		creds, ok := result["credentials"].([]any)
		if !ok {
			t.Fatalf("credentials is %T, want []any", result["credentials"])
		}
		cred0, ok := creds[0].(map[string]any)
		if !ok {
			t.Fatalf("credentials[0] is %T, want map[string]any", creds[0])
		}
		if cred0["token"] != "[REDACTED]" {
			t.Errorf("credentials[0].token = %v, want [REDACTED]", cred0["token"])
		}
		if cred0["type"] != "bearer" {
			t.Errorf("credentials[0].type = %v, want bearer", cred0["type"])
		}

		// metadata preserved
		metadata, ok := result["metadata"].(map[string]any)
		if !ok {
			t.Fatalf("metadata is %T, want map[string]any", result["metadata"])
		}
		if metadata["count"] != 42 {
			t.Errorf("metadata.count = %v, want 42", metadata["count"])
		}
		if metadata["enabled"] != true {
			t.Errorf("metadata.enabled = %v, want true", metadata["enabled"])
		}
	})

	t.Run("empty map", func(t *testing.T) {
		result := redactSensitive(map[string]any{})
		if len(result) != 0 {
			t.Errorf("expected empty map, got %v", result)
		}
	})

	t.Run("nil values preserved", func(t *testing.T) {
		input := map[string]any{
			"name":  nil,
			"token": nil,
		}
		result := redactSensitive(input)

		if result["name"] != nil {
			t.Errorf("name = %v, want nil", result["name"])
		}
		// nil token values are still nil (redactValue returns nil for nil values)
		if result["token"] != nil {
			t.Errorf("token = %v, want nil", result["token"])
		}
	})
}

func TestStripJSONC(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain json unchanged",
			input: `{"a": [1, 2], "b": {"c": null}}`,
			want:  `{"a": [1, 2], "b": {"c": null}}`,
		},
		{
			name:  "line comments blanked",
			input: "{\n  // leading\n  \"a\": 1 // trailing\n}",
			want:  "{\n            \n  \"a\": 1            \n}",
		},
		{
			name:  "line comment ends at CRLF",
			input: "{\"a\": 1, // note\r\n\"b\": 2}",
			want:  "{\"a\": 1,        \r\n\"b\": 2}",
		},
		{
			name:  "block comment keeps line breaks",
			input: "{/* one\ntwo */\"a\": 1}",
			want:  "{      \n      \"a\": 1}",
		},
		{
			name:  "comment markers inside strings kept",
			input: `{"url": "https://example.com//x", "glob": "src/**/*.ts", "note": "/* keep */ // too"}`,
			want:  `{"url": "https://example.com//x", "glob": "src/**/*.ts", "note": "/* keep */ // too"}`,
		},
		{
			name:  "escaped quote does not end string",
			input: `{"a": "say \"hi\" // still a string"}`,
			want:  `{"a": "say \"hi\" // still a string"}`,
		},
		{
			name:  "escaped backslash ends string",
			input: `{"p": "C:\\", /* c */ "q": 1}`,
			want:  `{"p": "C:\\",         "q": 1}`,
		},
		{
			name:  "trailing comma in object",
			input: `{"a": 1,}`,
			want:  `{"a": 1 }`,
		},
		{
			name:  "trailing comma in array",
			input: `[1, 2, ]`,
			want:  `[1, 2  ]`,
		},
		{
			name:  "trailing comma before comment and closer",
			input: "{\"a\": [\"x\",], // c\n}",
			want:  "{\"a\": [\"x\" ]      \n}",
		},
		{
			name:  "trailing comma after nested object",
			input: `{"a": {"b": true,},}`,
			want:  `{"a": {"b": true } }`,
		},
		{
			name:  "commas and closers inside strings kept",
			input: `{"a": ",}", "b": ",]"}`,
			want:  `{"a": ",}", "b": ",]"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := stripJSONC([]byte(tt.input))
			if err != nil {
				t.Fatalf("stripJSONC() error = %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("stripJSONC() =\n%q\nwant\n%q", got, tt.want)
			}
			if len(got) != len(tt.input) {
				t.Errorf("len = %d, want %d (offsets must be preserved)", len(got), len(tt.input))
			}
			if !json.Valid(got) {
				t.Errorf("stripJSONC() output is not valid JSON: %q", got)
			}
		})
	}
}

func TestStripJSONCRejectsMalformedInput(t *testing.T) {
	// Commas that do not follow a value are not trailing commas; OpenCode's
	// jsonc-parser rejects them, so the decoder must too.
	for _, input := range []string{`{,}`, `[,]`, `[1,,]`, `{"a": 1,,}`} {
		t.Run(input, func(t *testing.T) {
			if _, err := parseJSONCObject([]byte(input)); err == nil {
				t.Errorf("parseJSONCObject(%q) succeeded, want error", input)
			}
		})
	}

	t.Run("unterminated block comment", func(t *testing.T) {
		_, err := stripJSONC([]byte(`{"a": 1} /* open`))
		if !errors.Is(err, errUnterminatedBlockComment) {
			t.Errorf("stripJSONC() error = %v, want %v", err, errUnterminatedBlockComment)
		}
	})
}

func TestMergeConfigDeep(t *testing.T) {
	dst := map[string]any{
		"model":        "a",
		"theme":        "dark",
		"instructions": []any{"a.md", "b.md"},
		"provider": map[string]any{
			"openai": map[string]any{"options": map[string]any{"timeout": 1, "baseURL": "x"}},
		},
		"share": map[string]any{"mode": "manual"},
	}
	src := map[string]any{
		"model":        "b",
		"instructions": []any{"c.md"},
		"provider": map[string]any{
			"openai":    map[string]any{"options": map[string]any{"timeout": 2}},
			"anthropic": map[string]any{"name": "Anthropic"},
		},
		"share": "disabled",
	}
	want := map[string]any{
		"model":        "b",
		"theme":        "dark",
		"instructions": []any{"c.md"},
		"provider": map[string]any{
			"openai":    map[string]any{"options": map[string]any{"timeout": 2, "baseURL": "x"}},
			"anthropic": map[string]any{"name": "Anthropic"},
		},
		"share": "disabled",
	}

	got := mergeConfigDeep(dst, src)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mergeConfigDeep() =\n%#v\nwant\n%#v", got, want)
	}
	if dst["model"] != "a" {
		t.Errorf("mergeConfigDeep mutated dst: model = %v", dst["model"])
	}
}

// writeGlobalConfig points XDG_CONFIG_HOME at a temp dir holding the given
// opencode config files and returns the opencode config directory.
func writeGlobalConfig(t *testing.T, files map[string]string) string {
	t.Helper()
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	dir := filepath.Join(xdg, "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func loadConfigForTest(t *testing.T) ConfigView {
	t.Helper()
	view, err := Config(context.Background(), nil)
	if err != nil {
		t.Fatalf("Config() error = %v", err)
	}
	return view
}

// assertRawMatchesContent checks Raw is pretty JSON encoding exactly Content.
func assertRawMatchesContent(t *testing.T, view ConfigView) {
	t.Helper()
	want, err := json.MarshalIndent(view.Content, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if view.Raw != string(want) {
		t.Errorf("Raw =\n%s\nwant\n%s", view.Raw, want)
	}
}

func TestConfigMissing(t *testing.T) {
	dir := writeGlobalConfig(t, nil)
	if err := os.Mkdir(filepath.Join(dir, "opencode.jsonc"), 0o755); err != nil {
		t.Fatal(err)
	}

	view := loadConfigForTest(t)
	if view.Exists {
		t.Errorf("Exists = true, want false")
	}
	if want := filepath.Join(dir, "opencode.json"); view.Path != want {
		t.Errorf("Path = %q, want %q", view.Path, want)
	}
	if view.Content != nil || view.Raw != "" || view.ParseError != "" {
		t.Errorf("unexpected content for missing config: %+v", view)
	}
}

func TestConfigJSONOnly(t *testing.T) {
	dir := writeGlobalConfig(t, map[string]string{
		"opencode.json": `{
  "$schema": "https://opencode.ai/config.json",
  "model": "anthropic/claude-sonnet",
  "provider": {"openai": {"options": {"apiKey": "sk-json-only-secret"}}}
}`,
	})

	view := loadConfigForTest(t)
	if !view.Exists {
		t.Fatalf("Exists = false, want true (parse error %q)", view.ParseError)
	}
	if want := filepath.Join(dir, "opencode.json"); view.Path != want {
		t.Errorf("Path = %q, want %q", view.Path, want)
	}
	if view.Format != ConfigFormatJSON {
		t.Errorf("Format = %q, want %q", view.Format, ConfigFormatJSON)
	}
	if view.MergedPaths != nil {
		t.Errorf("MergedPaths = %v, want nil for a single file", view.MergedPaths)
	}
	if view.ParseError != "" {
		t.Errorf("ParseError = %q, want empty", view.ParseError)
	}
	if view.Content["model"] != "anthropic/claude-sonnet" {
		t.Errorf("model = %v", view.Content["model"])
	}
	if strings.Contains(view.Raw, "sk-json-only-secret") || !strings.Contains(view.Raw, "[REDACTED]") {
		t.Errorf("Raw not redacted: %s", view.Raw)
	}
	assertRawMatchesContent(t, view)
}

func TestConfigJSONCOnly(t *testing.T) {
	dir := writeGlobalConfig(t, map[string]string{
		"opencode.jsonc": `// Global OpenCode config
{
  "$schema": "https://opencode.ai/config.json",
  /* default model */
  "model": "anthropic/claude-sonnet",
  "provider": {
    "openai": {
      "options": {
        "apiKey": "sk-jsonc-secret", // inline key
        "baseURL": "https://api.example.com/v1",
      },
    },
  },
  "mcp": {
    "local": {"type": "local", "command": ["node", "server.js"], "environment": {"TOKEN": "env-secret"}},
  },
  "limit": 200000,
}
`,
	})

	view := loadConfigForTest(t)
	if !view.Exists {
		t.Fatalf("Exists = false, want true")
	}
	if view.ParseError != "" {
		t.Fatalf("ParseError = %q, want empty", view.ParseError)
	}
	if want := filepath.Join(dir, "opencode.jsonc"); view.Path != want {
		t.Errorf("Path = %q, want %q", view.Path, want)
	}
	if view.Format != ConfigFormatJSON {
		t.Errorf("Format = %q, want %q (Raw is re-encoded JSON)", view.Format, ConfigFormatJSON)
	}
	if view.MergedPaths != nil {
		t.Errorf("MergedPaths = %v, want nil for a single file", view.MergedPaths)
	}

	options := view.Content["provider"].(map[string]any)["openai"].(map[string]any)["options"].(map[string]any)
	if options["apiKey"] != "[REDACTED]" {
		t.Errorf("apiKey = %v, want [REDACTED]", options["apiKey"])
	}
	if options["baseURL"] != "https://api.example.com/v1" {
		t.Errorf("baseURL = %v", options["baseURL"])
	}
	env := view.Content["mcp"].(map[string]any)["local"].(map[string]any)["environment"].(map[string]any)
	if env["TOKEN"] != "[REDACTED]" {
		t.Errorf("environment.TOKEN = %v, want [REDACTED]", env["TOKEN"])
	}
	if view.Content["limit"] != json.Number("200000") {
		t.Errorf("limit = %#v, want json.Number(200000)", view.Content["limit"])
	}

	for _, leaked := range []string{"sk-jsonc-secret", "env-secret", "inline key", "default model"} {
		if strings.Contains(view.Raw, leaked) {
			t.Errorf("Raw contains %q:\n%s", leaked, view.Raw)
		}
	}
	if !json.Valid([]byte(view.Raw)) {
		t.Errorf("Raw is not valid JSON:\n%s", view.Raw)
	}
	assertRawMatchesContent(t, view)
}

func TestConfigJSONCCommentMarkersInStrings(t *testing.T) {
	writeGlobalConfig(t, map[string]string{
		"opencode.jsonc": `{
  "instructions": ["docs/**/*.md", "https://example.com/rules.md"], // urls
  "agent": {"review": {"prompt": "Flag /* block */ and // line markers"}},
  "watcher": {"ignore": ["**/node_modules//**"]},
  "theme": "say \"hi\" // not a comment",
}`,
	})

	view := loadConfigForTest(t)
	if view.ParseError != "" {
		t.Fatalf("ParseError = %q, want empty", view.ParseError)
	}
	wantInstructions := []any{"docs/**/*.md", "https://example.com/rules.md"}
	if got := view.Content["instructions"]; !reflect.DeepEqual(got, wantInstructions) {
		t.Errorf("instructions = %#v, want %#v", got, wantInstructions)
	}
	prompt := view.Content["agent"].(map[string]any)["review"].(map[string]any)["prompt"]
	if prompt != "Flag /* block */ and // line markers" {
		t.Errorf("prompt = %q", prompt)
	}
	ignore := view.Content["watcher"].(map[string]any)["ignore"]
	if !reflect.DeepEqual(ignore, []any{"**/node_modules//**"}) {
		t.Errorf("watcher.ignore = %#v", ignore)
	}
	if view.Content["theme"] != `say "hi" // not a comment` {
		t.Errorf("theme = %q", view.Content["theme"])
	}
}

func TestConfigJSONCTrailingCommas(t *testing.T) {
	writeGlobalConfig(t, map[string]string{
		"opencode.jsonc": `{
  "disabled_providers": ["a", "b",],
  "keybinds": {"leader": "ctrl+x",},
  "formatter": {"prettier": {"extensions": [".ts", ".tsx", /* more */],},},
}`,
	})

	view := loadConfigForTest(t)
	if view.ParseError != "" {
		t.Fatalf("ParseError = %q, want empty", view.ParseError)
	}
	if got := view.Content["disabled_providers"]; !reflect.DeepEqual(got, []any{"a", "b"}) {
		t.Errorf("disabled_providers = %#v", got)
	}
	if got := view.Content["keybinds"]; !reflect.DeepEqual(got, map[string]any{"leader": "ctrl+x"}) {
		t.Errorf("keybinds = %#v", got)
	}
	extensions := view.Content["formatter"].(map[string]any)["prettier"].(map[string]any)["extensions"]
	if !reflect.DeepEqual(extensions, []any{".ts", ".tsx"}) {
		t.Errorf("formatter.prettier.extensions = %#v", extensions)
	}
}

func TestConfigBothPresentMergesWithJSONCPrecedence(t *testing.T) {
	dir := writeGlobalConfig(t, map[string]string{
		"opencode.json": `{
  "model": "openai/gpt-5",
  "theme": "tokyonight",
  "instructions": ["a.md", "b.md"],
  "provider": {"openai": {"options": {"apiKey": "sk-from-json", "timeout": 1000}}}
}`,
		"opencode.jsonc": `{
  // jsonc wins on conflicts
  "model": "anthropic/claude-sonnet",
  "instructions": ["c.md"],
  "provider": {
    "openai": {"options": {"timeout": 5000}},
    "anthropic": {"options": {"apiKey": "sk-from-jsonc"}},
  },
}`,
	})

	view := loadConfigForTest(t)
	if view.ParseError != "" {
		t.Fatalf("ParseError = %q, want empty", view.ParseError)
	}
	jsonPath := filepath.Join(dir, "opencode.json")
	jsoncPath := filepath.Join(dir, "opencode.jsonc")
	if view.Path != jsoncPath {
		t.Errorf("Path = %q, want %q", view.Path, jsoncPath)
	}
	if want := []string{jsonPath, jsoncPath}; !reflect.DeepEqual(view.MergedPaths, want) {
		t.Errorf("MergedPaths = %v, want %v", view.MergedPaths, want)
	}

	if view.Content["model"] != "anthropic/claude-sonnet" {
		t.Errorf("model = %v, want the opencode.jsonc value", view.Content["model"])
	}
	if view.Content["theme"] != "tokyonight" {
		t.Errorf("theme = %v, want the opencode.json value", view.Content["theme"])
	}
	if got := view.Content["instructions"]; !reflect.DeepEqual(got, []any{"c.md"}) {
		t.Errorf("instructions = %#v, want arrays replaced by opencode.jsonc", got)
	}
	provider := view.Content["provider"].(map[string]any)
	openai := provider["openai"].(map[string]any)["options"].(map[string]any)
	if openai["timeout"] != json.Number("5000") {
		t.Errorf("openai.timeout = %#v, want 5000 from opencode.jsonc", openai["timeout"])
	}
	if openai["apiKey"] != "[REDACTED]" {
		t.Errorf("openai.apiKey = %v, want [REDACTED] kept from opencode.json", openai["apiKey"])
	}
	anthropic := provider["anthropic"].(map[string]any)["options"].(map[string]any)
	if anthropic["apiKey"] != "[REDACTED]" {
		t.Errorf("anthropic.apiKey = %v, want [REDACTED]", anthropic["apiKey"])
	}
	for _, leaked := range []string{"sk-from-json", "sk-from-jsonc"} {
		if strings.Contains(view.Raw, leaked) {
			t.Errorf("Raw contains %q", leaked)
		}
	}
	assertRawMatchesContent(t, view)
}

func TestConfigBothPresentReportsWhichFileFailed(t *testing.T) {
	dir := writeGlobalConfig(t, map[string]string{
		"opencode.json":  `{"model": "openai/gpt-5"}`,
		"opencode.jsonc": `{"provider": {"apiKey": "sk-broken-jsonc-secret"} "model": "x"}`,
	})

	view := loadConfigForTest(t)
	if !view.Exists {
		t.Fatalf("Exists = false, want true")
	}
	if view.Path != filepath.Join(dir, "opencode.jsonc") {
		t.Errorf("Path = %q", view.Path)
	}
	if !strings.HasPrefix(view.ParseError, "opencode.jsonc: ") {
		t.Errorf("ParseError = %q, want it to name opencode.jsonc", view.ParseError)
	}
	if strings.Contains(view.ParseError, "sk-broken-jsonc-secret") {
		t.Errorf("ParseError leaks file contents: %q", view.ParseError)
	}
	if view.Content != nil || view.Raw != "" {
		t.Errorf("Content/Raw should be empty on parse error, got %v / %q", view.Content, view.Raw)
	}
}

func TestConfigParseErrorDoesNotLeakContents(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		content string
	}{
		{
			name:    "missing comma",
			file:    "opencode.jsonc",
			content: `{"provider": {"openai": {"options": {"apiKey": "sk-live-SECRET-0123456789"}}} "model": "x"}`,
		},
		{
			name:    "unterminated block comment",
			file:    "opencode.jsonc",
			content: "{\"apiKey\": \"sk-live-SECRET-0123456789\"} /* password: hunter2-SECRET",
		},
		{
			name:    "truncated after comment",
			file:    "opencode.jsonc",
			content: "{\n  // token sk-live-SECRET-0123456789\n  \"token\": \"sk-live-SECRET-0123456789\",\n",
		},
		{
			name:    "syntax error in opencode.json",
			file:    "opencode.json",
			content: "{\"token\": \"sk-live-SECRET-0123456789\" bad}",
		},
		{
			name:    "top-level array",
			file:    "opencode.jsonc",
			content: `["sk-live-SECRET-0123456789"]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeGlobalConfig(t, map[string]string{tt.file: tt.content})

			view := loadConfigForTest(t)
			if !view.Exists {
				t.Fatalf("Exists = false, want true")
			}
			if view.ParseError == "" {
				t.Fatalf("ParseError is empty, want a parse failure")
			}
			for _, leaked := range []string{"SECRET", "sk-live", "hunter2", "password"} {
				if strings.Contains(view.ParseError, leaked) {
					t.Errorf("ParseError leaks %q: %q", leaked, view.ParseError)
				}
			}
			if view.Content != nil || view.Raw != "" {
				t.Errorf("Content/Raw should be empty on parse error, got %v / %q", view.Content, view.Raw)
			}
		})
	}
}

func TestSanitizeConfigParseError(t *testing.T) {
	err := errors.New("bad value \"sk-live-SECRET-0123456789\"\nnext line")
	got := sanitizeConfigParseError(err)
	if strings.Contains(got, "SECRET") || strings.Contains(got, "\n") {
		t.Errorf("sanitizeConfigParseError() = %q", got)
	}

	long := errors.New(strings.Repeat("x", 400))
	if got := sanitizeConfigParseError(long); len(got) > 300+len("…") {
		t.Errorf("sanitizeConfigParseError() len = %d, want capped", len(got))
	}
}
