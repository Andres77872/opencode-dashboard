package cache

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"opencode-dashboard/internal/stats"
)

func TestMessageProcessingModeRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	created := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	want := stats.MessageEntry{
		ID:             "priority-request",
		SessionID:      "session-1",
		SessionTitle:   "Session session-1",
		Role:           "assistant",
		TimeCreated:    created,
		Cost:           1.25,
		Tokens:         &stats.TokenStats{Input: 100, Output: 20, Reasoning: 5, Cache: stats.CacheStats{Read: 50}},
		ModelID:        "gpt-test",
		ProviderID:     "openai",
		ServiceTier:    "priority",
		ProcessingMode: stats.ProcessingModeFast,
	}

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertSessions(ctx, tx, "codex", []sessionRow{{
		SessionID: "session-1", Title: want.SessionTitle, TimeCreated: created,
		TimeUpdated: created, MessageCount: 1, Cost: want.Cost,
	}}); err != nil {
		rollback(tx)
		t.Fatalf("insert session: %v", err)
	}
	if err := insertMessages(ctx, tx, "codex", []messageRow{{Entry: want}}); err != nil {
		rollback(tx)
		t.Fatalf("insert message: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	pq := stats.PeriodQuery{FromTime: created.Add(-time.Hour), ToTime: created.Add(time.Hour)}
	list, err := store.Messages(ctx, "codex", pq, 1, 10, syncSort)
	if err != nil {
		t.Fatalf("Messages(): %v", err)
	}
	if len(list.Messages) != 1 {
		t.Fatalf("Messages() len = %d, want 1", len(list.Messages))
	}
	assertMessageProcessingMode(t, list.Messages[0], "priority", stats.ProcessingModeFast)

	byID, err := store.MessageByID(ctx, "codex", want.ID)
	if err != nil || byID == nil {
		t.Fatalf("MessageByID() = %#v, %v", byID, err)
	}
	assertMessageProcessingMode(t, *byID, "priority", stats.ProcessingModeFast)

	slice, err := store.messagesSlice(ctx, "codex", pq, 0, 10, syncSort)
	if err != nil || len(slice) != 1 {
		t.Fatalf("messagesSlice() = %d rows, %v", len(slice), err)
	}
	assertMessageProcessingMode(t, slice[0], "priority", stats.ProcessingModeFast)

	session, err := store.SessionByID(ctx, "codex", "session-1")
	if err != nil || session == nil || len(session.Messages) != 1 {
		t.Fatalf("SessionByID() = %#v, %v", session, err)
	}
	if got := session.Messages[0]; got.ServiceTier != "priority" || got.ProcessingMode != stats.ProcessingModeFast {
		t.Errorf("session message tier/mode = %q/%q, want priority/fast", got.ServiceTier, got.ProcessingMode)
	}
	updated := want
	updated.ServiceTier = "default"
	updated.ProcessingMode = stats.ProcessingModeStandard
	insertMessageRows(t, store, "codex", []stats.MessageEntry{updated})
	byID, err = store.MessageByID(ctx, "codex", want.ID)
	if err != nil || byID == nil {
		t.Fatalf("MessageByID() after upsert = %#v, %v", byID, err)
	}
	assertMessageProcessingMode(t, *byID, "default", stats.ProcessingModeStandard)

	// Empty metadata from another source stays absent in storage. "unknown" is
	// a processing-dimension fallback, not metadata that the cache invents.
	other := want
	other.ID = "non-codex-request"
	other.ServiceTier = ""
	other.ProcessingMode = ""
	insertMessageRows(t, store, "claude", []stats.MessageEntry{other})
	var tier, mode sql.NullString
	if err := store.db.QueryRowContext(ctx, `
		SELECT service_tier, processing_mode
		FROM message_index WHERE source_id = 'claude' AND message_id = 'non-codex-request'
	`).Scan(&tier, &mode); err != nil {
		t.Fatalf("read non-Codex metadata: %v", err)
	}
	if tier.Valid || mode.Valid {
		t.Errorf("non-Codex stored metadata = tier %v / mode %v, want both NULL", tier, mode)
	}
	if _, err := store.DailyDimension(ctx, "claude", "processing_mode", pq); err == nil {
		t.Error("non-Codex DailyDimension(processing_mode) succeeded, want a deterministic source-specific error")
	}
	if gap := gapDimensionRows("claude", "processing_mode", []stats.MessageEntry{other}, nil); len(gap) != 0 {
		t.Errorf("non-Codex gap processing-mode buckets = %#v, want none", gap)
	}
}

func TestProcessingModeDailyDimensionMatchesGapAggregation(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	created := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	messages := []stats.MessageEntry{
		processingModeMessage("fast-1", "s1", "assistant", created, stats.ProcessingModeFast, 10, 2, 1),
		processingModeMessage("fast-2", "s2", "assistant", created.Add(time.Minute), stats.ProcessingModeFast, 20, 3, 2),
		processingModeMessage("standard", "s1", "assistant", created.Add(2*time.Minute), stats.ProcessingModeStandard, 30, 4, 3),
		processingModeMessage("unknown", "s3", "assistant", created.Add(3*time.Minute), "", 40, 5, 4),
		processingModeMessage("prompt", "s4", "user", created.Add(4*time.Minute), "", 1000, 1000, 100),
	}
	insertMessageRows(t, store, "codex", messages)

	pq := stats.PeriodQuery{FromTime: created.Add(-time.Hour), ToTime: created.Add(time.Hour)}
	cached, err := store.DailyDimension(ctx, "codex", "processing_mode", pq)
	if err != nil {
		t.Fatalf("DailyDimension(processing_mode): %v", err)
	}
	gap := gapDimensionRows("codex", "processing_mode", messages, nil)
	assertModeDimensionTotals(t, cached.Days, map[string]modeDimensionTotals{
		"fast":     {messages: 2, sessions: 2, cost: 3, input: 30, output: 5},
		"standard": {messages: 1, sessions: 1, cost: 3, input: 30, output: 4},
		"unknown":  {messages: 1, sessions: 1, cost: 4, input: 40, output: 5},
	})
	assertModeDimensionTotals(t, gap, modeTotalsFromRows(cached.Days))
	for _, rows := range [][]stats.DimensionDayStats{cached.Days, gap} {
		for _, row := range rows {
			if row.CostStatus != stats.CostEstimatedAPIEquivalent || row.CostProvenance == nil || row.CostProvenance.ComputedCount != row.Messages {
				t.Errorf("processing-mode row %q cost metadata = %q/%#v, want estimated provenance for %d messages", row.Dimension, row.CostStatus, row.CostProvenance, row.Messages)
			}
		}
	}
}

func TestProcessingModeDailyDimensionPreservesMissingRowCost(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	created := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	fast := processingModeMessage("fast", "s1", "assistant", created, stats.ProcessingModeFast, 10, 2, 0.25)
	flex := processingModeMessage("unsupported-flex", "s2", "assistant", created, stats.ProcessingModeFlex, 20, 3, 0)
	flex.CostStatus = stats.CostMissing
	flex.CostProvenance = &stats.CostProvenance{Status: stats.CostMissing, Currency: "USD", MissingCount: 1}
	insertMessageRows(t, store, "codex", []stats.MessageEntry{fast, flex})

	result, err := store.DailyDimension(ctx, "codex", "processing_mode", stats.PeriodQuery{
		FromTime: created.Add(-time.Hour),
		ToTime:   created.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("DailyDimension(processing_mode): %v", err)
	}
	rows := modeRowsByDimension(result.Days)
	if got := rows["fast"]; got.CostStatus != stats.CostEstimatedAPIEquivalent || got.CostProvenance == nil || got.CostProvenance.ComputedCount != 1 {
		t.Errorf("cached fast cost metadata = %q/%#v, want estimated/one computed", got.CostStatus, got.CostProvenance)
	}
	if got := rows["flex"]; got.Cost != 0 || got.CostStatus != stats.CostMissing || got.CostProvenance == nil || got.CostProvenance.MissingCount != 1 {
		t.Errorf("cached unsupported Flex row = %#v, want zero numeric cost with missing provenance", got)
	}
}

func TestProcessingModeDailyDimensionMergesCachedAndLiveGap(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Minute)
	cutoff := now.Add(-6 * time.Hour)
	messages := []stats.MessageEntry{
		processingModeMessage("cached-fast", "s1", "assistant", now.Add(-8*time.Hour), stats.ProcessingModeFast, 11, 1, 1),
		processingModeMessage("cached-flex", "s4", "assistant", now.Add(-7*time.Hour), stats.ProcessingModeFlex, 7, 1, 0),
		processingModeMessage("gap-fast", "s2", "assistant", now.Add(-2*time.Hour), stats.ProcessingModeFast, 13, 2, 2),
		processingModeMessage("gap-unknown", "s3", "assistant", now.Add(-time.Hour), "", 17, 3, 3),
		processingModeMessage("gap-prompt", "s4", "user", now.Add(-30*time.Minute), "", 1000, 1000, 100),
	}
	messages[1].CostStatus = stats.CostMissing
	messages[1].CostProvenance = &stats.CostProvenance{Status: stats.CostMissing, Currency: "USD", MissingCount: 1}
	src := &syncFakeSource{id: "codex", messages: messages}
	store := newTestStore(t)
	if _, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{Cutoff: cutoff}); err != nil {
		t.Fatalf("SyncSourceWithOptions(): %v", err)
	}
	cached := WrapSource(store, src)
	result, err := cached.DailyDimension(ctx, "processing_mode", stats.PeriodQuery{
		FromTime: now.Add(-12 * time.Hour),
		ToTime:   now,
	})
	if err != nil {
		t.Fatalf("cached DailyDimension(processing_mode): %v", err)
	}
	assertModeDimensionTotals(t, result.Days, map[string]modeDimensionTotals{
		"fast":    {messages: 2, cost: 3, input: 24, output: 3},
		"flex":    {messages: 1, cost: 0, input: 7, output: 1},
		"unknown": {messages: 1, cost: 3, input: 17, output: 3},
	})
	if got := modeRowsByDimension(result.Days)["flex"]; got.CostStatus != stats.CostMissing || got.CostProvenance == nil || got.CostProvenance.MissingCount != 1 {
		t.Errorf("cached/live merged Flex metadata = %q/%#v, want missing provenance retained", got.CostStatus, got.CostProvenance)
	}
	mergedMessages, err := cached.Messages(ctx, stats.PeriodQuery{
		FromTime: now.Add(-12 * time.Hour),
		ToTime:   now,
	}, 1, 10, syncSort)
	if err != nil {
		t.Fatalf("cached Messages() merge: %v", err)
	}
	byID := make(map[string]stats.MessageEntry, len(mergedMessages.Messages))
	for _, entry := range mergedMessages.Messages {
		byID[entry.ID] = entry
	}
	assertMessageProcessingMode(t, byID["cached-fast"], "priority", stats.ProcessingModeFast)
	assertMessageProcessingMode(t, byID["gap-fast"], "priority", stats.ProcessingModeFast)
}

func insertMessageRows(t *testing.T, store *Store, sourceID string, entries []stats.MessageEntry) {
	t.Helper()
	tx, err := store.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]messageRow, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, messageRow{Entry: entry})
	}
	if err := insertMessages(context.Background(), tx, sourceID, rows); err != nil {
		rollback(tx)
		t.Fatalf("insert messages: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func assertMessageProcessingMode(t *testing.T, got stats.MessageEntry, tier string, mode stats.ProcessingMode) {
	t.Helper()
	if got.ServiceTier != tier || got.ProcessingMode != mode {
		t.Errorf("message tier/mode = %q/%q, want %q/%q", got.ServiceTier, got.ProcessingMode, tier, mode)
	}
}

func processingModeMessage(id, sessionID, role string, created time.Time, mode stats.ProcessingMode, input, output int64, cost float64) stats.MessageEntry {
	tier := ""
	switch mode {
	case stats.ProcessingModeFast:
		tier = "priority"
	case stats.ProcessingModeStandard:
		tier = "default"
	}
	entry := stats.MessageEntry{
		ID: id, SessionID: sessionID, Role: role, TimeCreated: created,
		Cost: cost, Tokens: &stats.TokenStats{Input: input, Output: output},
		ServiceTier: tier, ProcessingMode: mode,
	}
	if role == "assistant" {
		entry.CostStatus = stats.CostEstimatedAPIEquivalent
		entry.CostProvenance = &stats.CostProvenance{Status: stats.CostEstimatedAPIEquivalent, Currency: "USD", ComputedCount: 1}
	}
	return entry
}

func modeRowsByDimension(rows []stats.DimensionDayStats) map[string]stats.DimensionDayStats {
	result := make(map[string]stats.DimensionDayStats, len(rows))
	for _, row := range rows {
		result[row.Dimension] = row
	}
	return result
}

type modeDimensionTotals struct {
	messages int64
	sessions int64
	cost     float64
	input    int64
	output   int64
}

func modeTotalsFromRows(rows []stats.DimensionDayStats) map[string]modeDimensionTotals {
	totals := make(map[string]modeDimensionTotals)
	for _, row := range rows {
		total := totals[row.Dimension]
		total.messages += row.Messages
		total.sessions += row.Sessions
		total.cost += row.Cost
		total.input += row.Tokens.Input
		total.output += row.Tokens.Output
		totals[row.Dimension] = total
	}
	return totals
}

func assertModeDimensionTotals(t *testing.T, rows []stats.DimensionDayStats, want map[string]modeDimensionTotals) {
	t.Helper()
	got := modeTotalsFromRows(rows)
	if len(got) != len(want) {
		t.Fatalf("processing-mode buckets = %#v, want %#v", got, want)
	}
	for mode, expected := range want {
		actual, ok := got[mode]
		if !ok {
			t.Errorf("processing-mode bucket %q missing from %#v", mode, got)
			continue
		}
		if expected.sessions == 0 {
			// Cache/live boundary merges intentionally sum per-day session counts;
			// tests that span that boundary assert message/token/cost parity only.
			actual.sessions = 0
		}
		if actual != expected {
			t.Errorf("processing-mode bucket %q = %#v, want %#v", mode, actual, expected)
		}
	}
}
