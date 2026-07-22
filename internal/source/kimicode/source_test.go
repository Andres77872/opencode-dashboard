package kimicode

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

func TestKimiCodeSourceAggregatesWireRecordsAndPricing(t *testing.T) {
	home := writeKimiHome(t, map[string]sessionFixture{
		"main": {
			State: sessionState{
				CreatedAt: "2026-07-16T10:00:00Z",
				UpdatedAt: "2026-07-16T10:01:00Z",
				Title:     "Synthetic Kimi session",
				WorkDir:   "/home/synthetic/private-project",
				Agents:    map[string]agentMeta{"main": {Type: "main"}},
			},
			Wires: map[string][]string{
				"main": {
					`{"type":"metadata","protocol_version":"1.4","created_at":1784196000000}`,
					`{"type":"turn.prompt","input":[{"type":"text","text":"Inspect /home/synthetic/private-project"}],"origin":{"kind":"user"},"time":1784196001000}`,
					`{"type":"context.append_message","message":{"role":"user","content":[{"type":"text","text":"Inspect /home/synthetic/private-project"}],"origin":{"kind":"user"}},"time":1784196001000}`,
					`{"type":"context.append_loop_event","event":{"type":"step.begin","uuid":"step-1","turnId":"0","step":1},"time":1784196001100}`,
					`{"type":"llm.request","kind":"loop","provider":"kimi","model":"k3","modelAlias":"kimi-code/k3","turnStep":"0.1","time":1784196001200}`,
					`{"type":"context.append_loop_event","event":{"type":"content.part","uuid":"think-1","turnId":"0","step":1,"stepUuid":"step-1","part":{"type":"think","think":"Reason about /home/synthetic/private-project"}},"time":1784196001300}`,
					`{"type":"context.append_loop_event","event":{"type":"content.part","uuid":"text-1","turnId":"0","step":1,"stepUuid":"step-1","part":{"type":"text","text":"Done."}},"time":1784196001400}`,
					`{"type":"context.append_loop_event","event":{"type":"tool.call","uuid":"call-1","turnId":"0","step":1,"stepUuid":"step-1","toolCallId":"call-1","name":"Read","args":{"path":"/home/synthetic/private-project/README.md"},"description":"Read /home/synthetic/private-project/README.md secret=MUST_NOT_LEAK"},"time":1784196001500}`,
					`{"type":"context.append_loop_event","event":{"type":"tool.result","parentUuid":"call-1","toolCallId":"call-1","result":{"output":"read /home/synthetic/private-project/README.md","isError":false}},"time":1784196001600}`,
					`{"type":"context.append_loop_event","event":{"type":"step.end","uuid":"step-1","turnId":"0","step":1,"usage":{"inputOther":1000,"inputCacheRead":2000,"inputCacheCreation":300,"output":100}},"time":1784196001700}`,
					`{"type":"usage.record","model":"kimi-code/k3","usage":{"inputOther":1000,"inputCacheRead":2000,"inputCacheCreation":300,"output":100},"usageScope":"turn","time":1784196001700}`,
					`{"type":"context.append_message","message":{"role":"user","content":[{"type":"text","text":"internal reminder"}],"origin":{"kind":"injection","variant":"test"}},"time":1784196001800}`,
					`{"type":"turn.prompt","input":[{"type":"text","text":"Second prompt"}],"origin":{"kind":"user"},"time":1784196002000}`,
					`{"type":"context.append_loop_event","event":{"type":"step.begin","uuid":"step-2","turnId":"1","step":1},"time":1784196002100}`,
					`{"type":"llm.request","kind":"loop","provider":"kimi","model":"kimi-k2.6","turnStep":"1.1","time":1784196002200}`,
					`{"type":"usage.record","model":"kimi-k2.6","usage":{"inputOther":1000,"inputCacheRead":500,"inputCacheCreation":100,"output":50},"usageScope":"turn","time":1784196002300}`,
				},
			},
		},
	})
	src := New(Options{KimiHome: home, PathSource: "test fixture"})
	ctx := testContext(t)
	period := stats.PeriodQuery{Period: "all"}

	info := src.Info(ctx)
	if info.ID != source.SourceKimiCode || !info.Available || info.Diagnostics.Status != "ok" {
		t.Fatalf("Info() = %#v, want available Kimi Code source", info)
	}
	if info.CostPolicy.Status != string(stats.CostEstimatedAPIEquivalent) {
		t.Errorf("cost policy = %#v, want API-equivalent", info.CostPolicy)
	}

	overview, err := src.Overview(ctx, period)
	if err != nil {
		t.Fatalf("Overview(all): %v", err)
	}
	if overview.SourceID != kimiSourceID || overview.Sessions != 1 || overview.Messages != 4 {
		t.Errorf("Overview = %#v, want one session and four per-request rows", overview)
	}
	if overview.Tokens.Input != 2000 || overview.Tokens.Cache.Read != 2500 ||
		overview.Tokens.Cache.Write != 400 || overview.Tokens.Output != 150 {
		t.Errorf("token totals = %#v", overview.Tokens)
	}
	if math.Abs(overview.Cost-0.007325) > 1e-12 {
		t.Errorf("overview cost = %.12f, want 0.007325", overview.Cost)
	}

	models, err := src.Models(ctx, period)
	if err != nil {
		t.Fatalf("Models(all): %v", err)
	}
	if len(models.Models) != 2 {
		t.Fatalf("models = %#v, want K3 and K2.6", models.Models)
	}
	if findModel(t, models, "kimi-code/k3").ProviderID != "kimi" {
		t.Errorf("K3 provider = %q, want kimi", findModel(t, models, "kimi-code/k3").ProviderID)
	}

	tools, err := src.Tools(ctx, period)
	if err != nil {
		t.Fatalf("Tools(all): %v", err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != "Read" ||
		tools.Tools[0].Invocations != 1 || tools.Tools[0].Successes != 1 {
		t.Errorf("tools = %#v, want one completed Read", tools.Tools)
	}

	sessions, err := src.Sessions(ctx, stats.SessionQuery{Period: "all", Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("Sessions(all): %v", err)
	}
	if sessions.Total != 1 || sessions.Sessions[0].Title != "Synthetic Kimi session" {
		t.Errorf("sessions = %#v", sessions)
	}
	detail, err := src.SessionByID(ctx, "main")
	if err != nil {
		t.Fatalf("SessionByID(main): %v", err)
	}
	if detail == nil || detail.MessageCount != 4 {
		t.Fatalf("session detail = %#v", detail)
	}
	if strings.Contains(detail.Directory, "/home/synthetic") {
		t.Errorf("session directory leaked absolute path: %q", detail.Directory)
	}

	messages, err := src.Messages(ctx, period, 1, 20, stats.MessageSort{Field: stats.MessageSortTime, Direction: stats.MessageSortAsc})
	if err != nil {
		t.Fatalf("Messages(all): %v", err)
	}
	var firstAssistant stats.MessageEntry
	for _, msg := range messages.Messages {
		if msg.Role == "assistant" && msg.ModelID == "kimi-code/k3" {
			firstAssistant = msg
			break
		}
	}
	if firstAssistant.ID == "" {
		t.Fatalf("K3 assistant row not found: %#v", messages.Messages)
	}
	messageDetail, err := src.MessageByID(ctx, firstAssistant.ID)
	if err != nil {
		t.Fatalf("MessageByID(%q): %v", firstAssistant.ID, err)
	}
	if messageDetail == nil || len(messageDetail.Content.ReasoningParts) != 1 ||
		len(messageDetail.Content.ToolParts) != 1 {
		t.Fatalf("message detail = %#v", messageDetail)
	}
	encoded, _ := json.Marshal(messageDetail)
	if strings.Contains(string(encoded), "/home/synthetic") || strings.Contains(string(encoded), "MUST_NOT_LEAK") {
		t.Errorf("message detail leaked private tool metadata: %s", encoded)
	}
}

func TestKimiCodeRebuiltEngineUsageBeforeContentStaysOnOneRequest(t *testing.T) {
	home := writeKimiHome(t, map[string]sessionFixture{
		"v2-ordering": {
			State: sessionState{
				CreatedAt: "2026-07-16T10:00:00Z",
				UpdatedAt: "2026-07-16T10:01:00Z",
				Title:     "Rebuilt engine",
				WorkDir:   "/tmp/project",
				Agents:    map[string]agentMeta{"main": {Type: "main"}},
			},
			Wires: map[string][]string{
				"main": {
					`{"type":"metadata","protocol_version":"1.5","created_at":1784196000000}`,
					`{"type":"turn.prompt","input":[{"type":"text","text":"Inspect it"}],"origin":{"kind":"user"},"time":1784196001000}`,
					`{"type":"context.append_loop_event","event":{"type":"step.begin","uuid":"step-1","turnId":"0","step":1},"time":1784196001100}`,
					`{"type":"llm.request","kind":"loop","provider":"kimi","model":"k3","modelAlias":"kimi-code/k3","turnStep":"0.1","time":1784196001200}`,
					`{"type":"usage.record","model":"kimi-code/k3","usage":{"inputOther":1000,"inputCacheRead":2000,"inputCacheCreation":300,"output":100},"usageScope":"turn","time":1784196001300}`,
					`{"type":"context.append_loop_event","event":{"type":"content.part","uuid":"text-1","turnId":"0","step":1,"stepUuid":"step-1","part":{"type":"text","text":"Done."}},"time":1784196001400}`,
					`{"type":"context.append_loop_event","event":{"type":"tool.call","uuid":"call-1","turnId":"0","step":1,"stepUuid":"step-1","toolCallId":"call-1","name":"Read","args":{"path":"README.md"},"description":"Reading README.md"},"time":1784196001500}`,
					`{"type":"context.append_loop_event","event":{"type":"tool.result","parentUuid":"call-1","toolCallId":"call-1","result":{"output":"ok","isError":false}},"time":1784196001600}`,
					`{"type":"context.append_loop_event","event":{"type":"step.end","uuid":"step-1","turnId":"0","step":1,"usage":{"inputOther":1000,"inputCacheRead":2000,"inputCacheCreation":300,"output":100}},"time":1784196001700}`,
				},
			},
		},
	})
	src := New(Options{KimiHome: home})
	ctx := testContext(t)

	overview, err := src.Overview(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Overview(all): %v", err)
	}
	if overview.Messages != 2 {
		t.Fatalf("overview messages = %d, want one user row plus one request row", overview.Messages)
	}
	if overview.Tokens.Input != 1000 || overview.Tokens.Cache.Read != 2000 ||
		overview.Tokens.Cache.Write != 300 || overview.Tokens.Output != 100 {
		t.Errorf("overview tokens = %#v, want rebuilt-engine usage on the request row", overview.Tokens)
	}

	messages, err := src.Messages(ctx, stats.PeriodQuery{Period: "all"}, 1, 10, stats.MessageSort{
		Field: stats.MessageSortTime, Direction: stats.MessageSortAsc,
	})
	if err != nil {
		t.Fatalf("Messages(all): %v", err)
	}
	if len(messages.Messages) != 2 || messages.Messages[1].Role != "assistant" {
		t.Fatalf("messages = %#v, want one assistant request", messages.Messages)
	}
	detail, err := src.MessageByID(ctx, messages.Messages[1].ID)
	if err != nil {
		t.Fatalf("MessageByID(%q): %v", messages.Messages[1].ID, err)
	}
	if detail == nil || len(detail.Content.TextParts) != 1 || detail.Content.TextParts[0].Text != "Done." ||
		len(detail.Content.ToolParts) != 1 || detail.Content.ToolParts[0].State.Status != "completed" {
		t.Errorf("rebuilt-engine request detail = %#v", detail)
	}
}

func TestKimiCodeForkMarkerSuppressesCopiedHistory(t *testing.T) {
	parentLines := []string{
		`{"type":"turn.prompt","input":[{"type":"text","text":"Parent prompt"}],"origin":{"kind":"user"},"time":1784196001000}`,
		`{"type":"llm.request","kind":"loop","provider":"kimi","model":"k3","modelAlias":"kimi-code/k3","turnStep":"0.1","time":1784196001100}`,
		`{"type":"usage.record","model":"kimi-code/k3","usage":{"inputOther":100,"inputCacheRead":200,"inputCacheCreation":0,"output":10},"usageScope":"turn","time":1784196001200}`,
	}
	childLines := append(append([]string{}, parentLines...),
		`{"type":"forked","time":1784197000000}`,
		`{"type":"turn.prompt","input":[{"type":"text","text":"Intermediate copied fork prompt"}],"origin":{"kind":"user"},"time":1784197000100}`,
		`{"type":"llm.request","kind":"loop","provider":"kimi","model":"k3","modelAlias":"kimi-code/k3","turnStep":"0.2","time":1784197000200}`,
		`{"type":"usage.record","model":"kimi-code/k3","usage":{"inputOther":9000,"inputCacheRead":9000,"inputCacheCreation":0,"output":9000},"usageScope":"turn","time":1784197000300}`,
		`{"type":"forked","time":1784197000400}`,
		`{"type":"turn.prompt","input":[{"type":"text","text":"Child prompt"}],"origin":{"kind":"user"},"time":1784197001000}`,
		`{"type":"llm.request","kind":"loop","provider":"kimi","model":"k3","modelAlias":"kimi-code/k3","turnStep":"1.1","time":1784197001100}`,
		`{"type":"usage.record","model":"kimi-code/k3","usage":{"inputOther":300,"inputCacheRead":400,"inputCacheCreation":0,"output":20},"usageScope":"turn","time":1784197001200}`,
	)
	home := writeKimiHome(t, map[string]sessionFixture{
		"parent": {
			State: sessionState{
				CreatedAt: "2026-07-16T10:00:00Z", UpdatedAt: "2026-07-16T10:01:00Z",
				Title: "Parent", WorkDir: "/tmp/project", Agents: map[string]agentMeta{"main": {Type: "main"}},
			},
			Wires: map[string][]string{"main": parentLines},
		},
		"child": {
			State: sessionState{
				CreatedAt: "2026-07-16T10:10:00Z", UpdatedAt: "2026-07-16T10:11:00Z",
				Title: "Child", ForkedFrom: "parent", WorkDir: "/tmp/project",
				Agents: map[string]agentMeta{"main": {Type: "main"}},
			},
			Wires: map[string][]string{"main": childLines},
		},
	})
	src := New(Options{KimiHome: home})
	overview, err := src.Overview(testContext(t), stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Overview(all): %v", err)
	}
	if overview.Sessions != 2 || overview.Messages != 4 {
		t.Errorf("overview sessions/messages = %d/%d, want 2/4 without copied child history", overview.Sessions, overview.Messages)
	}
	if overview.Tokens.Input != 400 || overview.Tokens.Cache.Read != 600 || overview.Tokens.Output != 30 {
		t.Errorf("fork-deduped tokens = %#v, want parent + child-only usage", overview.Tokens)
	}
}

func TestKimiCodeReportsUnavailableAndPartialDiagnosticsWithoutLosingValidRows(t *testing.T) {
	t.Run("missing sessions directory", func(t *testing.T) {
		src := New(Options{KimiHome: t.TempDir()})
		info := src.Info(testContext(t))
		if info.Available || info.Diagnostics.Status != "unavailable" ||
			!strings.Contains(info.Diagnostics.Reason, "sessions directory not found") {
			t.Errorf("missing Kimi home Info() = %#v", info)
		}
		if _, err := src.Overview(testContext(t), stats.PeriodQuery{Period: "all"}); err == nil {
			t.Fatal("Overview() with missing Kimi sessions succeeded, want unavailable error")
		}
	})

	t.Run("malformed and unsupported lines are partial", func(t *testing.T) {
		home := writeKimiHome(t, map[string]sessionFixture{
			"partial": {
				State: sessionState{
					Title: "Partial", WorkDir: "/tmp/project",
					Agents: map[string]agentMeta{"main": {Type: "main"}},
				},
				Wires: map[string][]string{
					"main": {
						`{"type":"turn.prompt","input":[{"type":"text","text":"valid prompt"}],"origin":{"kind":"user"},"time":1784196001000}`,
						`{"type":"future.protocol.event","time":1784196001100}`,
						`{not-json`,
					},
				},
			},
		})
		src := New(Options{KimiHome: home})
		overview, err := src.Overview(testContext(t), stats.PeriodQuery{Period: "all"})
		if err != nil {
			t.Fatalf("partial Overview(all): %v", err)
		}
		if overview.Messages != 1 {
			t.Errorf("partial overview messages = %d, want valid prompt retained", overview.Messages)
		}
		info := src.Info(testContext(t))
		if !info.Available || info.Diagnostics.Status != "partial" ||
			info.Diagnostics.MalformedLines != 1 || info.Diagnostics.UnsupportedEvents != 1 {
			t.Errorf("partial diagnostics = %#v", info.Diagnostics)
		}
	})
}

func TestKimiCodeSubagentRowsRollUpIntoParentSession(t *testing.T) {
	parent := "main"
	home := writeKimiHome(t, map[string]sessionFixture{
		"subagents": {
			State: sessionState{
				CreatedAt: "2026-07-16T10:00:00Z", UpdatedAt: "2026-07-16T10:01:00Z",
				Title: "Subagents", WorkDir: "/tmp/project",
				Agents: map[string]agentMeta{
					"main":    {Type: "main"},
					"agent-0": {Type: "sub", ParentAgentID: &parent},
				},
			},
			Wires: map[string][]string{
				"main": {
					`{"type":"turn.prompt","input":[{"type":"text","text":"Main"}],"origin":{"kind":"user"},"time":1784196001000}`,
				},
				"agent-0": {
					`{"type":"turn.prompt","input":[{"type":"text","text":"Sub task"}],"origin":{"kind":"user"},"time":1784196002000}`,
					`{"type":"llm.request","kind":"loop","provider":"kimi","model":"k3","modelAlias":"kimi-code/k3","turnStep":"0.1","time":1784196002100}`,
					`{"type":"usage.record","model":"kimi-code/k3","usage":{"inputOther":10,"inputCacheRead":20,"inputCacheCreation":0,"output":3},"usageScope":"turn","time":1784196002200}`,
				},
			},
		},
	})
	src := New(Options{KimiHome: home})
	messages, err := src.Messages(testContext(t), stats.PeriodQuery{Period: "all"}, 1, 20, stats.DefaultMessageSort())
	if err != nil {
		t.Fatalf("Messages(all): %v", err)
	}
	subagentRows := 0
	for _, msg := range messages.Messages {
		if msg.IsSubagent {
			subagentRows++
			if msg.Agent != "agent-0" {
				t.Errorf("subagent label = %q, want agent-0", msg.Agent)
			}
			if msg.SessionID != "subagents" {
				t.Errorf("subagent SessionID = %q, want subagents", msg.SessionID)
			}
		}
	}
	if subagentRows != 2 {
		t.Errorf("subagent rows = %d, want prompt + request", subagentRows)
	}
}

func TestKimiCodeConfigIsStructurallyRedacted(t *testing.T) {
	home := writeKimiHome(t, map[string]sessionFixture{
		"config": {
			State: sessionState{Agents: map[string]agentMeta{"main": {Type: "main"}}},
			Wires: map[string][]string{"main": {`{"type":"metadata","protocol_version":"1.4","created_at":1784196000000}`}},
		},
	})
	config := `
default_model = "kimi-code/k3"

[providers.private]
type = "kimi"
base_url = "https://api.kimi.com/coding/v1"
api_key = "MUST_NOT_LEAK"

[providers.private.headers]
Authorization = "Bearer MUST_NOT_LEAK"
`
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	src := New(Options{KimiHome: home})
	view, err := src.Config(testContext(t))
	if err != nil {
		t.Fatalf("Config(): %v", err)
	}
	if !view.Exists || !view.Redacted || view.Content == nil || view.Raw == "" {
		t.Fatalf("Config() = %#v, want parsed redacted TOML", view)
	}
	encoded, _ := json.Marshal(view)
	if strings.Contains(string(encoded), "MUST_NOT_LEAK") {
		t.Errorf("Config() leaked secret: %s", encoded)
	}
}

func TestKimiPricingCatalogIncludesRequestedModelsAndManagedAliases(t *testing.T) {
	src := New(Options{KimiHome: t.TempDir()})
	pricing := src.loadPricing(testContext(t))
	expected := map[string]pricingRate{
		"kimi-k2.5":                {ContextTokens: 262144, InputPerMillion: 0.60, CacheHitPerMillion: 0.10, OutputPerMillion: 3.00},
		"kimi-k2.6":                {ContextTokens: 262144, InputPerMillion: 0.95, CacheHitPerMillion: 0.16, OutputPerMillion: 4.00},
		"kimi-k2.7-code":           {ContextTokens: 262144, InputPerMillion: 0.95, CacheHitPerMillion: 0.19, OutputPerMillion: 4.00},
		"kimi-k2.7-code-highspeed": {ContextTokens: 262144, InputPerMillion: 1.90, CacheHitPerMillion: 0.38, OutputPerMillion: 8.00},
		"kimi-k3":                  {ContextTokens: 1048576, InputPerMillion: 3.00, CacheHitPerMillion: 0.30, OutputPerMillion: 15.00},
	}
	for model, want := range expected {
		rate, ok := pricing.Models[model]
		if !ok {
			t.Errorf("pricing model %q is missing", model)
			continue
		}
		if rate.ContextTokens != want.ContextTokens ||
			rate.InputPerMillion != want.InputPerMillion ||
			rate.CacheHitPerMillion != want.CacheHitPerMillion ||
			rate.OutputPerMillion != want.OutputPerMillion {
			t.Errorf("pricing model %q = %#v, want %#v", model, rate, want)
		}
	}
	for alias, canonical := range map[string]string{
		"k3":                                  "kimi-k3",
		"kimi-code/k3":                        "kimi-k3",
		"kimi-code/kimi-for-coding":           "kimi-k2.7-code",
		"kimi-code/kimi-for-coding-highspeed": "kimi-k2.7-code-highspeed",
	} {
		if got := pricing.Aliases[alias]; got != canonical {
			t.Errorf("alias %q = %q, want %q", alias, got, canonical)
		}
	}
	unknown := computeCost("custom-provider/private-model", stats.TokenStats{Input: 1000, Output: 100}, pricing)
	if unknown.Cost != 0 || unknown.Status != stats.CostMissing || unknown.Provenance == nil || unknown.Provenance.MissingCount != 1 {
		t.Errorf("unknown custom model cost = %#v, want explicitly missing instead of guessed", unknown)
	}
}

func TestKimiCodeSafeFixtureShapeWhenConfigured(t *testing.T) {
	home := os.Getenv("KIMI_CODE_SAFE_FIXTURE")
	if home == "" {
		t.Skip("KIMI_CODE_SAFE_FIXTURE is not set")
	}
	src := New(Options{KimiHome: home, ScanTimeout: 30 * time.Second})
	ctx := testContext(t)
	info := src.Info(ctx)
	if !info.Available || info.Diagnostics.ScannedFiles == 0 {
		t.Errorf("safe fixture source info = %#v, want readable agent wires", info)
	}
	overview, err := src.Overview(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("safe fixture Overview(all): %v", err)
	}
	if overview.Sessions == 0 || overview.Requests == 0 || overview.Messages < overview.Requests {
		t.Errorf("safe fixture structural totals = %#v", overview)
	}
	if overview.Tokens.Input == 0 || overview.Tokens.Cache.Read == 0 || overview.Tokens.Output == 0 {
		t.Errorf("safe fixture tokens = %#v, want recorded K3 usage", overview.Tokens)
	}
	if overview.Cost <= 0 || overview.CostStatus != stats.CostEstimatedAPIEquivalent {
		t.Errorf("safe fixture cost = %.8f/%q, want a priced K3 API-equivalent estimate", overview.Cost, overview.CostStatus)
	}

	var userRows, assistantRows int64
	var messageTotal int64
	for page := 1; ; page++ {
		messages, err := src.Messages(ctx, stats.PeriodQuery{Period: "all"}, page, stats.MaxPageSize, stats.MessageSort{Field: stats.MessageSortTime, Direction: stats.MessageSortAsc})
		if err != nil {
			t.Fatalf("safe fixture Messages(all), page %d: %v", page, err)
		}
		messageTotal = messages.Total
		for _, message := range messages.Messages {
			switch message.Role {
			case "user":
				userRows++
			case "assistant":
				assistantRows++
			}
		}
		if userRows+assistantRows >= messages.Total || len(messages.Messages) == 0 {
			break
		}
	}
	if assistantRows != overview.Requests || userRows+assistantRows != messageTotal {
		t.Errorf("safe fixture user/assistant rows = %d/%d, overview requests/messages = %d/%d", userRows, assistantRows, overview.Requests, overview.Messages)
	}
	if overview.RequestAccounting == nil ||
		overview.RequestAccounting.UsageRecorded+overview.RequestAccounting.UsageRecovered+overview.RequestAccounting.UsageUnavailable != overview.Requests {
		t.Errorf("safe fixture request accounting = %#v, requests = %d", overview.RequestAccounting, overview.Requests)
	}

	disc := discoverSessions(ctx, home)
	var rawRequests, rawUsage int64
	for _, files := range disc.sessions {
		parsed, _, parseErr := parseSession(ctx, files)
		if parseErr != nil {
			t.Fatalf("read-only raw audit parse: %v", parseErr)
		}
		for _, agent := range parsed.Agents {
			seenRequests := make(map[string]bool)
			for _, record := range agent.Records {
				if record.Request != nil {
					if key := durableWireRecordKey(record); key != "" {
						if seenRequests[key] {
							continue
						}
						seenRequests[key] = true
					}
					rawRequests++
				}
				if record.Usage != nil {
					rawUsage++
				}
			}
		}
	}
	if overview.Requests < rawRequests || overview.RequestAccounting.UsageRecorded > rawUsage {
		t.Errorf("read-only raw audit requests/usages = %d/%d, normalized = %d/%#v", rawRequests, rawUsage, overview.Requests, overview.RequestAccounting)
	}

	models, err := src.Models(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("safe fixture Models(all): %v", err)
	}
	var modeledRequests int64
	for _, model := range models.Models {
		modeledRequests += model.Messages
	}
	if modeledRequests <= 0 || modeledRequests > overview.Requests {
		t.Errorf("safe fixture modeled requests = %d, overview requests = %d", modeledRequests, overview.Requests)
	}

	tools, err := src.Tools(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("safe fixture Tools(all): %v", err)
	}
	var invocations int64
	for _, tool := range tools.Tools {
		invocations += tool.Invocations
	}
	if invocations == 0 {
		t.Errorf("safe fixture tool invocations = %d, want at least one parsed call", invocations)
	}

	sessions, err := src.Sessions(ctx, stats.SessionQuery{Period: "all", Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("safe fixture Sessions(all): %v", err)
	}
	if sessions.Total < overview.Sessions {
		t.Errorf("safe fixture session rows = %d, active overview sessions = %d", sessions.Total, overview.Sessions)
	}
}

type sessionFixture struct {
	State sessionState
	Wires map[string][]string
}

func writeKimiHome(t *testing.T, fixtures map[string]sessionFixture) string {
	t.Helper()
	home := t.TempDir()
	for id, fixture := range fixtures {
		sessionDir := filepath.Join(home, "sessions", "wd_fixture", "session_"+id)
		if err := os.MkdirAll(sessionDir, 0o755); err != nil {
			t.Fatalf("create session dir: %v", err)
		}
		stateBytes, err := json.MarshalIndent(fixture.State, "", "  ")
		if err != nil {
			t.Fatalf("marshal state: %v", err)
		}
		if err := os.WriteFile(filepath.Join(sessionDir, "state.json"), stateBytes, 0o644); err != nil {
			t.Fatalf("write state: %v", err)
		}
		for agentID, lines := range fixture.Wires {
			agentDir := filepath.Join(sessionDir, "agents", agentID)
			if err := os.MkdirAll(agentDir, 0o755); err != nil {
				t.Fatalf("create agent dir: %v", err)
			}
			if err := os.WriteFile(filepath.Join(agentDir, "wire.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
				t.Fatalf("write wire: %v", err)
			}
		}
	}
	return home
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func findModel(t *testing.T, models stats.ModelStats, id string) stats.ModelEntry {
	t.Helper()
	for _, model := range models.Models {
		if model.ModelID == id {
			return model
		}
	}
	t.Fatalf("model %q not found in %#v", id, models.Models)
	return stats.ModelEntry{}
}
