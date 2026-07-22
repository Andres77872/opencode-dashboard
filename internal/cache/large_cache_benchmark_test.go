package cache

import (
	"context"
	"math"
	"os"
	"testing"
	"time"

	"opencode-dashboard/internal/stats"
)

func TestLargeCacheRollupParity(t *testing.T) {
	path := os.Getenv("OPENCODE_DASHBOARD_CACHE_BENCH_DB")
	if path == "" {
		t.Skip("set OPENCODE_DASHBOARD_CACHE_BENCH_DB to a disposable cache copy")
	}
	ctx := context.Background()
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open(%s): %v", path, err)
	}
	t.Cleanup(func() { _ = store.Close() })
	var sourceID string
	if err := store.db.QueryRowContext(ctx, `
		SELECT source_id FROM source_state
		WHERE status = 'ready'
		ORDER BY source_id = 'opencode' DESC, source_id
		LIMIT 1
	`).Scan(&sourceID); err != nil {
		t.Fatalf("select parity source: %v", err)
	}
	for _, period := range []string{"30d", "all"} {
		t.Run(period, func(t *testing.T) {
			pq := stats.PeriodQuery{Period: period}
			legacyOverview, err := legacyCachedOverview(ctx, store, sourceID, pq)
			if err != nil {
				t.Fatal(err)
			}
			rollupOverview, err := store.Overview(ctx, sourceID, pq)
			if err != nil {
				t.Fatal(err)
			}
			assertOverviewParity(t, legacyOverview, rollupOverview)

			legacyModels, err := legacyCachedModels(ctx, store, sourceID, pq)
			if err != nil {
				t.Fatal(err)
			}
			rollupModels, err := store.Models(ctx, sourceID, pq)
			if err != nil {
				t.Fatal(err)
			}
			assertModelParity(t, legacyModels, rollupModels)

			w, err := store.periodWindow(ctx, sourceID, pq)
			if err != nil {
				t.Fatal(err)
			}
			startMs, endMs := w.ms()
			legacyTrend, err := legacyCachedModelTrend(ctx, store, sourceID, period, startMs, endMs)
			if err != nil {
				t.Fatal(err)
			}
			rollupTrend, err := store.DailyDimension(ctx, sourceID, "model", pq)
			if err != nil {
				t.Fatal(err)
			}
			assertModelTrendParity(t, legacyTrend, rollupTrend)

			legacyDaily, err := legacyCachedDaily(ctx, store, sourceID, pq)
			if err != nil {
				t.Fatal(err)
			}
			rollupDaily, err := store.Daily(ctx, sourceID, pq)
			if err != nil {
				t.Fatal(err)
			}
			assertDailyParity(t, legacyDaily, rollupDaily)

			sq := stats.SessionQuery{Period: period, PageSize: 100}
			legacySessions, err := legacyCachedSessions(ctx, store, sourceID, sq)
			if err != nil {
				t.Fatal(err)
			}
			newSessions, err := store.Sessions(ctx, sourceID, sq)
			if err != nil {
				t.Fatal(err)
			}
			assertSessionParity(t, legacySessions, newSessions)
		})
	}
}

func assertDailyParity(t *testing.T, want, got stats.DailyStats) {
	t.Helper()
	if len(want.Days) != len(got.Days) {
		t.Errorf("daily row count legacy=%d rollup=%d", len(want.Days), len(got.Days))
		return
	}
	for i, legacy := range want.Days {
		rollup := got.Days[i]
		if legacy.Date != rollup.Date || legacy.Sessions != rollup.Sessions || legacy.Messages != rollup.Messages || legacy.Requests != rollup.Requests || legacy.Tokens != rollup.Tokens || !closeCost(legacy.Cost, rollup.Cost) {
			t.Errorf("day %s mismatch\nlegacy: %#v\nrollup: %#v", legacy.Date, legacy, rollup)
		}
		assertCostParity(t, legacy.CostStatus, legacy.CostProvenance, rollup.CostStatus, rollup.CostProvenance)
	}
	assertCostParity(t, want.CostStatus, want.CostProvenance, got.CostStatus, got.CostProvenance)
}

// assertSessionParity compares by id (not order) because the legacy and new
// queries have no deterministic tiebreak for equal sort keys.
func assertSessionParity(t *testing.T, want, got stats.SessionList) {
	t.Helper()
	if want.Total != got.Total {
		t.Errorf("session total legacy=%d new=%d", want.Total, got.Total)
	}
	if len(want.Sessions) != len(got.Sessions) {
		t.Errorf("session row count legacy=%d new=%d", len(want.Sessions), len(got.Sessions))
	}
	byID := make(map[string]stats.SessionEntry, len(got.Sessions))
	for _, entry := range got.Sessions {
		byID[entry.ID] = entry
	}
	for _, legacy := range want.Sessions {
		entry, ok := byID[legacy.ID]
		if !ok {
			t.Errorf("new sessions missing %q", legacy.ID)
			continue
		}
		if legacy.MessageCount != entry.MessageCount || !closeCost(legacy.Cost, entry.Cost) || !legacy.TimeCreated.Equal(entry.TimeCreated) {
			t.Errorf("session %q mismatch\nlegacy: %#v\nnew: %#v", legacy.ID, legacy, entry)
		}
		assertCostParity(t, legacy.CostStatus, legacy.CostProvenance, entry.CostStatus, entry.CostProvenance)
	}
}

func assertOverviewParity(t *testing.T, want, got stats.OverviewStats) {
	t.Helper()
	if want.Sessions != got.Sessions || want.Messages != got.Messages || want.Requests != got.Requests || want.Tokens != got.Tokens || want.Days != got.Days || !closeCost(want.Cost, got.Cost) {
		t.Errorf("overview rollup mismatch\nlegacy: %#v\nrollup: %#v", want, got)
	}
	assertCostParity(t, want.CostStatus, want.CostProvenance, got.CostStatus, got.CostProvenance)
}

func assertModelParity(t *testing.T, want, got stats.ModelStats) {
	t.Helper()
	byKey := make(map[cachedModelKey]stats.ModelEntry, len(got.Models))
	for _, model := range got.Models {
		byKey[cachedModelKey{modelID: model.ModelID, providerID: model.ProviderID}] = model
	}
	if len(want.Models) != len(got.Models) {
		t.Errorf("model row count legacy=%d rollup=%d", len(want.Models), len(got.Models))
	}
	for _, legacy := range want.Models {
		key := cachedModelKey{modelID: legacy.ModelID, providerID: legacy.ProviderID}
		rollup, ok := byKey[key]
		if !ok {
			t.Errorf("rollup missing model %q/%q", legacy.ModelID, legacy.ProviderID)
			continue
		}
		if legacy.Sessions != rollup.Sessions || legacy.Messages != rollup.Messages || legacy.Tokens != rollup.Tokens || !closeCost(legacy.Cost, rollup.Cost) {
			t.Errorf("model %q/%q mismatch\nlegacy: %#v\nrollup: %#v", legacy.ModelID, legacy.ProviderID, legacy, rollup)
		}
		assertCostParity(t, legacy.CostStatus, legacy.CostProvenance, rollup.CostStatus, rollup.CostProvenance)
	}
	assertCostParity(t, want.CostStatus, want.CostProvenance, got.CostStatus, got.CostProvenance)
}

func assertModelTrendParity(t *testing.T, want, got stats.DailyDimensionStats) {
	t.Helper()
	type key struct{ date, model string }
	byKey := make(map[key]stats.DimensionDayStats, len(got.Days))
	for _, day := range got.Days {
		byKey[key{date: day.Date, model: day.Dimension}] = day
	}
	if len(want.Days) != len(got.Days) {
		t.Errorf("model trend row count legacy=%d rollup=%d", len(want.Days), len(got.Days))
	}
	for _, legacy := range want.Days {
		rollup, ok := byKey[key{date: legacy.Date, model: legacy.Dimension}]
		if !ok {
			t.Errorf("rollup missing model trend %s/%s", legacy.Date, legacy.Dimension)
			continue
		}
		if legacy.Sessions != rollup.Sessions || legacy.Messages != rollup.Messages || legacy.Tokens != rollup.Tokens || !closeCost(legacy.Cost, rollup.Cost) {
			t.Errorf("model trend %s/%s mismatch\nlegacy: %#v\nrollup: %#v", legacy.Date, legacy.Dimension, legacy, rollup)
		}
		assertCostParity(t, legacy.CostStatus, legacy.CostProvenance, rollup.CostStatus, rollup.CostProvenance)
	}
	assertCostParity(t, want.CostStatus, want.CostProvenance, got.CostStatus, got.CostProvenance)
}

func assertCostParity(t *testing.T, wantStatus stats.CostStatus, want *stats.CostProvenance, gotStatus stats.CostStatus, got *stats.CostProvenance) {
	t.Helper()
	if wantStatus != gotStatus {
		t.Errorf("cost status legacy=%q rollup=%q", wantStatus, gotStatus)
		return
	}
	if want == nil || got == nil {
		if want != got {
			t.Errorf("cost provenance legacy=%#v rollup=%#v", want, got)
		}
		return
	}
	if want.ReportedCount != got.ReportedCount || want.ComputedCount != got.ComputedCount || want.MissingCount != got.MissingCount {
		t.Errorf("cost provenance legacy=%#v rollup=%#v", want, got)
	}
}

func closeCost(a, b float64) bool {
	scale := math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
	return math.Abs(a-b) <= scale*1e-10
}

// BenchmarkLargeCacheRollups is opt-in because it migrates the database path
// supplied in OPENCODE_DASHBOARD_CACHE_BENCH_DB. Always point it at a disposable
// copy. Run with -benchtime=1x to compare one cold-ish legacy scan with one
// rollup read on a production-sized cache.
func BenchmarkLargeCacheRollups(b *testing.B) {
	path := os.Getenv("OPENCODE_DASHBOARD_CACHE_BENCH_DB")
	if path == "" {
		b.Skip("set OPENCODE_DASHBOARD_CACHE_BENCH_DB to a disposable cache copy")
	}
	ctx := context.Background()
	migrationStart := time.Now()
	store, err := Open(ctx, path)
	if err != nil {
		b.Fatalf("Open(%s): %v", path, err)
	}
	b.Cleanup(func() { _ = store.Close() })
	b.Logf("open + structural migration: %s", time.Since(migrationStart).Round(time.Millisecond))

	sourceID := os.Getenv("OPENCODE_DASHBOARD_CACHE_BENCH_SOURCE")
	if sourceID == "" {
		if err := store.db.QueryRowContext(ctx, `
			SELECT source_id FROM source_state
			WHERE status = 'ready'
			ORDER BY source_id = 'opencode' DESC, source_id
			LIMIT 1
		`).Scan(&sourceID); err != nil {
			b.Fatalf("select benchmark source: %v", err)
		}
	}
	b.Logf("source: %s", sourceID)

	for _, period := range []string{"30d", "all"} {
		pq := stats.PeriodQuery{Period: period}
		b.Run(period+"/overview_legacy", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := legacyCachedOverview(ctx, store, sourceID, pq); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(period+"/overview_rollup", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := store.Overview(ctx, sourceID, pq); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(period+"/models_legacy", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := legacyCachedModels(ctx, store, sourceID, pq); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(period+"/models_rollup", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := store.Models(ctx, sourceID, pq); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(period+"/model_trend_legacy", func(b *testing.B) {
			w, err := store.periodWindow(ctx, sourceID, pq)
			if err != nil {
				b.Fatal(err)
			}
			startMs, endMs := w.ms()
			for i := 0; i < b.N; i++ {
				if _, err := store.dailyMessageDimension(ctx, sourceID, "model", "COALESCE(model_id, '')", period, stats.GranularityDay, startMs, endMs); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(period+"/model_trend_rollup", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := store.DailyDimension(ctx, sourceID, "model", pq); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(period+"/daily_legacy", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := legacyCachedDaily(ctx, store, sourceID, pq); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(period+"/daily_rollup", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := store.Daily(ctx, sourceID, pq); err != nil {
					b.Fatal(err)
				}
			}
		})
		sq := stats.SessionQuery{Period: period, PageSize: 100}
		b.Run(period+"/sessions_legacy", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := legacyCachedSessions(ctx, store, sourceID, sq); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(period+"/sessions_new", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := store.Sessions(ctx, sourceID, sq); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func legacyCachedOverview(ctx context.Context, store *Store, sourceID string, pq stats.PeriodQuery) (stats.OverviewStats, error) {
	w, err := store.periodWindow(ctx, sourceID, pq)
	if err != nil {
		return stats.OverviewStats{}, err
	}
	startMs, endMs := w.ms()
	var result stats.OverviewStats
	err = store.db.QueryRowContext(ctx, `
		SELECT
			COUNT(DISTINCT session_id), COUNT(*),
			COALESCE(SUM(CASE WHEN role = 'assistant' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(cost), 0),
			COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0), COALESCE(SUM(cache_read_tokens), 0),
			COALESCE(SUM(cache_write_tokens), 0),
			COUNT(DISTINCT DATE(time_created_ms / 1000, 'unixepoch'))
		FROM message_index
		WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?
	`, sourceID, startMs, endMs).Scan(
		&result.Sessions, &result.Messages, &result.Requests, &result.Cost, &result.Tokens.Input,
		&result.Tokens.Output, &result.Tokens.Reasoning, &result.Tokens.Cache.Read,
		&result.Tokens.Cache.Write, &result.Days,
	)
	if err != nil {
		return result, err
	}
	result.CostStatus, result.CostProvenance = store.costSummary(ctx, sourceID, startMs, endMs)
	return result, nil
}

func legacyCachedModels(ctx context.Context, store *Store, sourceID string, pq stats.PeriodQuery) (stats.ModelStats, error) {
	w, err := store.periodWindow(ctx, sourceID, pq)
	if err != nil {
		return stats.ModelStats{}, err
	}
	startMs, endMs := w.ms()
	rows, err := store.db.QueryContext(ctx, `
		SELECT
			COALESCE(model_id, ''), COALESCE(provider_id, ''),
			COUNT(DISTINCT session_id), COUNT(*), COALESCE(SUM(cost), 0),
			COALESCE(SUM(model_input_tokens), 0), COALESCE(SUM(model_output_tokens), 0),
			COALESCE(SUM(model_reasoning_tokens), 0), COALESCE(SUM(model_cache_read_tokens), 0),
			COALESCE(SUM(model_cache_write_tokens), 0)
		FROM message_index
		WHERE source_id = ? AND role = 'assistant' AND COALESCE(model_id, '') != ''
		  AND time_created_ms >= ? AND time_created_ms < ?
		GROUP BY model_id, provider_id
	`, sourceID, startMs, endMs)
	if err != nil {
		return stats.ModelStats{}, err
	}
	defer rows.Close()
	models := make([]stats.ModelEntry, 0)
	for rows.Next() {
		var model stats.ModelEntry
		if err := rows.Scan(
			&model.ModelID, &model.ProviderID, &model.Sessions, &model.Messages,
			&model.Cost, &model.Tokens.Input, &model.Tokens.Output,
			&model.Tokens.Reasoning, &model.Tokens.Cache.Read, &model.Tokens.Cache.Write,
		); err != nil {
			return stats.ModelStats{}, err
		}
		models = append(models, model)
	}
	if err := rows.Err(); err != nil {
		return stats.ModelStats{}, err
	}
	if err := rows.Close(); err != nil {
		return stats.ModelStats{}, err
	}
	for i := range models {
		models[i].CostStatus, models[i].CostProvenance, err = legacyCostSummaryForModel(ctx, store, sourceID, models[i].ModelID, models[i].ProviderID, startMs, endMs)
		if err != nil {
			return stats.ModelStats{}, err
		}
	}
	status, provenance, err := legacyCostSummary(ctx, store, sourceID, startMs, endMs)
	return stats.ModelStats{SourceID: sourceID, Models: models, CostStatus: status, CostProvenance: provenance}, err
}

func legacyCachedModelTrend(ctx context.Context, store *Store, sourceID, period string, startMs, endMs int64) (stats.DailyDimensionStats, error) {
	rows, err := store.db.QueryContext(ctx, `
		SELECT
			DATE(time_created_ms / 1000, 'unixepoch') AS day,
			COALESCE(model_id, '') AS dim,
			COUNT(DISTINCT session_id), COUNT(*), COALESCE(SUM(cost), 0),
			COALESCE(SUM(model_input_tokens), 0),
			COALESCE(SUM(model_output_tokens), 0),
			COALESCE(SUM(model_reasoning_tokens), 0),
			COALESCE(SUM(model_cache_read_tokens), 0),
			COALESCE(SUM(model_cache_write_tokens), 0)
		FROM message_index
		WHERE source_id = ? AND role = 'assistant' AND COALESCE(model_id, '') != ''
		  AND time_created_ms >= ? AND time_created_ms < ?
		GROUP BY day, dim
		ORDER BY day ASC, COUNT(*) DESC
	`, sourceID, startMs, endMs)
	if err != nil {
		return stats.DailyDimensionStats{}, err
	}
	days, err := scanDimensionRows(rows, sourceID)
	if closeErr := rows.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return stats.DailyDimensionStats{}, err
	}
	if err := store.attachDimensionCostSummaries(ctx, sourceID, "COALESCE(model_id, '')", stats.GranularityDay, startMs, endMs, days); err != nil {
		return stats.DailyDimensionStats{}, err
	}
	status, provenance := store.costSummary(ctx, sourceID, startMs, endMs)
	return stats.DailyDimensionStats{
		SourceID: sourceID, Days: days, Dimension: "model", Period: period,
		CostStatus: status, CostProvenance: provenance,
	}, nil
}

// legacyCachedDaily is the pre-rollup dailyDay implementation: a per-message
// window scan plus one full-calendar-day cost summary per day. On cached data
// with day-aligned window starts (30d/all) its output matches the rollup path
// exactly, because the cache holds no rows at/after the finality cutoff.
func legacyCachedDaily(ctx context.Context, store *Store, sourceID string, pq stats.PeriodQuery) (stats.DailyStats, error) {
	w, err := store.periodWindow(ctx, sourceID, pq)
	if err != nil {
		return stats.DailyStats{}, err
	}
	startMs, endMs := w.ms()
	rows, err := store.db.QueryContext(ctx, `
		SELECT
			DATE(time_created_ms / 1000, 'unixepoch') AS day,
			COUNT(DISTINCT session_id),
			COUNT(*),
			COALESCE(SUM(CASE WHEN role = 'assistant' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(cost), 0),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0),
			COALESCE(SUM(cache_read_tokens), 0),
			COALESCE(SUM(cache_write_tokens), 0)
		FROM message_index
		WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?
		GROUP BY day
	`, sourceID, startMs, endMs)
	if err != nil {
		return stats.DailyStats{}, err
	}
	byDay := make(map[string]stats.DayStats)
	scanErr := func() error {
		defer rows.Close()
		for rows.Next() {
			var d stats.DayStats
			d.SourceID = sourceID
			var cacheRead, cacheWrite int64
			if err := rows.Scan(&d.Date, &d.Sessions, &d.Messages, &d.Requests, &d.Cost, &d.Tokens.Input, &d.Tokens.Output, &d.Tokens.Reasoning, &cacheRead, &cacheWrite); err != nil {
				return err
			}
			d.Tokens.Cache.Read = cacheRead
			d.Tokens.Cache.Write = cacheWrite
			byDay[d.Date] = d
		}
		return rows.Err()
	}()
	if scanErr != nil {
		return stats.DailyStats{}, scanErr
	}
	for key, d := range byDay {
		dayStart := dayStartMs(key)
		d.CostStatus, d.CostProvenance, err = legacyCostSummaryWhere(ctx, store, sourceID, dayStart, dayStart+24*int64(time.Hour/time.Millisecond), "", nil)
		if err != nil {
			return stats.DailyStats{}, err
		}
		byDay[key] = d
	}
	days := make([]stats.DayStats, 0)
	for t := utcDay(w.start); t.Before(w.end); t = t.AddDate(0, 0, 1) {
		key := t.Format("2006-01-02")
		if d, ok := byDay[key]; ok {
			days = append(days, d)
		} else {
			days = append(days, stats.DayStats{SourceID: sourceID, Date: key})
		}
	}
	status, provenance, err := legacyCostSummary(ctx, store, sourceID, startMs, endMs)
	if err != nil {
		return stats.DailyStats{}, err
	}
	return stats.DailyStats{SourceID: sourceID, Days: days, Granularity: stats.GranularityDay, CostStatus: status, CostProvenance: provenance}, nil
}

// legacyCachedSessions is the pre-rewrite Sessions implementation: sessions
// joined to per-message rows before grouping, plus one cost summary query per
// returned session.
func legacyCachedSessions(ctx context.Context, store *Store, sourceID string, query stats.SessionQuery) (stats.SessionList, error) {
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 {
		query.PageSize = 20
	}
	if query.PageSize > 100 {
		query.PageSize = 100
	}
	pq := stats.PeriodQuery{Period: query.Period, From: query.From, To: query.To, FromTime: query.FromTime, ToTime: query.ToTime}
	w, err := store.periodWindow(ctx, sourceID, pq)
	if err != nil {
		return stats.SessionList{}, err
	}
	startMs, endMs := w.ms()
	args := []any{sourceID, startMs, endMs}
	where := `m.source_id = ? AND m.time_created_ms >= ? AND m.time_created_ms < ?`
	if query.ProjectID != "" {
		where += ` AND ss.project_id = ?`
		args = append(args, query.ProjectID)
	}
	countQuery := `SELECT COUNT(*) FROM (SELECT ss.session_id FROM sessions ss JOIN message_index m ON m.source_id = ss.source_id AND m.session_id = ss.session_id WHERE ` + where + ` GROUP BY ss.session_id)`
	var total int64
	if err := store.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return stats.SessionList{}, err
	}
	order := "MIN(ss.time_created_ms) DESC"
	switch query.Sort {
	case stats.SessionSortOldest:
		order = "MIN(ss.time_created_ms) ASC"
	case stats.SessionSortCost:
		order = "SUM(m.cost) DESC, MIN(ss.time_created_ms) DESC"
	case stats.SessionSortMessages:
		order = "COUNT(m.message_id) DESC, MIN(ss.time_created_ms) DESC"
	}
	listQuery := `
		SELECT
			ss.session_id, ss.title, COALESCE(ss.project_id, ''), COALESCE(ss.project_name, ''),
			MIN(ss.time_created_ms), MAX(ss.time_updated_ms), COUNT(m.message_id), COALESCE(SUM(m.cost), 0)
		FROM sessions ss
		JOIN message_index m ON m.source_id = ss.source_id AND m.session_id = ss.session_id
		WHERE ` + where + `
		GROUP BY ss.session_id
		ORDER BY ` + order + `
		LIMIT ? OFFSET ?
	`
	listArgs := append(append([]any{}, args...), query.PageSize, (query.Page-1)*query.PageSize)
	rows, err := store.db.QueryContext(ctx, listQuery, listArgs...)
	if err != nil {
		return stats.SessionList{}, err
	}
	entries := make([]stats.SessionEntry, 0)
	scanErr := func() error {
		defer rows.Close()
		for rows.Next() {
			var entry stats.SessionEntry
			var createdMs, updatedMs int64
			entry.SourceID = sourceID
			if err := rows.Scan(&entry.ID, &entry.Title, &entry.ProjectID, &entry.ProjectName, &createdMs, &updatedMs, &entry.MessageCount, &entry.Cost); err != nil {
				return err
			}
			entry.TimeCreated = time.UnixMilli(createdMs).UTC()
			entry.TimeUpdated = time.UnixMilli(updatedMs).UTC()
			entries = append(entries, entry)
		}
		return rows.Err()
	}()
	if scanErr != nil {
		return stats.SessionList{}, scanErr
	}
	for i := range entries {
		entries[i].CostStatus, entries[i].CostProvenance, err = legacyCostSummaryWhere(ctx, store, sourceID, startMs, endMs, "AND session_id = ?", []any{entries[i].ID})
		if err != nil {
			return stats.SessionList{}, err
		}
	}
	return stats.SessionList{SourceID: sourceID, Sessions: entries, Total: total, Page: query.Page, PageSize: query.PageSize}, nil
}

func legacyCostSummaryForModel(ctx context.Context, store *Store, sourceID, modelID, providerID string, startMs, endMs int64) (stats.CostStatus, *stats.CostProvenance, error) {
	return legacyCostSummaryWhere(ctx, store, sourceID, startMs, endMs, "AND COALESCE(model_id, '') = ? AND COALESCE(provider_id, '') = ?", []any{modelID, providerID})
}

func legacyCostSummary(ctx context.Context, store *Store, sourceID string, startMs, endMs int64) (stats.CostStatus, *stats.CostProvenance, error) {
	return legacyCostSummaryWhere(ctx, store, sourceID, startMs, endMs, "", nil)
}

func legacyCostSummaryWhere(ctx context.Context, store *Store, sourceID string, startMs, endMs int64, extra string, extraArgs []any) (stats.CostStatus, *stats.CostProvenance, error) {
	query := `
		SELECT COALESCE(cost_status, ''), COUNT(*)
		FROM message_index
		WHERE source_id = ? AND role = 'assistant' AND time_created_ms >= ? AND time_created_ms < ? ` + extra + `
		GROUP BY COALESCE(cost_status, '')
	`
	args := append([]any{sourceID, startMs, endMs}, extraArgs...)
	rows, err := store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	var counts costCounts
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return "", nil, err
		}
		counts.add(stats.CostStatus(status), count)
	}
	if err := rows.Err(); err != nil {
		return "", nil, err
	}
	status, provenance := counts.result()
	return status, provenance, nil
}
