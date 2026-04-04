package stats

import (
	"strings"
	"testing"
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
		{name: "30d", input: "30d", expected: 30, wantErr: false},
		{name: "1y", input: "1y", expected: 365, wantErr: false},
		{name: "all", input: "all", expected: allHistoricPeriodDays, wantErr: false},
		{name: "invalid empty", input: "", expected: 0, wantErr: true, errContains: "invalid period"},
		{name: "invalid 14d", input: "14d", expected: 0, wantErr: true, errContains: "invalid period"},
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

func TestGranularityConstants(t *testing.T) {
	if GranularityDay != "day" {
		t.Errorf("GranularityDay = %q, want %q", GranularityDay, "day")
	}
	if GranularityHour != "hour" {
		t.Errorf("GranularityHour = %q, want %q", GranularityHour, "hour")
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
