package claudecode

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opencode-dashboard/internal/stats"
)

func TestDuplicateClaudeTranscriptRecordsDoNotDoubleCountStableSessionMessageIdentity(t *testing.T) {
	// Stable dedupe and interaction-grouping semantics expected by this regression slice:
	// - UUID-bearing records dedupe by stable session/message identity plus
	//   equivalent semantic content, even when the same transcript is copied under
	//   another project/file path.
	// - UUID-less exact semantic duplicate records may dedupe by content fingerprint.
	// - Distinct assistant usage under the same user prompt folds into one
	//   user-facing interaction while continuing to contribute to tokens and cost.
	tests := []struct {
		name              string
		files             map[string][]string
		sessionID         string
		wantMessages      int64
		wantSessions      int64
		wantCost          float64
		wantInputTokens   int64
		wantOutputTokens  int64
		wantModelMessages int64
	}{
		{
			name: "duplicate transcript file copy with UUID-bearing session/message identity is counted once",
			files: map[string][]string{
				"-home-andres-projects-dedupe/duplicate-session.jsonl":      duplicateStableIdentityLines(),
				"-home-andres-projects-dedupe-copy/duplicate-session.jsonl": duplicateStableIdentityLines(),
			},
			sessionID:         "duplicate-session",
			wantMessages:      2,
			wantSessions:      1,
			wantCost:          30,
			wantInputTokens:   1000,
			wantOutputTokens:  200,
			wantModelMessages: 1,
		},
		{
			name: "duplicate UUID-bearing records in one transcript are counted once by session/message identity",
			files: map[string][]string{
				"-home-andres-projects-dedupe/duplicate-session.jsonl": append(
					duplicateStableIdentityLines(),
					duplicateStableIdentityLines()...,
				),
			},
			sessionID:         "duplicate-session",
			wantMessages:      2,
			wantSessions:      1,
			wantCost:          30,
			wantInputTokens:   1000,
			wantOutputTokens:  200,
			wantModelMessages: 1,
		},
		{
			name: "duplicate UUID-less semantic records are counted once while distinct assistant usage stays distinct",
			files: map[string][]string{
				"-home-andres-projects-dedupe/fingerprint-session.jsonl": uuidlessSemanticDuplicateWithDistinctAssistantLines(),
			},
			sessionID:         "fingerprint-session",
			wantMessages:      3,
			wantSessions:      1,
			wantCost:          45,
			wantInputTokens:   1500,
			wantOutputTokens:  300,
			wantModelMessages: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := newTempRegressionSource(t, tt.files)
			ctx := testContext(t)

			overview, err := src.Overview(ctx, stats.PeriodQuery{Period: "all"})
			if err != nil {
				t.Fatalf("Overview(all) failed: %v", err)
			}
			if overview.Messages != tt.wantMessages {
				t.Errorf("Overview().Messages = %d, want %d after duplicate dedupe and interaction grouping", overview.Messages, tt.wantMessages)
			}
			if overview.Sessions != tt.wantSessions {
				t.Errorf("Overview().Sessions = %d, want %d", overview.Sessions, tt.wantSessions)
			}
			if !approxEqual(overview.Cost, tt.wantCost) {
				t.Errorf("Overview().Cost = %.6f, want %.6f after duplicate dedupe with grouped assistant usage", overview.Cost, tt.wantCost)
			}
			if overview.Tokens.Input != tt.wantInputTokens || overview.Tokens.Output != tt.wantOutputTokens {
				t.Errorf("Overview().Tokens input/output = %d/%d, want %d/%d after duplicate dedupe with grouped assistant usage", overview.Tokens.Input, overview.Tokens.Output, tt.wantInputTokens, tt.wantOutputTokens)
			}

			messages, err := src.Messages(ctx, stats.PeriodQuery{Period: "all"}, 1, 100, stats.DefaultMessageSort())
			if err != nil {
				t.Fatalf("Messages(all) failed: %v", err)
			}
			if messages.Total != tt.wantMessages {
				t.Errorf("Messages().Total = %d, want %d after duplicate dedupe and interaction grouping", messages.Total, tt.wantMessages)
			}
			assertNoDuplicateMessageIDs(t, messages.Messages)
			if len(messages.Messages) != int(tt.wantMessages) {
				t.Fatalf("Messages().Messages len = %d, want %d rows", len(messages.Messages), tt.wantMessages)
			}
			// DefaultMessageSort is not chronological, so don't assume index 0 is a
			// specific role. Instead assert the set contains the expected user prompt
			// row plus assistant API-request rows, all attributed to the session.
			userRows, assistantRows := 0, 0
			for _, msg := range messages.Messages {
				if msg.SessionID != tt.sessionID {
					t.Errorf("Messages() row %q SessionID = %q, want %q", msg.ID, msg.SessionID, tt.sessionID)
				}
				switch msg.Role {
				case "user":
					userRows++
				case "assistant":
					assistantRows++
				}
			}
			if userRows != 1 {
				t.Errorf("Messages() user prompt rows = %d, want exactly 1 after dedupe", userRows)
			}
			if int64(assistantRows) != tt.wantModelMessages {
				t.Errorf("Messages() assistant API-request rows = %d, want %d after dedupe", assistantRows, tt.wantModelMessages)
			}

			session, err := src.SessionByID(ctx, tt.sessionID)
			if err != nil {
				t.Fatalf("SessionByID(%q) failed: %v", tt.sessionID, err)
			}
			if session == nil {
				t.Fatalf("SessionByID(%q) = nil, want session detail", tt.sessionID)
			}
			if session.MessageCount != tt.wantMessages {
				t.Errorf("SessionByID().MessageCount = %d, want %d after duplicate dedupe and interaction grouping", session.MessageCount, tt.wantMessages)
			}
			if !approxEqual(session.TotalCost, tt.wantCost) {
				t.Errorf("SessionByID().TotalCost = %.6f, want %.6f after duplicate dedupe with grouped assistant usage", session.TotalCost, tt.wantCost)
			}
			if session.TotalTokens.Input != tt.wantInputTokens || session.TotalTokens.Output != tt.wantOutputTokens {
				t.Errorf("SessionByID().TotalTokens input/output = %d/%d, want %d/%d after duplicate dedupe with grouped assistant usage", session.TotalTokens.Input, session.TotalTokens.Output, tt.wantInputTokens, tt.wantOutputTokens)
			}

			models, err := src.Models(ctx, stats.PeriodQuery{Period: "all"})
			if err != nil {
				t.Fatalf("Models(all) failed: %v", err)
			}
			model := findModelEntryByID(t, models, "claude-test-computed")
			if model.Messages != tt.wantModelMessages {
				t.Errorf("Models()[claude-test-computed].Messages = %d, want %d grouped interaction count after duplicate dedupe", model.Messages, tt.wantModelMessages)
			}
			if !approxEqual(model.Cost, tt.wantCost) {
				t.Errorf("Models()[claude-test-computed].Cost = %.6f, want %.6f after duplicate dedupe with grouped assistant usage", model.Cost, tt.wantCost)
			}
			if model.Tokens.Input != tt.wantInputTokens || model.Tokens.Output != tt.wantOutputTokens {
				t.Errorf("Models()[claude-test-computed].Tokens input/output = %d/%d, want %d/%d after duplicate dedupe with grouped assistant usage", model.Tokens.Input, model.Tokens.Output, tt.wantInputTokens, tt.wantOutputTokens)
			}
		})
	}
}

func TestToolResultOnlyUserRecordPairsToolDetailWithoutInflatingMessageInteractions(t *testing.T) {
	src := newTempRegressionSource(t, map[string][]string{
		"-home-andres-projects-tools/tool-result-session.jsonl": {
			`{"type":"user","uuid":"tool-user-1","session_id":"tool-result-session","timestamp":"2026-02-02T10:00:00Z","cwd":"/home/andres/projects/tools","message":{"role":"user","content":"Run the tool and summarize it."}}`,
			`{"type":"assistant","uuid":"tool-assistant-1","session_id":"tool-result-session","timestamp":"2026-02-02T10:00:01Z","cwd":"/home/andres/projects/tools","message":{"role":"assistant","model":"claude-test-computed","content":[{"type":"tool_use","id":"toolu_shell_1","name":"Bash","input":{"command":"printf done"}}],"usage":{"input_tokens":1000,"output_tokens":200}},"costUSD":30.0}`,
			`{"type":"user","uuid":"tool-result-1","session_id":"tool-result-session","timestamp":"2026-02-02T10:00:02Z","cwd":"/home/andres/projects/tools","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_shell_1","content":"tool completed successfully","is_error":false}]}}`,
			`{"type":"assistant","uuid":"tool-assistant-2","session_id":"tool-result-session","timestamp":"2026-02-02T10:00:03Z","cwd":"/home/andres/projects/tools","message":{"role":"assistant","model":"claude-test-computed","content":[{"type":"text","text":"The tool completed successfully."}],"usage":{"input_tokens":100,"output_tokens":25}},"costUSD":5.0}`,
		},
	})
	ctx := testContext(t)

	overview, err := src.Overview(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Overview(all) failed: %v", err)
	}
	// Rows: 1 user prompt + 2 assistant API requests. The tool_result-only user record
	// pairs onto the assistant tool message and never becomes its own row.
	if overview.Messages != 3 {
		t.Errorf("Overview().Messages = %d, want 3 (1 user + 2 assistant); tool_result-only records must not create rows", overview.Messages)
	}
	if !approxEqual(overview.Cost, 35) {
		t.Errorf("Overview().Cost = %.6f, want 35.000000", overview.Cost)
	}
	if overview.Tokens.Input != 1100 || overview.Tokens.Output != 225 {
		t.Errorf("Overview().Tokens input/output = %d/%d, want 1100/225", overview.Tokens.Input, overview.Tokens.Output)
	}

	messages, err := src.Messages(ctx, stats.PeriodQuery{Period: "all"}, 1, 100, stats.DefaultMessageSort())
	if err != nil {
		t.Fatalf("Messages(all) failed: %v", err)
	}
	if messages.Total != 3 {
		t.Errorf("Messages().Total = %d, want 3 rows (1 user + 2 assistant)", messages.Total)
	}
	if len(messages.Messages) != 3 {
		t.Fatalf("Messages().Messages len = %d, want 3 rows", len(messages.Messages))
	}
	if containsMessageID(messages.Messages, "claude_code:tool-result-session:tool-result-1") {
		t.Errorf("Messages() contains tool_result-only record %q; it should be paired onto the assistant tool detail, not listed as a row", "claude_code:tool-result-session:tool-result-1")
	}

	// The tool pairs onto the assistant-tool message (tool-assistant-1); the final
	// text lives on the assistant-final message (tool-assistant-2). The user prompt
	// text lives on its own user row.
	details := messageDetails(t, src, messages.Messages)
	if !detailsContainCompletedTool(t, details, "toolu_shell_1", "tool completed successfully") {
		t.Errorf("message details do not contain the completed paired tool toolu_shell_1")
	}
	if !detailsContainText(details, "Run the tool and summarize it.") {
		t.Errorf("message details missing user prompt text")
	}
	if !detailsContainText(details, "The tool completed successfully.") {
		t.Errorf("message details missing assistant final text")
	}

	session, err := src.SessionByID(ctx, "tool-result-session")
	if err != nil {
		t.Fatalf("SessionByID(tool-result-session) failed: %v", err)
	}
	if session == nil {
		t.Fatalf("SessionByID(tool-result-session) = nil, want session detail")
	}
	if session.MessageCount != 3 {
		t.Errorf("SessionByID().MessageCount = %d, want 3 rows (1 user + 2 assistant)", session.MessageCount)
	}
	if len(session.Messages) != 3 {
		t.Errorf("SessionByID().Messages len = %d, want 3 rows", len(session.Messages))
	}
	for _, msg := range session.Messages {
		if msg.ID == "claude_code:tool-result-session:tool-result-1" {
			t.Errorf("SessionByID().Messages contains tool_result-only record %q; it should only enrich assistant tool detail", msg.ID)
		}
	}

	tools, err := src.Tools(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Tools(all) failed: %v", err)
	}
	toolEntry := findToolEntryByName(t, tools, "Bash")
	if toolEntry.Invocations != 1 || toolEntry.Successes != 1 {
		t.Errorf("Tools()[Bash] invocations/successes = %d/%d, want 1/1", toolEntry.Invocations, toolEntry.Successes)
	}
}

func duplicateStableIdentityLines() []string {
	return []string{
		`{"type":"user","uuid":"dedupe-user-1","session_id":"duplicate-session","timestamp":"2026-02-01T10:00:00Z","cwd":"/home/andres/projects/dedupe","message":{"role":"user","content":"Explain why duplicate transcript copies must not double-count."}}`,
		`{"type":"assistant","uuid":"dedupe-assistant-1","session_id":"duplicate-session","timestamp":"2026-02-01T10:00:01Z","cwd":"/home/andres/projects/dedupe","message":{"role":"assistant","model":"claude-test-computed","content":[{"type":"text","text":"Deduplicate stable session/message identities before aggregation."}],"usage":{"input_tokens":1000,"output_tokens":200}},"costUSD":30.0}`,
	}
}

func uuidlessSemanticDuplicateWithDistinctAssistantLines() []string {
	duplicateUser := `{"type":"user","session_id":"fingerprint-session","timestamp":"2026-02-01T11:00:00Z","cwd":"/home/andres/projects/dedupe","message":{"role":"user","content":"Summarize fingerprint dedupe semantics."}}`
	duplicateAssistant := `{"type":"assistant","session_id":"fingerprint-session","timestamp":"2026-02-01T11:00:01Z","cwd":"/home/andres/projects/dedupe","message":{"role":"assistant","model":"claude-test-computed","content":[{"type":"text","text":"Exact UUID-less semantic duplicates may be fingerprint-deduped."}],"usage":{"input_tokens":1000,"output_tokens":200}},"costUSD":30.0}`
	distinctAssistant := `{"type":"assistant","session_id":"fingerprint-session","timestamp":"2026-02-01T11:00:02Z","cwd":"/home/andres/projects/dedupe","message":{"role":"assistant","model":"claude-test-computed","content":[{"type":"text","text":"Distinct assistant usage with different timestamp, usage, and content remains distinct."}],"usage":{"input_tokens":500,"output_tokens":100}},"costUSD":15.0}`
	return []string{duplicateUser, duplicateUser, duplicateAssistant, duplicateAssistant, distinctAssistant}
}

func newTempRegressionSource(t *testing.T, files map[string][]string) *Source {
	t.Helper()
	home := t.TempDir()
	projectsRoot := filepath.Join(home, "projects")
	for relPath, lines := range files {
		path := filepath.Join(projectsRoot, relPath)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create fixture dir for %s: %v", relPath, err)
		}
		content := strings.Join(lines, "\n") + "\n"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", relPath, err)
		}
	}
	return New(Options{
		ClaudeHome:          home,
		PathSource:          "temp regression fixture",
		PricingSnapshotPath: fixturePath(t, "pricing_snapshot.json"),
	})
}

func assertNoDuplicateMessageIDs(t *testing.T, messages []stats.MessageEntry) {
	t.Helper()
	seen := make(map[string]struct{}, len(messages))
	for _, msg := range messages {
		if _, ok := seen[msg.ID]; ok {
			t.Errorf("Messages() returned duplicate stable message ID %q", msg.ID)
			continue
		}
		seen[msg.ID] = struct{}{}
	}
}

func containsMessageID(messages []stats.MessageEntry, want string) bool {
	for _, msg := range messages {
		if msg.ID == want {
			return true
		}
	}
	return false
}

func findModelEntryByID(t *testing.T, models stats.ModelStats, modelID string) stats.ModelEntry {
	t.Helper()
	for _, model := range models.Models {
		if model.ModelID == modelID {
			return model
		}
	}
	t.Fatalf("model %q not found in %#v", modelID, models.Models)
	return stats.ModelEntry{}
}

func findToolEntryByName(t *testing.T, tools stats.ToolStats, name string) stats.ToolEntry {
	t.Helper()
	for _, tool := range tools.Tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found in %#v", name, tools.Tools)
	return stats.ToolEntry{}
}

func approxEqual(got, want float64) bool {
	return math.Abs(got-want) < 0.0000001
}
