package source

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"opencode-dashboard/internal/stats"
)

func TestRegistryResolve(t *testing.T) {
	tests := []struct {
		name       string
		selectedID string
		wantID     SourceID
		wantErr    error
		wantErrID  string
	}{
		{
			name:       "omitted source resolves to opencode",
			selectedID: "",
			wantID:     SourceOpenCode,
		},
		{
			name:       "explicit opencode resolves to opencode source",
			selectedID: string(SourceOpenCode),
			wantID:     SourceOpenCode,
		},
		{
			name:       "invalid source is rejected without fallback",
			selectedID: "not_registered",
			wantErr:    ErrInvalidSource,
			wantErrID:  "not_registered",
		},
		{
			name:       "invalid source with surrounding whitespace is rejected without fallback",
			selectedID: "  not_registered  ",
			wantErr:    ErrInvalidSource,
			wantErrID:  "not_registered",
		},
		{
			name:       "registered but unavailable source returns unavailable error",
			selectedID: string(SourceClaudeCode),
			wantErr:    ErrUnavailableSource,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewRegistry(SourceOpenCode)
			opencodeSource := newRegistryFakeSource(SourceOpenCode, true)
			claudeSource := newRegistryFakeSource(SourceClaudeCode, false)

			if err := registry.Register(opencodeSource); err != nil {
				t.Fatalf("Register(opencode) failed: %v", err)
			}
			if err := registry.Register(claudeSource); err != nil {
				t.Fatalf("Register(claude_code) failed: %v", err)
			}

			got, err := registry.Resolve(tt.selectedID)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("Resolve(%q) returned nil error, want %v", tt.selectedID, tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("Resolve(%q) error = %v, want errors.Is(..., %v)", tt.selectedID, err, tt.wantErr)
				}
				if tt.wantErrID != "" && !strings.Contains(err.Error(), tt.wantErrID) {
					t.Errorf("Resolve(%q) error = %q, want containing %q", tt.selectedID, err.Error(), tt.wantErrID)
				}
				if got != nil {
					t.Errorf("Resolve(%q) source = %#v, want nil on error", tt.selectedID, got)
				}
				if opencodeSource.overviewCalls != 0 {
					t.Errorf("Resolve(%q) touched default opencode source %d times; invalid/unavailable selections must not fallback", tt.selectedID, opencodeSource.overviewCalls)
				}
				return
			}

			if err != nil {
				t.Fatalf("Resolve(%q) returned unexpected error: %v", tt.selectedID, err)
			}
			if got == nil {
				t.Fatalf("Resolve(%q) returned nil source", tt.selectedID)
			}
			info := got.Info(context.Background())
			if info.ID != tt.wantID {
				t.Errorf("Resolve(%q).Info().ID = %q, want %q", tt.selectedID, info.ID, tt.wantID)
			}
		})
	}
}

func TestRegistryResolveBlankSourceIDsUseConfiguredDefault(t *testing.T) {
	tests := []struct {
		name       string
		selectedID string
	}{
		{name: "omitted source id", selectedID: ""},
		{name: "empty source id", selectedID: ""},
		{name: "whitespace source id", selectedID: " \t\n "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewRegistry(SourceClaudeCode)
			opencodeSource := newRegistryFakeSource(SourceOpenCode, true)
			claudeSource := newRegistryFakeSource(SourceClaudeCode, true)

			if err := registry.Register(opencodeSource); err != nil {
				t.Fatalf("Register(opencode) failed: %v", err)
			}
			if err := registry.Register(claudeSource); err != nil {
				t.Fatalf("Register(claude_code) failed: %v", err)
			}

			got, err := registry.Resolve(tt.selectedID)
			if err != nil {
				t.Fatalf("Resolve(%q) returned unexpected error: %v", tt.selectedID, err)
			}
			if got == nil {
				t.Fatalf("Resolve(%q) returned nil source", tt.selectedID)
			}
			if registry.DefaultID() != SourceClaudeCode {
				t.Fatalf("DefaultID() = %q, want %q", registry.DefaultID(), SourceClaudeCode)
			}
			if got.Info(context.Background()).ID != registry.DefaultID() {
				t.Errorf("Resolve(%q).Info().ID = %q, want configured DefaultID() %q", tt.selectedID, got.Info(context.Background()).ID, registry.DefaultID())
			}
		})
	}
}

func TestRegistryListIncludesSourceMetadata(t *testing.T) {
	registry := NewRegistry(SourceOpenCode)

	opencodeSource := newRegistryFakeSource(SourceOpenCode, true)
	opencodeSource.info.Label = "OpenCode"
	opencodeSource.info.Kind = "sqlite"
	opencodeSource.info.Path = "/home/test/.local/share/opencode/opencode.db"
	opencodeSource.info.PathSource = "--db"
	opencodeSource.info.ReadOnly = true
	opencodeSource.info.LocalOnly = true
	opencodeSource.info.Capabilities = []string{"overview", "daily", "messages"}

	claudeSource := newRegistryFakeSource(SourceClaudeCode, false)
	claudeSource.info.Label = "Claude Code"
	claudeSource.info.Kind = "jsonl"
	claudeSource.info.Path = "/home/test/.claude"
	claudeSource.info.PathSource = "$HOME/.claude"
	claudeSource.info.ReadOnly = true
	claudeSource.info.LocalOnly = true
	claudeSource.info.Warnings = []string{"plaintext transcripts may contain sensitive content"}

	if err := registry.Register(opencodeSource); err != nil {
		t.Fatalf("Register(opencode) failed: %v", err)
	}
	if err := registry.Register(claudeSource); err != nil {
		t.Fatalf("Register(claude_code) failed: %v", err)
	}

	infos := registry.List(context.Background())
	if len(infos) != 2 {
		t.Fatalf("List() returned %d sources, want 2: %#v", len(infos), infos)
	}

	byID := make(map[SourceID]SourceInfo, len(infos))
	for _, info := range infos {
		byID[info.ID] = info
	}

	openInfo, ok := byID[SourceOpenCode]
	if !ok {
		t.Fatalf("List() missing source %q: %#v", SourceOpenCode, infos)
	}
	if !openInfo.Available {
		t.Errorf("opencode Available = false, want true")
	}
	if !openInfo.Default {
		t.Errorf("opencode Default = false, want true")
	}
	if openInfo.Label != "OpenCode" {
		t.Errorf("opencode Label = %q, want OpenCode", openInfo.Label)
	}
	if openInfo.Kind != "sqlite" {
		t.Errorf("opencode Kind = %q, want sqlite", openInfo.Kind)
	}
	if openInfo.PathSource != "--db" {
		t.Errorf("opencode PathSource = %q, want --db", openInfo.PathSource)
	}

	claudeInfo, ok := byID[SourceClaudeCode]
	if !ok {
		t.Fatalf("List() missing source %q: %#v", SourceClaudeCode, infos)
	}
	if claudeInfo.Available {
		t.Errorf("claude_code Available = true, want false")
	}
	if claudeInfo.Default {
		t.Errorf("claude_code Default = true, want false")
	}
	if claudeInfo.Path != "/home/test/.claude" {
		t.Errorf("claude_code Path = %q, want /home/test/.claude", claudeInfo.Path)
	}
	if len(claudeInfo.Warnings) != 1 {
		t.Fatalf("claude_code Warnings len = %d, want 1: %#v", len(claudeInfo.Warnings), claudeInfo.Warnings)
	}
}

func TestRegistryCodexSourceSelectionSemantics(t *testing.T) {
	tests := []struct {
		name       string
		selectedID string
		wantID     SourceID
		wantErr    error
	}{
		{
			name:       "omitted source remains OpenCode even when Codex is available",
			selectedID: "",
			wantID:     SourceOpenCode,
		},
		{
			name:       "explicit codex resolves only Codex source",
			selectedID: string(SourceCodex),
			wantID:     SourceCodex,
		},
		{
			name:       "invalid source is rejected without Codex or OpenCode fallback",
			selectedID: "codex_typo",
			wantErr:    ErrInvalidSource,
		},
		{
			name:       "unsupported both still rejected",
			selectedID: "both",
			wantErr:    ErrUnsupportedSource,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewRegistry(SourceOpenCode)
			opencodeSource := newRegistryFakeSource(SourceOpenCode, true)
			claudeSource := newRegistryFakeSource(SourceClaudeCode, true)
			codexSource := newRegistryFakeSource(SourceCodex, true)

			for _, src := range []*registryFakeSource{opencodeSource, claudeSource, codexSource} {
				if err := registry.Register(src); err != nil {
					t.Fatalf("Register(%s) failed: %v", src.info.ID, err)
				}
			}

			got, err := registry.Resolve(tt.selectedID)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("Resolve(%q) error = nil, want %v", tt.selectedID, tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("Resolve(%q) error = %v, want errors.Is(..., %v)", tt.selectedID, err, tt.wantErr)
				}
				if got != nil {
					t.Errorf("Resolve(%q) source = %#v, want nil on error", tt.selectedID, got)
				}
				if opencodeSource.overviewCalls != 0 || codexSource.overviewCalls != 0 {
					t.Errorf("invalid selection touched fallback sources: opencode=%d codex=%d", opencodeSource.overviewCalls, codexSource.overviewCalls)
				}
				return
			}

			if err != nil {
				t.Fatalf("Resolve(%q) unexpected error: %v", tt.selectedID, err)
			}
			if got.Info(context.Background()).ID != tt.wantID {
				t.Errorf("Resolve(%q).Info().ID = %q, want %q", tt.selectedID, got.Info(context.Background()).ID, tt.wantID)
			}
			if tt.wantID == SourceCodex && opencodeSource.overviewCalls != 0 {
				t.Errorf("explicit Codex selection touched OpenCode fallback %d times", opencodeSource.overviewCalls)
			}
		})
	}
}

func TestRegistryListIncludesCodexMetadataAndStartupSelection(t *testing.T) {
	registry := NewRegistry(SourceOpenCode)
	registry.SetStartupID(SourceCodex)

	opencodeSource := newRegistryFakeSource(SourceOpenCode, true)
	codexSource := newRegistryFakeSource(SourceCodex, true)
	codexSource.info.Label = "Codex"
	codexSource.info.Kind = "jsonl"
	codexSource.info.Path = "/synthetic/codex"
	codexSource.info.PathSource = "--codex-home"
	codexSource.info.ReadOnly = true
	codexSource.info.LocalOnly = true
	codexSource.info.Capabilities = []string{"overview", "daily", "models", "tools", "projects", "sessions", "messages", "config"}
	codexSource.info.CostPolicy = CostPolicy{Status: "estimated_api_equivalent", Currency: "USD", PricingSnapshotID: "openai-codex-gpt-5.5-2026-04-23"}
	codexSource.info.Privacy = PrivacyInfo{PlaintextTranscripts: true, ReadOnly: true, LocalOnly: true, Redaction: true}

	if err := registry.Register(opencodeSource); err != nil {
		t.Fatalf("Register(opencode) failed: %v", err)
	}
	if err := registry.Register(codexSource); err != nil {
		t.Fatalf("Register(codex) failed: %v", err)
	}

	infos := registry.List(context.Background())
	byID := make(map[SourceID]SourceInfo, len(infos))
	for _, info := range infos {
		byID[info.ID] = info
	}
	codexInfo, ok := byID[SourceCodex]
	if !ok {
		t.Fatalf("List() missing Codex source: %#v", infos)
	}
	if !codexInfo.Available || codexInfo.Default || !codexInfo.Selected {
		t.Errorf("Codex available/default/selected = %v/%v/%v, want true/false/true", codexInfo.Available, codexInfo.Default, codexInfo.Selected)
	}
	if codexInfo.Kind != "jsonl" || !codexInfo.ReadOnly || !codexInfo.LocalOnly {
		t.Errorf("Codex kind/read/local = %q/%v/%v, want jsonl/true/true", codexInfo.Kind, codexInfo.ReadOnly, codexInfo.LocalOnly)
	}
	if codexInfo.CostPolicy.Status != "estimated_api_equivalent" {
		t.Errorf("Codex CostPolicy.Status = %q, want estimated_api_equivalent", codexInfo.CostPolicy.Status)
	}
	if !codexInfo.Privacy.PlaintextTranscripts || !codexInfo.Privacy.Redaction {
		t.Errorf("Codex privacy = %#v, want plaintext/redaction metadata", codexInfo.Privacy)
	}
}

func TestRegistryUnavailableCodexPlaceholderDoesNotFallback(t *testing.T) {
	registry := NewRegistry(SourceOpenCode)
	opencodeSource := newRegistryFakeSource(SourceOpenCode, true)
	if err := registry.Register(opencodeSource); err != nil {
		t.Fatalf("Register(opencode) failed: %v", err)
	}
	if err := registry.RegisterUnavailable(SourceInfo{
		ID:    SourceCodex,
		Label: "Codex",
		Kind:  "jsonl",
		Path:  "/synthetic/missing-codex",
		Diagnostics: SourceDiagnostics{
			Status: "empty",
			Reason: "Codex home contains no rollout transcripts",
		},
	}); err != nil {
		t.Fatalf("RegisterUnavailable(codex) failed: %v", err)
	}

	got, err := registry.Resolve(string(SourceCodex))
	if err == nil || !errors.Is(err, ErrUnavailableSource) {
		t.Fatalf("Resolve(codex unavailable) error = %v, want unavailable", err)
	}
	if got != nil {
		t.Errorf("Resolve(codex unavailable) source = %#v, want nil", got)
	}
	if opencodeSource.overviewCalls != 0 {
		t.Errorf("unavailable Codex selection touched OpenCode fallback %d times", opencodeSource.overviewCalls)
	}
}

func TestRegistryClose(t *testing.T) {
	tests := []struct {
		name         string
		closeErrByID map[SourceID]error
		wantErr      bool
	}{
		{
			name: "closes all registered sources successfully",
		},
		{
			name: "returns close error after attempting every source",
			closeErrByID: map[SourceID]error{
				SourceClaudeCode: errors.New("claude close failed"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewRegistry(SourceOpenCode)
			sources := []*registryFakeSource{
				newRegistryFakeSource(SourceOpenCode, true),
				newRegistryFakeSource(SourceClaudeCode, true),
			}

			for _, src := range sources {
				src.closeErr = tt.closeErrByID[src.info.ID]
				if err := registry.Register(src); err != nil {
					t.Fatalf("Register(%s) failed: %v", src.info.ID, err)
				}
			}

			err := registry.Close()
			if tt.wantErr && err == nil {
				t.Fatalf("Close() error = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Close() error = %v, want nil", err)
			}

			for _, src := range sources {
				if src.closeCalls != 1 {
					t.Errorf("source %s closeCalls = %d, want 1", src.info.ID, src.closeCalls)
				}
			}
		})
	}
}

type registryFakeSource struct {
	info          SourceInfo
	overviewCalls int
	closeCalls    int
	closeErr      error
}

func newRegistryFakeSource(id SourceID, available bool) *registryFakeSource {
	return &registryFakeSource{
		info: SourceInfo{
			ID:        id,
			Label:     fmt.Sprintf("%s source", id),
			Available: available,
		},
	}
}

func (s *registryFakeSource) Info(context.Context) SourceInfo {
	return s.info
}

func (s *registryFakeSource) Overview(context.Context, stats.PeriodQuery) (stats.OverviewStats, error) {
	s.overviewCalls++
	return stats.OverviewStats{Sessions: int64(s.overviewCalls)}, nil
}

func (s *registryFakeSource) Daily(context.Context, stats.PeriodQuery, ...stats.Granularity) (stats.DailyStats, error) {
	return stats.DailyStats{}, nil
}

func (s *registryFakeSource) DailyDimension(context.Context, string, stats.PeriodQuery) (stats.DailyDimensionStats, error) {
	return stats.DailyDimensionStats{}, nil
}

func (s *registryFakeSource) Models(context.Context, stats.PeriodQuery) (stats.ModelStats, error) {
	return stats.ModelStats{}, nil
}

func (s *registryFakeSource) Tools(context.Context, stats.PeriodQuery) (stats.ToolStats, error) {
	return stats.ToolStats{}, nil
}

func (s *registryFakeSource) Projects(context.Context, stats.PeriodQuery) (stats.ProjectStats, error) {
	return stats.ProjectStats{}, nil
}

func (s *registryFakeSource) ProjectByID(context.Context, string, stats.PeriodQuery, int, int) (*stats.ProjectDetail, error) {
	return nil, nil
}

func (s *registryFakeSource) Sessions(context.Context, stats.SessionQuery) (stats.SessionList, error) {
	return stats.SessionList{}, nil
}

func (s *registryFakeSource) SessionByID(context.Context, string) (*stats.SessionDetail, error) {
	return nil, nil
}

func (s *registryFakeSource) Messages(context.Context, stats.PeriodQuery, int, int, stats.MessageSort) (stats.MessageList, error) {
	return stats.MessageList{}, nil
}

func (s *registryFakeSource) MessageByID(context.Context, string) (*stats.MessageDetail, error) {
	return nil, nil
}

func (s *registryFakeSource) Config(context.Context) (stats.ConfigView, error) {
	return stats.ConfigView{}, nil
}

func (s *registryFakeSource) Close() error {
	s.closeCalls++
	return s.closeErr
}
