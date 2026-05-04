package stats

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"opencode-dashboard/internal/store"
)

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
// Supported period values: "1d", "7d", "30d", "1y", "all", "1h", "6h", "12h", "24h", "72h", "14d".
func ComputePeriodWindow(ctx context.Context, s *store.Store, period string) (PeriodWindow, error) {
	return ComputePeriodWindowFromQuery(ctx, s, PeriodQuery{Period: period})
}

// ComputePeriodWindowFromQuery dispatches to preset or explicit window computation.
// If pq.From is set, it delegates to explicitPeriodWindow.
// Otherwise it delegates to presetPeriodWindow based on pq.Period.
// If both are empty, defaults to "7d" preset.
func ComputePeriodWindowFromQuery(ctx context.Context, s *store.Store, pq PeriodQuery) (PeriodWindow, error) {
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

		return explicitPeriodWindow(from, to), nil
	}

	// Default to "7d" if period is empty
	period := pq.Period
	if period == "" {
		period = "7d"
	}

	return presetPeriodWindow(ctx, s, period)
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

	// Day-based presets (calendar-aligned in server timezone)
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

// hourPresetRegex matches hour-preset strings like "1h", "6h", "72h".
var hourPresetRegex = regexp.MustCompile(`^(\d+)h$`)

// parseHourPreset parses an hour preset string and returns the number of hours.
// Returns (0, false) if the string is not a valid hour preset.
func parseHourPreset(period string) (int, bool) {
	matches := hourPresetRegex.FindStringSubmatch(period)
	if matches == nil {
		return 0, false
	}

	switch matches[1] {
	case "1", "6", "12", "24", "72":
		// valid hour presets
	default:
		return 0, false
	}

	var hours int
	_, err := fmt.Sscanf(matches[1], "%d", &hours)
	if err != nil {
		return 0, false
	}

	return hours, true
}
