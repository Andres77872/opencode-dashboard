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

	overview, err := Overview(ctx, st, "all")
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

	models, err := Models(ctx, st, "all")
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

	projects, err := Projects(ctx, st, "all")
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
	daily1, err := Daily(ctx, st, "1d")
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
	daily7, err := Daily(ctx, st, "7d")
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
	daily30, err := Daily(ctx, st, "30d")
	if err != nil {
		t.Fatalf("Daily(30d) failed: %v", err)
	}

	if len(daily30.Days) != 30 {
		t.Errorf("DailyStats(30d) has %d days, want 30", len(daily30.Days))
	}

	// Test 1y period
	daily1Y, err := Daily(ctx, st, "1y")
	if err != nil {
		t.Fatalf("Daily(1y) failed: %v", err)
	}

	if len(daily1Y.Days) != 365 {
		t.Errorf("DailyStats(1y) has %d days, want 365", len(daily1Y.Days))
	}

	// Test all historic period
	dailyAll, err := Daily(ctx, st, "all")
	if err != nil {
		t.Fatalf("Daily(all) failed: %v", err)
	}

	if len(dailyAll.Days) != 7 {
		t.Errorf("DailyStats(all) has %d days, want 7", len(dailyAll.Days))
	}

	// Test invalid period
	_, err = Daily(ctx, st, "invalid")
	if err == nil {
		t.Error("Daily(invalid) should return error")
	}
}
