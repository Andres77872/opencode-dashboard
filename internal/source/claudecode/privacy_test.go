package claudecode

import (
	"strings"
	"testing"
	"time"

	"opencode-dashboard/internal/stats"
)

func TestConfigRedactsSettingsAndDoesNotDumpApplicationState(t *testing.T) {
	src := newFixtureSource(t, "valid_home")

	config, err := src.Config(testContext(t))
	if err != nil {
		t.Fatalf("Config() failed: %v", err)
	}
	assertAllSourceID(t, config.SourceID)
	if !config.Exists {
		t.Fatalf("Config().Exists = false, want true for settings fixture")
	}
	if !config.Redacted {
		t.Errorf("Config().Redacted = false, want true")
	}
	if !strings.HasSuffix(config.Path, "settings.json") {
		t.Errorf("Config().Path = %q, want settings.json", config.Path)
	}

	assertJSONDoesNotContain(t, config,
		"sk-ant-test-settings-secret",
		"sk-ant-test-env-secret",
		"Bearer header-secret-value",
		"nested-password-secret",
		"oauth-access-token-must-not-leak",
		"oauth-refresh-token-must-not-leak",
	)
}

func TestMessageDetailRedactsToolInputAndResultSecrets(t *testing.T) {
	src := newFixtureSource(t, "valid_home")
	list := readAllMessages(t, src)
	toolUseTime := time.Date(2026, 1, 5, 9, 0, 1, 0, time.UTC)
	toolMsg := findMessage(t, list, func(msg stats.MessageEntry) bool {
		return msg.SessionID == "tools-session" && msg.Role == "assistant" && msg.TimeCreated.Equal(toolUseTime)
	})

	detail := mustMessageDetail(t, src, toolMsg.ID)
	assertAllSourceID(t, detail.SourceID)
	tool := findToolPart(t, detail, "toolu_read_1")
	if tool.Tool != "Read" {
		t.Errorf("paired tool name = %q, want Read", tool.Tool)
	}
	if tool.State.Status != "completed" {
		t.Errorf("paired tool status = %q, want completed", tool.State.Status)
	}
	if !tool.State.Redacted {
		t.Errorf("paired tool Redacted = false, want true because input/result includes secret-like keys")
	}
	assertJSONDoesNotContain(t, tool,
		"sk-ant-test-tool-input-secret",
		"tool-result-secret-token",
	)
}

func TestLongContentAndSpillMarkersAreVisibleWithoutLeakingSpillFile(t *testing.T) {
	src := newFixtureSource(t, "valid_home")
	list := readAllMessages(t, src)

	// The user prompt text now lives on its own user row, separate from the assistant
	// row that carries the long content and spill tool.
	userMsg := findMessage(t, list, func(msg stats.MessageEntry) bool {
		return msg.SessionID == "long-content-session" && msg.Role == "user"
	})
	userDetail := mustMessageDetail(t, src, userMsg.ID)
	if !detailContainsText(userDetail, "Generate a very long answer and run a command that spills output.") {
		t.Errorf("user prompt row detail missing prompt text: %#v", userDetail.Content.TextParts)
	}

	longMsg := findMessage(t, list, func(msg stats.MessageEntry) bool {
		return msg.SessionID == "long-content-session" && msg.Role == "assistant"
	})

	detail := mustMessageDetail(t, src, longMsg.ID)
	if len(detail.Content.TextParts) == 0 {
		t.Fatalf("long content detail has no text parts")
	}
	var text stats.MessagePart
	foundLongText := false
	for _, part := range detail.Content.TextParts {
		if strings.Contains(part.Text, "LONG_CONTENT_START") || part.Truncation != nil {
			text = part
			foundLongText = true
			break
		}
	}
	if !foundLongText {
		t.Fatalf("long content detail has no truncated assistant text part: %#v", detail.Content.TextParts)
	}
	if text.Truncation == nil || !text.Truncation.Truncated {
		t.Fatalf("long text truncation = %#v, want visible truncated metadata", text.Truncation)
	}
	if text.Truncation.OriginalBytes <= text.Truncation.DisplayBytes {
		t.Errorf("truncation bytes original/display = %d/%d, want original > display", text.Truncation.OriginalBytes, text.Truncation.DisplayBytes)
	}
	if strings.Contains(text.Text, "LONG_CONTENT_END") {
		t.Errorf("truncated text still contains terminal marker LONG_CONTENT_END")
	}

	tool := findToolPart(t, detail, "toolu_spill_1")
	if tool.State.Metadata == nil {
		t.Fatalf("spill tool metadata is nil, want deferred/spill indicator")
	}
	if got, ok := tool.State.Metadata["deferred"].(bool); !ok || !got {
		t.Errorf("spill tool metadata[deferred] = %#v, want true", tool.State.Metadata["deferred"])
	}
	if got, ok := tool.State.Metadata["spill_file"].(bool); !ok || !got {
		t.Errorf("spill tool metadata[spill_file] = %#v, want true", tool.State.Metadata["spill_file"])
	}
	assertJSONDoesNotContain(t, tool,
		"secret-token-from-tool-input",
		"secret-from-spill-file-must-not-leak",
		"THIS SPILL FILE IS A FIXTURE",
	)
}
