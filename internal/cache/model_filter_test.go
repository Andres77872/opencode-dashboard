package cache

import (
	"context"
	"testing"
	"time"

	"opencode-dashboard/internal/stats"
)

func TestModelFilteredOverviewDailyMessages(t *testing.T) {
	ctx := context.Background()
	store := openRollupTestStore(t)
	base := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	payload := rollupPayload([]messageRow{
		rollupMessage("m1-a", "s1", base.Add(70*time.Minute), 2, 2, "m1", "p1", stats.CostReported),
		rollupMessage("m1-b", "s2", base.Add(80*time.Minute), 3, 3, "m1", "p1", stats.CostComputed),
		rollupUserMessage("user", "s2", base.Add(85*time.Minute)),
		rollupMessage("m2-a", "s2", base.Add(130*time.Minute), 4, 4, "m2", "p2", stats.CostMissing),
		rollupMessage("m1-edge", "s3", base.Add(185*time.Minute), 5, 5, "m1", "p1", stats.CostReported),
	})
	if err := store.replaceSource(ctx, payload, base.Add(4*time.Hour)); err != nil {
		t.Fatalf("replaceSource() failed: %v", err)
	}

	// Window with a trailing partial-hour edge so both the rollup and the
	// message_index edge paths contribute.
	pq := stats.PeriodQuery{FromTime: base.Add(time.Hour), ToTime: base.Add(190 * time.Minute), Model: "m1"}
	overview, err := store.Overview(ctx, string(rollupTestSourceID), pq)
	if err != nil {
		t.Fatalf("filtered Overview() failed: %v", err)
	}
	if overview.Sessions != 3 || overview.Messages != 3 || overview.Cost != 10 || overview.Tokens.Input != 10 || overview.Days != 1 {
		t.Errorf("m1 overview = %#v, want 3 sessions, 3 messages, $10, 10 input, 1 day", overview)
	}
	if overview.CostStatus != stats.CostMixed || overview.CostProvenance == nil ||
		overview.CostProvenance.ReportedCount != 2 || overview.CostProvenance.ComputedCount != 1 || overview.CostProvenance.MissingCount != 0 {
		t.Errorf("m1 overview provenance = %q/%#v, want mixed 2 reported/1 computed", overview.CostStatus, overview.CostProvenance)
	}

	providerMiss := pq
	providerMiss.Provider = "wrong"
	empty, err := store.Overview(ctx, string(rollupTestSourceID), providerMiss)
	if err != nil {
		t.Fatalf("provider-filtered Overview() failed: %v", err)
	}
	if empty.Messages != 0 || empty.Sessions != 0 {
		t.Errorf("m1/wrong-provider overview = %#v, want empty", empty)
	}

	daily, err := store.Daily(ctx, string(rollupTestSourceID), pq)
	if err != nil {
		t.Fatalf("filtered Daily() failed: %v", err)
	}
	if len(daily.Days) != 1 {
		t.Fatalf("filtered daily rows = %#v, want one", daily.Days)
	}
	if d := daily.Days[0]; d.Sessions != 3 || d.Messages != 3 || d.Cost != 10 || d.Tokens.Input != 10 {
		t.Errorf("m1 day = %#v, want 3 sessions, 3 messages, $10, 10 input", d)
	}

	hourly, err := store.Daily(ctx, string(rollupTestSourceID), pq, stats.GranularityHour)
	if err != nil {
		t.Fatalf("filtered Daily(hour) failed: %v", err)
	}
	byHour := map[string]stats.DayStats{}
	for _, row := range hourly.Days {
		byHour[row.Date] = row
	}
	if got := byHour["2026-07-01T10:00:00Z"]; got.Messages != 2 || got.Cost != 5 {
		t.Errorf("m1 10h bucket = %#v, want 2 messages, $5", got)
	}
	if got := byHour["2026-07-01T11:00:00Z"]; got.Messages != 0 {
		t.Errorf("m1 11h bucket = %#v, want empty (only m2 active)", got)
	}
	if got := byHour["2026-07-01T12:00:00Z"]; got.Messages != 1 || got.Cost != 5 {
		t.Errorf("m1 12h edge bucket = %#v, want 1 message, $5", got)
	}

	list, err := store.Messages(ctx, string(rollupTestSourceID), pq, 1, 50, stats.MessageSort{})
	if err != nil {
		t.Fatalf("filtered Messages() failed: %v", err)
	}
	if list.Total != 3 || len(list.Messages) != 3 {
		t.Fatalf("filtered messages = %#v (total %d), want the three m1 rows", list.Messages, list.Total)
	}
	for _, entry := range list.Messages {
		if entry.ModelID != "m1" {
			t.Errorf("filtered message %q has model %q, want m1", entry.ID, entry.ModelID)
		}
	}
}
