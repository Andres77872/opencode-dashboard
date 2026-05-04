package stats

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opencode-dashboard/internal/store"
	"opencode-dashboard/internal/store/fixture"
)

func TestOverviewWithFixture(t *testing.T) {
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

	overview, err := Overview(ctx, st, PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Overview() failed: %v", err)
	}

	// Fixture has 4 sessions
	if overview.Sessions != 4 {
		t.Errorf("Overview.Sessions = %d, want 4", overview.Sessions)
	}

	// Fixture has 12 messages (4 user + 4 assistant in session 1&3, 2 each in session 2&4)
	// Note: actual count depends on JSON parsing in the message data column
	if overview.Messages <= 0 {
		t.Errorf("Overview.Messages = %d, want > 0", overview.Messages)
	}

	// Total cost should be positive
	if overview.Cost <= 0 {
		t.Errorf("Overview.Cost = %.6f, want > 0", overview.Cost)
	}

	// Should have positive token counts
	if overview.Tokens.Input <= 0 {
		t.Errorf("Overview.Tokens.Input = %d, want > 0", overview.Tokens.Input)
	}
	if overview.Tokens.Output <= 0 {
		t.Errorf("Overview.Tokens.Output = %d, want > 0", overview.Tokens.Output)
	}

	// Days should be >= 1 (sessions span multiple days)
	if overview.Days < 1 {
		t.Errorf("Overview.Days = %d, want >= 1", overview.Days)
	}

	// CostPerDay should be calculated
	if overview.CostPerDay <= 0 {
		t.Errorf("Overview.CostPerDay = %.6f, want > 0", overview.CostPerDay)
	}
}

func TestSessionsWithFixture(t *testing.T) {
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

	list, err := Sessions(ctx, st, 1, 10)
	if err != nil {
		t.Fatalf("Sessions() failed: %v", err)
	}

	if list.Total != 4 {
		t.Errorf("Sessions.Total = %d, want 4", list.Total)
	}

	if len(list.Sessions) != 4 {
		t.Errorf("len(Sessions.Sessions) = %d, want 4", len(list.Sessions))
	}

	for _, s := range list.Sessions {
		if s.ID == "" {
			t.Error("SessionEntry.ID is empty")
		}
		if s.TimeCreated.IsZero() {
			t.Errorf("SessionEntry.TimeCreated is zero for %s", s.ID)
		}
	}
}

func TestSessionsWithQuery(t *testing.T) {
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

	list, err := SessionsWithQuery(ctx, st, SessionQuery{
		Page:     1,
		PageSize: 10,
		Sort:     SessionSortNewest,
	})
	if err != nil {
		t.Fatalf("SessionsWithQuery() failed: %v", err)
	}

	if list.Total != 4 {
		t.Errorf("SessionsWithQuery.Total = %d, want 4", list.Total)
	}

	oldestList, err := SessionsWithQuery(ctx, st, SessionQuery{
		Page:     1,
		PageSize: 10,
		Sort:     SessionSortOldest,
	})
	if err != nil {
		t.Fatalf("SessionsWithQuery(oldest) failed: %v", err)
	}

	if len(oldestList.Sessions) > 1 {
		if oldestList.Sessions[0].TimeCreated.After(oldestList.Sessions[len(oldestList.Sessions)-1].TimeCreated) {
			t.Error("Sessions with oldest sort are not in chronological order")
		}
	}
}

func TestSessionByIDWithFixture(t *testing.T) {
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

	detail, err := SessionByID(ctx, st, "ses-001")
	if err != nil {
		t.Fatalf("SessionByID() failed: %v", err)
	}

	if detail == nil {
		t.Fatal("SessionByID returned nil, want session detail")
	}

	if detail.ID != "ses-001" {
		t.Errorf("SessionDetail.ID = %s, want ses-001", detail.ID)
	}

	if detail.Title != "Implement auth middleware" {
		t.Errorf("SessionDetail.Title = %s, want 'Implement auth middleware'", detail.Title)
	}

	if len(detail.Messages) != 4 {
		t.Errorf("SessionDetail.Messages has %d messages, want 4", len(detail.Messages))
	}

	if detail.TimeCreated.IsZero() {
		t.Error("SessionDetail.TimeCreated is zero")
	}

	for _, msg := range detail.Messages {
		if msg.TimeCreated.IsZero() {
			t.Errorf("SessionMessage.TimeCreated is zero for %s", msg.ID)
		}
	}

	missing, err := SessionByID(ctx, st, "nonexistent")
	if err != nil {
		t.Fatalf("SessionByID(nonexistent) unexpected error: %v", err)
	}
	if missing != nil {
		t.Error("SessionByID(nonexistent) returned non-nil, want nil")
	}
}

func TestModelsWithFixture(t *testing.T) {
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

	models, err := Models(ctx, st, PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Models() failed: %v", err)
	}

	// Should have 4 different models: claude-3-sonnet, gpt-4, gpt-4-turbo, gpt-3.5-turbo
	if len(models.Models) != 4 {
		t.Errorf("len(ModelStats.Models) = %d, want 4", len(models.Models))
	}

	// Verify each model has expected data
	for _, m := range models.Models {
		if m.ModelID == "" {
			t.Error("ModelEntry.ModelID is empty")
		}
		if m.ProviderID == "" {
			t.Error("ModelEntry.ProviderID is empty")
		}
		if m.Messages <= 0 {
			t.Errorf("ModelEntry.Messages = %d for %s, want > 0", m.Messages, m.ModelID)
		}
		if m.Cost <= 0 {
			t.Errorf("ModelEntry.Cost = %.6f for %s, want > 0", m.Cost, m.ModelID)
		}
	}

	// Models should be sorted by cost descending
	if len(models.Models) >= 2 {
		if models.Models[0].Cost < models.Models[1].Cost {
			t.Errorf("Models not sorted by cost descending: first=%.6f, second=%.6f",
				models.Models[0].Cost, models.Models[1].Cost)
		}
	}
}

func TestModelsWithFixture_MultiStepTokens(t *testing.T) {
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

	models, err := Models(ctx, st, PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Models() failed: %v", err)
	}

	// Build lookup by model_id
	byID := make(map[string]ModelEntry)
	for _, m := range models.Models {
		byID[m.ModelID] = m
	}

	t.Run("multi-step accumulation", func(t *testing.T) {
		// claude-3-sonnet msg-001-02 has 2 step-finish parts:
		//   Part A: {input:500, output:300, reasoning:50, cache_read:100, cache_write:200}
		//   Part B: {input:200, output:400, reasoning:100, cache_read:0, cache_write:50}
		// msg-001-04: no parts, message-level tokens = {input:2000, output:800, reasoning:150}
		// Expected total: input=500+200+2000=2700, output=300+400+800=1500,
		//                 reasoning=50+100+150=300, cache_read=100+0+0=100, cache_write=200+50+0=250
		entry, ok := byID["claude-3-sonnet"]
		if !ok {
			t.Fatal("claude-3-sonnet not found in models")
		}
		if entry.Tokens.Input != 2700 {
			t.Errorf("claude-3-sonnet tokens.input = %d, want 2700", entry.Tokens.Input)
		}
		if entry.Tokens.Output != 1500 {
			t.Errorf("claude-3-sonnet tokens.output = %d, want 1500", entry.Tokens.Output)
		}
		if entry.Tokens.Reasoning != 300 {
			t.Errorf("claude-3-sonnet tokens.reasoning = %d, want 300", entry.Tokens.Reasoning)
		}
		if entry.Tokens.Cache.Read != 100 {
			t.Errorf("claude-3-sonnet tokens.cache.read = %d, want 100", entry.Tokens.Cache.Read)
		}
		if entry.Tokens.Cache.Write != 250 {
			t.Errorf("claude-3-sonnet tokens.cache.write = %d, want 250", entry.Tokens.Cache.Write)
		}
	})

	t.Run("cache propagation", func(t *testing.T) {
		// gpt-4-turbo has step-finish parts with non-zero cache and reasoning
		entry, ok := byID["gpt-4-turbo"]
		if !ok {
			t.Fatal("gpt-4-turbo not found in models")
		}
		if entry.Tokens.Cache.Read <= 0 {
			t.Errorf("gpt-4-turbo tokens.cache.read = %d, want > 0", entry.Tokens.Cache.Read)
		}
		if entry.Tokens.Cache.Write <= 0 {
			t.Errorf("gpt-4-turbo tokens.cache.write = %d, want > 0", entry.Tokens.Cache.Write)
		}
		if entry.Tokens.Reasoning <= 0 {
			t.Errorf("gpt-4-turbo tokens.reasoning = %d, want > 0", entry.Tokens.Reasoning)
		}
	})

	t.Run("fallback to message data", func(t *testing.T) {
		// gpt-3.5-turbo has NO step-finish parts, falls back to message.data.tokens
		// msg-004-02 tokens: {input:500, output:200, reasoning:30, cache:{read:0, write:0}}
		entry, ok := byID["gpt-3.5-turbo"]
		if !ok {
			t.Fatal("gpt-3.5-turbo not found in models")
		}
		if entry.Tokens.Input != 500 {
			t.Errorf("gpt-3.5-turbo tokens.input = %d, want 500 (fallback)", entry.Tokens.Input)
		}
		if entry.Tokens.Output != 200 {
			t.Errorf("gpt-3.5-turbo tokens.output = %d, want 200 (fallback)", entry.Tokens.Output)
		}
		if entry.Tokens.Reasoning != 30 {
			t.Errorf("gpt-3.5-turbo tokens.reasoning = %d, want 30 (fallback)", entry.Tokens.Reasoning)
		}
	})

	t.Run("average fields", func(t *testing.T) {
		for _, m := range models.Models {
			if m.Messages > 0 {
				if m.AvgTokensPerMessage == nil {
					t.Errorf("%s: avg_tokens_per_message is nil, want non-nil", m.ModelID)
					continue
				}
				wantInput := float64(m.Tokens.Input) / float64(m.Messages)
				if m.AvgTokensPerMessage.Input != wantInput {
					t.Errorf("%s avg_tokens_per_message.input = %.2f, want %.2f",
						m.ModelID, m.AvgTokensPerMessage.Input, wantInput)
				}
			} else {
				if m.AvgTokensPerMessage != nil {
					t.Errorf("%s: avg_tokens_per_message is non-nil, want nil (messages=0)", m.ModelID)
				}
			}

			if m.Sessions > 0 {
				if m.AvgTokensPerSession == nil {
					t.Errorf("%s: avg_tokens_per_session is nil, want non-nil", m.ModelID)
					continue
				}
				wantInput := float64(m.Tokens.Input) / float64(m.Sessions)
				if m.AvgTokensPerSession.Input != wantInput {
					t.Errorf("%s avg_tokens_per_session.input = %.2f, want %.2f",
						m.ModelID, m.AvgTokensPerSession.Input, wantInput)
				}
			} else {
				if m.AvgTokensPerSession != nil {
					t.Errorf("%s: avg_tokens_per_session is non-nil, want nil (sessions=0)", m.ModelID)
				}
			}
		}
	})

	t.Run("zero-token message does not panic", func(t *testing.T) {
		// gpt-4 has a zero-token message with no parts
		// Should still be present with valid (zero) tokens and no NaN/Inf
		entry, ok := byID["gpt-4"]
		if !ok {
			t.Fatal("gpt-4 not found in models")
		}
		// gpt-4 has msg-002-02 (800,400,50) + zero-token msg-002-04
		if entry.Tokens.Input != 800 {
			t.Errorf("gpt-4 tokens.input = %d, want 800", entry.Tokens.Input)
		}
		if entry.Messages != 2 {
			t.Errorf("gpt-4 messages = %d, want 2", entry.Messages)
		}
		// Averages should be computed without NaN/Inf
		if entry.AvgTokensPerMessage == nil {
			t.Error("gpt-4 avg_tokens_per_message is nil, want non-nil")
		} else {
			if isNaNOrInf(entry.AvgTokensPerMessage.Input) {
				t.Errorf("gpt-4 avg_tokens_per_message.input is NaN or Inf")
			}
		}
	})
}

func isNaNOrInf(f float64) bool {
	return f != f || f > 1e15 || f < -1e15
}

func TestProjectsWithFixture(t *testing.T) {
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

	projects, err := Projects(ctx, st, PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Projects() failed: %v", err)
	}

	// Should have 3 projects
	if len(projects.Projects) != 3 {
		t.Errorf("len(ProjectStats.Projects) = %d, want 3", len(projects.Projects))
	}

	// Verify project with multiple sessions (opencode-dashboard has 2 sessions)
	for _, p := range projects.Projects {
		if p.ProjectName == "opencode-dashboard" {
			if p.Sessions != 2 {
				t.Errorf("Project %q has %d sessions, want 2", p.ProjectName, p.Sessions)
			}
		}
		if p.ProjectName == "my-app" {
			if p.Sessions != 1 {
				t.Errorf("Project %q has %d sessions, want 1", p.ProjectName, p.Sessions)
			}
		}
	}
}

func TestDailyWithFixture(t *testing.T) {
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

	// Test 1d period (hourly distribution)
	daily1, err := Daily(ctx, st, PeriodQuery{Period: "1d"})
	if err != nil {
		t.Fatalf("Daily(1d) failed: %v", err)
	}

	if len(daily1.Days) != 24 {
		t.Errorf("DailyStats(1d) has %d days, want 24 (hourly buckets)", len(daily1.Days))
	}

	if daily1.Granularity != GranularityHour {
		t.Errorf("DailyStats(1d) granularity = %q, want %q", daily1.Granularity, GranularityHour)
	}

	// Check hourly date format
	for _, hour := range daily1.Days {
		if !strings.Contains(hour.Date, "T") || !strings.HasSuffix(hour.Date, "Z") {
			t.Errorf("Hourly date format incorrect: %s (want YYYY-MM-DDTHH:00:00Z)", hour.Date)
		}
	}

	// Test 7d period
	daily7, err := Daily(ctx, st, PeriodQuery{Period: "7d"})
	if err != nil {
		t.Fatalf("Daily(7d) failed: %v", err)
	}

	if len(daily7.Days) != 7 {
		t.Errorf("DailyStats(7d) has %d days, want 7", len(daily7.Days))
	}

	if daily7.Granularity != GranularityDay {
		t.Errorf("DailyStats(7d) granularity = %q, want %q", daily7.Granularity, GranularityDay)
	}

	// Days should be consecutive and in order
	for i := 1; i < len(daily7.Days); i++ {
		if daily7.Days[i].Date < daily7.Days[i-1].Date {
			t.Errorf("Days not in chronological order: day[%d]=%s < day[%d]=%s",
				i, daily7.Days[i].Date, i-1, daily7.Days[i-1].Date)
		}
	}

	// Test 30d period
	daily30, err := Daily(ctx, st, PeriodQuery{Period: "30d"})
	if err != nil {
		t.Fatalf("Daily(30d) failed: %v", err)
	}

	if len(daily30.Days) != 30 {
		t.Errorf("DailyStats(30d) has %d days, want 30", len(daily30.Days))
	}

	// Test 1y period
	daily1Y, err := Daily(ctx, st, PeriodQuery{Period: "1y"})
	if err != nil {
		t.Fatalf("Daily(1y) failed: %v", err)
	}

	if len(daily1Y.Days) != 365 {
		t.Errorf("DailyStats(1y) has %d days, want 365", len(daily1Y.Days))
	}

	// Test all historic period
	dailyAll, err := Daily(ctx, st, PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Daily(all) failed: %v", err)
	}

	if len(dailyAll.Days) != 7 {
		t.Errorf("DailyStats(all) has %d days, want 7", len(dailyAll.Days))
	}

	// Test invalid period
	_, err = Daily(ctx, st, PeriodQuery{Period: "invalid"})
	if err == nil {
		t.Error("Daily(invalid) should return error")
	}
}

// TestDailyWithExplicitGranularity validates granularity=day overrides auto-hour for 1d.
func TestDailyWithExplicitGranularity(t *testing.T) {
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

	// 1d with explicit granularity=day → 1 daily bucket
	dailyDay, err := Daily(ctx, st, PeriodQuery{Period: "1d"}, GranularityDay)
	if err != nil {
		t.Fatalf("Daily(1d, GranularityDay) failed: %v", err)
	}
	if len(dailyDay.Days) != 1 {
		t.Errorf("Daily(1d, GranularityDay) has %d entries, want 1", len(dailyDay.Days))
	}
	if dailyDay.Granularity != GranularityDay {
		t.Errorf("Daily(1d, GranularityDay) granularity = %q, want %q", dailyDay.Granularity, GranularityDay)
	}
	if strings.Contains(dailyDay.Days[0].Date, "T") {
		t.Errorf("Daily(1d, GranularityDay) date format = %q, want YYYY-MM-DD (no T hour precision)", dailyDay.Days[0].Date)
	}

	// 7d with explicit granularity=hour → 168 hourly buckets (7d × 24h)
	dailyHour, err := Daily(ctx, st, PeriodQuery{Period: "7d"}, GranularityHour)
	if err != nil {
		t.Fatalf("Daily(7d, GranularityHour) failed: %v", err)
	}
	if len(dailyHour.Days) != 168 {
		t.Errorf("Daily(7d, GranularityHour) has %d entries, want 168 (7d × 24h)", len(dailyHour.Days))
	}
	if dailyHour.Granularity != GranularityHour {
		t.Errorf("Daily(7d, GranularityHour) granularity = %q, want %q", dailyHour.Granularity, GranularityHour)
	}
	for _, h := range dailyHour.Days {
		if !strings.Contains(h.Date, "T") || !strings.HasSuffix(h.Date, "Z") {
			t.Errorf("Hourly date format incorrect: %s (want YYYY-MM-DDTHH:00:00Z)", h.Date)
		}
	}
	// Verify the hourly entries span multiple days (not just today)
	if len(dailyHour.Days) >= 48 { // at least 2 days
		firstDate := dailyHour.Days[0].Date
		lastDate := dailyHour.Days[len(dailyHour.Days)-1].Date
		if firstDate >= lastDate {
			t.Errorf("Hourly entries should span multiple days: first=%s, last=%s", firstDate, lastDate)
		}
	}
	// Verify each hour bucket has a unique key (no duplicates)
	seen := make(map[string]bool)
	for _, h := range dailyHour.Days {
		if seen[h.Date] {
			t.Errorf("Duplicate hour bucket: %s", h.Date)
		}
		seen[h.Date] = true
	}

	// 1d with granularity=hour (same as default for 1d) → 24 hourly
	dailyHour1d, err := Daily(ctx, st, PeriodQuery{Period: "1d"}, GranularityHour)
	if err != nil {
		t.Fatalf("Daily(1d, GranularityHour) failed: %v", err)
	}
	if len(dailyHour1d.Days) != 24 {
		t.Errorf("Daily(1d, GranularityHour) has %d entries, want 24", len(dailyHour1d.Days))
	}
	if dailyHour1d.Granularity != GranularityHour {
		t.Errorf("Daily(1d, GranularityHour) granularity = %q, want %q", dailyHour1d.Granularity, GranularityHour)
	}
}

// TestDailyDimensionWithFixture validates the dimension-grouped daily endpoint.
func TestDailyDimensionWithFixture(t *testing.T) {
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

	// Test model dimension
	result, err := DailyDimension(ctx, st, "model", PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("DailyDimension(model, all) failed: %v", err)
	}

	if result.Dimension != "model" {
		t.Errorf("DailyDimension.Dimension = %q, want %q", result.Dimension, "model")
	}
	if result.Period != "all" {
		t.Errorf("DailyDimension.Period = %q, want %q", result.Period, "all")
	}
	if len(result.Days) == 0 {
		t.Error("DailyDimension(model, all) returned 0 days, want > 0")
	}
	// Days should not be nil (empty slice, not null)
	if result.Days == nil {
		t.Error("DailyDimension.Days is nil, want empty slice []")
	}

	// Verify each entry has expected fields
	for _, day := range result.Days {
		if day.Date == "" {
			t.Error("DimensionDayStats.Date is empty")
		}
		if day.Dimension == "" {
			t.Error("DimensionDayStats.Dimension is empty")
		}
		if day.Sessions < 0 {
			t.Errorf("DimensionDayStats.Sessions = %d, want >= 0", day.Sessions)
		}
	}

	// Test tool dimension
	toolResult, err := DailyDimension(ctx, st, "tool", PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("DailyDimension(tool, all) failed: %v", err)
	}
	if toolResult.Dimension != "tool" {
		t.Errorf("DailyDimension.Dimension = %q, want %q", toolResult.Dimension, "tool")
	}

	// Test project dimension
	projResult, err := DailyDimension(ctx, st, "project", PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("DailyDimension(project, all) failed: %v", err)
	}
	if projResult.Dimension != "project" {
		t.Errorf("DailyDimension.Dimension = %q, want %q", projResult.Dimension, "project")
	}
}

// TestDailyDimensionInvalidDimension validates error handling for bad dimensions.
func TestDailyDimensionInvalidDimension(t *testing.T) {
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

	invalidDims := []string{"invalid", "", "  ", "MODEL"}
	for _, dim := range invalidDims {
		t.Run("dimension_"+dim, func(t *testing.T) {
			_, err := DailyDimension(ctx, st, dim, PeriodQuery{Period: "7d"})
			if err == nil {
				t.Errorf("DailyDimension(%q, 7d) should return error", dim)
			}
		})
	}
}

// TestProjectByIDWithFixture validates project detail endpoint.
func TestProjectByIDWithFixture(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	now := time.Now().UTC()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create custom fixture with string project IDs, matching the real store schema.
	b := fixture.NewBuilder()
	b.AddProject(fixture.NewProject("proj-alpha-123abc", "/test/alpha").Name("test-alpha"))

	s1 := fixture.NewSession("s-001", "proj-alpha-123abc").
		Title("Session one").
		CreatedAt(now.Add(-2 * time.Hour)).
		UpdatedAt(now.Add(-1 * time.Hour))
	msg1 := fixture.NewMessage("m-001", "s-001", "assistant").
		CreatedAt(now.Add(-2*time.Hour)).
		Cost(0.05).ModelID("gpt-4").ProviderID("openai").Tokens(500, 200, 50, 100, 50)
	s1.AddMessage(msg1)
	b.AddSession(s1)

	s2 := fixture.NewSession("s-002", "proj-alpha-123abc").
		Title("Session two").
		CreatedAt(now.Add(-3 * time.Hour)).
		UpdatedAt(now.Add(-2 * time.Hour))
	msg2 := fixture.NewMessage("m-002", "s-002", "assistant").
		CreatedAt(now.Add(-3*time.Hour)).
		Cost(0.03).ModelID("gpt-3.5-turbo").ProviderID("openai").Tokens(300, 100, 20, 0, 0)
	s2.AddMessage(msg2)
	b.AddSession(s2)

	dbPath, err := b.Build(ctx)
	if err != nil {
		t.Fatalf("Failed to create custom fixture: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(dbPath))

	st, err := store.Connect(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to connect to fixture: %v", err)
	}
	defer st.Close()

	// Test happy path with a string project ID.
	detail, err := ProjectByID(ctx, st, "proj-alpha-123abc", PeriodQuery{Period: "all"}, 1, 10)
	if err != nil {
		t.Fatalf("ProjectByID(proj-alpha-123abc, all) failed: %v", err)
	}
	if detail == nil {
		t.Fatal("ProjectByID returned nil, want project detail")
	}
	if detail.ProjectName != "test-alpha" {
		t.Errorf("ProjectDetail.ProjectName = %q, want %q", detail.ProjectName, "test-alpha")
	}
	if detail.TotalSessions != 2 {
		t.Errorf("ProjectDetail.TotalSessions = %d, want 2", detail.TotalSessions)
	}
	if detail.Sessions != 2 {
		t.Errorf("ProjectDetail.Sessions = %d, want 2", detail.Sessions)
	}

	// Recent sessions should be populated (2 sessions, limit 10)
	if detail.RecentSessions == nil {
		t.Error("ProjectDetail.RecentSessions is nil, want slice")
	}
	if len(detail.RecentSessions) != 2 {
		t.Errorf("ProjectDetail has %d recent sessions, want 2", len(detail.RecentSessions))
	}
	for _, s := range detail.RecentSessions {
		if s.ID == "" {
			t.Error("Recent session has empty ID")
		}
		if s.TimeCreated.IsZero() {
			t.Error("Recent session has zero TimeCreated")
		}
	}

	// Test 404 for non-existent project
	missing, err := ProjectByID(ctx, st, "does-not-exist", PeriodQuery{Period: "7d"}, 1, 10)
	if err != nil {
		t.Fatalf("ProjectByID(does-not-exist) unexpected error: %v", err)
	}
	if missing != nil {
		t.Error("ProjectByID(does-not-exist) returned non-nil, want nil (404)")
	}
}

// TestSessionQueryFilter validates SessionQuery.Filter and ProjectID fields.
func TestSessionQueryFilter(t *testing.T) {
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

	// Test without filter — should return all sessions
	allList, err := SessionsWithQuery(ctx, st, SessionQuery{
		Page:     1,
		PageSize: 10,
		Sort:     SessionSortNewest,
	})
	if err != nil {
		t.Fatalf("SessionsWithQuery(no filter) failed: %v", err)
	}
	if allList.Total != 4 {
		t.Errorf("SessionsWithQuery(no filter).Total = %d, want 4", allList.Total)
	}

	// Test with filter matching a session title
	filterList, err := SessionsWithQuery(ctx, st, SessionQuery{
		Page:     1,
		PageSize: 10,
		Filter:   "auth",
		Sort:     SessionSortNewest,
	})
	if err != nil {
		t.Fatalf("SessionsWithQuery(filter=auth) failed: %v", err)
	}
	if filterList.Total < 1 {
		t.Errorf("SessionsWithQuery(filter=auth).Total = %d, want >= 1", filterList.Total)
	}

	// Test with filter that matches nothing
	noMatch, err := SessionsWithQuery(ctx, st, SessionQuery{
		Page:     1,
		PageSize: 10,
		Filter:   "zzz_nonexistent",
		Sort:     SessionSortNewest,
	})
	if err != nil {
		t.Fatalf("SessionsWithQuery(filter=zzz) failed: %v", err)
	}
	if noMatch.Total != 0 {
		t.Errorf("SessionsWithQuery(filter=zzz).Total = %d, want 0", noMatch.Total)
	}
	if len(noMatch.Sessions) != 0 {
		t.Errorf("SessionsWithQuery(filter=zzz) returned %d sessions, want 0", len(noMatch.Sessions))
	}
}

// TestSessionQueryProjectID validates the ProjectID filter on sessions.
func TestSessionQueryProjectID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	now := time.Now().UTC()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create custom fixture with string project IDs, matching the real store schema.
	b := fixture.NewBuilder()
	b.AddProject(fixture.NewProject("proj-alpha", "/test/alpha").Name("alpha"))
	b.AddProject(fixture.NewProject("proj-beta", "/test/beta").Name("beta"))

	s1 := fixture.NewSession("s-001", "proj-alpha").Title("Alpha session 1").
		CreatedAt(now.Add(-1 * time.Hour)).UpdatedAt(now)
	s1.AddMessage(fixture.NewMessage("m-001", "s-001", "assistant").
		CreatedAt(now.Add(-1*time.Hour)).Cost(0.05).ModelID("gpt-4").ProviderID("openai").Tokens(500, 200, 50, 100, 50))
	b.AddSession(s1)

	s2 := fixture.NewSession("s-002", "proj-alpha").Title("Alpha session 2").
		CreatedAt(now.Add(-2 * time.Hour)).UpdatedAt(now.Add(-1 * time.Hour))
	s2.AddMessage(fixture.NewMessage("m-002", "s-002", "assistant").
		CreatedAt(now.Add(-2*time.Hour)).Cost(0.03).ModelID("gpt-4").ProviderID("openai").Tokens(300, 100, 20, 0, 0))
	b.AddSession(s2)

	s3 := fixture.NewSession("s-003", "proj-beta").Title("Beta session 1").
		CreatedAt(now.Add(-3 * time.Hour)).UpdatedAt(now.Add(-2 * time.Hour))
	s3.AddMessage(fixture.NewMessage("m-003", "s-003", "assistant").
		CreatedAt(now.Add(-3*time.Hour)).Cost(0.02).ModelID("gpt-3.5-turbo").ProviderID("openai").Tokens(200, 100, 10, 0, 0))
	b.AddSession(s3)

	dbPath, err := b.Build(ctx)
	if err != nil {
		t.Fatalf("Failed to create custom fixture: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(dbPath))

	st, err := store.Connect(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to connect to fixture: %v", err)
	}
	defer st.Close()

	// Filter by alpha project ID — should get 2 sessions.
	filtered, err := SessionsWithQuery(ctx, st, SessionQuery{
		Page:      1,
		PageSize:  10,
		ProjectID: "proj-alpha",
		Sort:      SessionSortNewest,
	})
	if err != nil {
		t.Fatalf("SessionsWithQuery(projectID=proj-alpha) failed: %v", err)
	}
	if filtered.Total != 2 {
		t.Errorf("SessionsWithQuery(projectID=proj-alpha).Total = %d, want 2", filtered.Total)
	}
	if len(filtered.Sessions) != 2 {
		t.Errorf("len(SessionsWithQuery(projectID=proj-alpha).Sessions) = %d, want 2", len(filtered.Sessions))
	}
	for _, s := range filtered.Sessions {
		if s.ProjectID != "proj-alpha" {
			t.Errorf("Session %q has ProjectID=%q, want %q", s.ID, s.ProjectID, "proj-alpha")
		}
	}

	// Filter by beta project ID — should get 1 session.
	filtered2, err := SessionsWithQuery(ctx, st, SessionQuery{
		Page:      1,
		PageSize:  10,
		ProjectID: "proj-beta",
		Sort:      SessionSortNewest,
	})
	if err != nil {
		t.Fatalf("SessionsWithQuery(projectID=proj-beta) failed: %v", err)
	}
	if filtered2.Total != 1 {
		t.Errorf("SessionsWithQuery(projectID=proj-beta).Total = %d, want 1", filtered2.Total)
	}

	// Filter by non-existent project — should get 0
	filtered0, err := SessionsWithQuery(ctx, st, SessionQuery{
		Page:      1,
		PageSize:  10,
		ProjectID: "proj-missing",
		Sort:      SessionSortNewest,
	})
	if err != nil {
		t.Fatalf("SessionsWithQuery(projectID=proj-missing) failed: %v", err)
	}
	if filtered0.Total != 0 {
		t.Errorf("SessionsWithQuery(projectID=proj-missing).Total = %d, want 0", filtered0.Total)
	}
}

// TestToolsSQLMatchLegacy validates that toolsSQL and toolsLegacy produce identical results.
func TestToolsSQLMatchLegacy(t *testing.T) {
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

	// Compute period window to get startMs/endMs for direct calls
	pw, err := ComputePeriodWindow(ctx, st, "all")
	if err != nil {
		t.Fatalf("ComputePeriodWindow(all) failed: %v", err)
	}

	// Run both paths directly
	sqlResult, err := toolsSQL(ctx, st, pw.StartMs, pw.EndMs)
	if err != nil {
		t.Fatalf("toolsSQL() failed: %v", err)
	}

	legacyResult, err := toolsLegacy(ctx, st, pw.StartMs, pw.EndMs)
	if err != nil {
		t.Fatalf("toolsLegacy() failed: %v", err)
	}

	// Compare counts
	if len(sqlResult.Tools) != len(legacyResult.Tools) {
		t.Errorf("toolsSQL has %d tools, toolsLegacy has %d tools", len(sqlResult.Tools), len(legacyResult.Tools))
	}

	// Build lookup by tool name for legacy results
	legacyByTool := make(map[string]ToolEntry)
	for _, t := range legacyResult.Tools {
		legacyByTool[t.Name] = t
	}

	// Compare each tool from SQL path against legacy
	for _, sqlTool := range sqlResult.Tools {
		legacyTool, ok := legacyByTool[sqlTool.Name]
		if !ok {
			t.Errorf("Tool %q exists in SQL but not in legacy path", sqlTool.Name)
			continue
		}
		if sqlTool.Invocations != legacyTool.Invocations {
			t.Errorf("Tool %q: SQL invocations=%d, legacy invocations=%d", sqlTool.Name, sqlTool.Invocations, legacyTool.Invocations)
		}
		if sqlTool.Successes != legacyTool.Successes {
			t.Errorf("Tool %q: SQL successes=%d, legacy successes=%d", sqlTool.Name, sqlTool.Successes, legacyTool.Successes)
		}
		if sqlTool.Failures != legacyTool.Failures {
			t.Errorf("Tool %q: SQL failures=%d, legacy failures=%d", sqlTool.Name, sqlTool.Failures, legacyTool.Failures)
		}
		if sqlTool.Sessions != legacyTool.Sessions {
			t.Errorf("Tool %q: SQL sessions=%d, legacy sessions=%d", sqlTool.Name, sqlTool.Sessions, legacyTool.Sessions)
		}
	}
}

func TestDailyHourlyNoInflationForRolling(t *testing.T) {
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

	tests := []struct {
		period   string
		minHours int
		maxHours int
	}{
		{period: "1h", minHours: 1, maxHours: 2},
		{period: "6h", minHours: 6, maxHours: 7},
		{period: "12h", minHours: 12, maxHours: 13},
		{period: "24h", minHours: 24, maxHours: 25},
		{period: "72h", minHours: 72, maxHours: 73},
	}

	for _, tt := range tests {
		t.Run(tt.period, func(t *testing.T) {
			stats, err := Daily(ctx, st, PeriodQuery{Period: tt.period})
			if err != nil {
				t.Fatalf("Daily(%q) failed: %v", tt.period, err)
			}
			got := len(stats.Days)
			if got < tt.minHours || got > tt.maxHours {
				t.Errorf("Daily(%q) returned %d buckets, want between %d and %d", tt.period, got, tt.minHours, tt.maxHours)
			}
		})
	}
}

func TestDailyHourlyUTCKeysAreRealUTC(t *testing.T) {
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

	stats, err := Daily(ctx, st, PeriodQuery{Period: "1d"})
	if err != nil {
		t.Fatalf("Daily(1d) failed: %v", err)
	}

	if len(stats.Days) != 24 {
		t.Fatalf("Daily(1d) returned %d buckets, want 24", len(stats.Days))
	}

	for _, h := range stats.Days {
		parsed, err := time.Parse(time.RFC3339, h.Date)
		if err != nil {
			t.Errorf("Failed to parse date %q as RFC3339: %v", h.Date, err)
			continue
		}
		if parsed.Location() != time.UTC {
			t.Errorf("Date %q parsed location = %v, want UTC", h.Date, parsed.Location())
		}
	}
}

func TestDailyHourlyDayPresetWindow(t *testing.T) {
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

	tests := []struct {
		period    string
		wantHours int
	}{
		{period: "7d", wantHours: 168},
		{period: "14d", wantHours: 336},
		{period: "30d", wantHours: 720},
	}

	for _, tt := range tests {
		t.Run(tt.period, func(t *testing.T) {
			stats, err := Daily(ctx, st, PeriodQuery{Period: tt.period}, GranularityHour)
			if err != nil {
				t.Fatalf("Daily(%q, GranularityHour) failed: %v", tt.period, err)
			}
			got := len(stats.Days)
			if got != tt.wantHours {
				t.Errorf("Daily(%q, GranularityHour) returned %d buckets, want %d", tt.period, got, tt.wantHours)
			}
		})
	}
}

func TestDailyHourlyExplicitRangeUTC(t *testing.T) {
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

	stats, err := Daily(ctx, st, PeriodQuery{
		From: "2026-01-15",
		To:   "2026-01-17",
	}, GranularityHour)
	if err != nil {
		t.Fatalf("Daily(explicit range, GranularityHour) failed: %v", err)
	}

	if len(stats.Days) != 72 {
		t.Errorf("Daily(explicit 3d) returned %d buckets, want 72", len(stats.Days))
	}

	for _, h := range stats.Days {
		parsed, err := time.Parse(time.RFC3339, h.Date)
		if err != nil {
			t.Errorf("Failed to parse date %q as RFC3339: %v", h.Date, err)
			continue
		}
		if parsed.Location() != time.UTC {
			t.Errorf("Date %q parsed location = %v, want UTC", h.Date, parsed.Location())
		}
	}
}
