package codex

import (
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
	assertJSONDoesNotContain(t, config, codexForbiddenText()...)
}

func TestCodexMessageDetailRedactsPromptAssistantToolPatchAndPaths(t *testing.T) {
	src := newFixtureSource(t, "privacy_home")
	messages := readAllMessages(t, src)
	if messages.Total != 1 || len(messages.Messages) != 1 {
		t.Fatalf("Messages total/len = %d/%d, want one privacy fixture interaction", messages.Total, len(messages.Messages))
	}
	detail := mustMessageDetail(t, src, messages.Messages[0].ID)
	assertCodexSourceID(t, detail.SourceID)
	assertJSONDoesNotContain(t, detail, codexForbiddenText()...)

	if len(detail.Content.TextParts) == 0 {
		t.Fatalf("TextParts empty, want redacted prompt/assistant placeholders")
	}
	for _, part := range detail.Content.TextParts {
		if !part.Redacted {
			t.Errorf("text part %#v Redacted = false, want redaction marker for privacy fixture", part)
		}
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
