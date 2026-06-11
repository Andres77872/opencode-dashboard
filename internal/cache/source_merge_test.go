package cache

import (
	"context"
	"testing"
	"time"

	"opencode-dashboard/internal/stats"
)

func msgIn(id, session string, created time.Time, cost float64) stats.MessageEntry {
	entry := testMessage(id, created, cost)
	entry.SessionID = session
	return entry
}

// seedMerge consolidates msgs into a fresh store with the given cutoff and
// returns the store plus a CachedSource over the same fake.
func seedMerge(t *testing.T, msgs []stats.MessageEntry, cutoff time.Time) (*Store, *CachedSource, *syncFakeSource) {
	t.Helper()
	src := &syncFakeSource{messages: msgs}
	store := newTestStore(t)
	if _, err := store.SyncSourceWithOptions(context.Background(), src, SyncOptions{Cutoff: cutoff}); err != nil {
		t.Fatalf("seed sync failed: %v", err)
	}
	cached := WrapSource(store, src)
	t.Cleanup(func() { _ = cached.Close() })
	return store, cached, src
}

func TestSplitPeriod(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-6 * time.Hour) // 06:00

	t.Run("zero cutoff is live only", func(t *testing.T) {
		sp, err := splitPeriod(stats.PeriodQuery{Period: "7d"}, time.Time{}, now)
		if err != nil {
			t.Fatal(err)
		}
		if sp.hasCache || !sp.hasLive {
			t.Fatalf("split = %+v, want live-only", sp)
		}
		if !sp.livePQ.FromTime.IsZero() {
			t.Fatalf("live-only passthrough must not constrain the query: %+v", sp.livePQ)
		}
	})

	t.Run("historical range is cache only", func(t *testing.T) {
		sp, err := splitPeriod(stats.PeriodQuery{From: "2026-06-01", To: "2026-06-05"}, cutoff, now)
		if err != nil {
			t.Fatal(err)
		}
		if !sp.hasCache || sp.hasLive {
			t.Fatalf("split = %+v, want cache-only", sp)
		}
		if !sp.cachePQ.ToTime.Equal(cutoff) {
			t.Fatalf("cachePQ.ToTime = %s, want cutoff %s", sp.cachePQ.ToTime, cutoff)
		}
	})

	t.Run("recent hour preset is live only", func(t *testing.T) {
		sp, err := splitPeriod(stats.PeriodQuery{Period: "1h"}, cutoff, now)
		if err != nil {
			t.Fatal(err)
		}
		if sp.hasCache || !sp.hasLive {
			t.Fatalf("split = %+v, want live-only", sp)
		}
		if !sp.livePQ.FromTime.Equal(now.Add(-time.Hour)) || !sp.livePQ.ToTime.Equal(now) {
			t.Fatalf("livePQ window = [%s, %s), want [now-1h, now)", sp.livePQ.FromTime, sp.livePQ.ToTime)
		}
	})

	t.Run("spanning preset splits at the cutoff", func(t *testing.T) {
		sp, err := splitPeriod(stats.PeriodQuery{Period: "7d"}, cutoff, now)
		if err != nil {
			t.Fatal(err)
		}
		if !sp.hasCache || !sp.hasLive {
			t.Fatalf("split = %+v, want both halves", sp)
		}
		if !sp.cachePQ.ToTime.Equal(cutoff) || !sp.livePQ.FromTime.Equal(cutoff) {
			t.Fatalf("split boundaries cache-to=%s live-from=%s, want both at cutoff %s", sp.cachePQ.ToTime, sp.livePQ.FromTime, cutoff)
		}
		if sp.livePQ.ToTime.Before(now) {
			t.Fatalf("livePQ.ToTime = %s, want resolved end", sp.livePQ.ToTime)
		}
	})

	t.Run("all period spans", func(t *testing.T) {
		sp, err := splitPeriod(stats.PeriodQuery{Period: "all"}, cutoff, now)
		if err != nil {
			t.Fatal(err)
		}
		if !sp.hasCache || !sp.hasLive {
			t.Fatalf("split = %+v, want both halves", sp)
		}
		if sp.cachePQ.Period != "all" || !sp.cachePQ.ToTime.Equal(cutoff) {
			t.Fatalf("cachePQ = %+v, want all capped at cutoff", sp.cachePQ)
		}
		if !sp.livePQ.FromTime.Equal(cutoff) {
			t.Fatalf("livePQ.FromTime = %s, want cutoff", sp.livePQ.FromTime)
		}
	})

	t.Run("invalid period errors", func(t *testing.T) {
		if _, err := splitPeriod(stats.PeriodQuery{Period: "2w"}, cutoff, now); err == nil {
			t.Fatalf("invalid period must error")
		}
	})
}

func TestMergeOverviewDedupesSpanningSession(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	cutoff := base.Add(-6 * time.Hour)
	_, cached, _ := seedMerge(t, []stats.MessageEntry{
		msgIn("a", "s1", base.Add(-10*time.Hour), 0.01),
	}, cutoff)

	sp, err := splitPeriod(stats.PeriodQuery{Period: "all"}, cutoff, base)
	if err != nil {
		t.Fatal(err)
	}
	c, err := cached.store.Overview(ctx, syncFakeSourceID, sp.cachePQ)
	if err != nil {
		t.Fatal(err)
	}
	gap := gapData{msgs: []stats.MessageEntry{
		msgIn("b", "s1", base.Add(-2*time.Hour), 0.02), // same session spans the cutoff
		msgIn("c", "s2", base.Add(-1*time.Hour), 0.04), // gap-only session
	}}
	merged, err := cached.mergeOverview(ctx, sp, c, gap)
	if err != nil {
		t.Fatal(err)
	}
	if merged.Messages != 3 {
		t.Fatalf("Messages = %d, want 3", merged.Messages)
	}
	if merged.Sessions != 2 {
		t.Fatalf("Sessions = %d, want 2 (s1 deduped across the cutoff)", merged.Sessions)
	}
	if diff := merged.Cost - 0.07; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("Cost = %f, want 0.07", merged.Cost)
	}
	// -10h and -2h/-1h are different UTC dates only when the boundary day is
	// shared; here all three are on 2026-06-10 → Days must be 1, not 2.
	if merged.Days != 1 {
		t.Fatalf("Days = %d, want 1 (boundary day counted once)", merged.Days)
	}
}

func TestMergeDailyBoundaryDayDedup(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	cutoff := base.Add(-6 * time.Hour) // 06:00 same day
	_, cached, _ := seedMerge(t, []stats.MessageEntry{
		msgIn("prev", "s0", base.Add(-30*time.Hour), 0.01), // 2026-06-09
		msgIn("am", "s1", cutoff.Add(-time.Hour), 0.02),    // 2026-06-10 05:00, cached
	}, cutoff)

	pq := stats.PeriodQuery{From: "2026-06-08", To: "2026-06-10"}
	sp, err := splitPeriod(pq, cutoff, base)
	if err != nil {
		t.Fatal(err)
	}
	c, err := cached.store.Daily(ctx, syncFakeSourceID, sp.cachePQ, stats.GranularityDay)
	if err != nil {
		t.Fatal(err)
	}
	gap := gapData{msgs: []stats.MessageEntry{
		msgIn("noon", "s1", base.Add(-2*time.Hour), 0.04),  // same session as "am"
		msgIn("later", "s2", base.Add(-1*time.Hour), 0.08), // new session
	}}
	merged, err := cached.mergeDaily(ctx, sp, stats.GranularityDay, c, gap)
	if err != nil {
		t.Fatal(err)
	}
	byDate := make(map[string]stats.DayStats)
	for _, d := range merged.Days {
		byDate[d.Date] = d
	}
	if d := byDate["2026-06-09"]; d.Messages != 1 || d.Sessions != 1 {
		t.Fatalf("2026-06-09 = %+v, want 1 message / 1 session from cache", d)
	}
	boundary := byDate["2026-06-10"]
	if boundary.Messages != 3 {
		t.Fatalf("boundary day messages = %d, want 3", boundary.Messages)
	}
	if boundary.Sessions != 2 {
		t.Fatalf("boundary day sessions = %d, want 2 (s1 deduped)", boundary.Sessions)
	}
	if len(merged.Days) != 3 {
		t.Fatalf("days = %d entries, want zero-filled 2026-06-08..10", len(merged.Days))
	}
}

func TestMergeDailyHourGranularityNoDoubleBucket(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	cutoff := base.Add(-6 * time.Hour)
	_, cached, _ := seedMerge(t, []stats.MessageEntry{
		msgIn("cached", "s1", base.Add(-7*time.Hour), 0.01),
	}, cutoff)

	sp, err := splitPeriod(stats.PeriodQuery{Period: "12h"}, cutoff, base)
	if err != nil {
		t.Fatal(err)
	}
	// The store resolves presets against the real clock; pin the cache half to
	// the same fixed window the split used so the test is deterministic.
	cachePQ := stats.PeriodQuery{FromTime: base.Add(-12 * time.Hour), ToTime: cutoff}
	c, err := cached.store.Daily(ctx, syncFakeSourceID, cachePQ, stats.GranularityHour)
	if err != nil {
		t.Fatal(err)
	}
	gap := gapData{msgs: []stats.MessageEntry{
		msgIn("fresh", "s1", base.Add(-2*time.Hour), 0.02),
	}}
	merged, err := cached.mergeDaily(ctx, sp, stats.GranularityHour, c, gap)
	if err != nil {
		t.Fatal(err)
	}
	var total int64
	for _, d := range merged.Days {
		if d.Messages > 1 {
			t.Fatalf("bucket %s has %d messages, want at most 1 (no double counting)", d.Date, d.Messages)
		}
		total += d.Messages
	}
	if total != 2 {
		t.Fatalf("summed bucket messages = %d, want 2", total)
	}
	if len(merged.Days) != 12 {
		t.Fatalf("hour buckets = %d, want continuous 12", len(merged.Days))
	}
	if got := merged.Days[0].Date; got != base.Add(-12*time.Hour).Format(hourKeyFormat) {
		t.Fatalf("first bucket = %s, want %s", got, base.Add(-12*time.Hour).Format(hourKeyFormat))
	}
}

func TestMergeMessagesPagination(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	cutoff := base.Add(-6 * time.Hour)
	cacheMsgs := []stats.MessageEntry{
		msgIn("c1", "s1", base.Add(-12*time.Hour), 0.05),
		msgIn("c2", "s1", base.Add(-11*time.Hour), 0.01),
		msgIn("c3", "s1", base.Add(-10*time.Hour), 0.04),
		msgIn("c4", "s1", base.Add(-9*time.Hour), 0.02),
		msgIn("c5", "s1", base.Add(-8*time.Hour), 0.03),
	}
	_, cached, _ := seedMerge(t, cacheMsgs, cutoff)
	gap := gapData{msgs: []stats.MessageEntry{
		msgIn("g1", "s1", base.Add(-3*time.Hour), 0.06),
		msgIn("g2", "s1", base.Add(-2*time.Hour), 0.005),
		msgIn("g3", "s1", base.Add(-1*time.Hour), 0.07),
	}}
	sp, err := splitPeriod(stats.PeriodQuery{Period: "all"}, cutoff, base)
	if err != nil {
		t.Fatal(err)
	}

	timeDesc := stats.MessageSort{Field: stats.MessageSortTime, Direction: stats.MessageSortDesc}
	page1, err := cached.mergeMessages(ctx, sp, 1, 4, timeDesc, gap)
	if err != nil {
		t.Fatal(err)
	}
	if page1.Total != 8 {
		t.Fatalf("Total = %d, want 8", page1.Total)
	}
	wantPage1 := []string{"g3", "g2", "g1", "c5"}
	for i, want := range wantPage1 {
		if page1.Messages[i].ID != want {
			t.Fatalf("page1[%d] = %s, want %s (full page1: %v)", i, page1.Messages[i].ID, want, idsOf(page1.Messages))
		}
	}
	page2, err := cached.mergeMessages(ctx, sp, 2, 4, timeDesc, gap)
	if err != nil {
		t.Fatal(err)
	}
	wantPage2 := []string{"c4", "c3", "c2", "c1"}
	for i, want := range wantPage2 {
		if page2.Messages[i].ID != want {
			t.Fatalf("page2[%d] = %s, want %s (full page2: %v)", i, page2.Messages[i].ID, want, idsOf(page2.Messages))
		}
	}

	timeAsc := stats.MessageSort{Field: stats.MessageSortTime, Direction: stats.MessageSortAsc}
	ascStraddle, err := cached.mergeMessages(ctx, sp, 2, 4, timeAsc, gap)
	if err != nil {
		t.Fatal(err)
	}
	wantAsc := []string{"c5", "g1", "g2", "g3"}
	for i, want := range wantAsc {
		if ascStraddle.Messages[i].ID != want {
			t.Fatalf("asc page2[%d] = %s, want %s (full: %v)", i, ascStraddle.Messages[i].ID, want, idsOf(ascStraddle.Messages))
		}
	}

	costDesc := stats.MessageSort{Field: stats.MessageSortCost, Direction: stats.MessageSortDesc}
	costPage, err := cached.mergeMessages(ctx, sp, 1, 3, costDesc, gap)
	if err != nil {
		t.Fatal(err)
	}
	wantCost := []string{"g3", "g1", "c1"} // 0.07, 0.06, 0.05
	for i, want := range wantCost {
		if costPage.Messages[i].ID != want {
			t.Fatalf("cost page[%d] = %s, want %s (full: %v)", i, costPage.Messages[i].ID, want, idsOf(costPage.Messages))
		}
	}
	costDeep, err := cached.mergeMessages(ctx, sp, 3, 3, costDesc, gap)
	if err != nil {
		t.Fatal(err)
	}
	wantDeep := []string{"c2", "g2"} // 0.01, 0.005 — the global tail
	if len(costDeep.Messages) != 2 {
		t.Fatalf("deep cost page = %v, want 2 entries", idsOf(costDeep.Messages))
	}
	for i, want := range wantDeep {
		if costDeep.Messages[i].ID != want {
			t.Fatalf("deep cost page[%d] = %s, want %s (full: %v)", i, costDeep.Messages[i].ID, want, idsOf(costDeep.Messages))
		}
	}
}

func idsOf(msgs []stats.MessageEntry) []string {
	ids := make([]string, 0, len(msgs))
	for _, m := range msgs {
		ids = append(ids, m.ID)
	}
	return ids
}

func TestMergeSessionsSpanningSession(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	cutoff := base.Add(-6 * time.Hour)
	_, cached, _ := seedMerge(t, []stats.MessageEntry{
		msgIn("a1", "s1", base.Add(-10*time.Hour), 0.01),
		msgIn("a2", "s1", base.Add(-9*time.Hour), 0.02),
		msgIn("b1", "s2", base.Add(-20*time.Hour), 0.04),
	}, cutoff)

	pq := stats.PeriodQuery{Period: "all"}
	sp, err := splitPeriod(pq, cutoff, base)
	if err != nil {
		t.Fatal(err)
	}
	query := stats.SessionQuery{Page: 1, PageSize: 20, Sort: stats.SessionSortNewest, Period: "all"}
	cacheQuery := query
	cacheQuery.ToTime = sp.cachePQ.ToTime
	gapMsg := msgIn("a3", "s1", base.Add(-2*time.Hour), 0.08)
	gapSessions := []stats.SessionEntry{{
		SourceID: syncFakeSourceID, ID: "s1", Title: "Session s1",
		TimeCreated: base.Add(-10 * time.Hour), TimeUpdated: base.Add(-2 * time.Hour),
		MessageCount: 3, Cost: 0.11, // whole-session rollups, must NOT be used directly
	}}
	merged, err := cached.mergeSessions(ctx, sp, query, cacheQuery, gapSessions, gapData{msgs: []stats.MessageEntry{gapMsg}})
	if err != nil {
		t.Fatal(err)
	}
	if merged.Total != 2 {
		t.Fatalf("Total = %d, want 2 (spanning session deduped)", merged.Total)
	}
	var s1 *stats.SessionEntry
	for i := range merged.Sessions {
		if merged.Sessions[i].ID == "s1" {
			s1 = &merged.Sessions[i]
		}
	}
	if s1 == nil {
		t.Fatalf("s1 missing from merged list: %+v", merged.Sessions)
	}
	if s1.MessageCount != 3 {
		t.Fatalf("s1 MessageCount = %d, want 3 (2 cached + 1 gap-scoped)", s1.MessageCount)
	}
	if diff := s1.Cost - 0.11; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("s1 Cost = %f, want 0.11", s1.Cost)
	}
	if !s1.TimeUpdated.Equal(base.Add(-2 * time.Hour)) {
		t.Fatalf("s1 TimeUpdated = %s, want gap activity time", s1.TimeUpdated)
	}
}

func TestMergeModelsDedupesSpanningSession(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	cutoff := base.Add(-6 * time.Hour)
	_, cached, _ := seedMerge(t, []stats.MessageEntry{
		msgIn("a", "s1", base.Add(-10*time.Hour), 0.01),
	}, cutoff)

	sp, err := splitPeriod(stats.PeriodQuery{Period: "all"}, cutoff, base)
	if err != nil {
		t.Fatal(err)
	}
	c, err := cached.store.Models(ctx, syncFakeSourceID, sp.cachePQ)
	if err != nil {
		t.Fatal(err)
	}
	gap := gapData{msgs: []stats.MessageEntry{
		msgIn("b", "s1", base.Add(-2*time.Hour), 0.02),
	}}
	merged, err := cached.mergeModels(ctx, sp, c, gap)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Models) != 1 {
		t.Fatalf("models = %d entries, want 1", len(merged.Models))
	}
	m := merged.Models[0]
	if m.Messages != 2 || m.Sessions != 1 {
		t.Fatalf("model rollup = %d msgs / %d sessions, want 2/1 (session deduped)", m.Messages, m.Sessions)
	}
	if m.AvgTokensPerMessage == nil || m.AvgTokensPerMessage.Input != 10 {
		t.Fatalf("averages not recomputed from merged totals: %+v", m.AvgTokensPerMessage)
	}
}

func TestMergeOverviewEmptyGapEqualsCache(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	cutoff := base.Add(-6 * time.Hour)
	_, cached, _ := seedMerge(t, []stats.MessageEntry{
		msgIn("a", "s1", base.Add(-10*time.Hour), 0.01),
	}, cutoff)
	sp, err := splitPeriod(stats.PeriodQuery{Period: "all"}, cutoff, base)
	if err != nil {
		t.Fatal(err)
	}
	c, err := cached.store.Overview(ctx, syncFakeSourceID, sp.cachePQ)
	if err != nil {
		t.Fatal(err)
	}
	merged, err := cached.mergeOverview(ctx, sp, c, gapData{})
	if err != nil {
		t.Fatal(err)
	}
	if merged != c {
		t.Fatalf("empty gap merge = %+v, want unchanged cache result %+v", merged, c)
	}
}
