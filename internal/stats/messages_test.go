package stats

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"opencode-dashboard/internal/store"
	"opencode-dashboard/internal/store/fixture"
)

// TestMessagesByPeriod tests the MessagesByPeriod query with various periods.
func TestMessagesByPeriod(t *testing.T) {
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
		name          string
		period        string
		page          int
		limit         int
		wantMinCount  int64
		wantMaxCount  int64
		checkOrdering bool
	}{
		{
			name:          "1d period returns recent messages",
			period:        "1d",
			page:          1,
			limit:         50,
			wantMinCount:  2, // Session 3 + Session 4 messages from today
			wantMaxCount:  10,
			checkOrdering: true,
		},
		{
			name:          "7d period returns messages from last week",
			period:        "7d",
			page:          1,
			limit:         50,
			wantMinCount:  8, // All messages across 4 sessions
			wantMaxCount:  20,
			checkOrdering: true,
		},
		{
			name:          "30d period returns same as 7d for fixture",
			period:        "30d",
			page:          1,
			limit:         50,
			wantMinCount:  8,
			wantMaxCount:  20,
			checkOrdering: true,
		},
		{
			name:          "all period returns all messages",
			period:        "all",
			page:          1,
			limit:         50,
			wantMinCount:  8,
			wantMaxCount:  20,
			checkOrdering: true,
		},
		{
			name:          "small limit returns limited messages",
			period:        "7d",
			page:          1,
			limit:         3,
			wantMinCount:  8, // Total count in database (12 messages in fixture)
			wantMaxCount:  20,
			checkOrdering: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list, err := MessagesByPeriod(ctx, st, tt.period, tt.page, tt.limit, DefaultMessageSort())
			if err != nil {
				t.Fatalf("MessagesByPeriod(%s) failed: %v", tt.period, err)
			}

			// Validate total count
			if list.Total < tt.wantMinCount {
				t.Errorf("MessagesByPeriod(%s).Total = %d, want >= %d", tt.period, list.Total, tt.wantMinCount)
			}
			if list.Total > tt.wantMaxCount {
				t.Errorf("MessagesByPeriod(%s).Total = %d, want <= %d", tt.period, list.Total, tt.wantMaxCount)
			}

			// Validate page size
			if list.PageSize != tt.limit {
				t.Errorf("MessagesByPeriod.PageSize = %d, want %d", list.PageSize, tt.limit)
			}

			// Validate page number
			if list.Page != tt.page {
				t.Errorf("MessagesByPeriod.Page = %d, want %d", list.Page, tt.page)
			}

			// Validate actual messages count (should be <= limit)
			if len(list.Messages) > tt.limit {
				t.Errorf("len(Messages) = %d, want <= %d (limit)", len(list.Messages), tt.limit)
			}

			// Validate ordering (time_created descending)
			if tt.checkOrdering && len(list.Messages) > 1 {
				for i := 1; i < len(list.Messages); i++ {
					if list.Messages[i].TimeCreated.After(list.Messages[i-1].TimeCreated) {
						t.Errorf("Messages not ordered by time_created DESC: msg[%d] @ %v > msg[%d] @ %v",
							i, list.Messages[i].TimeCreated, i-1, list.Messages[i-1].TimeCreated)
					}
				}
			}

			// Validate message fields are populated
			for _, msg := range list.Messages {
				if msg.ID == "" {
					t.Error("MessageEntry.ID is empty")
				}
				if msg.SessionID == "" {
					t.Error("MessageEntry.SessionID is empty")
				}
				if msg.Role == "" {
					t.Error("MessageEntry.Role is empty")
				}
				if msg.TimeCreated.IsZero() {
					t.Error("MessageEntry.TimeCreated is zero")
				}

				// Assistant messages should have cost/tokens/model info
				if msg.Role == "assistant" {
					if msg.Cost <= 0 {
						t.Errorf("Assistant message %s has Cost = %.6f, want > 0", msg.ID, msg.Cost)
					}
					if msg.Tokens == nil {
						t.Errorf("Assistant message %s has nil Tokens", msg.ID)
					} else {
						if msg.Tokens.Input <= 0 {
							t.Errorf("Assistant message %s Tokens.Input = %d, want > 0", msg.ID, msg.Tokens.Input)
						}
						if msg.Tokens.Output <= 0 {
							t.Errorf("Assistant message %s Tokens.Output = %d, want > 0", msg.ID, msg.Tokens.Output)
						}
					}
					if msg.ModelID == "" {
						t.Errorf("Assistant message %s has empty ModelID", msg.ID)
					}
					if msg.ProviderID == "" {
						t.Errorf("Assistant message %s has empty ProviderID", msg.ID)
					}
				}

				// User messages should NOT have cost/tokens/model info (only role)
				if msg.Role == "user" {
					if msg.Cost != 0 {
						t.Errorf("User message %s has Cost = %.6f, want 0", msg.ID, msg.Cost)
					}
					if msg.Tokens != nil {
						t.Errorf("User message %s has non-nil Tokens, want nil", msg.ID)
					}
				}
			}
		})
	}
}

// TestMessagesByPeriodPagination tests pagination correctness.
func TestMessagesByPeriodPagination(t *testing.T) {
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

	// Get all messages to know total count
	allList, err := MessagesByPeriod(ctx, st, "all", 1, 100, DefaultMessageSort())
	if err != nil {
		t.Fatalf("MessagesByPeriod(all) failed: %v", err)
	}

	totalCount := allList.Total

	// Test pagination with limit=2
	limit := 2

	page1, err := MessagesByPeriod(ctx, st, "all", 1, limit, DefaultMessageSort())
	if err != nil {
		t.Fatalf("MessagesByPeriod(page=1) failed: %v", err)
	}

	if page1.Total != totalCount {
		t.Errorf("Page1.Total = %d, want %d (same across all pages)", page1.Total, totalCount)
	}

	if len(page1.Messages) != limit {
		t.Errorf("len(Page1.Messages) = %d, want %d (limit)", len(page1.Messages), limit)
	}

	// If there are enough messages, test page 2
	if totalCount > int64(limit) {
		page2, err := MessagesByPeriod(ctx, st, "all", 2, limit, DefaultMessageSort())
		if err != nil {
			t.Fatalf("MessagesByPeriod(page=2) failed: %v", err)
		}

		if page2.Total != totalCount {
			t.Errorf("Page2.Total = %d, want %d (same across all pages)", page2.Total, totalCount)
		}

		if len(page2.Messages) > limit {
			t.Errorf("len(Page2.Messages) = %d, want <= %d (limit)", len(page2.Messages), limit)
		}

		// Verify page 2 messages are different from page 1
		if len(page2.Messages) > 0 {
			for _, p2Msg := range page2.Messages {
				for _, p1Msg := range page1.Messages {
					if p2Msg.ID == p1Msg.ID {
						t.Errorf("Page 2 contains same message ID %s as Page 1", p2Msg.ID)
					}
				}
			}
		}
	}
}

// TestMessagesByPeriodEmpty tests that empty periods return empty list, not error.
func TestMessagesByPeriodEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create empty fixture (no sessions/messages)
	dbPath, err := fixture.NewBuilder().Build(ctx)
	if err != nil {
		t.Fatalf("Failed to create empty fixture: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(dbPath))

	st, err := store.Connect(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to connect to empty fixture: %v", err)
	}
	defer st.Close()

	list, err := MessagesByPeriod(ctx, st, "7d", 1, 50, DefaultMessageSort())
	if err != nil {
		t.Fatalf("MessagesByPeriod on empty database should not error, got: %v", err)
	}

	if list.Total != 0 {
		t.Errorf("Empty database Total = %d, want 0", list.Total)
	}

	if len(list.Messages) != 0 {
		t.Errorf("Empty database returned %d messages, want 0", len(list.Messages))
	}

	// Messages array should be empty slice, not nil
	if list.Messages == nil {
		t.Error("Empty database Messages is nil, want empty slice []")
	}
}

// TestMessagesByPeriodInvalid tests invalid period handling.
func TestMessagesByPeriodInvalid(t *testing.T) {
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

	invalidPeriods := []string{"", "invalid", "14d", "7", "seven"}

	for _, period := range invalidPeriods {
		t.Run("period_"+period, func(t *testing.T) {
			_, err := MessagesByPeriod(ctx, st, period, 1, 50, DefaultMessageSort())
			if err == nil {
				t.Errorf("MessagesByPeriod(%q) should return error, got nil", period)
			}
		})
	}
}

// TestMessageByIDWithTextParts tests message detail with text parts.
func TestMessageByIDWithTextParts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create custom fixture with text parts
	now := time.Now().UTC()
	b := fixture.NewBuilder()

	b.AddProject(fixture.NewProject("proj-001", "/test").Name("test-project"))

	s1 := fixture.NewSession("ses-001", "proj-001").
		Title("Test session").
		CreatedAt(now).
		UpdatedAt(now)

	msg1 := fixture.NewMessage("msg-001", "ses-001", "user").CreatedAt(now)
	s1.AddMessage(msg1)

	msg2 := fixture.NewMessage("msg-002", "ses-001", "assistant").
		CreatedAt(now.Add(1*time.Minute)).
		Cost(0.05).
		ModelID("claude-3-sonnet").
		ProviderID("anthropic").
		Tokens(1000, 500, 100)
	s1.AddMessage(msg2)

	b.AddSession(s1)

	// Add text parts for msg-002
	b.AddPart(fixture.NewPart("part-001", "ses-001",
		fmt.Sprintf(`{"type":"text","text":"This is the assistant response text."}`)).
		MessageID("msg-002"))

	b.AddPart(fixture.NewPart("part-002", "ses-001",
		fmt.Sprintf(`{"type":"text","text":"Additional text content here."}`)).
		MessageID("msg-002"))

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

	detail, err := MessageByID(ctx, st, "msg-002")
	if err != nil {
		t.Fatalf("MessageByID(msg-002) failed: %v", err)
	}

	if detail == nil {
		t.Fatal("MessageByID returned nil, want message detail")
	}

	// Validate metadata
	if detail.ID != "msg-002" {
		t.Errorf("MessageDetail.ID = %s, want msg-002", detail.ID)
	}

	if detail.SessionID != "ses-001" {
		t.Errorf("MessageDetail.SessionID = %s, want ses-001", detail.SessionID)
	}

	if detail.Role != "assistant" {
		t.Errorf("MessageDetail.Role = %s, want assistant", detail.Role)
	}

	if detail.Cost != 0.05 {
		t.Errorf("MessageDetail.Cost = %.6f, want 0.05", detail.Cost)
	}

	if detail.ModelID != "claude-3-sonnet" {
		t.Errorf("MessageDetail.ModelID = %s, want claude-3-sonnet", detail.ModelID)
	}

	// Validate text parts
	if len(detail.Content.TextParts) != 2 {
		t.Errorf("MessageDetail.Content.TextParts has %d parts, want 2", len(detail.Content.TextParts))
	}

	for i, part := range detail.Content.TextParts {
		if part.Type != "text" {
			t.Errorf("TextPart[%d].Type = %s, want 'text'", i, part.Type)
		}
		if part.Text == "" {
			t.Errorf("TextPart[%d].Text is empty", i)
		}
	}

	// Should have no reasoning parts
	if len(detail.Content.ReasoningParts) != 0 {
		t.Errorf("MessageDetail.Content.ReasoningParts has %d parts, want 0", len(detail.Content.ReasoningParts))
	}
}

// TestMessageByIDWithReasoningParts tests message detail with reasoning parts.
func TestMessageByIDWithReasoningParts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	now := time.Now().UTC()
	b := fixture.NewBuilder()

	b.AddProject(fixture.NewProject("proj-001", "/test").Name("test-project"))

	s1 := fixture.NewSession("ses-001", "proj-001").
		Title("Test session").
		CreatedAt(now).
		UpdatedAt(now)

	msg1 := fixture.NewMessage("msg-001", "ses-001", "assistant").
		CreatedAt(now).
		Cost(0.08).
		ModelID("claude-3-sonnet").
		ProviderID("anthropic").
		Tokens(1500, 600, 200)
	s1.AddMessage(msg1)

	b.AddSession(s1)

	// Add reasoning parts only
	b.AddPart(fixture.NewPart("part-001", "ses-001",
		fmt.Sprintf(`{"type":"reasoning","text":"Thinking about the best approach..."}`)).
		MessageID("msg-001"))

	b.AddPart(fixture.NewPart("part-002", "ses-001",
		fmt.Sprintf(`{"type":"reasoning","text":"Considering edge cases..."}`)).
		MessageID("msg-001"))

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

	detail, err := MessageByID(ctx, st, "msg-001")
	if err != nil {
		t.Fatalf("MessageByID(msg-001) failed: %v", err)
	}

	if detail == nil {
		t.Fatal("MessageByID returned nil, want message detail")
	}

	// Should have no text parts
	if len(detail.Content.TextParts) != 0 {
		t.Errorf("MessageDetail.Content.TextParts has %d parts, want 0", len(detail.Content.TextParts))
	}

	// Should have 2 reasoning parts
	if len(detail.Content.ReasoningParts) != 2 {
		t.Errorf("MessageDetail.Content.ReasoningParts has %d parts, want 2", len(detail.Content.ReasoningParts))
	}

	for i, part := range detail.Content.ReasoningParts {
		if part.Type != "reasoning" {
			t.Errorf("ReasoningPart[%d].Type = %s, want 'reasoning'", i, part.Type)
		}
		if part.Text == "" {
			t.Errorf("ReasoningPart[%d].Text is empty", i)
		}
	}
}

// TestMessageByIDWithBothTextAndReasoning tests message detail with both text and reasoning.
func TestMessageByIDWithBothTextAndReasoning(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	now := time.Now().UTC()
	b := fixture.NewBuilder()

	b.AddProject(fixture.NewProject("proj-001", "/test").Name("test-project"))

	s1 := fixture.NewSession("ses-001", "proj-001").
		Title("Test session").
		CreatedAt(now).
		UpdatedAt(now)

	msg1 := fixture.NewMessage("msg-001", "ses-001", "assistant").
		CreatedAt(now).
		Cost(0.12).
		ModelID("claude-3-sonnet").
		ProviderID("anthropic").
		Tokens(2000, 800, 300)
	s1.AddMessage(msg1)

	b.AddSession(s1)

	// Add reasoning part
	b.AddPart(fixture.NewPart("part-001", "ses-001",
		fmt.Sprintf(`{"type":"reasoning","text":"Analyzing the user's request..."}`)).
		MessageID("msg-001"))

	// Add text parts
	b.AddPart(fixture.NewPart("part-002", "ses-001",
		fmt.Sprintf(`{"type":"text","text":"Here's my response."}`)).
		MessageID("msg-001"))

	b.AddPart(fixture.NewPart("part-003", "ses-001",
		fmt.Sprintf(`{"type":"text","text":"Additional context."}`)).
		MessageID("msg-001"))

	// Add another reasoning part
	b.AddPart(fixture.NewPart("part-004", "ses-001",
		fmt.Sprintf(`{"type":"reasoning","text":"Verifying the solution..."}`)).
		MessageID("msg-001"))

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

	detail, err := MessageByID(ctx, st, "msg-001")
	if err != nil {
		t.Fatalf("MessageByID(msg-001) failed: %v", err)
	}

	if detail == nil {
		t.Fatal("MessageByID returned nil, want message detail")
	}

	// Should have 2 text parts
	if len(detail.Content.TextParts) != 2 {
		t.Errorf("MessageDetail.Content.TextParts has %d parts, want 2", len(detail.Content.TextParts))
	}

	// Should have 2 reasoning parts
	if len(detail.Content.ReasoningParts) != 2 {
		t.Errorf("MessageDetail.Content.ReasoningParts has %d parts, want 2", len(detail.Content.ReasoningParts))
	}

	// Validate text parts
	for i, part := range detail.Content.TextParts {
		if part.Type != "text" {
			t.Errorf("TextPart[%d].Type = %s, want 'text'", i, part.Type)
		}
	}

	// Validate reasoning parts
	for i, part := range detail.Content.ReasoningParts {
		if part.Type != "reasoning" {
			t.Errorf("ReasoningPart[%d].Type = %s, want 'reasoning'", i, part.Type)
		}
	}
}

// TestMessageByIDWithNoParts tests message with no parts returns empty content.
func TestMessageByIDWithNoParts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	now := time.Now().UTC()
	b := fixture.NewBuilder()

	b.AddProject(fixture.NewProject("proj-001", "/test").Name("test-project"))

	s1 := fixture.NewSession("ses-001", "proj-001").
		Title("Test session").
		CreatedAt(now).
		UpdatedAt(now)

	msg1 := fixture.NewMessage("msg-001", "ses-001", "user").CreatedAt(now)
	s1.AddMessage(msg1)

	b.AddSession(s1)

	// No parts added for msg-001

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

	detail, err := MessageByID(ctx, st, "msg-001")
	if err != nil {
		t.Fatalf("MessageByID(msg-001) failed: %v", err)
	}

	if detail == nil {
		t.Fatal("MessageByID returned nil, want message detail")
	}

	// Validate metadata is still present
	if detail.ID != "msg-001" {
		t.Errorf("MessageDetail.ID = %s, want msg-001", detail.ID)
	}

	if detail.Role != "user" {
		t.Errorf("MessageDetail.Role = %s, want user", detail.Role)
	}

	// Content should be empty arrays, not nil
	if detail.Content.TextParts == nil {
		t.Error("Content.TextParts is nil, want empty slice []")
	}

	if len(detail.Content.TextParts) != 0 {
		t.Errorf("Content.TextParts has %d parts, want 0", len(detail.Content.TextParts))
	}

	if detail.Content.ReasoningParts == nil {
		t.Error("Content.ReasoningParts is nil, want empty slice []")
	}

	if len(detail.Content.ReasoningParts) != 0 {
		t.Errorf("Content.ReasoningParts has %d parts, want 0", len(detail.Content.ReasoningParts))
	}
}

// TestMessageByIDNotFound tests non-existent message returns nil.
func TestMessageByIDNotFound(t *testing.T) {
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

	detail, err := MessageByID(ctx, st, "nonexistent-msg")
	if err != nil {
		t.Fatalf("MessageByID(nonexistent) should not error, got: %v", err)
	}

	if detail != nil {
		t.Error("MessageByID(nonexistent) returned non-nil, want nil")
	}

	// Test empty ID
	detail2, err := MessageByID(ctx, st, "")
	if err != nil {
		t.Fatalf("MessageByID(empty) should not error, got: %v", err)
	}

	if detail2 != nil {
		t.Error("MessageByID(empty) returned non-nil, want nil")
	}
}

// TestTruncateContent tests content truncation logic.
func TestTruncateContent(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		maxChars int
		want     string
	}{
		{
			name:     "short content unchanged",
			content:  "Short text",
			maxChars: 500,
			want:     "Short text",
		},
		{
			name:     "long content truncated",
			content:  "This is a very long piece of text that exceeds the maximum character limit and should be truncated",
			maxChars: 20,
			want:     "This is a very long ...",
		},
		{
			name:     "exact length unchanged",
			content:  "Exactly twenty chars",
			maxChars: 20,
			want:     "Exactly twenty chars",
		},
		{
			name:     "empty content unchanged",
			content:  "",
			maxChars: 500,
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateContent(tt.content, tt.maxChars)
			if result != tt.want {
				t.Errorf("truncateContent() = %q, want %q", result, tt.want)
			}
		})
	}
}

// TestMessageByIDWithToolParts tests message detail with tool parts.
func TestMessageByIDWithToolParts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	now := time.Now().UTC()
	b := fixture.NewBuilder()

	b.AddProject(fixture.NewProject("proj-001", "/test").Name("test-project"))

	s1 := fixture.NewSession("ses-001", "proj-001").
		Title("Test session").
		CreatedAt(now).
		UpdatedAt(now)

	msg1 := fixture.NewMessage("msg-001", "ses-001", "assistant").
		CreatedAt(now).
		Cost(0.12).
		ModelID("claude-3-sonnet").
		ProviderID("anthropic").
		Tokens(2000, 800, 300)
	s1.AddMessage(msg1)

	b.AddSession(s1)

	// Add completed tool part
	toolData1 := `{"type":"tool","callID":"call-001","tool":"read_file","state":{"status":"completed","input":{"path":"/src/main.go"},"output":"package main\n\nfunc main() {}","title":"Read main.go","time":{"start":1000,"end":2000}}}`
	b.AddPart(fixture.NewPart("part-001", "ses-001", toolData1).MessageID("msg-001"))

	// Add pending tool part
	toolData2 := `{"type":"tool","callID":"call-002","tool":"bash","state":{"status":"pending","input":{"command":"ls -la"}}}`
	b.AddPart(fixture.NewPart("part-002", "ses-001", toolData2).MessageID("msg-001"))

	// Add error tool part
	toolData3 := `{"type":"tool","callID":"call-003","tool":"write_file","state":{"status":"error","input":{"path":"/src/test.go"},"error":"file not found"}}`
	b.AddPart(fixture.NewPart("part-003", "ses-001", toolData3).MessageID("msg-001"))

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

	detail, err := MessageByID(ctx, st, "msg-001")
	if err != nil {
		t.Fatalf("MessageByID(msg-001) failed: %v", err)
	}

	if detail == nil {
		t.Fatal("MessageByID returned nil, want message detail")
	}

	if len(detail.Content.ToolParts) != 3 {
		t.Errorf("MessageDetail.Content.ToolParts has %d parts, want 3", len(detail.Content.ToolParts))
	}

	for i, part := range detail.Content.ToolParts {
		if part.Type != "tool" {
			t.Errorf("ToolPart[%d].Type = %s, want 'tool'", i, part.Type)
		}
		if part.CallID == "" {
			t.Errorf("ToolPart[%d].CallID is empty", i)
		}
		if part.Tool == "" {
			t.Errorf("ToolPart[%d].Tool is empty", i)
		}
		if part.State.Status == "" {
			t.Errorf("ToolPart[%d].State.Status is empty", i)
		}
	}

	completed := detail.Content.ToolParts[0]
	if completed.State.Status != "completed" {
		t.Errorf("First tool part status = %s, want 'completed'", completed.State.Status)
	}
	if completed.State.Output == "" {
		t.Errorf("Completed tool part has empty output")
	}
	if completed.State.Title == "" {
		t.Errorf("Completed tool part has empty title")
	}

	pending := detail.Content.ToolParts[1]
	if pending.State.Status != "pending" {
		t.Errorf("Second tool part status = %s, want 'pending'", pending.State.Status)
	}

	errorTool := detail.Content.ToolParts[2]
	if errorTool.State.Status != "error" {
		t.Errorf("Third tool part status = %s, want 'error'", errorTool.State.Status)
	}
	if errorTool.State.Error == "" {
		t.Errorf("Error tool part has empty error message")
	}
}

// TestMessageByIDWithAllPartTypes tests message with text, reasoning, and tool parts.
func TestMessageByIDWithAllPartTypes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	now := time.Now().UTC()
	b := fixture.NewBuilder()

	b.AddProject(fixture.NewProject("proj-001", "/test").Name("test-project"))

	s1 := fixture.NewSession("ses-001", "proj-001").
		Title("Test session").
		CreatedAt(now).
		UpdatedAt(now)

	msg1 := fixture.NewMessage("msg-001", "ses-001", "assistant").
		CreatedAt(now).
		Cost(0.15).
		ModelID("claude-3-sonnet").
		ProviderID("anthropic").
		Tokens(2500, 900, 400)
	s1.AddMessage(msg1)

	b.AddSession(s1)

	b.AddPart(fixture.NewPart("part-001", "ses-001", `{"type":"reasoning","text":"Analyzing..."}`).MessageID("msg-001"))
	b.AddPart(fixture.NewPart("part-002", "ses-001", `{"type":"tool","callID":"call-001","tool":"bash","state":{"status":"completed","input":{"cmd":"echo test"},"output":"test","title":"Run command"}}`).MessageID("msg-001"))
	b.AddPart(fixture.NewPart("part-003", "ses-001", `{"type":"text","text":"Here's the result."}`).MessageID("msg-001"))

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

	detail, err := MessageByID(ctx, st, "msg-001")
	if err != nil {
		t.Fatalf("MessageByID(msg-001) failed: %v", err)
	}

	if len(detail.Content.TextParts) != 1 {
		t.Errorf("TextParts count = %d, want 1", len(detail.Content.TextParts))
	}
	if len(detail.Content.ReasoningParts) != 1 {
		t.Errorf("ReasoningParts count = %d, want 1", len(detail.Content.ReasoningParts))
	}
	if len(detail.Content.ToolParts) != 1 {
		t.Errorf("ToolParts count = %d, want 1", len(detail.Content.ToolParts))
	}
}

// TestParseToolPart tests parsing tool part JSON.
func TestParseToolPart(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		wantTool string
		wantStat string
	}{
		{
			name:     "completed tool",
			json:     `{"type":"tool","callID":"call-1","tool":"read","state":{"status":"completed","input":{"path":"/a.go"},"output":"code","title":"Read"}}`,
			wantTool: "read",
			wantStat: "completed",
		},
		{
			name:     "pending tool",
			json:     `{"type":"tool","callID":"call-2","tool":"bash","state":{"status":"pending","input":{"cmd":"ls"}}}`,
			wantTool: "bash",
			wantStat: "pending",
		},
		{
			name:     "error tool",
			json:     `{"type":"tool","callID":"call-3","tool":"write","state":{"status":"error","input":{"path":"/b.go"},"error":"not found"}}`,
			wantTool: "write",
			wantStat: "error",
		},
		{
			name:     "running tool",
			json:     `{"type":"tool","callID":"call-4","tool":"search","state":{"status":"running","input":{"q":"test"},"title":"Searching"}}`,
			wantTool: "search",
			wantStat: "running",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			part, err := parseToolPart(tt.json)
			if err != nil {
				t.Fatalf("parseToolPart() error: %v", err)
			}
			if part.Tool != tt.wantTool {
				t.Errorf("Tool = %s, want %s", part.Tool, tt.wantTool)
			}
			if part.State.Status != tt.wantStat {
				t.Errorf("Status = %s, want %s", part.State.Status, tt.wantStat)
			}
		})
	}
}
