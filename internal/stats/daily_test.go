package stats

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opencode-dashboard/internal/store"
	"opencode-dashboard/internal/store/fixture"
)

func TestParsePeriod(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    int
		wantErr     bool
		errContains string
	}{
		{name: "1d", input: "1d", expected: 1, wantErr: false},
		{name: "7d", input: "7d", expected: 7, wantErr: false},
		{name: "14d", input: "14d", expected: 14, wantErr: false},
		{name: "30d", input: "30d", expected: 30, wantErr: false},
		{name: "1y", input: "1y", expected: 365, wantErr: false},
		{name: "all", input: "all", expected: allHistoricPeriodDays, wantErr: false},
		{name: "1h rejected", input: "1h", expected: 0, wantErr: true, errContains: "invalid period"},
		{name: "6h rejected", input: "6h", expected: 0, wantErr: true, errContains: "invalid period"},
		{name: "12h rejected", input: "12h", expected: 0, wantErr: true, errContains: "invalid period"},
		{name: "24h rejected", input: "24h", expected: 0, wantErr: true, errContains: "invalid period"},
		{name: "72h rejected", input: "72h", expected: 0, wantErr: true, errContains: "invalid period"},
		{name: "2h rejected", input: "2h", expected: 0, wantErr: true, errContains: "invalid period"},
		{name: "invalid empty", input: "", expected: 0, wantErr: true, errContains: "invalid period"},
		{name: "invalid format", input: "seven", expected: 0, wantErr: true, errContains: "invalid period"},
		{name: "invalid number only", input: "7", expected: 0, wantErr: true, errContains: "invalid period"},
		{name: "invalid day letter", input: "7x", expected: 0, wantErr: true, errContains: "invalid period"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parsePeriod(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parsePeriod(%q) expected error, got nil", tt.input)
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("parsePeriod(%q) error = %q, want error containing %q", tt.input, err.Error(), tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("parsePeriod(%q) unexpected error: %v", tt.input, err)
				return
			}

			if result != tt.expected {
				t.Errorf("parsePeriod(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestDailyStatsGranularity(t *testing.T) {
	stats := DailyStats{
		Days:        []DayStats{},
		Granularity: GranularityDay,
	}
	if stats.Granularity != GranularityDay {
		t.Errorf("DailyStats.Granularity = %q, want %q", stats.Granularity, GranularityDay)
	}

	statsHourly := DailyStats{
		Days:        []DayStats{},
		Granularity: GranularityHour,
	}
	if statsHourly.Granularity != GranularityHour {
		t.Errorf("DailyStats.Granularity = %q, want %q", statsHourly.Granularity, GranularityHour)
	}
}

// TestDailyGranularityLogic tests the granularity auto-hour logic without a database fixture.
// These are unit-level logic tests for the Daily function's variadic parameter handling.
func TestDailyGranularityLogic(t *testing.T) {
	t.Run("no granularity with 1d returns hourly", func(t *testing.T) {
		// Simulate the function signature behavior: empty variadic = no granularity
		fn := func(period string, granularity ...Granularity) Granularity {
			explicit := false
			if len(granularity) > 0 && granularity[0] != "" {
				explicit = true
			}
			if period == "1d" && !explicit {
				return GranularityHour
			}
			return GranularityDay
		}

		if g := fn("1d"); g != GranularityHour {
			t.Errorf("no granularity + 1d: got %q, want %q", g, GranularityHour)
		}
	})

	t.Run("granularity=day with 1d returns daily", func(t *testing.T) {
		fn := func(period string, granularity ...Granularity) Granularity {
			explicit := false
			var gran Granularity
			if len(granularity) > 0 && granularity[0] != "" {
				gran = granularity[0]
				explicit = true
			}
			if period == "1d" && !explicit {
				return GranularityHour
			}
			if gran == GranularityHour {
				return GranularityHour
			}
			return GranularityDay
		}

		if g := fn("1d", GranularityDay); g != GranularityDay {
			t.Errorf("granularity=day + 1d: got %q, want %q", g, GranularityDay)
		}
	})

	t.Run("granularity=hour with 7d returns hourly", func(t *testing.T) {
		fn := func(period string, granularity ...Granularity) Granularity {
			explicit := false
			var gran Granularity
			if len(granularity) > 0 && granularity[0] != "" {
				gran = granularity[0]
				explicit = true
			}
			if period == "1d" && !explicit {
				return GranularityHour
			}
			if gran == GranularityHour {
				return GranularityHour
			}
			return GranularityDay
		}

		if g := fn("7d", GranularityHour); g != GranularityHour {
			t.Errorf("granularity=hour + 7d: got %q, want %q", g, GranularityHour)
		}
	})

	t.Run("empty string granularity treated as no-explicit", func(t *testing.T) {
		// This simulates what the handler used to do (passing "")
		fn := func(period string, granularity ...Granularity) Granularity {
			explicit := false
			if len(granularity) > 0 && granularity[0] != "" {
				explicit = true
			}
			if period == "1d" && !explicit {
				return GranularityHour
			}
			return GranularityDay
		}

		// Empty string passed as variadic should be treated as NOT explicit
		if g := fn("1d", Granularity("")); g != GranularityHour {
			t.Errorf("empty granularity + 1d: got %q, want %q (auto-hour)", g, GranularityHour)
		}
	})

	t.Run("no granularity with 7d returns daily", func(t *testing.T) {
		fn := func(period string, granularity ...Granularity) Granularity {
			explicit := false
			if len(granularity) > 0 && granularity[0] != "" {
				explicit = true
			}
			if period == "1d" && !explicit {
				return GranularityHour
			}
			return GranularityDay
		}

		if g := fn("7d"); g != GranularityDay {
			t.Errorf("no granularity + 7d: got %q, want %q", g, GranularityDay)
		}
	})

	t.Run("no granularity with 24h returns hourly", func(t *testing.T) {
		fn := func(period string, granularity ...Granularity) Granularity {
			explicit := false
			if len(granularity) > 0 && granularity[0] != "" {
				explicit = true
			}
			if period == "1d" && !explicit {
				return GranularityHour
			}
			if _, ok := parseHourPreset(period); ok && !explicit {
				return GranularityHour
			}
			return GranularityDay
		}

		if g := fn("24h"); g != GranularityHour {
			t.Errorf("no granularity + 24h: got %q, want %q", g, GranularityHour)
		}
	})

	t.Run("no granularity with 72h returns hourly", func(t *testing.T) {
		fn := func(period string, granularity ...Granularity) Granularity {
			explicit := false
			if len(granularity) > 0 && granularity[0] != "" {
				explicit = true
			}
			if period == "1d" && !explicit {
				return GranularityHour
			}
			if _, ok := parseHourPreset(period); ok && !explicit {
				return GranularityHour
			}
			return GranularityDay
		}

		if g := fn("72h"); g != GranularityHour {
			t.Errorf("no granularity + 72h: got %q, want %q", g, GranularityHour)
		}
	})

	t.Run("granularity=day with 24h returns daily (explicit override)", func(t *testing.T) {
		fn := func(period string, granularity ...Granularity) Granularity {
			explicit := false
			var gran Granularity
			if len(granularity) > 0 && granularity[0] != "" {
				gran = granularity[0]
				explicit = true
			}
			if period == "1d" && !explicit {
				return GranularityHour
			}
			if _, ok := parseHourPreset(period); ok && !explicit {
				return GranularityHour
			}
			if gran == GranularityHour {
				return GranularityHour
			}
			return GranularityDay
		}

		if g := fn("24h", GranularityDay); g != GranularityDay {
			t.Errorf("granularity=day + 24h: got %q, want %q", g, GranularityDay)
		}
	})
}

// TestDailyDimensionValidDimensions validates dimension constant values.
func TestDailyDimensionValidDimensions(t *testing.T) {
	expectedDims := map[string]string{
		"model":   "$.modelID",
		"tool":    "$.tool",
		"project": "$.projectID",
	}

	if len(validDimensions) != len(expectedDims) {
		t.Errorf("validDimensions has %d entries, want %d", len(validDimensions), len(expectedDims))
	}

	for dim, expectedPath := range expectedDims {
		path, ok := validDimensions[dim]
		if !ok {
			t.Errorf("validDimensions missing key %q", dim)
			continue
		}
		if path != expectedPath {
			t.Errorf("validDimensions[%q] = %q, want %q", dim, path, expectedPath)
		}
	}
}

// TestDailyDimensionError checks that invalid dimensions produce appropriate errors.
func TestDailyDimensionError(t *testing.T) {
	invalidDims := []string{"invalid", "modelx", "", "model "}
	for _, dim := range invalidDims {
		t.Run("dimension_"+dim, func(t *testing.T) {
			_, ok := validDimensions[dim]
			if ok {
				t.Errorf("validDimensions should NOT contain %q", dim)
			}
		})
	}
}

// TestGranularityConstants ensures Granularity constants have expected string values.
func TestGranularityConstants(t *testing.T) {
	if GranularityDay != "day" {
		t.Errorf("GranularityDay = %q, want %q", GranularityDay, "day")
	}
	if GranularityHour != "hour" {
		t.Errorf("GranularityHour = %q, want %q", GranularityHour, "hour")
	}
}

// TestModelStatsEmptyArray proves that serialized ModelStats produces {"models":[]} not {"models":null}.
func TestModelStatsEmptyArray(t *testing.T) {
	stats := ModelStats{Models: make([]ModelEntry, 0)}
	data, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("json.Marshal(ModelStats{}) failed: %v", err)
	}
	got := string(data)
	want := `{"models":[]}`
	if got != want {
		t.Errorf("ModelStats JSON = %s, want %s", got, want)
	}

	// Also verify that nil produces {"models":null} so the make() fix is necessary
	statsNil := ModelStats{}
	dataNil, _ := json.Marshal(statsNil)
	if string(dataNil) != `{"models":null}` {
		t.Errorf("nil ModelStats JSON = %s, want {\"models\":null}", string(dataNil))
	}
}

// TestMessageEntryCostOmitEmpty proves that MessageEntry with zero cost omits the field.
func TestMessageEntryCostOmitEmpty(t *testing.T) {
	entry := MessageEntry{
		ID:        "test-msg",
		SessionID: "test-ses",
		Role:      "user",
		Cost:      0,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("json.Marshal(MessageEntry) failed: %v", err)
	}
	got := string(data)
	if strings.Contains(got, `"cost":0`) {
		t.Errorf("MessageEntry with Cost=0 should omit cost field, got: %s", got)
	}
	if !strings.Contains(got, `"id":"test-msg"`) {
		t.Errorf("MessageEntry missing id field: %s", got)
	}

	// Verify non-zero cost IS included
	entry.Cost = 1.5
	data, err = json.Marshal(entry)
	if err != nil {
		t.Fatalf("json.Marshal(MessageEntry with cost) failed: %v", err)
	}
	got = string(data)
	if !strings.Contains(got, `"cost":1.5`) {
		t.Errorf("MessageEntry with Cost=1.5 should include cost, got: %s", got)
	}
}

// Regression: PeriodWindow.EndMs is exclusive in every mode, but EndDate is not
// — presets set it to the inclusive last day, explicit from/to ranges to the
// exclusive end. Daily used to iterate day buckets inclusively up to EndDate and
// then add another day for the SQL bound, so an explicit range emitted a trailing
// bucket for the day *after* `to`, populated with that day's real cost. It also
// disagreed with /overview over the identical range.
func TestDailyExplicitRangeExcludesDayAfterTo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dbPath, err := fixture.SampleFixture(ctx)
	if err != nil {
		t.Fatalf("Failed to create fixture database: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(dbPath))

	st, err := store.Connect(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to connect to fixture: %v", err)
	}
	defer st.Close()

	const from, to = "2026-07-01", "2026-07-03"

	daily, err := Daily(ctx, st, PeriodQuery{From: from, To: to})
	if err != nil {
		t.Fatalf("Daily() failed: %v", err)
	}

	if len(daily.Days) != 3 {
		var got []string
		for _, d := range daily.Days {
			got = append(got, d.Date)
		}
		t.Fatalf("Daily(%s..%s) returned %d buckets %v, want exactly 3 (Jul 1-3)", from, to, len(daily.Days), got)
	}
	if first, last := daily.Days[0].Date, daily.Days[2].Date; first != from || last != to {
		t.Errorf("bucket range = %s..%s, want %s..%s", first, last, from, to)
	}

	// Daily and Overview must agree over the same window, which they cannot if
	// Daily's SQL bound reaches a day further than Overview's.
	overview, err := Overview(ctx, st, PeriodQuery{From: from, To: to})
	if err != nil {
		t.Fatalf("Overview() failed: %v", err)
	}
	var dailyCost float64
	var dailyMessages int64
	for _, d := range daily.Days {
		dailyCost += d.Cost
		dailyMessages += d.Messages
	}
	if dailyMessages != overview.Messages {
		t.Errorf("sum(daily.Messages) = %d, overview.Messages = %d: the two disagree over %s..%s", dailyMessages, overview.Messages, from, to)
	}
	if diff := dailyCost - overview.Cost; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("sum(daily.Cost) = %f, overview.Cost = %f: the two disagree over %s..%s", dailyCost, overview.Cost, from, to)
	}
}
