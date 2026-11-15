package quota

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
	"time"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
	"opencode-dashboard/internal/store"
	"opencode-dashboard/internal/store/fixture"
)

func nativeQuotaFixture(t *testing.T, observed time.Time, importing bool) string {
	t.Helper()
	b := fixture.NewV2Builder().AddProject(fixture.NewProject("p", "/work/p")).
		AddSession(fixture.V2Session{ID: "s", ProjectID: "p", Created: observed.Add(-time.Hour)})
	add := func(id, provider string, seq int, at time.Time, event map[string]any) {
		data := fixture.V2Assistant(provider, "sonnet", nil)
		data["native"] = map[string]any{"rateLimit": event}
		b.AddRow(fixture.V2Row{ID: id, SessionID: "s", Type: "assistant", Seq: seq, Created: at, Data: data})
	}
	add("quota", stats.ClaudeAgentProvider, 1, observed, map[string]any{
		"status": "allowed", "unifiedWindows": map[string]any{
			"seven_day": map[string]any{"utilization": 0.07, "resetsAt": observed.Add(24 * time.Hour).Unix()},
		},
	})
	// A later status-only event is not a measured zero. Another provider's
	// similarly shaped metadata must never be read as Claude account quota.
	add("status-only", stats.ClaudeAgentProvider, 2, observed.Add(time.Second), map[string]any{"status": "allowed", "rateLimitType": "five_hour"})
	add("other-provider", "anthropic", 3, observed.Add(2*time.Second), map[string]any{"rateLimitType": "seven_day", "utilization": 0.99})
	add("newer-created", stats.ClaudeAgentProvider, 4, observed.Add(-time.Minute), map[string]any{"rateLimitType": "seven_day", "utilization": 0.06})
	// The older response was updated after the more recently created one.
	// Freshness follows the quota observation rather than the request start.
	b.Exec(`UPDATE session_message SET time_created = ? WHERE id = 'quota'`, observed.Add(-2*time.Hour).UnixMilli())
	if importing {
		b.WithLegacyTables().SetKV("migration.v1-v2", `{"phase":"sessions"}`).
			Exec(`INSERT INTO session (id, project_id, slug, directory, version, time_created, time_updated) VALUES ('legacy', 'p', 'legacy', '/work', '1.18', 1, 1)`)
	}
	path, err := b.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Dir(path)) })
	return path
}

func TestClaudeQuotaUsesNativeOpenCodeSnapshotWithoutDoubleCounting(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	observed := now.Add(-time.Minute)
	path := nativeQuotaFixture(t, observed, false)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	statusline := filepath.Join(t.TempDir(), "snapshot.json")
	writeTestSnapshot(t, statusline, ClaudeSnapshot{SavedAtMS: now.Add(-time.Hour).UnixMilli(), FiveHour: &ClaudeWindow{UsedPercentage: 90}, SevenDay: &ClaudeWindow{UsedPercentage: 5}})
	p := &claudeProvider{snapshotPath: statusline, openCodeDBPath: path, now: func() time.Time { return now }}
	got := p.quota(context.Background())
	if got.Status != StatusOK || got.SourceID != source.SourceOpenCode || got.SourceProviderID != stats.ClaudeAgentProvider || got.AsOfMS != observed.UnixMilli() {
		t.Fatalf("quota = %+v, want native Claude Agent snapshot", got)
	}
	if len(got.Windows) != 1 || got.Windows[0].ID != "weekly" || !approxEqual(got.Windows[0].UsedPercent, 7) || got.Windows[0].ResetsAt != observed.Add(24*time.Hour).Unix() {
		t.Fatalf("windows = %+v; want only the measured native weekly 7%% (no sum or older 5h)", got.Windows)
	}
	p.now = func() time.Time { return now.Add(time.Hour) }
	stale := p.quota(context.Background())
	if stale.Status != StatusStale || stale.AsOfMS != got.AsOfMS {
		t.Fatalf("poll refreshed observation time: %+v", stale)
	}
	// A newer statusline reading replaces the entire observation, while the
	// single monitor stays associated with OpenCode's Claude Agent provider.
	writeTestSnapshot(t, statusline, ClaudeSnapshot{SavedAtMS: now.UnixMilli(), FiveHour: &ClaudeWindow{UsedPercentage: 12}})
	p.now = func() time.Time { return now }
	newer := p.quota(context.Background())
	if len(newer.Windows) != 1 || newer.Windows[0].UsedPercent != 12 || newer.AsOfMS != now.UnixMilli() || newer.SourceID != source.SourceOpenCode {
		t.Fatalf("newer statusline = %+v", newer)
	}
	after, err := os.ReadFile(path)
	if err != nil || sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("quota lookup modified the OpenCode database")
	}
}

func TestClaudeNativeQuotaParsingPreservesMeasuredZeroAndMissingWindows(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		percent     float64
		missing     bool
	}{
		{"measured zero", `{"unifiedWindows":{"five_hour":{"utilization":0,"resetsAt":1794945600}}}`, 0, false},
		{"top-level event", `{"rateLimitType":"five_hour","utilization":0.42,"resetsAt":1794945600000}`, 42, false},
		{"no utilization", `{"rateLimitType":"five_hour","status":"rejected"}`, 0, true},
		{"negative utilization", `{"unifiedWindows":{"five_hour":{"utilization":-1}}}`, 0, true},
		{"invalid event", `{"unifiedWindows":`, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snap := parseClaudeNativeSnapshot(tc.input, 1000)
			if tc.missing {
				if snap != nil {
					t.Fatalf("missing quota became a measurement: %+v", snap)
				}
				return
			}
			if snap == nil || snap.FiveHour == nil || snap.SevenDay != nil || snap.FiveHour.UsedPercentage != tc.percent || snap.FiveHour.ResetsAt != 1794945600 {
				t.Fatalf("snapshot = %+v", snap)
			}
		})
	}
}

func TestClaudeAgentQuotaFollowsOpenCodeSourceWithoutDuplicateProvider(t *testing.T) {
	t.Setenv("OPENCODE_DASHBOARD_MINIMAX_API_KEY", "")
	now := time.Now().UTC()
	path := nativeQuotaFixture(t, now.Add(-time.Minute), false)
	for _, tc := range []struct {
		name           string
		openCode, code bool
		count          int
	}{
		{"both enabled", true, true, 1},
		{"only OpenCode enabled", true, false, 1},
		{"both disabled", false, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(Options{
				OpenCodeDBPath: path, ClaudeSnapshotPath: filepath.Join(t.TempDir(), "missing.json"),
				CodexHome: t.TempDir(), KimiHome: t.TempDir(), MiniMaxAuthPath: filepath.Join(t.TempDir(), "missing.json"),
				SourceEnabled: func(id source.SourceID) bool {
					return id == source.SourceOpenCode && tc.openCode || id == source.SourceClaudeCode && tc.code
				},
			})
			response := svc.Quotas(context.Background())
			count := 0
			for _, q := range response.Providers {
				if q.Provider == ProviderClaude {
					count++
					if q.Status != StatusOK || q.SourceProviderID != stats.ClaudeAgentProvider {
						t.Fatalf("Claude Agent quota = %+v", q)
					}
				}
			}
			if count != tc.count {
				t.Fatalf("got %d Claude monitors, want %d", count, tc.count)
			}
		})
	}
}

func TestClaudeAgentQuotaAvailableDuringOpenCodeHistoryImport(t *testing.T) {
	path := nativeQuotaFixture(t, time.Now().UTC(), true)
	ctx := context.Background()
	st, err := store.Connect(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Version() != store.SchemaV1 {
		t.Fatal("fixture must still read 1.x history for analytics")
	}
	st.Close()
	snap, err := latestOpenCodeClaudeSnapshot(ctx, path)
	if err != nil || snap == nil || snap.SevenDay == nil || !approxEqual(snap.SevenDay.UsedPercentage, 7) {
		t.Fatalf("2.x quota during 1.x import = %+v, %v", snap, err)
	}
}
