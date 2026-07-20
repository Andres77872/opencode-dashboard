package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opencode-dashboard/internal/config"
	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

func TestParseSourceSelectionAcceptsKimiCode(t *testing.T) {
	got, err := parseSourceSelection(" kimi_code ")
	if err != nil {
		t.Fatalf("parseSourceSelection(kimi_code): %v", err)
	}
	if got != source.SourceKimiCode {
		t.Fatalf("parseSourceSelection(kimi_code) = %q, want %q", got, source.SourceKimiCode)
	}
	_, err = parseSourceSelection("kimi_typo")
	if err == nil || !strings.Contains(err.Error(), "kimi_code") {
		t.Fatalf("invalid source error = %v, want supported list to mention kimi_code", err)
	}
}

func TestBuildWebRegistryRegistersKimiCodeAsStartupSource(t *testing.T) {
	kimiHome := writeMainKimiFixture(t)
	missingRoot := t.TempDir()
	registry, err := buildWebRegistry(
		nil,
		nil,
		dbSelection{Path: filepath.Join(missingRoot, "missing-opencode.db"), Source: "test missing OpenCode"},
		source.SourceKimiCode,
		config.PathSelection{Path: filepath.Join(missingRoot, "missing-claude"), Source: "test missing Claude"},
		"",
		config.PathSelection{Path: filepath.Join(missingRoot, "missing-codex"), Source: "test missing Codex"},
		"",
		extraRegistrySelection{kimi: &homeRegistrySelection{
			selection:    config.PathSelection{Path: kimiHome, Source: "test Kimi fixture"},
			explicitHome: kimiHome,
		}},
	)
	if err != nil {
		t.Fatalf("buildWebRegistry(): %v", err)
	}
	defer registry.Close()

	infos := registry.List(context.Background())
	byID := make(map[source.SourceID]source.SourceInfo)
	for _, info := range infos {
		byID[info.ID] = info
	}
	kimiInfo, ok := byID[source.SourceKimiCode]
	if !ok {
		t.Fatalf("registry missing Kimi Code: %#v", infos)
	}
	if !kimiInfo.Available || !kimiInfo.Selected {
		t.Errorf("Kimi info = %#v, want available selected startup source", kimiInfo)
	}
	if kimiInfo.Path != kimiHome || kimiInfo.PathSource != "test Kimi fixture" {
		t.Errorf("Kimi path metadata = %q/%q", kimiInfo.Path, kimiInfo.PathSource)
	}
	if kimiInfo.CostPolicy.Status != string(stats.CostEstimatedAPIEquivalent) {
		t.Errorf("Kimi cost policy = %#v", kimiInfo.CostPolicy)
	}

	selected, err := registry.Resolve(string(source.SourceKimiCode))
	if err != nil {
		t.Fatalf("Resolve(kimi_code): %v", err)
	}
	overview, err := selected.Overview(context.Background(), stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Kimi Overview(all): %v", err)
	}
	if overview.SourceID != string(source.SourceKimiCode) || overview.Sessions != 1 || overview.Messages != 2 {
		t.Errorf("Kimi overview = %#v, want one prompt + one model request", overview)
	}
}

func TestBuildWebRegistryKeepsConfiguredUnavailableKimiVisible(t *testing.T) {
	root := t.TempDir()
	missingKimi := filepath.Join(root, "missing-kimi")
	registry, err := buildWebRegistry(
		nil,
		nil,
		dbSelection{Path: filepath.Join(root, "missing-opencode.db"), Source: "test missing OpenCode"},
		source.SourceOpenCode,
		config.PathSelection{Path: filepath.Join(root, "missing-claude"), Source: "test missing Claude"},
		"",
		config.PathSelection{Path: filepath.Join(root, "missing-codex"), Source: "test missing Codex"},
		"",
		extraRegistrySelection{kimi: &homeRegistrySelection{
			selection:    config.PathSelection{Path: missingKimi, Source: "--kimi-home"},
			explicitHome: missingKimi,
		}},
	)
	if err != nil {
		t.Fatalf("buildWebRegistry(): %v", err)
	}
	defer registry.Close()

	var kimiInfo source.SourceInfo
	for _, info := range registry.List(context.Background()) {
		if info.ID == source.SourceKimiCode {
			kimiInfo = info
			break
		}
	}
	if kimiInfo.ID != source.SourceKimiCode || kimiInfo.Available {
		t.Fatalf("configured unavailable Kimi info = %#v", kimiInfo)
	}
	if kimiInfo.Path != missingKimi || kimiInfo.PathSource != "--kimi-home" {
		t.Errorf("configured unavailable Kimi path = %q/%q", kimiInfo.Path, kimiInfo.PathSource)
	}
	if !strings.Contains(strings.ToLower(kimiInfo.Diagnostics.Reason), "not found") {
		t.Errorf("configured unavailable Kimi diagnostics = %#v", kimiInfo.Diagnostics)
	}
}

func writeMainKimiFixture(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	sessionDir := filepath.Join(home, "sessions", "wd_fixture", "session_main")
	agentDir := filepath.Join(sessionDir, "agents", "main")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("create Kimi fixture: %v", err)
	}
	state := `{
  "createdAt": "2026-07-16T10:00:00Z",
  "updatedAt": "2026-07-16T10:01:00Z",
  "title": "Kimi fixture",
  "workDir": "/tmp/kimi-fixture",
  "agents": {"main": {"type": "main", "parentAgentId": null}},
  "custom": {}
}`
	if err := os.WriteFile(filepath.Join(sessionDir, "state.json"), []byte(state), 0o644); err != nil {
		t.Fatalf("write Kimi state: %v", err)
	}
	wire := strings.Join([]string{
		`{"type":"turn.prompt","input":[{"type":"text","text":"hello"}],"origin":{"kind":"user"},"time":1784196001000}`,
		`{"type":"llm.request","kind":"loop","provider":"kimi","model":"k3","modelAlias":"kimi-code/k3","turnStep":"0.1","time":1784196001100}`,
		`{"type":"usage.record","model":"kimi-code/k3","usage":{"inputOther":100,"inputCacheRead":200,"inputCacheCreation":0,"output":10},"usageScope":"turn","time":1784196001200}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(agentDir, "wire.jsonl"), []byte(wire), 0o644); err != nil {
		t.Fatalf("write Kimi wire: %v", err)
	}
	return home
}
