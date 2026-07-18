package stats

import (
	"context"
	"os"
	"testing"

	"opencode-dashboard/internal/store"
)

// BenchmarkModelsLargeDB is opt-in so normal test runs never depend on a
// developer's OpenCode database. It provides a repeatable check against the
// large-database path that motivated the windowed model aggregation.
func BenchmarkModelsLargeDB(b *testing.B) {
	path := os.Getenv("OPENCODE_BENCH_DB")
	if path == "" {
		b.Skip("set OPENCODE_BENCH_DB to benchmark a real OpenCode database")
	}

	st, err := store.Connect(context.Background(), path)
	if err != nil {
		b.Fatal(err)
	}
	defer st.Close()

	period := os.Getenv("OPENCODE_BENCH_PERIOD")
	if period == "" {
		period = "30d"
	}
	pq := PeriodQuery{Period: period}

	b.ResetTimer()
	for range b.N {
		result, err := Models(context.Background(), st, pq)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(len(result.Models)), "models/op")
	}
}
