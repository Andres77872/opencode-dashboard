package cache

import (
	"context"
	"testing"
	"time"

	"opencode-dashboard/internal/stats"
)

func TestDailyRollupsKeepPartialEdgesExact(t *testing.T) {
	ctx := context.Background()
	store := openRollupTestStore(t)
	base := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	payload := rollupPayload([]messageRow{
		rollupMessage("before", "a", base.Add(50*time.Minute), 1, 1, "m1", "p1", stats.CostReported),
		rollupMessage("a-full", "a", base.Add(70*time.Minute), 2, 2, "m1", "p1", stats.CostComputed),
		rollupMessage("b-full", "b", base.Add(80*time.Minute), 3, 3, "m1", "p1", stats.CostReported),
		rollupUserMessage("b-user", "b", base.Add(90*time.Minute)),
		rollupMessage("c-full", "c", base.Add(150*time.Minute), 4, 4, "m2", "p2", stats.CostMissing),
		rollupMessage("c-edge", "c", base.Add(185*time.Minute), 5, 5, "m2", "p2", stats.CostReported),
	})
	if err := store.replaceSource(ctx, payload, base.Add(4*time.Hour)); err != nil {
		t.Fatalf("replaceSource() failed: %v", err)
	}

	pq := stats.PeriodQuery{FromTime: base.Add(55 * time.Minute), ToTime: base.Add(190 * time.Minute)}
	daily, err := store.Daily(ctx, string(rollupTestSourceID), pq)
	if err != nil {
		t.Fatalf("Daily() failed: %v", err)
	}
	if len(daily.Days) != 1 {
		t.Fatalf("daily rows = %#v, want one", daily.Days)
	}
	day := daily.Days[0]
	if day.Date != "2026-07-01" || day.Sessions != 3 || day.Messages != 5 || day.Cost != 14 || day.Tokens.Input != 14 {
		t.Errorf("partial-edge day = %#v, want 3 sessions / 5 messages / $14 / 14 input", day)
	}
	if day.CostStatus != stats.CostMixed || day.CostProvenance == nil ||
		day.CostProvenance.ReportedCount != 2 || day.CostProvenance.ComputedCount != 1 || day.CostProvenance.MissingCount != 1 {
		t.Errorf("partial-edge day provenance = %q/%#v, want mixed 2 reported/1 computed/1 missing", day.CostStatus, day.CostProvenance)
	}
	if daily.CostStatus != stats.CostMixed {
		t.Errorf("daily list cost status = %q, want mixed", daily.CostStatus)
	}

	hourly, err := store.Daily(ctx, string(rollupTestSourceID), pq, stats.GranularityHour)
	if err != nil {
		t.Fatalf("Daily(hour) failed: %v", err)
	}
	if len(hourly.Days) != 4 {
		t.Fatalf("hourly rows = %#v, want four (09..12)", hourly.Days)
	}
	byHour := map[string]stats.DayStats{}
	for _, row := range hourly.Days {
		byHour[row.Date] = row
	}
	// dailyHourly truncates the window start down to 09:00, so the 09 bucket
	// regains the 09:50 message that the day-granularity window excluded.
	if got := byHour["2026-07-01T09:00:00Z"]; got.Sessions != 1 || got.Messages != 1 || got.Cost != 1 || got.CostStatus != stats.CostReported {
		t.Errorf("09 bucket = %#v, want 1 session, 1 message, $1, reported", got)
	}
	if got := byHour["2026-07-01T10:00:00Z"]; got.Sessions != 2 || got.Messages != 3 || got.Cost != 5 || got.CostStatus != stats.CostMixed {
		t.Errorf("10 bucket = %#v, want 2 sessions, 3 messages, $5, mixed", got)
	}
	if got := byHour["2026-07-01T11:00:00Z"]; got.Sessions != 1 || got.Messages != 1 || got.Cost != 4 || got.CostStatus != stats.CostMissing {
		t.Errorf("11 bucket = %#v, want 1 session, 1 message, $4, missing", got)
	}
	if got := byHour["2026-07-01T12:00:00Z"]; got.Sessions != 1 || got.Messages != 1 || got.Cost != 5 || got.CostStatus != stats.CostReported {
		t.Errorf("12 edge bucket = %#v, want 1 session, 1 message, $5, reported", got)
	}
}

func TestAlignedDailyDoesNotReadMessageIndex(t *testing.T) {
	ctx := context.Background()
	store := openRollupTestStore(t)
	base := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	payload := rollupPayload([]messageRow{
		rollupMessage("a", "s1", base.Add(70*time.Minute), 2, 2, "m1", "p1", stats.CostReported),
		rollupMessage("b", "s2", base.Add(130*time.Minute), 3, 3, "m2", "p2", stats.CostComputed),
	})
	if err := store.replaceSource(ctx, payload, base.Add(4*time.Hour)); err != nil {
		t.Fatalf("replaceSource() failed: %v", err)
	}
	// As in TestAlignedOverviewAndModelsDoNotReadMessageIndex: hour-aligned
	// windows must be answerable from the rollup tables alone.
	if _, err := store.db.ExecContext(ctx, `DROP TABLE message_index`); err != nil {
		t.Fatalf("drop message_index: %v", err)
	}
	pq := stats.PeriodQuery{FromTime: base.Add(time.Hour), ToTime: base.Add(3 * time.Hour)}
	daily, err := store.Daily(ctx, string(rollupTestSourceID), pq)
	if err != nil {
		t.Fatalf("aligned Daily() touched message_index: %v", err)
	}
	if len(daily.Days) != 1 || daily.Days[0].Sessions != 2 || daily.Days[0].Messages != 2 || daily.Days[0].Cost != 5 {
		t.Errorf("aligned daily = %#v, want one day with 2 sessions/messages and $5", daily.Days)
	}
	hourly, err := store.Daily(ctx, string(rollupTestSourceID), pq, stats.GranularityHour)
	if err != nil {
		t.Fatalf("aligned Daily(hour) touched message_index: %v", err)
	}
	if len(hourly.Days) != 2 {
		t.Fatalf("aligned hourly = %#v, want two buckets", hourly.Days)
	}
	if hourly.Days[0].Messages != 1 || hourly.Days[1].Messages != 1 {
		t.Errorf("aligned hourly buckets = %#v, want one message each", hourly.Days)
	}
}

func TestSessionsListWindowedAggregation(t *testing.T) {
	ctx := context.Background()
	store := openRollupTestStore(t)
	base := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	payload := rollupPayload([]messageRow{
		rollupMessage("g1", "gamma", base.Add(30*time.Minute), 0.1, 1, "m1", "p1", stats.CostReported),
		rollupMessage("g2", "gamma", base.Add(40*time.Minute), 0.1, 1, "m1", "p1", stats.CostMissing),
		rollupMessage("g3", "gamma", base.Add(50*time.Minute), 0.1, 1, "m1", "p1", stats.CostReported),
		rollupMessage("a1", "alpha", base.Add(65*time.Minute), 1, 1, "m1", "p1", stats.CostReported),
		rollupMessage("a2", "alpha", base.Add(70*time.Minute), 1, 1, "m1", "p1", stats.CostReported),
		rollupMessage("b1", "beta", base.Add(120*time.Minute), 5, 1, "m1", "p1", stats.CostComputed),
	})
	if err := store.replaceSource(ctx, payload, base.Add(4*time.Hour)); err != nil {
		t.Fatalf("replaceSource() failed: %v", err)
	}

	full := stats.SessionQuery{FromTime: base, ToTime: base.Add(4 * time.Hour), PageSize: 10}

	list, err := store.Sessions(ctx, string(rollupTestSourceID), full)
	if err != nil {
		t.Fatalf("Sessions() failed: %v", err)
	}
	if list.Total != 3 || len(list.Sessions) != 3 {
		t.Fatalf("sessions = %#v (total %d), want three", list.Sessions, list.Total)
	}
	if list.Sessions[0].ID != "beta" || list.Sessions[1].ID != "alpha" || list.Sessions[2].ID != "gamma" {
		t.Errorf("newest order = %v, want beta, alpha, gamma", []string{list.Sessions[0].ID, list.Sessions[1].ID, list.Sessions[2].ID})
	}
	byID := map[string]stats.SessionEntry{}
	for _, entry := range list.Sessions {
		byID[entry.ID] = entry
	}
	if got := byID["gamma"]; got.MessageCount != 3 || !closeCost(got.Cost, 0.3) || got.CostStatus != stats.CostMixed {
		t.Errorf("gamma = %#v, want 3 messages, $0.3, mixed cost", got)
	}
	if got := byID["beta"]; got.MessageCount != 1 || got.Cost != 5 || got.CostStatus != stats.CostComputed {
		t.Errorf("beta = %#v, want 1 message, $5, computed cost", got)
	}

	costSorted := full
	costSorted.Sort = stats.SessionSortCost
	list, err = store.Sessions(ctx, string(rollupTestSourceID), costSorted)
	if err != nil {
		t.Fatalf("Sessions(cost) failed: %v", err)
	}
	if list.Sessions[0].ID != "beta" || list.Sessions[1].ID != "alpha" || list.Sessions[2].ID != "gamma" {
		t.Errorf("cost order = %v, want beta, alpha, gamma", []string{list.Sessions[0].ID, list.Sessions[1].ID, list.Sessions[2].ID})
	}

	msgSorted := full
	msgSorted.Sort = stats.SessionSortMessages
	list, err = store.Sessions(ctx, string(rollupTestSourceID), msgSorted)
	if err != nil {
		t.Fatalf("Sessions(messages) failed: %v", err)
	}
	if list.Sessions[0].ID != "gamma" || list.Sessions[1].ID != "alpha" || list.Sessions[2].ID != "beta" {
		t.Errorf("messages order = %v, want gamma, alpha, beta", []string{list.Sessions[0].ID, list.Sessions[1].ID, list.Sessions[2].ID})
	}

	// Window-scoped counts: excluding gamma's first message shrinks its
	// per-session aggregate; excluding all gamma activity drops the session.
	trimmed := full
	trimmed.FromTime = base.Add(35 * time.Minute)
	list, err = store.Sessions(ctx, string(rollupTestSourceID), trimmed)
	if err != nil {
		t.Fatalf("Sessions(trimmed) failed: %v", err)
	}
	for _, entry := range list.Sessions {
		if entry.ID == "gamma" && (entry.MessageCount != 2 || !closeCost(entry.Cost, 0.2)) {
			t.Errorf("trimmed gamma = %#v, want 2 messages, $0.2", entry)
		}
	}
	noGamma := full
	noGamma.FromTime = base.Add(time.Hour)
	list, err = store.Sessions(ctx, string(rollupTestSourceID), noGamma)
	if err != nil {
		t.Fatalf("Sessions(noGamma) failed: %v", err)
	}
	if list.Total != 2 {
		t.Errorf("windowed total = %d, want 2 (gamma outside window)", list.Total)
	}

	// The cached filter matches session ids and project names — never the
	// synthesized titles.
	filtered := full
	filtered.Filter = "amm"
	list, err = store.Sessions(ctx, string(rollupTestSourceID), filtered)
	if err != nil {
		t.Fatalf("Sessions(filter=amm) failed: %v", err)
	}
	if list.Total != 1 || len(list.Sessions) != 1 || list.Sessions[0].ID != "gamma" {
		t.Errorf("filter amm = %#v (total %d), want just gamma", list.Sessions, list.Total)
	}
	filtered.Filter = "session" // synthesized titles all contain "Session"
	list, err = store.Sessions(ctx, string(rollupTestSourceID), filtered)
	if err != nil {
		t.Fatalf("Sessions(filter=session) failed: %v", err)
	}
	if list.Total != 0 {
		t.Errorf("filter on synthesized title text matched %d sessions, want 0", list.Total)
	}
	filtered.Filter = "proj"
	list, err = store.Sessions(ctx, string(rollupTestSourceID), filtered)
	if err != nil {
		t.Fatalf("Sessions(filter=proj) failed: %v", err)
	}
	if list.Total != 3 {
		t.Errorf("project-name filter total = %d, want 3", list.Total)
	}

	paged := full
	paged.PageSize = 2
	paged.Page = 2
	list, err = store.Sessions(ctx, string(rollupTestSourceID), paged)
	if err != nil {
		t.Fatalf("Sessions(page 2) failed: %v", err)
	}
	if list.Total != 3 || len(list.Sessions) != 1 || list.Sessions[0].ID != "gamma" {
		t.Errorf("page 2 = %#v (total %d), want just gamma", list.Sessions, list.Total)
	}
}
