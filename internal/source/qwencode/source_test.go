package qwencode

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

func TestQwenCodeReconcilesTranscriptsTelemetryAndUsageLog(t *testing.T) {
	home := t.TempDir()
	// One API call recorded three times (assistant record + telemetry echo +
	// usage-log row), one subagent call recorded twice (telemetry + usage
	// log), and one transcript-less session recorded only in the usage log.
	writeChat(t, home, "-home-synthetic-private-project", "sess-1", []string{
		`{"type":"user","uuid":"u1","sessionId":"sess-1","timestamp":"2026-07-16T10:00:00.000Z","cwd":"/home/synthetic/private-project","version":"0.20.0","message":{"role":"user","parts":[{"text":"Inspect /home/synthetic/private-project"}]}}`,
		`{"type":"system","subtype":"slash_command","uuid":"s0","sessionId":"sess-1","timestamp":"2026-07-16T10:00:00.500Z","cwd":"/home/synthetic/private-project","systemPayload":{"command":"/init"}}`,
		`{"type":"system","subtype":"ui_telemetry","uuid":"s1","sessionId":"sess-1","timestamp":"2026-07-16T10:00:04.000Z","cwd":"/home/synthetic/private-project","systemPayload":{"uiEvent":{"event.name":"qwen-code.api_response","model":"qwen3.7-plus","auth_type":"openai","input_token_count":1000,"output_token_count":150,"cached_content_token_count":600,"thoughts_token_count":50,"total_token_count":1150,"duration_ms":1200}}}`,
		`{"type":"assistant","uuid":"a1","sessionId":"sess-1","timestamp":"2026-07-16T10:00:05.000Z","cwd":"/home/synthetic/private-project","model":"qwen3.7-plus","message":{"role":"model","parts":[{"text":"Reasoning about the repo","thought":true},{"text":"Done."},{"functionCall":{"id":"call-1","name":"read_file","args":{"path":"/home/synthetic/private-project/README.md","secret":"MUST_NOT_LEAK"}}}]},"usageMetadata":{"promptTokenCount":1000,"candidatesTokenCount":150,"cachedContentTokenCount":600,"thoughtsTokenCount":50,"totalTokenCount":1150}}`,
		`{"type":"tool_result","uuid":"tr1","sessionId":"sess-1","timestamp":"2026-07-16T10:00:06.000Z","cwd":"/home/synthetic/private-project","message":{"role":"user","parts":[{"functionResponse":{"id":"call-1"}}]},"toolCallResult":{"callId":"call-1","status":"success","resultDisplay":"read /home/synthetic/private-project/README.md"}}`,
		`{"type":"system","subtype":"ui_telemetry","uuid":"s2","sessionId":"sess-1","timestamp":"2026-07-16T10:00:08.000Z","cwd":"/home/synthetic/private-project","systemPayload":{"uiEvent":{"event.name":"qwen-code.api_response","model":"qwen3.7-plus","auth_type":"openai","subagent_name":"managed-auto-memory-extractor","input_token_count":200,"output_token_count":30,"cached_content_token_count":0,"thoughts_token_count":10,"total_token_count":230,"duration_ms":800}}}`,
		`{"type":"system","subtype":"ui_telemetry","uuid":"s3","sessionId":"sess-1","timestamp":"2026-07-16T10:00:09.000Z","cwd":"/home/synthetic/private-project","systemPayload":{"uiEvent":{"event.name":"qwen-code.api_error","model":"qwen3.7-plus","auth_type":"openai","status_code":429,"duration_ms":100}}}`,
	})
	writeUsageLog(t, home, "2026-07", []string{
		`{"schemaVersion":1,"id":"ur-1","timestamp":"2026-07-16T10:00:05.100Z","localDate":"2026-07-16","localMonth":"2026-07","sessionId":"sess-1","model":"qwen3.7-plus","authType":"openai","source":"main","inputTokens":1000,"outputTokens":150,"cachedTokens":600,"thoughtsTokens":50,"totalTokens":1150,"apiDurationMs":1200}`,
		`{"schemaVersion":1,"id":"ur-2","timestamp":"2026-07-16T10:00:08.100Z","localDate":"2026-07-16","localMonth":"2026-07","sessionId":"sess-1","model":"qwen3.7-plus","authType":"openai","source":"managed-auto-memory-extractor","inputTokens":200,"outputTokens":30,"cachedTokens":0,"thoughtsTokens":10,"totalTokens":230,"apiDurationMs":800}`,
		`{"schemaVersion":1,"id":"ur-3","timestamp":"2026-07-17T09:00:00.000Z","localDate":"2026-07-17","localMonth":"2026-07","sessionId":"sess-2","model":"qwen-custom-endpoint","authType":"openai","source":"main","inputTokens":500,"outputTokens":50,"cachedTokens":100,"thoughtsTokens":0,"totalTokens":550,"apiDurationMs":900}`,
	})
	writeUsageRecords(t, home, []string{
		`{"version":1,"sessionId":"sess-2","timestamp":1784624460000,"startTime":1784624400000,"project":"/home/synthetic/other-project","durationMs":60000,"models":{"qwen-custom-endpoint":{"requests":1,"inputTokens":500,"outputTokens":50,"cachedTokens":100,"thoughtsTokens":0,"totalTokens":550,"totalLatencyMs":900}},"tools":{"totalCalls":0,"totalSuccess":0,"totalFail":0,"byName":{}},"files":{"linesAdded":0,"linesRemoved":0}}`,
	})

	src := New(Options{QwenHome: home, PathSource: "test fixture"})
	ctx := testContext(t)
	period := stats.PeriodQuery{Period: "all"}

	info := src.Info(ctx)
	if info.ID != source.SourceQwenCode || !info.Available || info.Diagnostics.Status != "ok" {
		t.Fatalf("Info() = %#v, want available Qwen Code source", info)
	}
	if info.CostPolicy.Status != string(stats.CostEstimatedAPIEquivalent) {
		t.Errorf("cost policy = %#v, want API-equivalent", info.CostPolicy)
	}

	overview, err := src.Overview(ctx, period)
	if err != nil {
		t.Fatalf("Overview(all): %v", err)
	}
	if overview.Sessions != 2 || overview.Messages != 4 {
		t.Errorf("overview sessions/messages = %d/%d, want 2/4 (user + assistant + subagent + usage-only)", overview.Sessions, overview.Messages)
	}
	if overview.Requests != 3 {
		t.Errorf("overview requests = %d, want 3 assistant/API-request rows without the user prompt", overview.Requests)
	}
	if overview.Tokens.Input != 1000 || overview.Tokens.Output != 170 ||
		overview.Tokens.Reasoning != 60 || overview.Tokens.Cache.Read != 700 || overview.Tokens.Cache.Write != 0 {
		t.Errorf("token totals = %#v, want overlap-free sums across all three stores", overview.Tokens)
	}
	if math.Abs(overview.Cost-0.000552) > 1e-12 {
		t.Errorf("overview cost = %.12f, want 0.000552", overview.Cost)
	}
	if overview.CostStatus != stats.CostMixed {
		t.Errorf("overview cost status = %q, want mixed (priced qwen3.7-plus + off-catalog qwen-custom-endpoint)", overview.CostStatus)
	}

	models, err := src.Models(ctx, period)
	if err != nil {
		t.Fatalf("Models(all): %v", err)
	}
	if len(models.Models) != 2 {
		t.Fatalf("models = %#v, want qwen3.7-plus and qwen-custom-endpoint", models.Models)
	}
	plus := findModel(t, models, "qwen3.7-plus")
	if plus.ProviderID != "qwen" || plus.Messages != 2 {
		t.Errorf("qwen3.7-plus entry = %#v, want provider qwen with 2 requests", plus)
	}
	custom := findModel(t, models, "qwen-custom-endpoint")
	if custom.CostStatus != stats.CostMissing {
		t.Errorf("qwen-custom-endpoint cost status = %q, want missing (outside the bundled catalog)", custom.CostStatus)
	}

	tools, err := src.Tools(ctx, period)
	if err != nil {
		t.Fatalf("Tools(all): %v", err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != "read_file" ||
		tools.Tools[0].Invocations != 1 || tools.Tools[0].Successes != 1 {
		t.Errorf("tools = %#v, want one completed read_file", tools.Tools)
	}

	sessions, err := src.Sessions(ctx, stats.SessionQuery{Period: "all", Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("Sessions(all): %v", err)
	}
	if sessions.Total != 2 {
		t.Fatalf("sessions total = %d, want transcript + usage-only sessions", sessions.Total)
	}
	detail, err := src.SessionByID(ctx, "sess-1")
	if err != nil {
		t.Fatalf("SessionByID(sess-1): %v", err)
	}
	if detail == nil || detail.MessageCount != 3 {
		t.Fatalf("session detail = %#v, want 3 rows", detail)
	}
	if strings.Contains(detail.Directory, "/home/synthetic") {
		t.Errorf("session directory leaked absolute path: %q", detail.Directory)
	}
	var subagentRows int
	for _, msg := range detail.Messages {
		if msg.IsSubagent {
			subagentRows++
			if msg.Agent != "managed-auto-memory-extractor" {
				t.Errorf("subagent label = %q", msg.Agent)
			}
		}
	}
	if subagentRows != 1 {
		t.Errorf("subagent rows = %d, want exactly one telemetry-synthesized request", subagentRows)
	}

	messages, err := src.Messages(ctx, period, 1, 20, stats.MessageSort{Field: stats.MessageSortTime, Direction: stats.MessageSortAsc})
	if err != nil {
		t.Fatalf("Messages(all): %v", err)
	}
	var transcriptAssistant stats.MessageEntry
	for _, msg := range messages.Messages {
		if msg.Role == "assistant" && msg.SessionID == "sess-1" && !msg.IsSubagent {
			transcriptAssistant = msg
			break
		}
	}
	if transcriptAssistant.ID == "" {
		t.Fatalf("transcript assistant row not found: %#v", messages.Messages)
	}
	if transcriptAssistant.Tokens == nil || transcriptAssistant.Tokens.Input != 400 ||
		transcriptAssistant.Tokens.Output != 100 || transcriptAssistant.Tokens.Reasoning != 50 ||
		transcriptAssistant.Tokens.Cache.Read != 600 {
		t.Errorf("assistant tokens = %#v, want cached carved from input and thoughts from output", transcriptAssistant.Tokens)
	}

	messageDetail, err := src.MessageByID(ctx, transcriptAssistant.ID)
	if err != nil {
		t.Fatalf("MessageByID(%q): %v", transcriptAssistant.ID, err)
	}
	if messageDetail == nil || len(messageDetail.Content.ReasoningParts) != 1 ||
		len(messageDetail.Content.TextParts) != 1 || len(messageDetail.Content.ToolParts) != 1 {
		t.Fatalf("message detail = %#v", messageDetail)
	}
	if messageDetail.Content.ToolParts[0].State.Status != "completed" {
		t.Errorf("tool status = %q, want completed", messageDetail.Content.ToolParts[0].State.Status)
	}
	encoded, _ := json.Marshal(messageDetail)
	if strings.Contains(string(encoded), "/home/synthetic") || strings.Contains(string(encoded), "MUST_NOT_LEAK") {
		t.Errorf("message detail leaked private data: %s", encoded)
	}
}

func TestQwenCodeGeminiAuthKeepsAdditiveThoughtCounters(t *testing.T) {
	home := t.TempDir()
	writeChat(t, home, "-tmp-project", "gem-1", []string{
		`{"type":"system","subtype":"ui_telemetry","uuid":"s1","sessionId":"gem-1","timestamp":"2026-07-16T10:00:00.000Z","cwd":"/tmp/project","systemPayload":{"uiEvent":{"event.name":"qwen-code.api_response","model":"gemini-2.5-pro","auth_type":"gemini","input_token_count":100,"output_token_count":40,"cached_content_token_count":0,"thoughts_token_count":25,"total_token_count":165}}}`,
		`{"type":"assistant","uuid":"a1","sessionId":"gem-1","timestamp":"2026-07-16T10:00:01.000Z","cwd":"/tmp/project","model":"gemini-2.5-pro","message":{"role":"model","parts":[{"text":"Done."}]},"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":40,"cachedContentTokenCount":0,"thoughtsTokenCount":25,"totalTokenCount":165}}`,
	})
	src := New(Options{QwenHome: home})
	overview, err := src.Overview(testContext(t), stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Overview(all): %v", err)
	}
	if overview.Messages != 1 {
		t.Fatalf("messages = %d, want the telemetry echo folded into the assistant row", overview.Messages)
	}
	// Gemini-native counters are additive: candidates excludes thoughts.
	if overview.Tokens.Output != 40 || overview.Tokens.Reasoning != 25 || overview.Tokens.Input != 100 {
		t.Errorf("gemini tokens = %#v, want output kept at 40 with 25 reasoning", overview.Tokens)
	}
}

func TestQwenCodeUsageLogOnlyHomeIsAvailable(t *testing.T) {
	home := t.TempDir()
	writeUsageLog(t, home, "2026-07", []string{
		`{"schemaVersion":1,"id":"ur-1","timestamp":"2026-07-16T10:00:00.000Z","localDate":"2026-07-16","localMonth":"2026-07","sessionId":"only-usage","model":"qwen3.7-max","authType":"qwen-oauth","source":"main","inputTokens":300,"outputTokens":20,"cachedTokens":0,"thoughtsTokens":0,"totalTokens":320,"apiDurationMs":500}`,
	})
	src := New(Options{QwenHome: home})
	ctx := testContext(t)
	info := src.Info(ctx)
	if !info.Available {
		t.Fatalf("Info() = %#v, want available with usage log only", info)
	}
	overview, err := src.Overview(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Overview(all): %v", err)
	}
	if overview.Sessions != 1 || overview.Messages != 1 {
		t.Errorf("overview = %#v, want one synthesized session/request", overview)
	}
	if overview.Tokens.Input != 300 || overview.Tokens.Output != 20 {
		t.Errorf("tokens = %#v", overview.Tokens)
	}
	if overview.CostStatus != stats.CostEstimatedAPIEquivalent || overview.Cost <= 0 {
		t.Errorf("cost = %.8f/%q, want priced qwen3.7-max estimate", overview.Cost, overview.CostStatus)
	}
	models, err := src.Models(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Models(all): %v", err)
	}
	if len(models.Models) != 1 || models.Models[0].ProviderID != "qwen" {
		t.Errorf("models = %#v, want qwen provider via model prefix", models.Models)
	}
}

func TestQwenCodeReportsUnavailableAndPartialDiagnostics(t *testing.T) {
	t.Run("missing home directory", func(t *testing.T) {
		src := New(Options{QwenHome: filepath.Join(t.TempDir(), "does-not-exist")})
		info := src.Info(testContext(t))
		if info.Available || info.Diagnostics.Status != "unavailable" ||
			!strings.Contains(info.Diagnostics.Reason, "home directory not found") {
			t.Errorf("missing home Info() = %#v", info)
		}
		if _, err := src.Overview(testContext(t), stats.PeriodQuery{Period: "all"}); err == nil {
			t.Fatal("Overview() with missing Qwen home succeeded, want unavailable error")
		}
	})

	t.Run("empty home", func(t *testing.T) {
		src := New(Options{QwenHome: t.TempDir()})
		info := src.Info(testContext(t))
		if info.Available || info.Diagnostics.Status != "empty" {
			t.Errorf("empty home Info() = %#v", info)
		}
	})

	t.Run("malformed and unknown lines are partial", func(t *testing.T) {
		home := t.TempDir()
		writeChat(t, home, "-tmp-project", "partial", []string{
			`{"type":"user","uuid":"u1","sessionId":"partial","timestamp":"2026-07-16T10:00:00.000Z","cwd":"/tmp/project","message":{"role":"user","parts":[{"text":"valid prompt"}]}}`,
			`{"type":"future_record_kind","uuid":"x1","timestamp":"2026-07-16T10:00:01.000Z"}`,
			`{not-json`,
		})
		src := New(Options{QwenHome: home})
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

func TestQwenCodeConfigIsStructurallyRedacted(t *testing.T) {
	home := t.TempDir()
	writeChat(t, home, "-tmp-project", "cfg", []string{
		`{"type":"user","uuid":"u1","sessionId":"cfg","timestamp":"2026-07-16T10:00:00.000Z","cwd":"/tmp/project","message":{"role":"user","parts":[{"text":"hello"}]}}`,
	})
	settings := `{
  "model": {"name": "qwen3.7-max", "baseUrl": "https://dashscope.example/v1"},
  "env": {"DASHSCOPE_API_KEY": "MUST_NOT_LEAK", "SOME_TOKEN_PLAN_API_KEY": "MUST_NOT_LEAK"},
  "security": {"auth": {"selectedType": "openai", "apiKey": "MUST_NOT_LEAK"}}
}`
	if err := os.WriteFile(filepath.Join(home, "settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	src := New(Options{QwenHome: home})
	view, err := src.Config(testContext(t))
	if err != nil {
		t.Fatalf("Config(): %v", err)
	}
	if !view.Exists || !view.Redacted || view.Content == nil || view.Raw == "" || view.Format != stats.ConfigFormatJSON {
		t.Fatalf("Config() = %#v, want parsed redacted JSON", view)
	}
	encoded, _ := json.Marshal(view)
	if strings.Contains(string(encoded), "MUST_NOT_LEAK") {
		t.Errorf("Config() leaked secret: %s", encoded)
	}
}

func TestQwenPricingCatalogPricesKnownModelsAndRefusesToGuess(t *testing.T) {
	src := New(Options{QwenHome: t.TempDir()})
	pricing := src.loadPricing(testContext(t))
	expected := map[string]pricingRate{
		"qwen3.7-max":         {InputPerMillion: 2.5, CacheHitPerMillion: 0.25, OutputPerMillion: 7.5},
		"qwen3.7-plus":        {InputPerMillion: 0.4, CacheHitPerMillion: 0.04, OutputPerMillion: 1.6},
		"qwen3-coder-plus":    {InputPerMillion: 1.0, CacheHitPerMillion: 0.1, OutputPerMillion: 5.0},
		"qwen3-max":           {InputPerMillion: 1.2, CacheHitPerMillion: 0.24, OutputPerMillion: 6.0},
		"qwen3.8-max":         {InputPerMillion: 2.0, CacheHitPerMillion: 0.25, CacheWritePerMillion: 2.5, OutputPerMillion: 6.0},
		"qwen3.8-max-preview": {InputPerMillion: 2.0, CacheHitPerMillion: 0.25, CacheWritePerMillion: 2.5, OutputPerMillion: 6.0},
	}
	for model, want := range expected {
		rate, ok := pricing.Models[model]
		if !ok {
			t.Errorf("pricing model %q is missing", model)
			continue
		}
		if rate.InputPerMillion != want.InputPerMillion ||
			rate.CacheHitPerMillion != want.CacheHitPerMillion ||
			rate.CacheWritePerMillion != want.CacheWritePerMillion ||
			rate.OutputPerMillion != want.OutputPerMillion {
			t.Errorf("pricing model %q = %#v, want %#v", model, rate, want)
		}
	}
	if got := pricing.Aliases["coder-model"]; got != "qwen3.7-max" {
		t.Errorf("coder-model alias = %q, want qwen3.7-max (current qwen-oauth mainline)", got)
	}
	unknown := computeCost("custom-endpoint/private-model", "custom-endpoint", stats.TokenStats{Input: 1000, Output: 100}, pricing)
	if unknown.Cost != 0 || unknown.Status != stats.CostMissing || unknown.Provenance == nil || unknown.Provenance.MissingCount != 1 {
		t.Errorf("unknown model cost = %#v, want explicitly missing instead of guessed", unknown)
	}
}

// The GA and preview 3.8 Max listings share one Model Studio price, so they must
// stay in lockstep rather than drifting into two different estimates.
func TestQwenMax38GAAndPreviewSharePricing(t *testing.T) {
	src := New(Options{QwenHome: t.TempDir()})
	ctx := testContext(t)
	pricing := src.loadPricing(ctx)

	ga, preview := pricing.Models["qwen3.8-max"], pricing.Models["qwen3.8-max-preview"]
	if ga.InputPerMillion != preview.InputPerMillion ||
		ga.CacheHitPerMillion != preview.CacheHitPerMillion ||
		ga.CacheWritePerMillion != preview.CacheWritePerMillion ||
		ga.OutputPerMillion != preview.OutputPerMillion {
		t.Fatalf("qwen3.8-max %#v and qwen3.8-max-preview %#v have diverged", ga, preview)
	}
	for _, model := range []string{"qwen3.8-max", "qwen3.8-max-preview"} {
		got := src.ResolvePricing(ctx, "qwen", model)
		if got.Kind != source.PricingResolutionExact || got.TargetModelID != model || got.Rate == nil {
			t.Fatalf("%s resolution = %#v, want an exact catalog match", model, got)
		}
		// 1M of each billable bucket: 2.0 in + 6.0 out + 0.25 cache read + 2.5 cache write.
		cost := computeCost(model, "qwen", stats.TokenStats{
			Input:  1_000_000,
			Output: 1_000_000,
			Cache:  stats.CacheStats{Read: 1_000_000, Write: 1_000_000},
		}, pricing)
		if cost.Status != stats.CostEstimatedAPIEquivalent || math.Abs(cost.Cost-10.75) > 1e-9 {
			t.Fatalf("%s cost = %#v, want 10.75 estimated", model, cost)
		}
	}
}

// Only the 3.8 Max rows publish a cache-write price; every other model keeps
// billing writes at its plain input rate.
func TestQwenCacheWriteFallsBackToInputRate(t *testing.T) {
	src := New(Options{QwenHome: t.TempDir()})
	pricing := src.loadPricing(testContext(t))

	tokens := stats.TokenStats{Cache: stats.CacheStats{Write: 1_000_000}}
	if got := computeCost("qwen3.8-max", "qwen", tokens, pricing); math.Abs(got.Cost-2.5) > 1e-9 {
		t.Errorf("qwen3.8-max cache-write cost = %.9f, want 2.5 (published rate)", got.Cost)
	}
	if got := computeCost("qwen3-max", "qwen", tokens, pricing); math.Abs(got.Cost-1.2) > 1e-9 {
		t.Errorf("qwen3-max cache-write cost = %.9f, want 1.2 (input-rate fallback)", got.Cost)
	}
}

func writeChat(t *testing.T, home, projectDir, sessionID string, lines []string) {
	t.Helper()
	chatsDir := filepath.Join(home, "projects", projectDir, "chats")
	if err := os.MkdirAll(chatsDir, 0o755); err != nil {
		t.Fatalf("create chats dir: %v", err)
	}
	path := filepath.Join(chatsDir, sessionID+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write chat transcript: %v", err)
	}
}

func writeUsageLog(t *testing.T, home, month string, lines []string) {
	t.Helper()
	usageDir := filepath.Join(home, "usage")
	if err := os.MkdirAll(usageDir, 0o755); err != nil {
		t.Fatalf("create usage dir: %v", err)
	}
	path := filepath.Join(usageDir, "token-usage-"+month+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write usage log: %v", err)
	}
}

func writeUsageRecords(t *testing.T, home string, lines []string) {
	t.Helper()
	path := filepath.Join(home, "usage_record.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write usage records: %v", err)
	}
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
