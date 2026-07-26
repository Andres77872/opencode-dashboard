package cache

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

const rollupTestSourceID source.SourceID = "rollup_test"

func openRollupTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "usage-cache.sqlite"))
	if err != nil {
		t.Fatalf("Open() failed: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func rollupMessage(id, sessionID string, at time.Time, cost float64, input int64, modelID, providerID string, status stats.CostStatus) messageRow {
	entry := stats.MessageEntry{
		ID:          id,
		SessionID:   sessionID,
		Role:        "assistant",
		TimeCreated: at,
		Cost:        cost,
		Tokens:      &stats.TokenStats{Input: input},
		ModelID:     modelID,
		ProviderID:  providerID,
		CostStatus:  status,
	}
	return messageRow{Entry: entry, ProjectID: "project", ProjectName: "Project"}
}

func rollupUserMessage(id, sessionID string, at time.Time) messageRow {
	return messageRow{
		Entry:     stats.MessageEntry{ID: id, SessionID: sessionID, Role: "user", TimeCreated: at},
		ProjectID: "project", ProjectName: "Project",
	}
}

func accountedRollupMessage(id, sessionID string, at time.Time, modelID string, trace stats.RequestTrace, usage stats.UsageStatus) messageRow {
	row := rollupMessage(id, sessionID, at, 1, 1, modelID, "provider", stats.CostReported)
	row.Entry.RequestTrace = trace
	row.Entry.UsageStatus = usage
	if usage == stats.UsageStatusUnavailable {
		row.Entry.Cost = 0
		row.Entry.Tokens = nil
	}
	return row
}

func rollupPayload(messages []messageRow) sourcePayload {
	sessions := map[string]time.Time{}
	for _, message := range messages {
		if current, ok := sessions[message.Entry.SessionID]; !ok || message.Entry.TimeCreated.Before(current) {
			sessions[message.Entry.SessionID] = message.Entry.TimeCreated
		}
	}
	rows := make([]sessionRow, 0, len(sessions))
	for id, created := range sessions {
		rows = append(rows, sessionRow{
			SessionID: id, Title: id, ProjectID: "project", ProjectName: "Project",
			TimeCreated: created, TimeUpdated: created, Status: stats.CostReported,
		})
	}
	return sourcePayload{
		Info:     source.SourceInfo{ID: rollupTestSourceID, Label: "Rollup test", Kind: "test", Available: true},
		Projects: []projectRow{{ProjectID: "project", ProjectName: "Project"}},
		Sessions: rows,
		Messages: messages,
	}
}

func TestOverviewAndModelRollupsKeepPartialEdgesExact(t *testing.T) {
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
	overview, err := store.Overview(ctx, string(rollupTestSourceID), pq)
	if err != nil {
		t.Fatalf("Overview() failed: %v", err)
	}
	if overview.Sessions != 3 || overview.Messages != 5 || overview.Cost != 14 || overview.Tokens.Input != 14 || overview.Days != 1 {
		t.Errorf("partial-edge overview = %#v, want 3 sessions / 5 messages / $14 / 14 input / 1 day", overview)
	}
	if overview.CostStatus != stats.CostMixed || overview.CostProvenance == nil ||
		overview.CostProvenance.ReportedCount != 2 || overview.CostProvenance.ComputedCount != 1 || overview.CostProvenance.MissingCount != 1 {
		t.Errorf("partial-edge cost provenance = %q/%#v, want mixed 2 reported/1 computed/1 missing", overview.CostStatus, overview.CostProvenance)
	}

	models, err := store.Models(ctx, string(rollupTestSourceID), pq)
	if err != nil {
		t.Fatalf("Models() failed: %v", err)
	}
	if len(models.Models) != 2 {
		t.Fatalf("Models() rows = %#v, want two", models.Models)
	}
	byModel := map[string]stats.ModelEntry{}
	for _, model := range models.Models {
		byModel[model.ModelID] = model
	}
	if got := byModel["m1"]; got.Sessions != 2 || got.Messages != 2 || got.Cost != 5 || got.Tokens.Input != 5 || got.CostStatus != stats.CostMixed {
		t.Errorf("m1 = %#v, want 2 sessions/messages, $5, 5 input, mixed cost", got)
	}
	if got := byModel["m2"]; got.Sessions != 1 || got.Messages != 2 || got.Cost != 9 || got.Tokens.Input != 9 || got.CostStatus != stats.CostMixed {
		t.Errorf("m2 = %#v, want 1 session, 2 messages, $9, 9 input, mixed cost", got)
	}

	trend, err := store.DailyDimension(ctx, string(rollupTestSourceID), "model", pq)
	if err != nil {
		t.Fatalf("DailyDimension(model) failed: %v", err)
	}
	if len(trend.Days) != 2 {
		t.Fatalf("model trend = %#v, want two rows", trend.Days)
	}
	for _, day := range trend.Days {
		if day.Dimension == "m1" && (day.Sessions != 2 || day.Messages != 2 || day.Cost != 5) {
			t.Errorf("m1 trend = %#v, want 2 sessions/messages and $5", day)
		}
		if day.Dimension == "m2" && (day.Sessions != 1 || day.Messages != 2 || day.Cost != 9) {
			t.Errorf("m2 trend = %#v, want 1 session, 2 messages and $9", day)
		}
	}
}

func TestRequestAccountingSurvivesHourlyRollupsAndDetailReads(t *testing.T) {
	ctx := context.Background()
	store := openRollupTestStore(t)
	base := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	unavailable := accountedRollupMessage("unavailable", "s3", base.Add(130*time.Minute), "m2", stats.RequestTraceObserved, stats.UsageStatusUnavailable)
	unavailable.Entry.UsageUnavailableReason = stats.UsageUnavailableInterrupted
	payload := rollupPayload([]messageRow{
		rollupUserMessage("prompt", "s1", base.Add(5*time.Minute)),
		accountedRollupMessage("recorded-observed", "s1", base.Add(10*time.Minute), "m1", stats.RequestTraceObserved, stats.UsageStatusRecorded),
		accountedRollupMessage("recorded-inferred", "s1", base.Add(20*time.Minute), "m1", stats.RequestTraceInferred, stats.UsageStatusRecorded),
		accountedRollupMessage("recovered", "s2", base.Add(70*time.Minute), "m2", stats.RequestTraceObserved, stats.UsageStatusRecovered),
		unavailable,
	})
	if err := store.replaceSource(ctx, payload, base.Add(4*time.Hour)); err != nil {
		t.Fatalf("replaceSource() failed: %v", err)
	}

	pq := stats.PeriodQuery{FromTime: base, ToTime: base.Add(4 * time.Hour)}
	overview, err := store.Overview(ctx, string(rollupTestSourceID), pq)
	if err != nil {
		t.Fatalf("Overview() failed: %v", err)
	}
	assertRequestAccounting(t, "overview", overview.Requests, overview.RequestAccounting, 2, 1, 1, stats.TraceCoverageMixed)
	if overview.RequestAccounting.UsageUnavailableReasons.Interrupted != 1 {
		t.Errorf("overview unavailable reasons = %#v, want one interrupted", overview.RequestAccounting.UsageUnavailableReasons)
	}
	if overview.Messages != 5 {
		t.Errorf("overview messages = %d, want transcript count 5", overview.Messages)
	}

	daily, err := store.Daily(ctx, string(rollupTestSourceID), pq)
	if err != nil {
		t.Fatalf("Daily() failed: %v", err)
	}
	if len(daily.Days) != 1 {
		t.Fatalf("daily rows = %#v, want one day", daily.Days)
	}
	assertRequestAccounting(t, "daily row", daily.Days[0].Requests, daily.Days[0].RequestAccounting, 2, 1, 1, stats.TraceCoverageMixed)
	assertRequestAccounting(t, "daily total", overview.Requests, daily.RequestAccounting, 2, 1, 1, stats.TraceCoverageMixed)

	filtered, err := store.Overview(ctx, string(rollupTestSourceID), stats.PeriodQuery{
		FromTime: base, ToTime: base.Add(4 * time.Hour), Model: "m1",
	})
	if err != nil {
		t.Fatalf("filtered Overview() failed: %v", err)
	}
	assertRequestAccounting(t, "filtered overview", filtered.Requests, filtered.RequestAccounting, 2, 0, 0, stats.TraceCoverageMixed)
	if filtered.Messages != 2 {
		t.Errorf("filtered overview messages = %d, want two native assistant rows", filtered.Messages)
	}

	entry, err := store.MessageByID(ctx, string(rollupTestSourceID), "unavailable")
	if err != nil {
		t.Fatalf("MessageByID() failed: %v", err)
	}
	if entry == nil || entry.RequestTrace != stats.RequestTraceObserved ||
		entry.UsageStatus != stats.UsageStatusUnavailable ||
		entry.UsageUnavailableReason != stats.UsageUnavailableInterrupted ||
		entry.Tokens != nil {
		t.Errorf("cached unavailable request = %#v, want observed/unavailable with unknown tokens", entry)
	}
	detail, err := store.SessionByID(ctx, string(rollupTestSourceID), "s2")
	if err != nil {
		t.Fatalf("SessionByID() failed: %v", err)
	}
	if detail == nil || len(detail.Messages) != 1 || detail.Messages[0].RequestTrace != stats.RequestTraceObserved || detail.Messages[0].UsageStatus != stats.UsageStatusRecovered {
		t.Errorf("cached session request provenance = %#v, want observed/recovered", detail)
	}
}

func TestEmptyKimiCacheWindowReportsUnknownTraceCoverage(t *testing.T) {
	ctx := context.Background()
	store := openRollupTestStore(t)
	from := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	pq := stats.PeriodQuery{FromTime: from, ToTime: from.Add(time.Hour)}
	overview, err := store.Overview(ctx, "kimi_code", pq)
	if err != nil {
		t.Fatalf("Overview() failed: %v", err)
	}
	if overview.RequestAccounting == nil || overview.RequestAccounting.TraceCoverage != stats.TraceCoverageUnknown {
		t.Errorf("empty Kimi overview accounting = %#v, want unknown", overview.RequestAccounting)
	}
	daily, err := store.Daily(ctx, "kimi_code", pq, stats.GranularityHour)
	if err != nil {
		t.Fatalf("Daily() failed: %v", err)
	}
	if daily.RequestAccounting == nil || daily.RequestAccounting.TraceCoverage != stats.TraceCoverageUnknown {
		t.Errorf("empty Kimi daily accounting = %#v, want unknown", daily.RequestAccounting)
	}
}

func TestRequestProvenanceSurvivesCacheReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "usage-cache.sqlite")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() failed: %v", err)
	}
	base := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	request := accountedRollupMessage("legacy-request", "s1", base.Add(10*time.Minute), "m1", stats.RequestTraceInferred, stats.UsageStatusRecorded)
	request.Entry.CostStatus = stats.CostEstimatedAPIEquivalent
	request.Entry.CostProvenance = &stats.CostProvenance{
		Status: stats.CostEstimatedAPIEquivalent, Currency: "USD", ComputedCount: 1,
		PricingSnapshotID: "kimi-pricing-v-test", PricingSource: "https://example.test/kimi-pricing",
		Note: "Kimi API-equivalent test estimate",
	}
	payload := rollupPayload([]messageRow{request})
	payload.Info.ID = source.SourceKimiCode
	payload.Info.CostPolicy = source.CostPolicy{
		Status: string(stats.CostEstimatedAPIEquivalent), Currency: "USD",
		PricingSnapshotID: "kimi-pricing-v-test", PricingSource: "https://example.test/kimi-pricing",
		Note: "Kimi API-equivalent test estimate",
	}
	if err := store.replaceSource(ctx, payload, base.Add(time.Hour)); err != nil {
		t.Fatalf("replaceSource() failed: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	store, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen cache: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	pq := stats.PeriodQuery{FromTime: base, ToTime: base.Add(time.Hour)}
	overview, err := store.Overview(ctx, string(source.SourceKimiCode), pq)
	if err != nil {
		t.Fatalf("Overview() after reopen failed: %v", err)
	}
	assertRequestAccounting(t, "reopened overview", overview.Requests, overview.RequestAccounting, 1, 0, 0, stats.TraceCoverageSuccessfulOnly)
	assertCachedPricingProvenance(t, "reopened overview", overview.CostProvenance)
	daily, err := store.Daily(ctx, string(source.SourceKimiCode), pq)
	if err != nil || len(daily.Days) != 1 {
		t.Fatalf("Daily() after reopen = %#v, %v", daily, err)
	}
	assertCachedPricingProvenance(t, "reopened daily total", daily.CostProvenance)
	assertCachedPricingProvenance(t, "reopened daily row", daily.Days[0].CostProvenance)
	models, err := store.Models(ctx, string(source.SourceKimiCode), pq)
	if err != nil || len(models.Models) != 1 {
		t.Fatalf("Models() after reopen = %#v, %v", models, err)
	}
	assertCachedPricingProvenance(t, "reopened model total", models.CostProvenance)
	assertCachedPricingProvenance(t, "reopened model row", models.Models[0].CostProvenance)
	entry, err := store.MessageByID(ctx, string(source.SourceKimiCode), "legacy-request")
	if err != nil {
		t.Fatalf("MessageByID() after reopen failed: %v", err)
	}
	if entry == nil || entry.RequestTrace != stats.RequestTraceInferred || entry.UsageStatus != stats.UsageStatusRecorded {
		t.Errorf("reopened message provenance = %#v, want inferred/recorded", entry)
	}
}

func assertCachedPricingProvenance(t *testing.T, label string, provenance *stats.CostProvenance) {
	t.Helper()
	if provenance == nil || provenance.PricingSnapshotID != "kimi-pricing-v-test" ||
		provenance.PricingSource != "https://example.test/kimi-pricing" ||
		provenance.Note != "Kimi API-equivalent test estimate" {
		t.Errorf("%s cost provenance = %#v, want pinned snapshot/source/note", label, provenance)
	}
}

func assertRequestAccounting(t *testing.T, label string, requests int64, accounting *stats.RequestAccounting, recorded, recovered, unavailable int64, coverage stats.TraceCoverage) {
	t.Helper()
	if requests != recorded+recovered+unavailable || accounting == nil ||
		accounting.UsageRecorded != recorded || accounting.UsageRecovered != recovered ||
		accounting.UsageUnavailable != unavailable || accounting.TraceCoverage != coverage {
		t.Errorf("%s request accounting = requests %d / %#v, want %d recorded, %d recovered, %d unavailable, %s", label, requests, accounting, recorded, recovered, unavailable, coverage)
	}
}

func TestAlignedOverviewAndModelsDoNotReadMessageIndex(t *testing.T) {
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
	// Removing the source-of-truth table in this isolated database makes this a
	// strong regression check: aligned reads can only succeed if they use the
	// hourly totals, session memberships, and provenance rollups exclusively.
	if _, err := store.db.ExecContext(ctx, `DROP TABLE message_index`); err != nil {
		t.Fatalf("drop message_index: %v", err)
	}
	pq := stats.PeriodQuery{FromTime: base.Add(time.Hour), ToTime: base.Add(3 * time.Hour)}
	overview, err := store.Overview(ctx, string(rollupTestSourceID), pq)
	if err != nil {
		t.Fatalf("aligned Overview() touched message_index: %v", err)
	}
	if overview.Sessions != 2 || overview.Messages != 2 || overview.Cost != 5 {
		t.Errorf("aligned overview = %#v, want 2 sessions/messages and $5", overview)
	}
	models, err := store.Models(ctx, string(rollupTestSourceID), pq)
	if err != nil {
		t.Fatalf("aligned Models() touched message_index: %v", err)
	}
	if len(models.Models) != 2 {
		t.Errorf("aligned models = %#v, want two rows", models.Models)
	}
	trend, err := store.DailyDimension(ctx, string(rollupTestSourceID), "model", pq)
	if err != nil {
		t.Fatalf("aligned DailyDimension(model) touched message_index: %v", err)
	}
	if len(trend.Days) != 2 {
		t.Errorf("aligned model trend = %#v, want two rows", trend.Days)
	}
}

func TestIncrementalFillRefreshesOnlyChangedSuffix(t *testing.T) {
	ctx := context.Background()
	store := openRollupTestStore(t)
	base := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	oldCutoff := base.Add(4 * time.Hour)
	initial := rollupPayload([]messageRow{
		rollupMessage("old", "untouched", base.Add(10*time.Minute), 1, 1, "old-model", "p", stats.CostReported),
		rollupMessage("span-old", "spanning", base.Add(130*time.Minute), 2, 2, "span-model", "p", stats.CostReported),
	})
	if err := store.replaceSource(ctx, initial, oldCutoff); err != nil {
		t.Fatalf("replaceSource() failed: %v", err)
	}

	// Protect every historical aggregate table. A former full rebuild would
	// fire one of these triggers during the incremental fill.
	for _, table := range []string{"hourly_usage", "overview_hourly", "overview_hourly_sessions", "overview_hourly_cost", "hourly_model_sessions", "hourly_model_cost"} {
		trigger := "protect_" + table
		if _, err := store.db.ExecContext(ctx, `CREATE TRIGGER `+trigger+` BEFORE DELETE ON `+table+`
			WHEN OLD.bucket_start_ms < `+itoa64(oldCutoff.UnixMilli())+`
			BEGIN SELECT RAISE(FAIL, 'historical aggregate deleted'); END`); err != nil {
			t.Fatalf("create %s trigger: %v", table, err)
		}
	}

	// Simulate one stale pre-v4 suffix row whose upstream message disappeared.
	if _, err := store.db.ExecContext(ctx, `
		INSERT INTO sessions(source_id, session_id, title, time_created_ms, time_updated_ms)
		VALUES (?, 'ghost', 'ghost', ?, ?);
		INSERT INTO message_index(source_id, message_id, session_id, role, time_created_ms)
		VALUES (?, 'ghost', 'ghost', 'assistant', ?)
	`, string(rollupTestSourceID), oldCutoff.UnixMilli(), oldCutoff.UnixMilli(), string(rollupTestSourceID), oldCutoff.Add(5*time.Minute).UnixMilli()); err != nil {
		t.Fatalf("seed stale suffix row: %v", err)
	}

	next := rollupPayload([]messageRow{
		rollupMessage("span-new", "spanning", oldCutoff.Add(10*time.Minute), 3, 3, "span-model", "p", stats.CostComputed),
		rollupMessage("new", "new", oldCutoff.Add(20*time.Minute), 4, 4, "new-model", "p", stats.CostReported),
	})
	if err := store.fillSource(ctx, next, oldCutoff, oldCutoff.Add(time.Hour)); err != nil {
		t.Fatalf("incremental fillSource() failed: %v", err)
	}

	var spanningCount int64
	if err := store.db.QueryRowContext(ctx, `SELECT message_count FROM sessions WHERE source_id = ? AND session_id = 'spanning'`, string(rollupTestSourceID)).Scan(&spanningCount); err != nil {
		t.Fatalf("read spanning session rollup: %v", err)
	}
	if spanningCount != 2 {
		t.Errorf("spanning message_count = %d, want 2 across historical + new suffix", spanningCount)
	}
	if got := queryInt(t, store.db, `SELECT COUNT(*) FROM sessions WHERE source_id = ? AND session_id = 'ghost'`, string(rollupTestSourceID)); got != 0 {
		t.Errorf("ghost session count = %d, want removed after its last stale message was deleted", got)
	}
	pq := stats.PeriodQuery{FromTime: base, ToTime: oldCutoff.Add(time.Hour)}
	overview, err := store.Overview(ctx, string(rollupTestSourceID), pq)
	if err != nil {
		t.Fatalf("Overview() after incremental fill: %v", err)
	}
	if overview.Sessions != 3 || overview.Messages != 4 || overview.Cost != 10 {
		t.Errorf("post-fill overview = %#v, want 3 sessions / 4 messages / $10", overview)
	}
}

func itoa64(v int64) string {
	return strconv.FormatInt(v, 10)
}

func TestModelTrendHourlyBucketsMatchRollups(t *testing.T) {
	ctx := context.Background()
	store := openRollupTestStore(t)
	base := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	payload := rollupPayload([]messageRow{
		rollupMessage("a", "s1", base.Add(70*time.Minute), 2, 2, "m1", "p1", stats.CostReported),
		rollupMessage("b", "s1", base.Add(95*time.Minute), 3, 3, "m1", "p1", stats.CostReported),
		rollupMessage("c", "s2", base.Add(130*time.Minute), 4, 4, "m2", "p2", stats.CostComputed),
	})
	if err := store.replaceSource(ctx, payload, base.Add(4*time.Hour)); err != nil {
		t.Fatalf("replaceSource() failed: %v", err)
	}

	pq := stats.PeriodQuery{FromTime: base, ToTime: base.Add(4 * time.Hour)}
	hourly, err := store.DailyDimension(ctx, string(rollupTestSourceID), "model", pq, stats.GranularityHour)
	if err != nil {
		t.Fatalf("DailyDimension(model, hour) failed: %v", err)
	}
	if hourly.Granularity != stats.GranularityHour {
		t.Errorf("hourly trend granularity = %q, want %q", hourly.Granularity, stats.GranularityHour)
	}
	if len(hourly.Days) != 2 {
		t.Fatalf("hourly model trend = %#v, want two rows", hourly.Days)
	}
	byKey := map[string]stats.DimensionDayStats{}
	for _, row := range hourly.Days {
		byKey[row.Date+" "+row.Dimension] = row
	}
	if got := byKey["2026-07-01T10:00:00Z m1"]; got.Messages != 2 || got.Cost != 5 || got.Sessions != 1 || got.Tokens.Input != 5 {
		t.Errorf("m1 hourly bucket = %#v, want 2 messages, $5, 1 session, 5 input", got)
	}
	if got := byKey["2026-07-01T11:00:00Z m2"]; got.Messages != 1 || got.Cost != 4 || got.Sessions != 1 || got.Tokens.Input != 4 {
		t.Errorf("m2 hourly bucket = %#v, want 1 message, $4, 1 session, 4 input", got)
	}

	daily, err := store.DailyDimension(ctx, string(rollupTestSourceID), "model", pq)
	if err != nil {
		t.Fatalf("DailyDimension(model, day) failed: %v", err)
	}
	if daily.Granularity != stats.GranularityDay {
		t.Errorf("daily trend granularity = %q, want %q", daily.Granularity, stats.GranularityDay)
	}
	dayTotals := map[string]int64{}
	for _, row := range daily.Days {
		dayTotals[row.Dimension] += row.Messages
	}
	hourTotals := map[string]int64{}
	for _, row := range hourly.Days {
		hourTotals[row.Dimension] += row.Messages
	}
	for dim, want := range dayTotals {
		if hourTotals[dim] != want {
			t.Errorf("%s hourly total = %d messages, want %d (same as daily)", dim, hourTotals[dim], want)
		}
	}
}
