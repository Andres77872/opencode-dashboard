package codex

import (
	"fmt"
	"strings"
	"testing"

	"opencode-dashboard/internal/stats"
)

func TestCodexConfigRedactsValuesAndDoesNotReadAuthLogsOrState(t *testing.T) {
	src := newFixtureSource(t, "privacy_home")
	config, err := src.Config(testContext(t))
	if err != nil {
		t.Fatalf("Config() failed: %v", err)
	}
	assertCodexSourceID(t, config.SourceID)
	if !config.Exists {
		t.Fatalf("Config().Exists = false, want true for synthetic config.toml")
	}
	if !config.Redacted {
		t.Errorf("Config().Redacted = false, want true")
	}
	if !strings.HasSuffix(config.Path, "config.toml") {
		t.Errorf("Config().Path = %q, want config.toml", config.Path)
	}
	if config.Format != stats.ConfigFormatTOML {
		t.Errorf("Config().Format = %q, want %q", config.Format, stats.ConfigFormatTOML)
	}
	if config.ParseError != "" {
		t.Errorf("Config().ParseError = %q, want empty", config.ParseError)
	}

	// Content must be a structured TOML document, not the legacy lines shape.
	if _, hasLines := config.Content["lines"]; hasLines {
		t.Errorf("Config().Content has legacy %q key, want structured document", "lines")
	}
	providers, ok := config.Content["model_providers"].(map[string]any)
	if !ok {
		t.Fatalf("Content[model_providers] = %#v, want map", config.Content["model_providers"])
	}
	openai, ok := providers["openai"].(map[string]any)
	if !ok {
		t.Fatalf("model_providers.openai = %#v, want map", providers["openai"])
	}
	if openai["api_key"] != "[REDACTED]" {
		t.Errorf("openai.api_key = %#v, want [REDACTED]", openai["api_key"])
	}
	if openai["name"] != "OpenAI" {
		t.Errorf("openai.name = %#v, want unredacted value", openai["name"])
	}
	env, ok := openai["env"].(map[string]any)
	if !ok || env["OPENAI_API_KEY"] != "[REDACTED]" {
		t.Errorf("openai.env = %#v, want inner values redacted", openai["env"])
	}

	// Raw preserves comments/structure with secrets masked.
	if config.Raw == "" {
		t.Fatalf("Config().Raw is empty, want redacted original text")
	}
	if !strings.Contains(config.Raw, "# synthetic codex config") {
		t.Errorf("Config().Raw lost the leading comment:\n%s", config.Raw)
	}
	if !strings.Contains(config.Raw, "[model_providers.openai]") {
		t.Errorf("Config().Raw lost table headers:\n%s", config.Raw)
	}

	assertJSONDoesNotContain(t, config, codexForbiddenText()...)
}

func TestRedactConfigMap(t *testing.T) {
	input := map[string]any{
		"model": "gpt-5.5",
		"model_providers": map[string]any{
			"openai": map[string]any{
				"name":    "OpenAI",
				"api_key": "SECRET_VALUE",
				"env":     map[string]any{"ANY_VAR": "SECRET_ENV"},
				"headers": map[string]any{"Authorization": "Bearer SECRET"},
			},
		},
		"tokens":  []any{"SECRET_A", "SECRET_B"},
		"notify":  []any{"echo", "done"},
		"nested":  []any{map[string]any{"password": "SECRET_PW", "safe": "ok"}},
		"project": nil,
	}
	redacted, changed := redactConfigMap(input)
	if !changed {
		t.Fatalf("changed = false, want true")
	}
	providers := redacted["model_providers"].(map[string]any)["openai"].(map[string]any)
	if providers["api_key"] != "[REDACTED]" {
		t.Errorf("api_key = %#v", providers["api_key"])
	}
	if providers["name"] != "OpenAI" {
		t.Errorf("name = %#v, want untouched", providers["name"])
	}
	env := providers["env"].(map[string]any)
	if env["ANY_VAR"] != "[REDACTED]" {
		t.Errorf("env.ANY_VAR = %#v, want redacted via env prefix", env["ANY_VAR"])
	}
	headers := providers["headers"].(map[string]any)
	if headers["Authorization"] != "[REDACTED]" {
		t.Errorf("headers.Authorization = %#v", headers["Authorization"])
	}
	tokens := redacted["tokens"].([]any)
	if tokens[0] != "[REDACTED]" || tokens[1] != "[REDACTED]" {
		t.Errorf("tokens = %#v, want element-wise redaction", tokens)
	}
	notify := redacted["notify"].([]any)
	if notify[0] != "echo" {
		t.Errorf("notify = %#v, want untouched", notify)
	}
	nested := redacted["nested"].([]any)[0].(map[string]any)
	if nested["password"] != "[REDACTED]" || nested["safe"] != "ok" {
		t.Errorf("nested = %#v", nested)
	}

	if _, unchanged := redactConfigMap(map[string]any{"model": "x"}); unchanged {
		t.Errorf("changed = true for benign map, want false")
	}
}

func TestRedactConfigLines(t *testing.T) {
	t.Run("toml", func(t *testing.T) {
		input := strings.Join([]string{
			"# keep this comment",
			"model = \"gpt-5.5\"",
			"",
			"[model_providers.openai]",
			"api_key = \"SECRET_VALUE\"",
			"env = { ANY_VAR = \"SECRET_ENV\" }",
			"provider = { api_key = \"SECRET_INLINE\", name = \"x\" }",
			"multi_secret_token = \"\"\"",
			"SECRET_BLOCK",
			"\"\"\"",
			"notify = [\"echo\", \"done\"]",
			"  indented_password = \"SECRET_INDENT\"",
		}, "\n")
		redacted, changed := redactConfigLines(stats.ConfigFormatTOML, []byte(input))
		if !changed {
			t.Fatalf("changed = false, want true")
		}
		for _, secret := range []string{"SECRET_VALUE", "SECRET_ENV", "SECRET_INLINE", "SECRET_BLOCK", "SECRET_INDENT"} {
			if strings.Contains(redacted, secret) {
				t.Errorf("raw TOML leaked %q:\n%s", secret, redacted)
			}
		}
		for _, keep := range []string{"# keep this comment", "model = \"gpt-5.5\"", "[model_providers.openai]", "notify = [\"echo\", \"done\"]"} {
			if !strings.Contains(redacted, keep) {
				t.Errorf("raw TOML lost %q:\n%s", keep, redacted)
			}
		}
		if !strings.Contains(redacted, "  indented_password = \"[REDACTED]\"") {
			t.Errorf("raw TOML lost indentation on redacted line:\n%s", redacted)
		}
	})

	t.Run("json", func(t *testing.T) {
		input := strings.Join([]string{
			"{",
			"  \"model\": \"gpt-5.5\",",
			"  \"apiKey\": \"SECRET_VALUE\",",
			"  \"env\": {",
			"    \"inner\": \"SECRET_CONTAINER\"",
			"  },",
			"  \"safe\": true",
			"}",
		}, "\n")
		redacted, changed := redactConfigLines(stats.ConfigFormatJSON, []byte(input))
		if !changed {
			t.Fatalf("changed = false, want true")
		}
		for _, secret := range []string{"SECRET_VALUE", "SECRET_CONTAINER"} {
			if strings.Contains(redacted, secret) {
				t.Errorf("raw JSON leaked %q:\n%s", secret, redacted)
			}
		}
		if !strings.Contains(redacted, "  \"apiKey\": \"[REDACTED]\",") {
			t.Errorf("raw JSON lost comma/indent on redacted line:\n%s", redacted)
		}
		if !strings.Contains(redacted, "\"safe\": true") {
			t.Errorf("raw JSON lost benign line:\n%s", redacted)
		}
	})

	t.Run("yaml", func(t *testing.T) {
		input := strings.Join([]string{
			"model: gpt-5.5",
			"api_key: SECRET_VALUE",
			"env:",
			"  ANY_VAR: SECRET_CONTAINER",
			"secret_note: |",
			"  SECRET_BLOCK_LINE",
			"safe: yes",
		}, "\n")
		redacted, changed := redactConfigLines(stats.ConfigFormatYAML, []byte(input))
		if !changed {
			t.Fatalf("changed = false, want true")
		}
		for _, secret := range []string{"SECRET_VALUE", "SECRET_CONTAINER", "SECRET_BLOCK_LINE"} {
			if strings.Contains(redacted, secret) {
				t.Errorf("raw YAML leaked %q:\n%s", secret, redacted)
			}
		}
		for _, keep := range []string{"model: gpt-5.5", "safe: yes"} {
			if !strings.Contains(redacted, keep) {
				t.Errorf("raw YAML lost %q:\n%s", keep, redacted)
			}
		}
	})
}

func TestCodexConfigParseErrorFallback(t *testing.T) {
	src := newTempCodexSource(t, map[string][]string{
		"config.toml": {
			"model = \"gpt-5.5\"",
			"api_key = \"SYNTHETIC_CONFIG_SECRET_MUST_NOT_LEAK\"",
			"broken = [unclosed",
		},
	})
	config, err := src.Config(testContext(t))
	if err != nil {
		t.Fatalf("Config() failed: %v", err)
	}
	if !config.Exists {
		t.Fatalf("Exists = false, want true")
	}
	if config.ParseError == "" {
		t.Fatalf("ParseError empty, want sanitized parser message")
	}
	if config.Content != nil {
		t.Errorf("Content = %#v, want nil on parse failure", config.Content)
	}
	if config.Raw == "" {
		t.Errorf("Raw empty, want line-redacted text even on parse failure")
	}
	assertJSONDoesNotContain(t, config, codexForbiddenText()...)
}

func TestCodexConfigJSONAndYAMLEndToEnd(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		src := newTempCodexSource(t, map[string][]string{
			"config.json": {
				"{",
				"  \"model\": \"gpt-5.5\",",
				"  \"apiKey\": \"SYNTHETIC_CONFIG_SECRET_MUST_NOT_LEAK\"",
				"}",
			},
		})
		config, err := src.Config(testContext(t))
		if err != nil {
			t.Fatalf("Config() failed: %v", err)
		}
		if config.Format != stats.ConfigFormatJSON || !config.Exists {
			t.Fatalf("format/exists = %q/%t, want json/true", config.Format, config.Exists)
		}
		if config.Content["apiKey"] != "[REDACTED]" {
			t.Errorf("apiKey = %#v", config.Content["apiKey"])
		}
		if config.Content["model"] != "gpt-5.5" {
			t.Errorf("model = %#v", config.Content["model"])
		}
		assertJSONDoesNotContain(t, config, codexForbiddenText()...)
	})

	t.Run("yaml", func(t *testing.T) {
		src := newTempCodexSource(t, map[string][]string{
			"config.yaml": {
				"model: gpt-5.5",
				"api_key: SYNTHETIC_CONFIG_SECRET_MUST_NOT_LEAK",
				"history:",
				"  persistence: save-all",
			},
		})
		config, err := src.Config(testContext(t))
		if err != nil {
			t.Fatalf("Config() failed: %v", err)
		}
		if config.Format != stats.ConfigFormatYAML || !config.Exists {
			t.Fatalf("format/exists = %q/%t, want yaml/true", config.Format, config.Exists)
		}
		if config.Content["api_key"] != "[REDACTED]" {
			t.Errorf("api_key = %#v", config.Content["api_key"])
		}
		history, ok := config.Content["history"].(map[string]any)
		if !ok || history["persistence"] != "save-all" {
			t.Errorf("history = %#v", config.Content["history"])
		}
		assertJSONDoesNotContain(t, config, codexForbiddenText()...)
	})
}

func TestSanitizeParseError(t *testing.T) {
	longSecret := strings.Repeat("s", 40)
	err := fmt.Errorf("toml: line 3: invalid value %q", "SYNTHETIC_"+longSecret+"_MUST_NOT_LEAK")
	msg := sanitizeParseError(err)
	if msg == "" {
		t.Fatalf("sanitized message empty")
	}
	if strings.Contains(msg, "MUST_NOT_LEAK") || strings.Contains(msg, longSecret) {
		t.Errorf("sanitized message leaked content: %q", msg)
	}
	if !strings.Contains(msg, "line 3") {
		t.Errorf("sanitized message lost position info: %q", msg)
	}
	if sanitizeParseError(nil) != "" {
		t.Errorf("nil error should sanitize to empty string")
	}
}

func TestCodexMessageDetailRedactsPromptAssistantToolPatchAndPaths(t *testing.T) {
	src := newFixtureSource(t, "privacy_home")
	messages := readAllMessages(t, src)
	// 1 user prompt row + 1 assistant API request row (carries the content).
	if messages.Total != 2 || len(messages.Messages) != 2 {
		t.Fatalf("Messages total/len = %d/%d, want two privacy fixture rows", messages.Total, len(messages.Messages))
	}

	// Every row's detail must stay redacted and never leak a secret sentinel.
	for _, msg := range messages.Messages {
		rowDetail := mustMessageDetail(t, src, msg.ID)
		assertCodexSourceID(t, rowDetail.SourceID)
		assertJSONDoesNotContain(t, rowDetail, codexForbiddenText()...)
		for _, part := range rowDetail.Content.TextParts {
			if !part.Redacted {
				t.Errorf("text part %#v Redacted = false, want redaction marker for privacy fixture", part)
			}
		}
	}

	// The assistant request row carries the redacted prompt-driven content,
	// including the redacted tool/args/output/patch.
	assistant := findMessage(t, messages, func(m stats.MessageEntry) bool {
		return m.Role == "assistant"
	})
	detail := mustMessageDetail(t, src, assistant.ID)
	if len(detail.Content.TextParts) == 0 {
		t.Fatalf("TextParts empty, want redacted assistant placeholders")
	}
	tool := findToolPart(t, detail, "privacy-call")
	if !tool.State.Redacted {
		t.Errorf("privacy tool Redacted = false, want redacted args/output/patch")
	}
	assertJSONDoesNotContain(t, tool, codexForbiddenText()...)
}

func TestCodexDiagnosticsAndAggregatesDoNotLeakSkippedArtifacts(t *testing.T) {
	src := newFixtureSource(t, "privacy_home")
	ctx := testContext(t)
	period := stats.PeriodQuery{Period: "all"}

	if _, err := src.Overview(ctx, period); err != nil {
		t.Fatalf("Overview(all) failed: %v", err)
	}
	info := src.Info(ctx)
	assertJSONDoesNotContain(t, info, codexForbiddenText()...)

	for _, run := range []struct {
		name string
		call func(t *testing.T) any
	}{
		{name: "overview", call: func(t *testing.T) any {
			got, err := src.Overview(ctx, period)
			if err != nil {
				t.Fatalf("Overview: %v", err)
			}
			return got
		}},
		{name: "daily", call: func(t *testing.T) any {
			got, err := src.Daily(ctx, period, stats.GranularityDay)
			if err != nil {
				t.Fatalf("Daily: %v", err)
			}
			return got
		}},
		{name: "models", call: func(t *testing.T) any {
			got, err := src.Models(ctx, period)
			if err != nil {
				t.Fatalf("Models: %v", err)
			}
			return got
		}},
		{name: "tools", call: func(t *testing.T) any {
			got, err := src.Tools(ctx, period)
			if err != nil {
				t.Fatalf("Tools: %v", err)
			}
			return got
		}},
		{name: "sessions", call: func(t *testing.T) any {
			got, err := src.Sessions(ctx, stats.SessionQuery{Page: 1, PageSize: 10, Period: "all"})
			if err != nil {
				t.Fatalf("Sessions: %v", err)
			}
			return got
		}},
		{name: "messages", call: func(t *testing.T) any {
			got, err := src.Messages(ctx, period, 1, 50, chronologicalMessageSort())
			if err != nil {
				t.Fatalf("Messages: %v", err)
			}
			return got
		}},
	} {
		t.Run(run.name, func(t *testing.T) {
			assertJSONDoesNotContain(t, run.call(t), codexForbiddenText()...)
		})
	}
}
