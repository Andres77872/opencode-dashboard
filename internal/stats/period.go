package stats

import (
	"context"
	"fmt"
	"time"

	"opencode-dashboard/internal/store"
)

// PeriodWindow holds the computed start and end boundaries for a statistical period.
// StartDate and EndDate are the UTC midnight times; StartMs and EndMs are their
// Unix millisecond equivalents used in SQL queries.
type PeriodWindow struct {
	StartDate time.Time
	EndDate   time.Time
	StartMs   int64
	EndMs     int64
}

// ComputePeriodWindow calculates the UTC-midnight-aligned time window for the given period string.
// Supported period values: "1d", "7d", "30d", "1y", "all".
// Returns an error for invalid period values.
func ComputePeriodWindow(ctx context.Context, s *store.Store, period string) (PeriodWindow, error) {
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
