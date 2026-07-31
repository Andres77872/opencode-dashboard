package cache

import (
	"context"
	"testing"
	"time"

	"opencode-dashboard/internal/stats"
)

// TestUnknownUsageSurvivesCacheRoundTrip pins that "the source recorded no
// usage for this request" is not silently converted into "the source recorded
// zero tokens" — which would present an authoritative zero cache read and zero
// output for a request whose usage is simply unknown.
func TestUnknownUsageSurvivesCacheRoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	created := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name            string
		entry           stats.MessageEntry
		wantTokens      bool
		wantCacheRead   int64
		wantUsageStatus stats.UsageStatus
	}{
		{
			name: "assistant row with no recorded usage reads back as unknown",
			entry: stats.MessageEntry{
				ID: "m-nil", SessionID: "s1", Role: "assistant", TimeCreated: created,
				ModelID: "gpt-5.6-sol", ProviderID: "anthropic", CostStatus: stats.CostMissing,
				Tokens: nil,
			},
			wantTokens:      false,
			wantUsageStatus: stats.UsageStatusUnavailable,
		},
		{
			name: "a measured zero cache read stays a measured zero",
			entry: stats.MessageEntry{
				ID: "m-zero-cache", SessionID: "s1", Role: "assistant", TimeCreated: created,
				ModelID: "claude-opus-5", ProviderID: "anthropic", CostStatus: stats.CostComputed,
				UsageStatus: stats.UsageStatusRecorded,
				Tokens:      &stats.TokenStats{Input: 1035, Output: 413, Cache: stats.CacheStats{Read: 0, Write: 23034}},
			},
			wantTokens:      true,
			wantCacheRead:   0,
			wantUsageStatus: stats.UsageStatusRecorded,
		},
		{
			name: "a recorded cache read round-trips unchanged",
			entry: stats.MessageEntry{
				ID: "m-cache", SessionID: "s1", Role: "assistant", TimeCreated: created,
				ModelID: "claude-opus-5", ProviderID: "anthropic", CostStatus: stats.CostComputed,
				UsageStatus: stats.UsageStatusRecorded,
				Tokens:      &stats.TokenStats{Input: 500, Output: 300, Cache: stats.CacheStats{Read: 20000, Write: 1000}},
			},
			wantTokens:      true,
			wantCacheRead:   20000,
			wantUsageStatus: stats.UsageStatusRecorded,
		},
	}

	rows := make([]messageRow, 0, len(tests))
	for _, tt := range tests {
		rows = append(rows, messageRow{Entry: tt.entry, ProjectID: "p1", ProjectName: "p1"})
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := insertMessages(ctx, tx, "claude_code", rows); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.MessageByID(ctx, "claude_code", tt.entry.ID)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if got == nil {
				t.Fatal("message not found")
			}
			if tt.wantTokens {
				if got.Tokens == nil {
					t.Fatal("Tokens = nil, want the recorded usage")
				}
				if got.Tokens.Cache.Read != tt.wantCacheRead {
					t.Errorf("Cache.Read = %d, want %d", got.Tokens.Cache.Read, tt.wantCacheRead)
				}
			} else if got.Tokens != nil {
				t.Errorf("Tokens = %+v, want nil; unknown usage must not read back as zeros", *got.Tokens)
			}
			if got.UsageStatus != tt.wantUsageStatus {
				t.Errorf("UsageStatus = %q, want %q", got.UsageStatus, tt.wantUsageStatus)
			}
		})
	}
}
