package stats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestConfigRedactsNestedSecretContainersInBothPreviews(t *testing.T) {
	const content = `{
		"model": "custom/model",
		"provider": {"custom": {"options": {"headers": {
			"Authorization": "audit-bearer-secret",
			"X-Api-Key": "audit-header-secret",
			"nested": {"values": ["audit-nested-secret", {"value": "audit-array-secret"}]}
		}}}},
		"mcp": {"local": {"environment": {
			"DATABASE_URL": "audit-environment-secret", "PORT": 6543, "ENABLED": true
		}}},
		"items": [[{"refresh_token": "audit-refresh-secret", "name": "visible"}]],
		"authorization": {"value": "audit-auth-object-secret"},
		"maxTokens": 4096, "enabled": true, "empty": null
	}`
	dir := writeGlobalConfig(t, map[string]string{"opencode.json": content})
	view := loadConfigForTest(t)
	assertRawMatchesContent(t, view)
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{
		"audit-bearer-secret", "audit-header-secret", "audit-environment-secret",
		"audit-nested-secret", "audit-array-secret", "audit-refresh-secret", "audit-auth-object-secret",
	} {
		if strings.Contains(string(encoded), secret) {
			t.Errorf("config preview exposes %q", secret)
		}
	}
	if view.Content["model"] != "custom/model" || view.Content["maxTokens"] != json.Number("4096") || view.Content["enabled"] != true || view.Content["empty"] != nil {
		t.Errorf("non-sensitive settings changed: %#v", view.Content)
	}
	if !strings.Contains(view.Raw, `"name": "visible"`) {
		t.Error("non-sensitive setting inside nested arrays was lost")
	}
	// Previewing must not rewrite the source or mutate its in-memory input.
	onDisk, err := os.ReadFile(filepath.Join(dir, "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != content {
		t.Error("preview modified source settings")
	}
}

func TestSecretRedactionPreservesContainerShapeWithoutMutatingInput(t *testing.T) {
	input := map[string]any{
		"headers": map[string]any{"custom": []any{map[string]any{"value": "private"}, 123, true, nil}},
		"api-key": []any{"private", map[string]any{"value": "private"}},
	}
	before, _ := json.Marshal(input)
	result := redactSensitive(input)
	after, _ := json.Marshal(input)
	if string(before) != string(after) {
		t.Fatal("redaction mutated its input")
	}
	want := map[string]any{
		"headers": map[string]any{"custom": []any{map[string]any{"value": "[REDACTED]"}, "[REDACTED]", "[REDACTED]", nil}},
		"api-key": []any{"[REDACTED]", map[string]any{"value": "[REDACTED]"}},
	}
	if !reflect.DeepEqual(result, want) {
		t.Errorf("redaction = %#v, want %#v", result, want)
	}
}
