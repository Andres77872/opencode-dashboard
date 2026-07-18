package stats

import (
	"context"
	"os"
	"testing"

	"opencode-dashboard/internal/store"
)

// BenchmarkOverviewLargeDB is opt-in because it is intended for measuring the
// real, read-only OpenCode database that motivated the overview/cache work.
// Example:
//
//	OPENCODE_BENCH_DB=/path/to/opencode.db go test ./internal/stats \
//	  -run '^$' -bench '^BenchmarkOverviewLargeDB$' -benchtime=1x
func BenchmarkOverviewLargeDB(b *testing.B) {
	st, pq := openOverviewBenchmarkStore(b)
	defer st.Close()

	b.ResetTimer()
	for range b.N {
		result, err := Overview(context.Background(), st, pq)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(result.Messages), "messages/op")
	}
}

// BenchmarkDailyModelDimensionLargeDB covers the extra query issued only after
// the Overview Usage grouping is switched to Model.
func BenchmarkDailyModelDimensionLargeDB(b *testing.B) {
	st, pq := openOverviewBenchmarkStore(b)
	defer st.Close()

	b.ResetTimer()
	for range b.N {
		result, err := DailyDimension(context.Background(), st, "model", pq)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(len(result.Days)), "rows/op")
	}
}

func openOverviewBenchmarkStore(b *testing.B) (*store.Store, PeriodQuery) {
	b.Helper()
	path := os.Getenv("OPENCODE_BENCH_DB")
	if path == "" {
		b.Skip("set OPENCODE_BENCH_DB to benchmark a real OpenCode database")
	}
	st, err := store.Connect(context.Background(), path)
	if err != nil {
		b.Fatal(err)
	}
	period := os.Getenv("OPENCODE_BENCH_PERIOD")
	if period == "" {
		period = "30d"
	}
	return st, PeriodQuery{Period: period}
}
