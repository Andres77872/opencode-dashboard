package claudecode

import (
	"encoding/json"
	"strings"
	"testing"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

func TestParserToleratesMalformedUnknownAndMissingOptionalFields(t *testing.T) {
	src := newFixtureSource(t, "valid_home")
	ctx := testContext(t)

	list, err := src.Messages(ctx, stats.PeriodQuery{Period: "all"}, 1, 200, stats.DefaultMessageSort())
	if err != nil {
		t.Fatalf("Messages(all) failed: %v", err)
	}

	interaction := findMessage(t, list, func(msg stats.MessageEntry) bool {
		return msg.SessionID == "malformed-session" && msg.ID == "claude_code:malformed-session:malformed-user-before"
	})
	assertAllSourceID(t, interaction.SourceID)
	if interaction.Role != "assistant" {
		t.Errorf("malformed-session grouped interaction role = %q, want assistant after folding the assistant row into the user prompt", interaction.Role)
	}
	if interaction.SessionTitle != "This valid line appears before malformed JSON." {
		t.Errorf("malformed-session title = %q, want user prompt text", interaction.SessionTitle)
	}
	if interaction.ModelID != "" {
		t.Errorf("missing optional model normalized as %q, want empty/unknown", interaction.ModelID)
	}
	if interaction.ProviderID != "" {
		t.Errorf("missing optional provider normalized as %q, want empty/unknown", interaction.ProviderID)
	}
	if interaction.CostStatus != stats.CostMissing {
		t.Errorf("missing optional cost status = %q, want %q", interaction.CostStatus, stats.CostMissing)
	}
	if interaction.Cost != 0 {
		t.Errorf("missing optional cost compatibility value = %v, want 0", interaction.Cost)
	}

	count := 0
	for _, msg := range list.Messages {
		if msg.SessionID == "malformed-session" {
			count++
			if msg.Role == "user" {
				t.Errorf("malformed-session contains separate user row %#v; user prompt should be folded into one interaction detail", msg)
			}
		}
	}
	if count != 1 {
		t.Errorf("malformed-session message count = %d, want 1 grouped interaction", count)
	}

	detail := mustMessageDetail(t, src, interaction.ID)
	if !detailContainsText(detail, "This valid line appears before malformed JSON.") {
		t.Errorf("MessageDetail(%q) missing folded user prompt text: %#v", interaction.ID, detail.Content.TextParts)
	}
	if !detailContainsText(detail, "Optional fields are missing") {
		t.Errorf("MessageDetail(%q) missing folded assistant text: %#v", interaction.ID, detail.Content.TextParts)
	}

	info := src.Info(ctx)
	if info.Diagnostics.MalformedLines != 1 {
		t.Errorf("MalformedLines = %d, want 1", info.Diagnostics.MalformedLines)
	}
	if info.Diagnostics.UnsupportedEvents != 1 {
		t.Errorf("UnsupportedEvents = %d, want 1", info.Diagnostics.UnsupportedEvents)
	}
}

func TestParserDiagnosticsDoNotLeakRawLinesOrUnknownPayloads(t *testing.T) {
	src := newFixtureSource(t, "valid_home")
	ctx := testContext(t)

	if _, err := src.Messages(ctx, stats.PeriodQuery{Period: "all"}, 1, 200, stats.DefaultMessageSort()); err != nil {
		t.Fatalf("Messages(all) failed: %v", err)
	}

	info := src.Info(ctx)
	encoded, err := json.Marshal(info.Diagnostics)
	if err != nil {
		t.Fatalf("marshal diagnostics: %v", err)
	}
	text := string(encoded)
	for _, forbidden := range []string{
		"this is not valid json",
		"unknown-event-secret-must-not-leak",
		"raw_secret",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("diagnostics leaked raw transcript payload %q in %s", forbidden, text)
		}
	}
	if info.Diagnostics.Status == "" {
		t.Errorf("Diagnostics.Status is empty, want parse health status")
	}
	if info.ID != source.SourceClaudeCode {
		t.Errorf("Info().ID = %q, want %q", info.ID, source.SourceClaudeCode)
	}
}

func TestParserPreservesTopLevelIsMetaTrueOnly(t *testing.T) {
	tests := []struct {
		name          string
		line          string
		wantIsMeta    bool
		wantRole      string
		wantSessionID string
		wantText      string
	}{
		{
			name:          "explicit top-level true marks user-shaped metadata",
			line:          `{"type":"user","uuid":"meta-user-1","sessionId":"safe-shape-session","timestamp":"2026-04-01T10:00:02Z","cwd":"/redacted/safe-project","isMeta":true,"message":{"role":"user","content":"METADATA PROMPT-LIKE TEXT: this must not become a dashboard interaction."}}`,
			wantIsMeta:    true,
			wantRole:      "user",
			wantSessionID: "safe-shape-session",
			wantText:      "METADATA PROMPT-LIKE TEXT",
		},
		{
			name:          "missing isMeta keeps legitimate prompt eligible",
			line:          `{"type":"user","message":{"role":"user","content":"Optional fields are missing but this prompt is legitimate."}}`,
			wantRole:      "user",
			wantSessionID: "fallback-session",
			wantText:      "Optional fields are missing",
		},
		{
			name:          "explicit false does not mark metadata",
			line:          `{"type":"user","uuid":"meta-false-user","session_id":"meta-false-session","timestamp":"2026-04-01T10:00:00Z","isMeta":false,"message":{"role":"user","content":"Explicit false remains a real prompt candidate."}}`,
			wantRole:      "user",
			wantSessionID: "meta-false-session",
			wantText:      "Explicit false remains",
		},
		{
			name:          "string true is malformed metadata marker and ignored",
			line:          `{"type":"user","uuid":"meta-string-user","session_id":"meta-string-session","timestamp":"2026-04-01T10:00:00Z","isMeta":"true","message":{"role":"user","content":"String true must not over-filter this prompt."}}`,
			wantRole:      "user",
			wantSessionID: "meta-string-session",
			wantText:      "String true must not",
		},
		{
			name:          "numeric true is malformed metadata marker and ignored",
			line:          `{"type":"user","uuid":"meta-number-user","session_id":"meta-number-session","timestamp":"2026-04-01T10:00:00Z","isMeta":1,"message":{"role":"user","content":"Numeric metadata markers must not suppress prompts."}}`,
			wantRole:      "user",
			wantSessionID: "meta-number-session",
			wantText:      "Numeric metadata markers",
		},
		{
			name:          "nested message isMeta is not a top-level metadata marker",
			line:          `{"type":"user","uuid":"nested-meta-user","session_id":"nested-meta-session","timestamp":"2026-04-01T10:00:00Z","message":{"role":"user","isMeta":true,"content":"Nested metadata-looking fields must not over-filter prompts."}}`,
			wantRole:      "user",
			wantSessionID: "nested-meta-session",
			wantText:      "Nested metadata-looking",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := mustParseClaudeJSONLine(t, tt.line)

			if record.IsMeta != tt.wantIsMeta {
				t.Errorf("parsedRecord.IsMeta = %v, want %v", record.IsMeta, tt.wantIsMeta)
			}
			if record.Role != tt.wantRole {
				t.Errorf("parsedRecord.Role = %q, want %q", record.Role, tt.wantRole)
			}
			if record.SessionID != tt.wantSessionID {
				t.Errorf("parsedRecord.SessionID = %q, want %q", record.SessionID, tt.wantSessionID)
			}
			if !parsedTextContains(record, tt.wantText) {
				t.Errorf("parsedRecord.TextParts = %#v, want text containing %q", record.TextParts, tt.wantText)
			}
		})
	}
}

func TestParserToleratesSafeShapeIdentityAndExtraUsageKeys(t *testing.T) {
	record := mustParseClaudeJSONLine(t, `{"type":"assistant","uuid":"safe-assistant-tool-1","parentUuid":"safe-user-1","sessionId":"safe-shape-session","requestId":"req_safe_tool_1","timestamp":"2026-04-01T10:00:01Z","cwd":"/redacted/safe-project","message":{"id":"msg_safe_tool_1","role":"assistant","model":"claude-test-computed","content":[{"type":"tool_use","id":"toolu_safe_read","name":"Read","input":{"file_path":"REDACTED.md"}}],"usage":{"input_tokens":100,"output_tokens":10,"cache_read_input_tokens":5,"cache_creation_input_tokens":7,"cache_creation":{"ephemeral_5m_input_tokens":7},"server_tool_use":{"web_search_requests":0},"service_tier":"standard","inference_geo":"us","iterations":1,"speed":"normal"}}}`)

	if record.IsMeta {
		t.Errorf("assistant safe-shape record IsMeta = true, want false without explicit top-level isMeta:true")
	}
	if record.SessionID != "safe-shape-session" {
		t.Errorf("SessionID = %q, want camelCase sessionId parsed", record.SessionID)
	}
	if record.UUID != "safe-assistant-tool-1" {
		t.Errorf("UUID = %q, want top-level uuid parsed", record.UUID)
	}
	if record.RequestID != "req_safe_tool_1" {
		t.Errorf("RequestID = %q, want top-level requestId parsed", record.RequestID)
	}
	if record.APIMessageID != "msg_safe_tool_1" {
		t.Errorf("APIMessageID = %q, want nested message.id parsed", record.APIMessageID)
	}
	if record.Role != "assistant" {
		t.Errorf("Role = %q, want assistant", record.Role)
	}
	if record.Model != "claude-test-computed" {
		t.Errorf("Model = %q, want claude-test-computed", record.Model)
	}
	if !record.HasUsage {
		t.Fatalf("HasUsage = false, want standard token fields parsed despite extra usage keys")
	}
	if record.Usage.Input != 100 || record.Usage.Output != 10 || record.Usage.CacheRead != 5 || record.Usage.CacheCreate != 7 {
		t.Errorf("Usage = %#v, want input/output/cache read/cache create 100/10/5/7", record.Usage)
	}
	if record.ReportedUSD != nil {
		t.Errorf("ReportedUSD = %v, want nil; safe extra usage keys must not be mistaken for reported cost", *record.ReportedUSD)
	}
	if len(record.ToolUses) != 1 || record.ToolUses[0].ID != "toolu_safe_read" {
		t.Errorf("ToolUses = %#v, want safe tool_use parsed", record.ToolUses)
	}
}

func TestParserSplitsCacheCreationUsageTiers(t *testing.T) {
	tests := []struct {
		name      string
		raw       map[string]any
		wantUsage tokenUsage
	}{
		{
			name: "nested five minute and one hour cache creation are split",
			raw: map[string]any{
				"input_tokens":                int64(11),
				"output_tokens":               int64(7),
				"cache_read_input_tokens":     int64(3),
				"cache_creation_input_tokens": int64(30),
				"cache_creation":              map[string]any{"ephemeral_5m_input_tokens": int64(10), "ephemeral_1h_input_tokens": int64(20)},
			},
			wantUsage: tokenUsage{Input: 11, Output: 7, CacheRead: 3, CacheCreate: 30, CacheCreate5m: 10, CacheCreate1h: 20},
		},
		{
			name: "aggregate only cache creation remains five minute default",
			raw: map[string]any{
				"cache_creation_input_tokens": int64(30),
			},
			wantUsage: tokenUsage{CacheCreate: 30, CacheCreate5m: 30},
		},
		{
			name: "aggregate total remains public cache write count when nested split is partial",
			raw: map[string]any{
				"cache_creation_input_tokens": int64(50),
				"cache_creation":              map[string]any{"ephemeral_5m_input_tokens": int64(10), "ephemeral_1h_input_tokens": int64(20)},
			},
			wantUsage: tokenUsage{CacheCreate: 50, CacheCreate5m: 30, CacheCreate1h: 20},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usage, ok := parseUsage(tt.raw)
			if !ok {
				t.Fatalf("parseUsage() ok = false, want true")
			}
			if usage != tt.wantUsage {
				t.Errorf("parseUsage() = %#v, want %#v", usage, tt.wantUsage)
			}
		})
	}
}

func mustParseClaudeJSONLine(t *testing.T, line string) parsedRecord {
	t.Helper()
	record, ok, malformed := parseLine(transcriptFile{Path: "synthetic-safe-shape.jsonl", ProjectID: "-redacted-safe", SessionID: "fallback-session"}, 1, line)
	if malformed {
		t.Fatalf("parseLine(%s) reported malformed JSON", line)
	}
	if !ok {
		t.Fatalf("parseLine(%s) returned ok=false, want parsed record", line)
	}
	return record
}

func parsedTextContains(record parsedRecord, want string) bool {
	for _, text := range record.TextParts {
		if strings.Contains(text, want) {
			return true
		}
	}
	return false
}
