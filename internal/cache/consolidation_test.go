package cache

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/source/codex"
	"opencode-dashboard/internal/stats"
)

func TestSyncSourceOverridesCodexInteractiveScanTimeout(t *testing.T) {
	fixture := filepath.Join("..", "source", "codex", "testdata", "valid_home")
	period := stats.PeriodQuery{Period: "all"}

	direct := codex.New(codex.Options{
		CodexHome:   fixture,
		PathSource:  "test fixture",
		ScanTimeout: time.Nanosecond,
	})
	_, err := direct.Overview(context.Background(), period)
	if err == nil || !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("direct Codex Overview() error = %v, want context deadline exceeded", err)
	}

	// Use a fresh source so a failed interactive load cannot affect the cache
	// control. Cache consolidation supplies its own per-call deadline, which
	// must take precedence over the source's short interactive fallback.
	forCache := codex.New(codex.Options{
		CodexHome:   fixture,
		PathSource:  "test fixture",
		ScanTimeout: time.Nanosecond,
	})
	store := newTestStore(t)
	if err := store.SyncSource(context.Background(), forCache); err != nil {
		t.Fatalf("SyncSource() with short Codex interactive timeout failed: %v", err)
	}
}

func TestSyncSourcePrefersConsolidationData(t *testing.T) {
	created := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	src := &consolidationOnlySource{
		data: source.ConsolidationData{
			Sessions: []stats.SessionEntry{{
				ID:           "bulk-session",
				ProjectID:    "bulk-project",
				ProjectName:  "Bulk Project",
				TimeCreated:  created,
				TimeUpdated:  created,
				MessageCount: 1,
			}},
			Messages: []source.ConsolidationMessage{{
				Entry: stats.MessageEntry{
					ID:          "bulk-message",
					SessionID:   "bulk-session",
					Role:        "assistant",
					TimeCreated: created,
					Cost:        0.25,
					Tokens: &stats.TokenStats{
						Input:     11,
						Output:    7,
						Reasoning: 3,
						Cache:     stats.CacheStats{Read: 5, Write: 2},
					},
					ModelID:    "bulk-model",
					ProviderID: "bulk-provider",
				},
				Tools: []source.ConsolidationTool{{Name: "bulk-tool", Status: "completed"}},
			}},
			CostStatus: stats.CostReported,
		},
	}
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.SyncSource(ctx, src); err != nil {
		t.Fatalf("SyncSource() using ConsolidationData failed: %v", err)
	}

	period := stats.PeriodQuery{Period: "all"}
	overview, err := store.Overview(ctx, consolidationOnlySourceID, period)
	if err != nil {
		t.Fatalf("cached Overview() failed: %v", err)
	}
	if overview.Sessions != 1 || overview.Messages != 1 || overview.Cost != 0.25 {
		t.Errorf("cached overview sessions/messages/cost = %d/%d/%v, want 1/1/0.25", overview.Sessions, overview.Messages, overview.Cost)
	}
	wantTokens := stats.TokenStats{Input: 11, Output: 7, Reasoning: 3, Cache: stats.CacheStats{Read: 5, Write: 2}}
	if overview.Tokens != wantTokens {
		t.Errorf("cached overview tokens = %#v, want %#v", overview.Tokens, wantTokens)
	}

	tools, err := store.Tools(ctx, consolidationOnlySourceID, period)
	if err != nil {
		t.Fatalf("cached Tools() failed: %v", err)
	}
	if len(tools.Tools) != 1 {
		t.Fatalf("cached tools = %#v, want one tool", tools.Tools)
	}
	tool := tools.Tools[0]
	if tool.Name != "bulk-tool" || tool.Invocations != 1 || tool.Successes != 1 || tool.Failures != 0 || tool.Sessions != 1 {
		t.Errorf("cached tool aggregation = %#v, want one successful bulk-tool invocation in one session", tool)
	}
}

const consolidationOnlySourceID = "cache_consolidation_test"

type consolidationOnlySource struct {
	data source.ConsolidationData
}

func (s *consolidationOnlySource) Info(context.Context) source.SourceInfo {
	return source.SourceInfo{
		ID:        source.SourceID(consolidationOnlySourceID),
		Label:     "Cache Consolidation Test",
		Kind:      "test",
		Available: true,
		ReadOnly:  true,
		LocalOnly: true,
	}
}

func (s *consolidationOnlySource) ConsolidationData(context.Context, stats.PeriodQuery) (source.ConsolidationData, error) {
	return s.data, nil
}

func (s *consolidationOnlySource) Sessions(context.Context, stats.SessionQuery) (stats.SessionList, error) {
	return stats.SessionList{}, errors.New("legacy Sessions must not be called for a ConsolidationSource")
}

func (s *consolidationOnlySource) Messages(context.Context, stats.PeriodQuery, int, int, stats.MessageSort) (stats.MessageList, error) {
	return stats.MessageList{}, errors.New("legacy Messages must not be called for a ConsolidationSource")
}

func (s *consolidationOnlySource) MessageByID(context.Context, string) (*stats.MessageDetail, error) {
	return nil, errors.New("legacy MessageByID must not be called for a ConsolidationSource")
}

func (s *consolidationOnlySource) Overview(context.Context, stats.PeriodQuery) (stats.OverviewStats, error) {
	return stats.OverviewStats{}, nil
}

func (s *consolidationOnlySource) Daily(context.Context, stats.PeriodQuery, ...stats.Granularity) (stats.DailyStats, error) {
	return stats.DailyStats{}, nil
}

func (s *consolidationOnlySource) DailyDimension(context.Context, string, stats.PeriodQuery) (stats.DailyDimensionStats, error) {
	return stats.DailyDimensionStats{}, nil
}

func (s *consolidationOnlySource) Models(context.Context, stats.PeriodQuery) (stats.ModelStats, error) {
	return stats.ModelStats{}, nil
}

func (s *consolidationOnlySource) Tools(context.Context, stats.PeriodQuery) (stats.ToolStats, error) {
	return stats.ToolStats{}, nil
}

func (s *consolidationOnlySource) Projects(context.Context, stats.PeriodQuery) (stats.ProjectStats, error) {
	return stats.ProjectStats{}, nil
}

func (s *consolidationOnlySource) ProjectByID(context.Context, string, stats.PeriodQuery, int, int) (*stats.ProjectDetail, error) {
	return nil, nil
}

func (s *consolidationOnlySource) SessionByID(context.Context, string) (*stats.SessionDetail, error) {
	return nil, nil
}

func (s *consolidationOnlySource) Config(context.Context) (stats.ConfigView, error) {
	return stats.ConfigView{}, nil
}
