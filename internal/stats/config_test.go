package stats

import (
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
