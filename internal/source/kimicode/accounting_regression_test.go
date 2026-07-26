package kimicode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opencode-dashboard/internal/stats"
)

func TestKimiRequestAccountingCombinesAllAgentTypesAndAttempts(t *testing.T) {
	parent := "main"
	home := writeKimiHome(t, map[string]sessionFixture{
		"accounting": {
			State: sessionState{
				CreatedAt: "2026-07-16T10:00:00Z", UpdatedAt: "2026-07-16T10:10:00Z",
				Title: "Untitled", WorkDir: "/private/fixture/project",
				Agents: map[string]agentMeta{
					"main":        {Type: "main"},
					"sub":         {Type: "sub", ParentAgentID: &parent},
					"independent": {Type: "independent"},
					"legacy":      {Type: "sub", ParentAgentID: &parent},
				},
			},
			Wires: map[string][]string{
				"main": {
					`{"type":"config.update","profileName":"main-profile","time":1784196000000}`,
					`{"type":"turn.prompt","input":[{"type":"text","text":"Main title"}],"origin":{"kind":"user"},"time":1784196000100}`,
					`{"type":"context.append_message","message":{"role":"user","content":[{"type":"text","text":"Main title"}]},"time":1784196000101}`,
					`{"type":"turn.prompt","input":[{"type":"text","text":"/skill user action"}],"origin":{"kind":"skill_activation","trigger":"user-slash"},"time":1784196000200}`,
					`{"type":"turn.prompt","input":[{"type":"text","text":"hidden system task"}],"origin":{"kind":"system_trigger"},"time":1784196000300}`,
					`{"type":"turn.steer","input":[{"type":"text","text":"hidden background task"}],"origin":{"kind":"background_task"},"time":1784196000400}`,
					`{"type":"llm.request","kind":"loop","provider":"kimi","modelAlias":"kimi-code/k3","turnStep":"0.1","time":1784196001000}`,
					`{"type":"usage.record","model":"kimi-code/k3","usage":{"inputOther":10,"inputCacheRead":1,"inputCacheCreation":2,"output":1},"time":1784196001100}`,
					`{"type":"llm.request","kind":"loop","provider":"kimi","modelAlias":"kimi-code/k3","turnStep":"0.1","attempt":2,"time":1784196001200}`,
					`{"type":"llm.request","kind":"compaction","provider":"kimi","modelAlias":"kimi-code/k3","turnStep":"0.2","time":1784196001300}`,
					`{"type":"context.append_loop_event","event":{"type":"step.end","uuid":"compact-step","turnId":"0","step":2,"usage":{"inputOther":30,"inputCacheRead":3,"inputCacheCreation":4,"output":3}},"time":1784196001400}`,
				},
				"sub": {
					`{"type":"config.update","profileName":"sub-profile","time":1784196002000}`,
					`{"type":"turn.prompt","input":[{"type":"text","text":"hidden sub task"}],"origin":{"kind":"system_trigger"},"time":1784196002010}`,
					`{"type":"llm.request","kind":"loop","provider":"kimi","modelAlias":"kimi-code/k3","turnStep":"0.1","time":1784196002100}`,
					`{"type":"usage.record","model":"kimi-code/k3","usage":{"inputOther":40,"output":4},"time":1784196002200}`,
				},
				"independent": {
					`{"type":"config.update","profileName":"independent-profile","time":1784196003000}`,
					`{"type":"llm.request","kind":"loop","provider":"kimi","modelAlias":"kimi-code/k3","turnStep":"0.1","time":1784196003100}`,
				},
				"legacy": {
					`{"type":"usage.record","model":"kimi-code/k3","usage":{"inputOther":50,"output":5},"time":1784196004000}`,
					`{"type":"usage.record","model":"kimi-code/k3","usage":{"inputOther":60,"output":6},"time":1784196004100}`,
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
	if overview.Requests != 7 || overview.Messages != 9 || overview.Sessions != 1 {
		t.Fatalf("overview requests/messages/sessions = %d/%d/%d, want 7/9/1", overview.Requests, overview.Messages, overview.Sessions)
	}
	if overview.Tokens.Input != 190 || overview.Tokens.Output != 19 ||
		overview.Tokens.Cache.Read != 4 || overview.Tokens.Cache.Write != 6 {
		t.Errorf("combined tokens = %#v", overview.Tokens)
	}
	if overview.RequestAccounting == nil || overview.RequestAccounting.UsageRecorded != 4 ||
		overview.RequestAccounting.UsageRecovered != 1 || overview.RequestAccounting.UsageUnavailable != 2 ||
		overview.RequestAccounting.TraceCoverage != stats.TraceCoverageMixed {
		t.Errorf("request accounting = %#v", overview.RequestAccounting)
	}

	daily, err := src.Daily(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Daily(all): %v", err)
	}
	if len(daily.Days) != 1 || daily.Days[0].Requests != 7 || daily.Days[0].RequestAccounting == nil ||
		daily.RequestAccounting == nil || daily.RequestAccounting.TraceCoverage != stats.TraceCoverageMixed {
		t.Errorf("daily request accounting = %#v", daily)
	}

	sessions, err := src.Sessions(ctx, stats.SessionQuery{Period: "all", Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("Sessions(all): %v", err)
	}
	if len(sessions.Sessions) != 1 || sessions.Sessions[0].Title != "Main title" {
		t.Errorf("session title = %#v, want main-agent prompt", sessions.Sessions)
	}

	messages, err := src.Messages(ctx, stats.PeriodQuery{Period: "all"}, 1, 20, stats.DefaultMessageSort())
	if err != nil {
		t.Fatalf("Messages(all): %v", err)
	}
	var recorded, recovered, unavailable, inferred int
	for _, message := range messages.Messages {
		if message.Role != "assistant" {
			continue
		}
		switch message.UsageStatus {
		case stats.UsageStatusRecorded:
			recorded++
		case stats.UsageStatusRecovered:
			recovered++
		case stats.UsageStatusUnavailable:
			unavailable++
		}
		if message.RequestTrace == stats.RequestTraceInferred {
			inferred++
		}
		if message.Agent == "independent-profile" && message.IsSubagent {
			t.Error("independent agent was incorrectly classified as a subagent")
		}
	}
	if recorded != 4 || recovered != 1 || unavailable != 2 || inferred != 2 {
		t.Errorf("row provenance recorded/recovered/unavailable/inferred = %d/%d/%d/%d", recorded, recovered, unavailable, inferred)
	}
}

func TestKimiCanonicalUsageReplacesStepEndRecovery(t *testing.T) {
	home := writeKimiHome(t, map[string]sessionFixture{
		"replacement": {
			State: sessionState{WorkDir: "/private/replacement", Agents: map[string]agentMeta{"main": {Type: "main"}}},
			Wires: map[string][]string{"main": {
				`{"type":"llm.request","provider":"kimi","modelAlias":"kimi-code/k3","turnStep":"0.1","time":1784196001000}`,
				`{"type":"context.append_loop_event","event":{"type":"step.end","uuid":"step-1","usage":{"inputOther":10,"output":1}},"time":1784196001100}`,
				`{"type":"usage.record","model":"kimi-code/k3","usage":{"inputOther":20,"output":2},"time":1784196001200}`,
			}},
		},
	})
	src := New(Options{KimiHome: home})
	overview, err := src.Overview(testContext(t), stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Overview(all): %v", err)
	}
	if overview.Requests != 1 || overview.Tokens.Input != 20 || overview.Tokens.Output != 2 ||
		overview.RequestAccounting == nil || overview.RequestAccounting.UsageRecorded != 1 || overview.RequestAccounting.UsageRecovered != 0 {
		t.Errorf("canonical replacement overview = %#v", overview)
	}
}

func TestKimiUnavailableUsageReasonsUseOnlyPersistedEvidence(t *testing.T) {
	home := writeKimiHome(t, map[string]sessionFixture{
		"reasons": {
			State: sessionState{
				WorkDir: "/private/reasons",
				Agents: map[string]agentMeta{
					"cancelled": {Type: "independent"},
					"late":      {Type: "independent"},
					"mismatch":  {Type: "independent"},
					"retry":     {Type: "independent"},
					"terminal":  {Type: "independent"},
				},
			},
			Wires: map[string][]string{
				"cancelled": {
					`{"type":"context.append_loop_event","event":{"type":"step.begin","uuid":"c-step","turnId":"0","step":1},"time":1784196001000}`,
					`{"type":"llm.request","provider":"kimi","modelAlias":"kimi-code/k3","turnStep":"0.1","time":1784196001010}`,
					`{"type":"turn.cancel","time":1784196001020}`,
					`{"type":"turn.prompt","input":[{"type":"text","text":"next"}],"origin":{"kind":"user"},"time":1784196001030}`,
				},
				"late": {
					`{"type":"context.append_loop_event","event":{"type":"step.begin","uuid":"l-step","turnId":"1","step":1},"time":1784196002000}`,
					`{"type":"llm.request","provider":"kimi","modelAlias":"kimi-code/k3","turnStep":"1.1","time":1784196002010}`,
					`{"type":"turn.cancel","turnId":"1","time":1784196002020}`,
					`{"type":"usage.record","model":"kimi-code/k3","usage":{"inputOther":2,"output":1},"time":1784196002030}`,
				},
				"mismatch": {
					`{"type":"context.append_loop_event","event":{"type":"step.begin","uuid":"m-step","turnId":"2","step":1},"time":1784196003000}`,
					`{"type":"llm.request","provider":"kimi","modelAlias":"kimi-code/k3","turnStep":"2.1","time":1784196003010}`,
					`{"type":"turn.cancel","turnId":"other","time":1784196003020}`,
				},
				"retry": {
					`{"type":"llm.request","provider":"kimi","modelAlias":"kimi-code/k3","turnStep":"3.1","attempt":1,"time":1784196004000}`,
					`{"type":"llm.request","provider":"kimi","modelAlias":"kimi-code/k3","turnStep":"3.1","attempt":2,"time":1784196004010}`,
				},
				"terminal": {
					`{"type":"llm.request","provider":"kimi","modelAlias":"kimi-code/k3","turnStep":"4.1","time":1784196005000}`,
					`{"type":"context.append_loop_event","event":{"type":"step.end","uuid":"t-step","turnId":"4","step":1},"time":1784196005010}`,
				},
			},
		},
	})
	src := New(Options{KimiHome: home})
	overview, err := src.Overview(testContext(t), stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Overview(all): %v", err)
	}
	wantReasons := stats.UsageUnavailableReasons{Cancelled: 1, Interrupted: 2, Failed: 1, Unknown: 1}
	if overview.Requests != 6 || overview.RequestAccounting == nil ||
		overview.RequestAccounting.UsageRecorded != 1 ||
		overview.RequestAccounting.UsageUnavailable != 5 ||
		overview.RequestAccounting.UsageUnavailableReasons != wantReasons {
		t.Fatalf("request accounting = %#v, want recorded=1 unavailable=5 reasons=%#v", overview.RequestAccounting, wantReasons)
	}
	messages, err := src.Messages(testContext(t), stats.PeriodQuery{Period: "all"}, 1, 20, stats.DefaultMessageSort())
	if err != nil {
		t.Fatalf("Messages(all): %v", err)
	}
	for _, message := range messages.Messages {
		if message.UsageStatus != stats.UsageStatusUnavailable && message.UsageUnavailableReason != "" {
			t.Errorf("non-unavailable request %q retained reason %q", message.ID, message.UsageUnavailableReason)
		}
	}
}

func TestKimiDurableDedupeDoesNotCollapseIdentitylessUsage(t *testing.T) {
	usage := `{"type":"usage.record","model":"kimi-code/k3","usage":{"inputOther":7,"output":1},"time":1784196001300}`
	home := writeKimiHome(t, map[string]sessionFixture{
		"dedupe": {
			State: sessionState{WorkDir: "/private/dedupe", Agents: map[string]agentMeta{"main": {Type: "main"}}},
			Wires: map[string][]string{"main": {
				`{"type":"llm.request","provider":"kimi","modelAlias":"kimi-code/k3","turnStep":"0.1","time":1784196001000}`,
				`{"type":"context.append_loop_event","event":{"type":"content.part","uuid":"content-1","part":{"type":"text","text":"once"}},"time":1784196001100}`,
				`{"type":"context.append_loop_event","event":{"type":"content.part","uuid":"content-1","part":{"type":"text","text":"once"}},"time":1784196001100}`,
				usage,
				usage,
			}},
		},
	})
	src := New(Options{KimiHome: home})
	overview, err := src.Overview(testContext(t), stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Overview(all): %v", err)
	}
	if overview.Requests != 2 || overview.Tokens.Input != 14 || overview.RequestAccounting == nil || overview.RequestAccounting.UsageRecorded != 2 {
		t.Errorf("identity-less usage accounting = %#v", overview)
	}
	messages, _ := src.Messages(testContext(t), stats.PeriodQuery{Period: "all"}, 1, 10, stats.DefaultMessageSort())
	for _, message := range messages.Messages {
		if message.RequestTrace != stats.RequestTraceObserved {
			continue
		}
		detail, err := src.MessageByID(testContext(t), message.ID)
		if err != nil || detail == nil {
			t.Fatalf("MessageByID(%q): %#v, %v", message.ID, detail, err)
		}
		if len(detail.Content.TextParts) != 1 {
			t.Errorf("durably duplicated content parts = %#v, want one", detail.Content.TextParts)
		}
	}
}

func TestKimiDurableRequestIDDoesNotCollapseDistinctAttempts(t *testing.T) {
	home := writeKimiHome(t, map[string]sessionFixture{
		"durable-retries": {
			State: sessionState{WorkDir: "/private/durable-retries", Agents: map[string]agentMeta{"main": {Type: "main"}}},
			Wires: map[string][]string{"main": {
				`{"type":"llm.request","requestId":"logical-request","attempt":1,"turnStep":"0.1","time":1784196001000}`,
				`{"type":"llm.request","requestId":"logical-request","attempt":2,"turnStep":"0.1","time":1784196001100}`,
				`{"type":"llm.request","requestId":"logical-request","attempt":2,"turnStep":"0.1","time":1784196001100}`,
			}},
		},
	})
	overview, err := New(Options{KimiHome: home}).Overview(testContext(t), stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Overview(all): %v", err)
	}
	if overview.Requests != 2 || overview.RequestAccounting == nil || overview.RequestAccounting.UsageUnavailable != 2 {
		t.Errorf("durable retry accounting = %#v, want two distinct attempts and one duplicated event removed", overview)
	}
}

func TestKimiRepeatedIdenticalUserSlashPromptsRemainDistinct(t *testing.T) {
	prompt := `{"type":"turn.prompt","input":[{"type":"text","text":"/skill repeat"}],"origin":{"kind":"skill_activation","trigger":"user-slash"},"time":1784196001000}`
	echo := `{"type":"context.append_message","message":{"role":"user","content":[{"type":"text","text":"/skill repeat"}],"origin":{"kind":"skill_activation","trigger":"user-slash"}},"time":1784196001001}`
	control := `{"type":"goal.update","goal":{"status":"running"},"time":1784196001000}`
	home := writeKimiHome(t, map[string]sessionFixture{
		"repeated-prompts": {
			State: sessionState{WorkDir: "/private/repeated-prompts", Agents: map[string]agentMeta{"main": {Type: "main"}}},
			// Real Kimi wires can place goal.update between turn.prompt and its
			// context echo. Each pair is one prompt; the repeated pair is distinct.
			Wires: map[string][]string{"main": {prompt, control, echo, prompt, control, echo}},
		},
	})
	overview, err := New(Options{KimiHome: home}).Overview(testContext(t), stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Overview(all): %v", err)
	}
	if overview.Messages != 2 || overview.Requests != 0 {
		t.Errorf("repeated user-slash prompts = messages %d / requests %d, want two transcript prompts and no outbound requests", overview.Messages, overview.Requests)
	}
}

func TestKimiV2MetadataAndSessionIndexFallback(t *testing.T) {
	home := t.TempDir()
	created := int64(1784196000000)
	writeManualKimiSession(t, home, "workspace-a", "v2-alpha", map[string]any{
		"id": "v2-alpha", "version": 2, "createdAt": created, "updatedAt": created + 1000,
		"title": "Alpha", "cwd": "/private/a/shared", "workDir": "/wrong/path",
		"agents": map[string]any{"main": map[string]any{"type": "main", "labels": map[string]string{"label": "metadata-profile"}}},
	}, map[string][]string{"main": {
		`{"type":"config.update","profileName":"wire-profile","time":1784196000100}`,
		`{"type":"llm.request","provider":"kimi","modelAlias":"kimi-code/k3","turnStep":"0.1","time":1784196000200}`,
		`{"type":"usage.record","model":"kimi-code/k3","usage":{"inputOther":1,"output":1},"time":1784196000300}`,
	}}, nil)
	betaDir := writeManualKimiSession(t, home, "workspace-b", "v2-beta", map[string]any{
		"id": "v2-beta", "version": 2, "createdAt": created + 2000, "updatedAt": created + 3000,
		"title": "Beta", "agents": map[string]any{"main": map[string]any{"type": "main"}},
	}, map[string][]string{"main": {
		`{"type":"llm.request","provider":"kimi","modelAlias":"kimi-code/k3","turnStep":"0.1"}`,
		`{"type":"usage.record","model":"kimi-code/k3","usage":{"inputOther":2,"output":1}}`,
	}}, nil)
	indexLine, _ := json.Marshal(map[string]any{"sessionId": "v2-beta", "sessionDir": betaDir, "workDir": "/private/b/shared"})
	if err := os.WriteFile(filepath.Join(home, "session_index.jsonl"), append(indexLine, '\n'), 0o600); err != nil {
		t.Fatalf("write session index: %v", err)
	}

	src := New(Options{KimiHome: home})
	ctx := testContext(t)
	projects, err := src.Projects(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Projects(all): %v", err)
	}
	if len(projects.Projects) != 2 || projects.Projects[0].ProjectID == projects.Projects[1].ProjectID {
		t.Fatalf("collision-resistant projects = %#v", projects.Projects)
	}
	for _, project := range projects.Projects {
		if project.ProjectName != "shared" || strings.Contains(project.ProjectID, "/private/") {
			t.Errorf("private project identity = %#v", project)
		}
	}
	alpha, err := src.SessionByID(ctx, "v2-alpha")
	if err != nil || alpha == nil {
		t.Fatalf("SessionByID(v2-alpha): %#v, %v", alpha, err)
	}
	if alpha.TimeCreated.UnixMilli() != created || !strings.HasSuffix(alpha.Directory, "/shared") {
		t.Errorf("v2 alpha metadata = %#v", alpha)
	}
	if len(alpha.Messages) != 1 || alpha.Messages[0].Agent != "wire-profile" {
		t.Errorf("wire profile attribution = %#v", alpha.Messages)
	}
	beta, err := src.SessionByID(ctx, "v2-beta")
	if err != nil || beta == nil || !strings.HasSuffix(beta.Directory, "/shared") {
		t.Errorf("session-index cwd fallback = %#v, %v", beta, err)
	}
	if len(beta.Messages) != 1 || beta.Messages[0].TimeCreated.UnixMilli() != created+2000 {
		t.Errorf("session-time fallback = %#v", beta.Messages)
	}
}

func TestKimiRootWireFallbackAndLegacyDiagnostics(t *testing.T) {
	t.Run("canonical root only", func(t *testing.T) {
		home := t.TempDir()
		writeManualKimiSession(t, home, "workspace", "root-canonical", map[string]any{"workDir": "/private/root"}, nil, []string{
			`{"type":"llm.request","provider":"kimi","modelAlias":"kimi-code/k3","turnStep":"0.1","time":1784196001000}`,
			`{"type":"usage.record","model":"kimi-code/k3","usage":{"inputOther":3,"output":1},"time":1784196001100}`,
		})
		overview, err := New(Options{KimiHome: home}).Overview(testContext(t), stats.PeriodQuery{Period: "all"})
		if err != nil || overview.Requests != 1 || overview.Tokens.Input != 3 {
			t.Fatalf("root-only canonical overview = %#v, %v", overview, err)
		}
	})

	t.Run("legacy UI root only", func(t *testing.T) {
		home := t.TempDir()
		writeManualKimiSession(t, home, "workspace", "root-legacy", map[string]any{"workDir": "/private/root"}, nil, []string{
			`{"type":"metadata","protocol_version":"1.10"}`,
			`{"timestamp":1784196001.0,"message":{"type":"TurnBegin","payload":{}}}`,
			`{"timestamp":1784196002.0,"message":{"type":"StatusUpdate","payload":{"token_usage":{"input_other":999,"output":999}}}}`,
		})
		src := New(Options{KimiHome: home})
		overview, err := src.Overview(testContext(t), stats.PeriodQuery{Period: "all"})
		if err != nil || overview.Requests != 0 || overview.Tokens.Input != 0 {
			t.Fatalf("legacy root overview = %#v, %v", overview, err)
		}
		info := src.Info(testContext(t))
		if info.Diagnostics.Status != "partial" || !strings.Contains(info.Diagnostics.Reason, "legacy UI-only") {
			t.Errorf("legacy root diagnostics = %#v", info.Diagnostics)
		}
	})

	t.Run("main shadows root", func(t *testing.T) {
		home := t.TempDir()
		writeManualKimiSession(t, home, "workspace", "shadowed", map[string]any{
			"workDir": "/private/root", "agents": map[string]any{"main": map[string]any{"type": "main"}},
		}, map[string][]string{"main": {
			`{"type":"llm.request","provider":"kimi","modelAlias":"kimi-code/k3","turnStep":"0.1","time":1784196001000}`,
			`{"type":"usage.record","model":"kimi-code/k3","usage":{"inputOther":4,"output":1},"time":1784196001100}`,
		}}, []string{
			`{"type":"usage.record","model":"kimi-code/k3","usage":{"inputOther":999,"output":999},"time":1784196001200}`,
		})
		src := New(Options{KimiHome: home})
		overview, err := src.Overview(testContext(t), stats.PeriodQuery{Period: "all"})
		if err != nil || overview.Requests != 1 || overview.Tokens.Input != 4 {
			t.Fatalf("shadowed root overview = %#v, %v", overview, err)
		}
		if info := src.Info(testContext(t)); !strings.Contains(info.Diagnostics.Reason, "shadowed") {
			t.Errorf("shadowed root diagnostics = %#v", info.Diagnostics)
		}
	})
}

func TestKimiOnePassReaderResetsForkAndContinuesAfterOversizedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wire.jsonl")
	lines := []string{
		`{"type":"llm.request","turnStep":"parent","time":1784196000000}`,
		`{"type":"usage.record","model":"kimi-code/k3","usage":{"inputOther":999,"output":999},"time":1784196000100}`,
		`{"type":"forked","time":1784196000200}`,
		`{"type":"llm.request","turnStep":"child"}`,
		`{"type":"llm.request","turnStep":"child"}`,
		`{"oversized":"` + strings.Repeat("x", 256) + `"}`,
		`{"type":"usage.record","model":"kimi-code/k3","usage":{"inputOther":5,"output":1},"time":1784196000300}`,
		`{torn`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatalf("write wire: %v", err)
	}
	records, diag, err := parseWireFileWithLimit(context.Background(), agentFile{Path: path}, 128)
	if err != nil {
		t.Fatalf("parse bounded wire: %v", err)
	}
	var requests, usages int
	for _, record := range records {
		if record.Request != nil {
			requests++
		}
		if record.Usage != nil {
			usages++
		}
	}
	if requests != 2 || usages != 1 || diag.MalformedLines != 2 ||
		!strings.Contains(diag.Reason, "oversized") {
		t.Errorf("bounded parse records=%d requests=%d usages=%d diag=%#v", len(records), requests, usages, diag)
	}
}

func writeManualKimiSession(
	t *testing.T,
	home, workspace, sessionID string,
	state map[string]any,
	wires map[string][]string,
	rootWire []string,
) string {
	t.Helper()
	sessionDir := filepath.Join(home, "sessions", workspace, sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("create manual session: %v", err)
	}
	stateBytes, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal manual state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "state.json"), stateBytes, 0o600); err != nil {
		t.Fatalf("write manual state: %v", err)
	}
	for agentID, lines := range wires {
		agentDir := filepath.Join(sessionDir, "agents", agentID)
		if err := os.MkdirAll(agentDir, 0o755); err != nil {
			t.Fatalf("create manual agent: %v", err)
		}
		if err := os.WriteFile(filepath.Join(agentDir, "wire.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatalf("write manual agent wire: %v", err)
		}
	}
	if rootWire != nil {
		if err := os.WriteFile(filepath.Join(sessionDir, "wire.jsonl"), []byte(strings.Join(rootWire, "\n")+"\n"), 0o600); err != nil {
			t.Fatalf("write manual root wire: %v", err)
		}
	}
	return sessionDir
}

func TestParseStateTimeAcceptsV1AndV2Representations(t *testing.T) {
	want := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	for _, value := range []any{want.Format(time.RFC3339Nano), want.UnixMilli(), float64(want.UnixMilli())} {
		if got := parseStateTime(value); !got.Equal(want) {
			t.Errorf("parseStateTime(%T(%v)) = %v, want %v", value, value, got, want)
		}
	}
}
