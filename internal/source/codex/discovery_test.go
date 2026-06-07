package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

func TestCodexDiscoveryScansOnlyRolloutTranscripts(t *testing.T) {
	discovered := discoverTranscripts(testContext(t), fixturePath(t, "privacy_home"))
	if !discovered.available {
		t.Fatalf("discoverTranscripts(privacy_home).available = false, diagnostics: %#v", discovered.diagnostics)
	}
	if discovered.diagnostics.ScannedFiles != 1 {
		t.Errorf("ScannedFiles = %d, want exactly 1 rollout-*.jsonl under sessions", discovered.diagnostics.ScannedFiles)
	}
	if len(discovered.files) != 1 {
		t.Fatalf("files len = %d, want 1 allowed rollout file: %#v", len(discovered.files), discovered.files)
	}
	file := discovered.files[0]
	if filepath.Base(file.Path) != "rollout-2026-01-02T04-05-06Z-privacy-session.jsonl" {
		t.Errorf("discovered file = %q, want privacy rollout", file.Path)
	}
	path := filepath.ToSlash(file.Path)
	for _, forbidden := range []string{"auth.json", "logs_2.sqlite", "state_5.sqlite", "history.jsonl", "models_cache.json", "/cache/", "/skills/", "/tmp/", "/plugins/", "/logs/", "not-a-rollout.jsonl", "rollout-ignored.txt"} {
		if strings.Contains(path, forbidden) {
			t.Errorf("discovered disallowed Codex artifact %q in path %q", forbidden, path)
		}
	}
}

func TestCodexDiscoveryAvailabilityForValidEmptyMissingHomes(t *testing.T) {
	tests := []struct {
		name             string
		home             string
		wantAvailable    bool
		wantScannedFiles int64
		wantReason       string
	}{
		{name: "valid home discovers rollout", home: fixturePath(t, "valid_home"), wantAvailable: true, wantScannedFiles: 1},
		{name: "empty home is reported honestly", home: fixturePath(t, "empty_home"), wantAvailable: false, wantReason: "empty"},
		{name: "missing home is unavailable", home: filepath.Join(t.TempDir(), "missing-codex-home"), wantAvailable: false, wantReason: "not found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := New(Options{CodexHome: tt.home, PathSource: "test fixture", PricingSnapshotPath: fixturePath(t, "pricing_snapshot.json")})
			info := src.Info(testContext(t))
			if info.ID != source.SourceCodex {
				t.Errorf("Info().ID = %q, want %q", info.ID, source.SourceCodex)
			}
			if info.Kind != "jsonl" {
				t.Errorf("Info().Kind = %q, want jsonl", info.Kind)
			}
			if info.Available != tt.wantAvailable {
				t.Errorf("Info().Available = %v, want %v; diagnostics: %#v", info.Available, tt.wantAvailable, info.Diagnostics)
			}
			if info.Diagnostics.ScannedFiles != tt.wantScannedFiles {
				t.Errorf("ScannedFiles = %d, want %d", info.Diagnostics.ScannedFiles, tt.wantScannedFiles)
			}
			if tt.wantReason != "" && !strings.Contains(strings.ToLower(info.Diagnostics.Reason), tt.wantReason) && !strings.Contains(strings.ToLower(info.Diagnostics.Status), tt.wantReason) {
				t.Errorf("diagnostics = %#v, want reason/status containing %q", info.Diagnostics, tt.wantReason)
			}
		})
	}
}

func TestCodexSourceScanningIsReadOnly(t *testing.T) {
	src, home := newCopiedFixtureSource(t, "privacy_home")
	before := snapshotTree(t, home)

	ctx := testContext(t)
	_ = src.Info(ctx)
	_, _ = src.Overview(ctx, stats.PeriodQuery{Period: "all"})
	_, _ = src.Config(ctx)

	after := snapshotTree(t, home)
	assertTreeUnchanged(t, before, after)
}

func TestCodexParserTreatsDisappearedTranscriptAsMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "rollout-missing.jsonl")
	_, _, err := parseTranscriptFile(testContext(t), transcriptFile{Path: missing, SessionID: "gone"})
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("parseTranscriptFile missing error = %v, want os.ErrNotExist", err)
	}
}

func TestCodexDoesNotReadPrivateHomeForOmittedOpenCodeSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	privateCodex := filepath.Join(home, ".codex")
	writeTempCodexHomeAt(t, privateCodex, map[string][]string{
		"sessions/2026/01/02/rollout-2026-01-02T10-00-00Z-private.jsonl": {
			`{"timestamp":"2026-01-02T10:00:00Z","type":"session_meta","payload":{"id":"private-session","model_provider":"openai"}}`,
			`{"timestamp":"2026-01-02T10:00:01Z","type":"event_msg","payload":{"type":"user_message","turn_id":"private-turn","message":"SYNTHETIC_PRIVATE_HOME_PROMPT_MUST_NOT_LEAK"}}`,
		},
	})

	opencodeSource := newCodexDiscoveryFakeSource(source.SourceOpenCode, true)
	registry := source.NewRegistry(source.SourceOpenCode)
	if err := registry.Register(opencodeSource); err != nil {
		t.Fatalf("Register(opencode) failed: %v", err)
	}

	selected, err := registry.Resolve("")
	if err != nil {
		t.Fatalf("Resolve(omitted source) failed: %v", err)
	}
	if selected.Info(testContext(t)).ID != source.SourceOpenCode {
		t.Fatalf("omitted source selected %q, want opencode", selected.Info(testContext(t)).ID)
	}
	if opencodeSource.messagesCalls != 0 {
		t.Errorf("opencode fake messagesCalls = %d before any endpoint, want 0", opencodeSource.messagesCalls)
	}
}

func writeTempCodexHomeAt(t *testing.T, home string, files map[string][]string) {
	t.Helper()
	for relPath, lines := range files {
		path := filepath.Join(home, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create fixture dir for %s: %v", relPath, err)
		}
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", relPath, err)
		}
	}
}

type codexDiscoveryFakeSource struct {
	info          source.SourceInfo
	messagesCalls int
}

func newCodexDiscoveryFakeSource(id source.SourceID, available bool) *codexDiscoveryFakeSource {
	return &codexDiscoveryFakeSource{info: source.SourceInfo{ID: id, Label: string(id), Available: available, ReadOnly: true, LocalOnly: true}}
}

func (s *codexDiscoveryFakeSource) Info(context.Context) source.SourceInfo { return s.info }
func (s *codexDiscoveryFakeSource) Overview(context.Context, stats.PeriodQuery) (stats.OverviewStats, error) {
	return stats.OverviewStats{SourceID: string(s.info.ID)}, nil
}
func (s *codexDiscoveryFakeSource) Daily(context.Context, stats.PeriodQuery, ...stats.Granularity) (stats.DailyStats, error) {
	return stats.DailyStats{SourceID: string(s.info.ID)}, nil
}
func (s *codexDiscoveryFakeSource) DailyDimension(context.Context, string, stats.PeriodQuery) (stats.DailyDimensionStats, error) {
	return stats.DailyDimensionStats{SourceID: string(s.info.ID)}, nil
}
func (s *codexDiscoveryFakeSource) Models(context.Context, stats.PeriodQuery) (stats.ModelStats, error) {
	return stats.ModelStats{SourceID: string(s.info.ID)}, nil
}
func (s *codexDiscoveryFakeSource) Tools(context.Context, stats.PeriodQuery) (stats.ToolStats, error) {
	return stats.ToolStats{SourceID: string(s.info.ID)}, nil
}
func (s *codexDiscoveryFakeSource) Projects(context.Context, stats.PeriodQuery) (stats.ProjectStats, error) {
	return stats.ProjectStats{SourceID: string(s.info.ID)}, nil
}
func (s *codexDiscoveryFakeSource) ProjectByID(context.Context, string, stats.PeriodQuery, int, int) (*stats.ProjectDetail, error) {
	return nil, nil
}
func (s *codexDiscoveryFakeSource) Sessions(context.Context, stats.SessionQuery) (stats.SessionList, error) {
	return stats.SessionList{SourceID: string(s.info.ID)}, nil
}
func (s *codexDiscoveryFakeSource) SessionByID(context.Context, string) (*stats.SessionDetail, error) {
	return nil, nil
}
func (s *codexDiscoveryFakeSource) Messages(context.Context, stats.PeriodQuery, int, int, stats.MessageSort) (stats.MessageList, error) {
	s.messagesCalls++
	return stats.MessageList{SourceID: string(s.info.ID)}, nil
}
func (s *codexDiscoveryFakeSource) MessageByID(context.Context, string) (*stats.MessageDetail, error) {
	return nil, nil
}
func (s *codexDiscoveryFakeSource) Config(context.Context) (stats.ConfigView, error) {
	return stats.ConfigView{SourceID: string(s.info.ID)}, nil
}
