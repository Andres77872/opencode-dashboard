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
		})
	}
}

func assertOverviewParity(t *testing.T, want, got stats.OverviewStats) {
	t.Helper()
	if want.Sessions != got.Sessions || want.Messages != got.Messages || want.Tokens != got.Tokens || want.Days != got.Days || !closeCost(want.Cost, got.Cost) {
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
			COUNT(DISTINCT session_id), COUNT(*), COALESCE(SUM(cost), 0),
			COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0), COALESCE(SUM(cache_read_tokens), 0),
			COALESCE(SUM(cache_write_tokens), 0),
			COUNT(DISTINCT DATE(time_created_ms / 1000, 'unixepoch'))
		FROM message_index
		WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?
	`, sourceID, startMs, endMs).Scan(
		&result.Sessions, &result.Messages, &result.Cost, &result.Tokens.Input,
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
