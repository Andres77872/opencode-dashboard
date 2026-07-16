package cache

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/source/kimicode"
	"opencode-dashboard/internal/stats"
)

func TestCacheBackedKimiCodeSourceMatchesLiveAggregates(t *testing.T) {
	fixture := writeCacheKimiFixture(t)
	ctx := context.Background()
	live := kimicode.New(kimicode.Options{KimiHome: fixture.home, PathSource: "test fixture"})
	store := newTestStore(t)
	if err := store.SyncSource(ctx, live); err != nil {
		t.Fatalf("SyncSource(Kimi Code) failed: %v", err)
	}
	cached := WrapSource(store, live)
	period := stats.PeriodQuery{Period: "all"}

	liveOverview, err := live.Overview(ctx, period)
	if err != nil {
		t.Fatalf("live Kimi Overview() failed: %v", err)
	}
	cacheOverview, err := cached.Overview(ctx, period)
	if err != nil {
		t.Fatalf("cached Kimi Overview() failed: %v", err)
	}
	if cacheOverview.SourceID != string(source.SourceKimiCode) ||
		cacheOverview.Sessions != liveOverview.Sessions ||
		cacheOverview.Messages != liveOverview.Messages ||
		cacheOverview.Tokens != liveOverview.Tokens ||
		cacheOverview.Cost != liveOverview.Cost {
		t.Errorf("cached Kimi overview = %#v, want live %#v", cacheOverview, liveOverview)
	}

	liveMessages, err := live.Messages(ctx, period, 1, 100, stats.MessageSort{Field: stats.MessageSortTime, Direction: stats.MessageSortAsc})
	if err != nil {
		t.Fatalf("live Kimi Messages() failed: %v", err)
	}
	cacheMessages, err := cached.Messages(ctx, period, 1, 100, stats.MessageSort{Field: stats.MessageSortTime, Direction: stats.MessageSortAsc})
	if err != nil {
		t.Fatalf("cached Kimi Messages() failed: %v", err)
	}
	if cacheMessages.Total != liveMessages.Total || len(cacheMessages.Messages) != len(liveMessages.Messages) {
		t.Fatalf("cached Kimi messages total/len = %d/%d, want %d/%d", cacheMessages.Total, len(cacheMessages.Messages), liveMessages.Total, len(liveMessages.Messages))
	}
	for i := range liveMessages.Messages {
		if cacheMessages.Messages[i].ID != liveMessages.Messages[i].ID ||
			cacheMessages.Messages[i].SourceID != string(source.SourceKimiCode) {
			t.Errorf("cached Kimi message[%d] = %#v, want live ID and Kimi source", i, cacheMessages.Messages[i])
		}
	}

	for _, forbidden := range []string{"KIMI_CACHE_PRIVATE_PROMPT_42", "KIMI_CACHE_PRIVATE_TOOL_42"} {
		if location := findTextInCache(t, store.db, forbidden); location != "" {
			t.Fatalf("Kimi cache persisted forbidden text %q at %s", forbidden, location)
		}
	}
}

func TestKimiCodeFingerprintTracksStateAndWireButIgnoresAuxiliaryFiles(t *testing.T) {
	fixture := writeCacheKimiFixture(t)
	info := source.SourceInfo{
		ID:   source.SourceKimiCode,
		Kind: "jsonl",
		Path: fixture.home,
	}
	ctx := context.Background()

	initial, err := sourceFingerprint(ctx, info)
	if err != nil {
		t.Fatalf("initial Kimi fingerprint: %v", err)
	}
	plansDir := filepath.Join(filepath.Dir(fixture.wirePath), "plans")
	if err := os.MkdirAll(plansDir, 0o755); err != nil {
		t.Fatalf("create ignored Kimi plans directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(plansDir, "plan.md"), []byte("ignored"), 0o644); err != nil {
		t.Fatalf("write ignored Kimi plan: %v", err)
	}
	afterPlan, err := sourceFingerprint(ctx, info)
	if err != nil {
		t.Fatalf("Kimi fingerprint after ignored plan: %v", err)
	}
	if afterPlan != initial {
		t.Errorf("Kimi fingerprint changed for ignored plans file: %q -> %q", initial, afterPlan)
	}

	wire, err := os.OpenFile(fixture.wirePath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open Kimi wire for append: %v", err)
	}
	_, writeErr := wire.WriteString(`{"type":"turn.prompt","input":[{"type":"text","text":"new"}],"origin":{"kind":"user"},"time":1735725605000}` + "\n")
	closeErr := wire.Close()
	if writeErr != nil {
		t.Fatalf("append Kimi wire: %v", writeErr)
	}
	if closeErr != nil {
		t.Fatalf("close Kimi wire: %v", closeErr)
	}
	afterWire, err := sourceFingerprint(ctx, info)
	if err != nil {
		t.Fatalf("Kimi fingerprint after wire append: %v", err)
	}
	if afterWire == initial {
		t.Errorf("Kimi fingerprint did not change after wire append")
	}

	state, err := os.ReadFile(fixture.statePath)
	if err != nil {
		t.Fatalf("read Kimi state: %v", err)
	}
	state = append(state, '\n')
	if err := os.WriteFile(fixture.statePath, state, 0o644); err != nil {
		t.Fatalf("update Kimi state: %v", err)
	}
	afterState, err := sourceFingerprint(ctx, info)
	if err != nil {
		t.Fatalf("Kimi fingerprint after state update: %v", err)
	}
	if afterState == afterWire {
		t.Errorf("Kimi fingerprint did not change after state update")
	}
}

type cacheKimiFixture struct {
	home      string
	statePath string
	wirePath  string
}

func writeCacheKimiFixture(t *testing.T) cacheKimiFixture {
	t.Helper()
	home := t.TempDir()
	sessionDir := filepath.Join(home, "sessions", "wd_fixture", "session_cache-main")
	agentDir := filepath.Join(sessionDir, "agents", "main")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("create Kimi cache fixture: %v", err)
	}
	statePath := filepath.Join(sessionDir, "state.json")
	state := `{
  "createdAt": "2025-01-01T10:00:00Z",
  "updatedAt": "2025-01-01T10:01:00Z",
  "title": "KIMI_CACHE_PRIVATE_PROMPT_42",
  "workDir": "/private/kimi-cache/kimi-project",
  "agents": {"main": {"type": "main", "parentAgentId": null}}
}`
	if err := os.WriteFile(statePath, []byte(state), 0o644); err != nil {
		t.Fatalf("write Kimi cache state: %v", err)
	}
	wirePath := filepath.Join(agentDir, "wire.jsonl")
	wire := strings.Join([]string{
		`{"type":"turn.prompt","input":[{"type":"text","text":"KIMI_CACHE_PRIVATE_PROMPT_42"}],"origin":{"kind":"user"},"time":1735725601000}`,
		`{"type":"llm.request","kind":"loop","provider":"kimi","model":"k3","modelAlias":"kimi-code/k3","turnStep":"0.1","time":1735725602000}`,
		`{"type":"context.append_loop_event","event":{"type":"tool.call","uuid":"call-1","toolCallId":"call-1","name":"Read","description":"KIMI_CACHE_PRIVATE_TOOL_42"},"time":1735725603000}`,
		`{"type":"context.append_loop_event","event":{"type":"tool.result","toolCallId":"call-1","result":{"output":"KIMI_CACHE_PRIVATE_TOOL_42","isError":false}},"time":1735725603500}`,
		`{"type":"usage.record","model":"kimi-code/k3","usage":{"inputOther":100,"inputCacheRead":200,"inputCacheCreation":10,"output":20},"usageScope":"turn","time":1735725604000}`,
	}, "\n") + "\n"
	if err := os.WriteFile(wirePath, []byte(wire), 0o644); err != nil {
		t.Fatalf("write Kimi cache wire: %v", err)
	}
	return cacheKimiFixture{home: home, statePath: statePath, wirePath: wirePath}
}
