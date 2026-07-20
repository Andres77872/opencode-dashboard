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

func TestParseSourceSelectionAcceptsQwenCode(t *testing.T) {
	got, err := parseSourceSelection(" qwen_code ")
	if err != nil {
		t.Fatalf("parseSourceSelection(qwen_code): %v", err)
	}
	if got != source.SourceQwenCode {
		t.Fatalf("parseSourceSelection(qwen_code) = %q, want %q", got, source.SourceQwenCode)
	}
	_, err = parseSourceSelection("qwen_typo")
	if err == nil || !strings.Contains(err.Error(), "qwen_code") {
		t.Fatalf("invalid source error = %v, want supported list to mention qwen_code", err)
	}
}

func TestBuildWebRegistryRegistersQwenCodeAsStartupSource(t *testing.T) {
	qwenHome := writeMainQwenFixture(t)
	missingRoot := t.TempDir()
	registry, err := buildWebRegistry(
		nil,
		nil,
		dbSelection{Path: filepath.Join(missingRoot, "missing-opencode.db"), Source: "test missing OpenCode"},
		source.SourceQwenCode,
		config.PathSelection{Path: filepath.Join(missingRoot, "missing-claude"), Source: "test missing Claude"},
		"",
		config.PathSelection{Path: filepath.Join(missingRoot, "missing-codex"), Source: "test missing Codex"},
		"",
		extraRegistrySelection{qwen: &homeRegistrySelection{
			selection:    config.PathSelection{Path: qwenHome, Source: "test Qwen fixture"},
			explicitHome: qwenHome,
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
	qwenInfo, ok := byID[source.SourceQwenCode]
	if !ok {
		t.Fatalf("registry missing Qwen Code: %#v", infos)
	}
	if !qwenInfo.Available || !qwenInfo.Selected {
		t.Errorf("Qwen info = %#v, want available selected startup source", qwenInfo)
	}
	if qwenInfo.Path != qwenHome || qwenInfo.PathSource != "test Qwen fixture" {
		t.Errorf("Qwen path metadata = %q/%q", qwenInfo.Path, qwenInfo.PathSource)
	}
	if qwenInfo.CostPolicy.Status != string(stats.CostEstimatedAPIEquivalent) {
		t.Errorf("Qwen cost policy = %#v", qwenInfo.CostPolicy)
	}

	selected, err := registry.Resolve(string(source.SourceQwenCode))
	if err != nil {
		t.Fatalf("Resolve(qwen_code): %v", err)
	}
	overview, err := selected.Overview(context.Background(), stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Qwen Overview(all): %v", err)
	}
	if overview.SourceID != string(source.SourceQwenCode) || overview.Sessions != 1 || overview.Messages != 2 {
		t.Errorf("Qwen overview = %#v, want one prompt + one model request", overview)
	}
}

func TestBuildWebRegistryKeepsConfiguredUnavailableQwenVisible(t *testing.T) {
	root := t.TempDir()
	missingQwen := filepath.Join(root, "missing-qwen")
	registry, err := buildWebRegistry(
		nil,
		nil,
		dbSelection{Path: filepath.Join(root, "missing-opencode.db"), Source: "test missing OpenCode"},
		source.SourceOpenCode,
		config.PathSelection{Path: filepath.Join(root, "missing-claude"), Source: "test missing Claude"},
		"",
		config.PathSelection{Path: filepath.Join(root, "missing-codex"), Source: "test missing Codex"},
		"",
		extraRegistrySelection{qwen: &homeRegistrySelection{
			selection:    config.PathSelection{Path: missingQwen, Source: "--qwen-home"},
			explicitHome: missingQwen,
		}},
	)
	if err != nil {
		t.Fatalf("buildWebRegistry(): %v", err)
	}
	defer registry.Close()

	var qwenInfo source.SourceInfo
	for _, info := range registry.List(context.Background()) {
		if info.ID == source.SourceQwenCode {
			qwenInfo = info
			break
		}
	}
	if qwenInfo.ID != source.SourceQwenCode || qwenInfo.Available {
		t.Fatalf("configured unavailable Qwen info = %#v", qwenInfo)
	}
	if qwenInfo.Path != missingQwen || qwenInfo.PathSource != "--qwen-home" {
		t.Errorf("configured unavailable Qwen path = %q/%q", qwenInfo.Path, qwenInfo.PathSource)
	}
	if !strings.Contains(strings.ToLower(qwenInfo.Diagnostics.Reason), "not found") {
		t.Errorf("configured unavailable Qwen diagnostics = %#v", qwenInfo.Diagnostics)
	}
}

func writeMainQwenFixture(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	chatsDir := filepath.Join(home, "projects", "-tmp-qwen-fixture", "chats")
	if err := os.MkdirAll(chatsDir, 0o755); err != nil {
		t.Fatalf("create Qwen fixture: %v", err)
	}
	transcript := strings.Join([]string{
		`{"type":"user","uuid":"u1","sessionId":"fixture","timestamp":"2026-07-16T10:00:00.000Z","cwd":"/tmp/qwen-fixture","message":{"role":"user","parts":[{"text":"hello"}]}}`,
		`{"type":"assistant","uuid":"a1","sessionId":"fixture","timestamp":"2026-07-16T10:00:01.000Z","cwd":"/tmp/qwen-fixture","model":"qwen3.7-plus","message":{"role":"model","parts":[{"text":"hi"}]},"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":10,"cachedContentTokenCount":0,"thoughtsTokenCount":0,"totalTokenCount":110}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(chatsDir, "fixture.jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatalf("write Qwen transcript: %v", err)
	}
	return home
}
