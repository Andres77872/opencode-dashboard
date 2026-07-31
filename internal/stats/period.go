package stats

import (
	"context"
	"fmt"
	"time"

	"opencode-dashboard/internal/store"
)

// DefaultPeriodPreset is the preset used when callers omit both a preset and
// an explicit from/to range.
const (
	DefaultPeriodPreset   = "7d"
	allHistoricPeriodDays = -1
)

type periodPreset struct {
	name  string
	hours int
	days  int
	all   bool
}

// periodPresets is the canonical backend period catalog and its executable
// meaning, ordered from the shortest rolling window to complete history.
// Keeping names and resolution in one table prevents a schema-valid preset
// from drifting away from the runtime parser.
var periodPresets = []periodPreset{
	{name: "1h", hours: 1}, {name: "6h", hours: 6},
	{name: "12h", hours: 12}, {name: "24h", hours: 24},
	{name: "72h", hours: 72}, {name: "1d", days: 1},
	{name: "7d", days: 7}, {name: "14d", days: 14},
	{name: "30d", days: 30}, {name: "1y", days: 365},
	{name: "all", all: true},
}

// SupportedPeriodPresets returns a defensive copy of the canonical period
// catalog. Callers may safely sort or filter the result without changing the
// presets seen by another backend surface.
func SupportedPeriodPresets() []string {
	result := make([]string, 0, len(periodPresets))
	for _, preset := range periodPresets {
		result = append(result, preset.name)
	}
	return result
}

// IsSupportedPeriodPreset reports whether period is one of the exact,
// case-sensitive backend presets. An empty period is not itself a preset;
// entrypoints that support omission should substitute DefaultPeriodPreset.
func IsSupportedPeriodPreset(period string) bool {
	for _, candidate := range periodPresets {
		if period == candidate.name {
			return true
		}
	}
	return false
}

// PeriodWindow holds the computed start and end boundaries for a statistical period.
// StartDate and EndDate are UTC midnight times; StartMs and EndMs are their
// Unix millisecond equivalents used in SQL queries.
type PeriodWindow struct {
	StartDate time.Time
	EndDate   time.Time
	StartMs   int64
	EndMs     int64
}

// ComputePeriodWindow is a backward-compatible wrapper around ComputePeriodWindowFromQuery.
// Supported period values are exposed by SupportedPeriodPresets.
func ComputePeriodWindow(ctx context.Context, s *store.Store, period string) (PeriodWindow, error) {
	return ComputePeriodWindowFromQuery(ctx, s, PeriodQuery{Period: period})
}

// ComputePeriodWindowFromQuery dispatches to preset or explicit window computation.
// If pq.From is set, it delegates to explicitPeriodWindow.
// Otherwise it delegates to presetPeriodWindow based on pq.Period.
// If both are empty, defaults to DefaultPeriodPreset.
func ComputePeriodWindowFromQuery(ctx context.Context, s *store.Store, pq PeriodQuery) (PeriodWindow, error) {
	// Internal time-precision bounds beat everything (cache gap-merge layer).
	if from, to, ok := pq.TimeBounds(); ok {
		return explicitPeriodWindow(from, to), nil
	}

	// From beats Period — explicit range mode
	if pq.From != "" {
		from, err := time.ParseInLocation("2006-01-02", pq.From, time.UTC)
		if err != nil {
			return PeriodWindow{}, fmt.Errorf("invalid from date %q: expected YYYY-MM-DD format", pq.From)
		}

		var to time.Time
		if pq.To != "" {
			to, err = time.ParseInLocation("2006-01-02", pq.To, time.UTC)
			if err != nil {
				return PeriodWindow{}, fmt.Errorf("invalid to date %q: expected YYYY-MM-DD format", pq.To)
			}
			// to is midnight exclusive, so add 1 day
			to = to.AddDate(0, 0, 1)
		} else {
			to = time.Now().UTC()
		}

		return capWindowEnd(explicitPeriodWindow(from, to), pq.ToTime), nil
	}

	// Apply the shared default if period is empty.
	period := pq.Period
	if period == "" {
		period = DefaultPeriodPreset
	}

	window, err := presetPeriodWindow(ctx, s, period)
	if err != nil {
		return PeriodWindow{}, err
	}
	return capWindowEnd(window, pq.ToTime), nil
}

// capWindowEnd clamps the window end to toTime when set, so a caller can
// bound a preset or explicit window at the cache finality cutoff.
func capWindowEnd(w PeriodWindow, toTime time.Time) PeriodWindow {
	if toTime.IsZero() {
		return w
	}
	toTime = toTime.UTC()
	if toTime.UnixMilli() >= w.EndMs {
		return w
	}
	w.EndDate = toTime
	w.EndMs = toTime.UnixMilli()
	return w
}

// presetPeriodWindow handles all preset strings.
// Hour presets (1h, 6h, 12h, 24h, 72h) use rolling UTC window: now - duration → now.
// Day presets (1d, 7d, 14d, 30d, 1y) use UTC calendar-day-aligned.
// "all" queries the earliest activity date from the database.
func presetPeriodWindow(ctx context.Context, s *store.Store, period string) (PeriodWindow, error) {
	// Check for hour-based presets first (rolling window)
	if hours, ok := parseHourPreset(period); ok {
		now := time.Now().UTC()
		start := now.Add(-time.Duration(hours) * time.Hour)
		return PeriodWindow{
			StartDate: start,
			EndDate:   now,
			StartMs:   start.UnixMilli(),
			EndMs:     now.UnixMilli(),
		}, nil
	}

	// Day-based presets (calendar-aligned in UTC)
	days, err := parsePeriod(period)
	if err != nil {
		return PeriodWindow{}, err
	}

	now := time.Now().UTC()
	endDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	startDate := endDate

	if days == allHistoricPeriodDays {
		startDate, err = queryEarliestActivityDate(ctx, s)
		if err != nil {
			return PeriodWindow{}, fmt.Errorf("query earliest activity date: %w", err)
		}
		if startDate.IsZero() {
			startDate = endDate
		}
	} else if days > 0 {
		startDate = endDate.AddDate(0, 0, -days+1)
	}

	endMs := endDate.AddDate(0, 0, 1).UnixMilli()

	return PeriodWindow{
		StartDate: startDate,
		EndDate:   endDate,
		StartMs:   startDate.UnixMilli(),
		EndMs:     endMs,
	}, nil
}

// explicitPeriodWindow wraps pre-parsed date boundaries into a PeriodWindow.
func explicitPeriodWindow(from, to time.Time) PeriodWindow {
	return PeriodWindow{
		StartDate: from,
		EndDate:   to,
		StartMs:   from.UnixMilli(),
		EndMs:     to.UnixMilli(),
	}
}

// hourBucketLayout is the Date key layout for hourly trend buckets. Daily and
// DailyDimension both emit it so hour-grained series merge across endpoints.
const hourBucketLayout = "2006-01-02T15:04:05Z"

// ResolveGranularity returns the effective trend bucket granularity for a
// period query: an explicit granularity always wins; otherwise "1d" and the
// hour presets bucket hourly and everything else buckets daily. Every trend
// producer (Daily, DailyDimension, the cache, and the snapshot sources)
// resolves through this single rule so grouped views bucket identically.
func ResolveGranularity(pq PeriodQuery, granularity ...Granularity) Granularity {
	if len(granularity) > 0 && granularity[0] != "" {
		return granularity[0]
	}
	if pq.Period == "1d" {
		return GranularityHour
	}
	if _, ok := parseHourPreset(pq.Period); ok {
		return GranularityHour
	}
	return GranularityDay
}

// BucketKey formats t as its trend bucket's Date key: the UTC calendar day
// (YYYY-MM-DD) for day granularity, the UTC hour's timestamp for hour
// granularity. Matches the keys Daily emits in both granularities.
func BucketKey(t time.Time, gran Granularity) string {
	if gran == GranularityHour {
		return t.UTC().Truncate(time.Hour).Format(hourBucketLayout)
	}
	return t.UTC().Format("2006-01-02")
}

// HourPresetHours resolves a canonical rolling-hour preset. Cache and snapshot
// adapters use this instead of maintaining executable copies of the catalog.
func HourPresetHours(period string) (int, bool) {
	for _, preset := range periodPresets {
		if preset.name == period && preset.hours > 0 {
			return preset.hours, true
		}
	}
	return 0, false
}

func parseHourPreset(period string) (int, bool) {
	return HourPresetHours(period)
}

func calendarPresetDays(period string) (int, bool) {
	for _, preset := range periodPresets {
		if preset.name != period || preset.hours > 0 {
			continue
		}
		if preset.all {
			return allHistoricPeriodDays, true
		}
		return preset.days, preset.days > 0
	}
	return 0, false
}
