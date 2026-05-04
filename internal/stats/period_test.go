package stats

import (
	"context"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// ComputePeriodWindowFromQuery — preset mode (day presets)
// ---------------------------------------------------------------------------

func TestComputePeriodWindowFromQuery_defaultPeriod(t *testing.T) {
	// Empty period + empty from → defaults to "7d" preset
	pw, err := ComputePeriodWindowFromQuery(context.Background(), nil, PeriodQuery{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should behave like "7d" — StartDate is 6 days before EndDate
	wantMin := 6*24*time.Hour - time.Second
	wantMax := 6*24*time.Hour + time.Second
	gotDiff := pw.EndDate.Sub(pw.StartDate)
	if gotDiff < wantMin || gotDiff > wantMax {
		t.Errorf("default period date range diff = %v, want ~%v", gotDiff, 6*24*time.Hour)
	}

	// Both dates should be midnight in UTC
	if pw.StartDate.Hour() != 0 || pw.StartDate.Minute() != 0 || pw.StartDate.Second() != 0 {
		t.Errorf("StartDate is not midnight: %v", pw.StartDate)
	}
	if pw.EndDate.Hour() != 0 || pw.EndDate.Minute() != 0 || pw.EndDate.Second() != 0 {
		t.Errorf("EndDate is not midnight: %v", pw.EndDate)
	}

	// Both dates must be in UTC
	if pw.StartDate.Location() != time.UTC {
		t.Errorf("StartDate location = %v, want %v", pw.StartDate.Location(), time.UTC)
	}
	if pw.EndDate.Location() != time.UTC {
		t.Errorf("EndDate location = %v, want %v", pw.EndDate.Location(), time.UTC)
	}
}

func TestComputePeriodWindowFromQuery_dayPresets(t *testing.T) {
	tests := []struct {
		name        string
		period      string
		wantDayDiff int // expected calendar days between start and end (end - start)
	}{
		{
			name:        "1d today only",
			period:      "1d",
			wantDayDiff: 0, // same day: start == end
		},
		{
			name:        "7d last 7 days",
			period:      "7d",
			wantDayDiff: 6, // 7 days = today + 6 prior = 7 total
		},
		{
			name:        "14d last 14 days",
			period:      "14d",
			wantDayDiff: 13,
		},
		{
			name:        "30d last 30 days",
			period:      "30d",
			wantDayDiff: 29,
		},
		{
			name:        "1y last 365 days",
			period:      "1y",
			wantDayDiff: 364,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pw, err := ComputePeriodWindowFromQuery(context.Background(), nil, PeriodQuery{Period: tt.period})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Both dates must be midnight in UTC
			if pw.StartDate.Hour() != 0 || pw.StartDate.Minute() != 0 || pw.StartDate.Second() != 0 {
				t.Errorf("StartDate is not midnight: %v", pw.StartDate)
			}
			if pw.EndDate.Hour() != 0 || pw.EndDate.Minute() != 0 || pw.EndDate.Second() != 0 {
				t.Errorf("EndDate is not midnight: %v", pw.EndDate)
			}

			// Timezone must be UTC
			if pw.StartDate.Location() != time.UTC {
				t.Errorf("StartDate timezone = %v, want %v", pw.StartDate.Location(), time.UTC)
			}
			if pw.EndDate.Location() != time.UTC {
				t.Errorf("EndDate timezone = %v, want %v", pw.EndDate.Location(), time.UTC)
			}

			// Calendar-day alignment check (UTC)
			year, month, day := pw.EndDate.Date()
			endMidnight := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
			if !pw.EndDate.Equal(endMidnight) {
				t.Errorf("EndDate %v is not midnight in UTC", pw.EndDate)
			}

			// StartDate should be EndDate - wantDayDiff days
			expectedStart := pw.EndDate.AddDate(0, 0, -tt.wantDayDiff)
			if !pw.StartDate.Equal(expectedStart) {
				t.Errorf("StartDate = %v, want %v (EndDate - %d days)", pw.StartDate, expectedStart, tt.wantDayDiff)
			}

			// EndMs should be EndDate + 1 day (exclusive)
			expectedEndMs := pw.EndDate.AddDate(0, 0, 1).UnixMilli()
			if pw.EndMs != expectedEndMs {
				t.Errorf("EndMs = %d, want %d (EndDate + 1 day)", pw.EndMs, expectedEndMs)
			}

			// StartMs should match StartDate
			if pw.StartMs != pw.StartDate.UnixMilli() {
				t.Errorf("StartMs = %d, but StartDate.UnixMilli() = %d", pw.StartMs, pw.StartDate.UnixMilli())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ComputePeriodWindowFromQuery — rolling hour presets
// ---------------------------------------------------------------------------

func TestComputePeriodWindowFromQuery_hourPresets(t *testing.T) {
	tests := []struct {
		name      string
		period    string
		wantHours int
	}{
		{name: "1h rolling", period: "1h", wantHours: 1},
		{name: "6h rolling", period: "6h", wantHours: 6},
		{name: "12h rolling", period: "12h", wantHours: 12},
		{name: "24h rolling", period: "24h", wantHours: 24},
		{name: "72h rolling", period: "72h", wantHours: 72},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := time.Now()
			pw, err := ComputePeriodWindowFromQuery(context.Background(), nil, PeriodQuery{Period: tt.period})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			after := time.Now()

			// EndDate should be approximately now (within execution time)
			if pw.EndDate.Before(before) || pw.EndDate.After(after) {
				t.Errorf("EndDate %v not within [before=%v, after=%v]", pw.EndDate, before, after)
			}

			// Duration should be approximately wantHours
			expectedDuration := time.Duration(tt.wantHours) * time.Hour
			gotDuration := pw.EndDate.Sub(pw.StartDate)

			// Allow 1 second tolerance for execution delay
			lower := expectedDuration - time.Second
			upper := expectedDuration + time.Second
			if gotDuration < lower || gotDuration > upper {
				t.Errorf("hour preset %q: duration = %v, want ~%v", tt.period, gotDuration, expectedDuration)
			}

			// StartMs and EndMs should be consistent with dates
			if pw.StartMs != pw.StartDate.UnixMilli() {
				t.Errorf("StartMs mismatch: %d vs %d", pw.StartMs, pw.StartDate.UnixMilli())
			}
			if pw.EndMs != pw.EndDate.UnixMilli() {
				t.Errorf("EndMs mismatch: %d vs %d", pw.EndMs, pw.EndDate.UnixMilli())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ComputePeriodWindowFromQuery — 24h (rolling) vs 1d (calendar-aligned)
// ---------------------------------------------------------------------------

func TestHourVsDayPresetSemantics(t *testing.T) {
	// 24h is a ROLLING window: now - 24h → now (not midnight-aligned)
	// 1d is CALENDAR-ALIGNED: today midnight → tomorrow midnight
	t.Run("24h is rolling, not calendar-aligned", func(t *testing.T) {
		pw24h, err := ComputePeriodWindowFromQuery(context.Background(), nil, PeriodQuery{Period: "24h"})
		if err != nil {
			t.Fatal(err)
		}

		// StartDate should NOT be midnight-relative (it's a rolling window)
		// so StartMs should be close to EndMs - 24h
		expectedMs := pw24h.EndMs - 24*3600*1000
		diff := pw24h.StartMs - expectedMs
		if diff < -100 || diff > 100 {
			t.Errorf("24h: StartMs %d not within 100ms of EndMs-24h (%d), diff=%d", pw24h.StartMs, expectedMs, diff)
		}
	})

	t.Run("1d is calendar-midnight aligned", func(t *testing.T) {
		pw1d, err := ComputePeriodWindowFromQuery(context.Background(), nil, PeriodQuery{Period: "1d"})
		if err != nil {
			t.Fatal(err)
		}

		// Both dates must be midnight
		if pw1d.StartDate.Hour() != 0 || pw1d.EndDate.Hour() != 0 {
			t.Errorf("1d: dates not midnight: Start=%v, End=%v", pw1d.StartDate, pw1d.EndDate)
		}
		// Same day: StartDate == EndDate
		if !pw1d.StartDate.Equal(pw1d.EndDate) {
			t.Errorf("1d: StartDate %v != EndDate %v (should be same day)", pw1d.StartDate, pw1d.EndDate)
		}
		// EndMs should be exactly 24h after StartMs
		if pw1d.EndMs-pw1d.StartMs != 24*3600*1000 {
			t.Errorf("1d: EndMs - StartMs = %d ms, want 86400000", pw1d.EndMs-pw1d.StartMs)
		}
	})
}

// ---------------------------------------------------------------------------
// ComputePeriodWindowFromQuery — explicit range (custom dates)
// ---------------------------------------------------------------------------

func TestComputePeriodWindowFromQuery_explicitRange(t *testing.T) {
	pw, err := ComputePeriodWindowFromQuery(context.Background(), nil, PeriodQuery{
		From: "2026-01-15",
		To:   "2026-01-20",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// from → Jan 15 midnight in UTC
	expectedFrom := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	if !pw.StartDate.Equal(expectedFrom) {
		t.Errorf("StartDate = %v, want %v", pw.StartDate, expectedFrom)
	}

	// to is midnight exclusive → Jan 21 midnight (2026-01-20 + 1 day)
	expectedTo := time.Date(2026, 1, 21, 0, 0, 0, 0, time.UTC)
	if !pw.EndDate.Equal(expectedTo) {
		t.Errorf("EndDate = %v, want %v", pw.EndDate, expectedTo)
	}

	// Milliseconds
	if pw.StartMs != expectedFrom.UnixMilli() {
		t.Errorf("StartMs = %d, want %d", pw.StartMs, expectedFrom.UnixMilli())
	}
	if pw.EndMs != expectedTo.UnixMilli() {
		t.Errorf("EndMs = %d, want %d", pw.EndMs, expectedTo.UnixMilli())
	}
}

func TestComputePeriodWindowFromQuery_explicitFromOnly(t *testing.T) {
	// from only → to defaults to now (server timezone)
	before := time.Now()
	pw, err := ComputePeriodWindowFromQuery(context.Background(), nil, PeriodQuery{
		From: "2026-04-01",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	after := time.Now()

	// Start should be exact
	expectedFrom := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	if !pw.StartDate.Equal(expectedFrom) {
		t.Errorf("StartDate = %v, want %v", pw.StartDate, expectedFrom)
	}

	// End should be approximately now (within execution window)
	if pw.EndDate.Before(before) || pw.EndDate.After(after) {
		t.Errorf("EndDate %v not within [%v, %v]", pw.EndDate, before, after)
	}
}

func TestComputePeriodWindowFromQuery_fromPrecedesPeriod(t *testing.T) {
	// Both from and period set → from wins (period ignored)
	pw, err := ComputePeriodWindowFromQuery(context.Background(), nil, PeriodQuery{
		From:   "2026-02-01",
		To:     "2026-02-10",
		Period: "1d", // should be ignored
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedFrom := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	expectedTo := time.Date(2026, 2, 11, 0, 0, 0, 0, time.UTC) // +1 day exclusive

	if !pw.StartDate.Equal(expectedFrom) {
		t.Errorf("StartDate = %v, want %v (from should beat period)", pw.StartDate, expectedFrom)
	}
	if !pw.EndDate.Equal(expectedTo) {
		t.Errorf("EndDate = %v, want %v (should be from+to range, not 1d)", pw.EndDate, expectedTo)
	}
}

// ---------------------------------------------------------------------------
// ComputePeriodWindowFromQuery — error cases
// ---------------------------------------------------------------------------

func TestComputePeriodWindowFromQuery_invalidFrom(t *testing.T) {
	tests := []struct {
		name     string
		from     string
		errMatch string
	}{
		{name: "bad format", from: "15-01-2026", errMatch: "invalid from date"},
		{name: "not a date", from: "abc", errMatch: "invalid from date"},
		{name: "partial date", from: "2026-01", errMatch: "invalid from date"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ComputePeriodWindowFromQuery(context.Background(), nil, PeriodQuery{From: tt.from})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errMatch) {
				t.Errorf("error = %q, want containing %q", err.Error(), tt.errMatch)
			}
		})
	}
}

func TestComputePeriodWindowFromQuery_invalidTo(t *testing.T) {
	tests := []struct {
		name     string
		from     string
		to       string
		errMatch string
	}{
		{name: "bad to format", from: "2026-01-01", to: "20-01-2026", errMatch: "invalid to date"},
		{name: "not a date", from: "2026-01-01", to: "abc", errMatch: "invalid to date"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ComputePeriodWindowFromQuery(context.Background(), nil, PeriodQuery{
				From: tt.from,
				To:   tt.to,
			})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errMatch) {
				t.Errorf("error = %q, want containing %q", err.Error(), tt.errMatch)
			}
		})
	}
}

func TestComputePeriodWindowFromQuery_invalidPeriod(t *testing.T) {
	tests := []struct {
		name     string
		period   string
		errMatch string
	}{
		{name: "bogus string", period: "bogus", errMatch: "invalid period"},
		{name: "number only", period: "42", errMatch: "invalid period"},
		{name: "unsupported days", period: "2d", errMatch: "invalid period"},
		{name: "empty string", period: "", errMatch: ""}, // defaults to "7d"
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ComputePeriodWindowFromQuery(context.Background(), nil, PeriodQuery{Period: tt.period})
			if tt.errMatch == "" {
				// Empty period should default to "7d", no error
				if err != nil {
					t.Errorf("unexpected error for empty period: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errMatch) {
				t.Errorf("error = %q, want containing %q", err.Error(), tt.errMatch)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseHourPreset — unit tests
// ---------------------------------------------------------------------------

func TestParseHourPreset(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   int
		wantOK bool
	}{
		{name: "1h", input: "1h", want: 1, wantOK: true},
		{name: "6h", input: "6h", want: 6, wantOK: true},
		{name: "12h", input: "12h", want: 12, wantOK: true},
		{name: "24h", input: "24h", want: 24, wantOK: true},
		{name: "72h", input: "72h", want: 72, wantOK: true},
		{name: "2h rejected", input: "2h", want: 0, wantOK: false},
		{name: "0h rejected", input: "0h", want: 0, wantOK: false},
		{name: "100h rejected", input: "100h", want: 0, wantOK: false},
		{name: "empty rejected", input: "", want: 0, wantOK: false},
		{name: "just h rejected", input: "h", want: 0, wantOK: false},
		{name: "day preset rejected", input: "1d", want: 0, wantOK: false},
		{name: "uppercase", input: "1H", want: 0, wantOK: false},
		{name: "with plus", input: "+1h", want: 0, wantOK: false},
		{name: "negative", input: "-1h", want: 0, wantOK: false},
		{name: "trailing", input: "1h ", want: 0, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseHourPreset(tt.input)
			if ok != tt.wantOK {
				t.Errorf("parseHourPreset(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Errorf("parseHourPreset(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// explicitPeriodWindow — direct unit test
// ---------------------------------------------------------------------------

func TestExplicitPeriodWindow(t *testing.T) {
	from := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)

	pw := explicitPeriodWindow(from, to)

	if !pw.StartDate.Equal(from) {
		t.Errorf("StartDate = %v, want %v", pw.StartDate, from)
	}
	if !pw.EndDate.Equal(to) {
		t.Errorf("EndDate = %v, want %v", pw.EndDate, to)
	}
	if pw.StartMs != from.UnixMilli() {
		t.Errorf("StartMs = %d, want %d", pw.StartMs, from.UnixMilli())
	}
	if pw.EndMs != to.UnixMilli() {
		t.Errorf("EndMs = %d, want %d", pw.EndMs, to.UnixMilli())
	}
}

// ---------------------------------------------------------------------------
// ComputePeriodWindowFromQuery — backward compat wrapper
// ---------------------------------------------------------------------------

func TestComputePeriodWindow_backwardCompat(t *testing.T) {
	// Old callers pass just a period string via the wrapper
	pw, err := ComputePeriodWindow(context.Background(), nil, "7d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should behave like "7d" preset — StartDate is 6 days before EndDate
	wantMin := 6*24*time.Hour - time.Second
	wantMax := 6*24*time.Hour + time.Second
	gotDiff := pw.EndDate.Sub(pw.StartDate)
	if gotDiff < wantMin || gotDiff > wantMax {
		t.Errorf("backward compat date range diff = %v, want ~%v", gotDiff, 6*24*time.Hour)
	}
}

// ---------------------------------------------------------------------------
// Timezone boundary: day presets always midnight in UTC
// ---------------------------------------------------------------------------

func TestDayPresetTimezoneBoundary(t *testing.T) {
	pw, err := ComputePeriodWindowFromQuery(context.Background(), nil, PeriodQuery{Period: "1d"})
	if err != nil {
		t.Fatal(err)
	}

	if pw.StartDate.Location() != time.UTC {
		t.Errorf("StartDate timezone = %v, want time.UTC", pw.StartDate.Location())
	}
	if pw.EndDate.Location() != time.UTC {
		t.Errorf("EndDate timezone = %v, want time.UTC", pw.EndDate.Location())
	}

	startY, startM, startD := pw.StartDate.Date()
	midnight := time.Date(startY, startM, startD, 0, 0, 0, 0, pw.StartDate.Location())
	if !pw.StartDate.Equal(midnight) {
		t.Errorf("StartDate %v is not midnight at its own location %v", pw.StartDate, pw.StartDate.Location())
	}

	expectedStartMs := midnight.UnixMilli()
	if pw.StartMs != expectedStartMs {
		t.Errorf("StartMs = %d, want %d (UTC midnight ms)", pw.StartMs, expectedStartMs)
	}
}

// ---------------------------------------------------------------------------
// UTC boundary: rolling hour presets must use UTC for key consistency with SQL
// ---------------------------------------------------------------------------

func TestHourPresetUTCTimezone(t *testing.T) {
	pw, err := ComputePeriodWindowFromQuery(context.Background(), nil, PeriodQuery{Period: "24h"})
	if err != nil {
		t.Fatal(err)
	}

	if pw.StartDate.Location() != time.UTC {
		t.Errorf("StartDate timezone = %v, want time.UTC", pw.StartDate.Location())
	}
	if pw.EndDate.Location() != time.UTC {
		t.Errorf("EndDate timezone = %v, want time.UTC", pw.EndDate.Location())
	}
}
