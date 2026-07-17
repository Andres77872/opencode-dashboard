package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"opencode-dashboard/internal/stats"
)

func TestConsolidationDataExportsFilteredMetadataOnly(t *testing.T) {
	start := time.Date(2026, time.July, 16, 10, 0, 0, 0, time.UTC)
	secret := "raw transcript and tool payload must not escape"
	tokens := &stats.TokenStats{Input: 11, Output: 7}
	provenance := &stats.CostProvenance{
		Status: stats.CostComputed, Currency: "USD", ComputedCount: 1, Note: "message provenance",
	}
	included := &messageRecord{
		Entry: stats.MessageEntry{
			SourceID: claudeSourceID, ID: "included", SessionID: "current", SessionTitle: secret, Role: "assistant",
			TimeCreated: start.Add(time.Hour), Cost: 0.25, Tokens: tokens,
			CostStatus: stats.CostComputed, CostProvenance: provenance,
		},
		TextParts:      []stats.MessagePart{{Type: "text", Text: secret}},
		ReasoningParts: []stats.MessagePart{{Type: "reasoning", Text: secret}},
		ToolParts: []stats.ToolPart{{
			Tool: "Read",
			State: stats.ToolState{
				Status: "completed", Input: map[string]interface{}{"secret": secret}, Output: secret,
			},
		}},
	}
	before := &messageRecord{Entry: stats.MessageEntry{ID: "before", SessionID: "old", Role: "user", TimeCreated: start.Add(-time.Second)}}
	atEnd := &messageRecord{Entry: stats.MessageEntry{ID: "at-end", SessionID: "future", Role: "assistant", TimeCreated: start.Add(2 * time.Hour)}}
	snap := &snapshot{
		sessionMap: map[string]*sessionRecord{
			"old":     {ID: "old", Title: "Old", Created: before.Entry.TimeCreated, Updated: before.Entry.TimeCreated, Messages: []*messageRecord{before}},
			"current": {ID: "current", Title: secret, Created: included.Entry.TimeCreated, Updated: included.Entry.TimeCreated, Messages: []*messageRecord{included}},
			"future":  {ID: "future", Title: "Future", Created: atEnd.Entry.TimeCreated, Updated: atEnd.Entry.TimeCreated, Messages: []*messageRecord{atEnd}},
		},
		ordered: []*messageRecord{before, included, atEnd},
	}
	src := New(Options{ClaudeHome: t.TempDir(), SnapshotTTL: time.Hour})
	src.snapshot, src.loadedAt = snap, time.Now()
	pq := stats.PeriodQuery{FromTime: start, ToTime: start.Add(2 * time.Hour)}

	data, err := src.ConsolidationData(context.Background(), pq)
	if err != nil {
		t.Fatalf("ConsolidationData() error = %v", err)
	}
	if src.snapshot != nil || !src.loadedAt.IsZero() {
		t.Fatalf("full snapshot cache was not released: snapshot=%p loadedAt=%v", src.snapshot, src.loadedAt)
	}
	if len(data.Sessions) != 3 {
		t.Fatalf("sessions = %d, want all 3 snapshot sessions", len(data.Sessions))
	}
	for _, session := range data.Sessions {
		if session.Title != "" {
			t.Errorf("session %q exported prompt-derived title %q", session.ID, session.Title)
		}
	}
	if len(data.Messages) != 1 || data.Messages[0].Entry.ID != included.Entry.ID {
		t.Fatalf("messages = %#v, want only the in-window message", data.Messages)
	}
	if data.Messages[0].Entry.SessionTitle != "" {
		t.Errorf("message exported prompt-derived session title %q", data.Messages[0].Entry.SessionTitle)
	}
	if got := data.Messages[0].Tools; len(got) != 1 || got[0].Name != "Read" || got[0].Status != "completed" {
		t.Fatalf("tools = %#v, want only Read/completed metadata", got)
	}

	list, err := snap.messages(pq, 1, 100, stats.MessageSort{Field: stats.MessageSortTime, Direction: stats.MessageSortAsc})
	if err != nil {
		t.Fatalf("snapshot.messages() error = %v", err)
	}
	if data.CostStatus != list.CostStatus || !reflect.DeepEqual(data.CostProvenance, list.CostProvenance) {
		t.Errorf("batch cost metadata = %q/%#v, MessageList = %q/%#v", data.CostStatus, data.CostProvenance, list.CostStatus, list.CostProvenance)
	}
	if data.Messages[0].Entry.Tokens == included.Entry.Tokens || data.Messages[0].Entry.CostProvenance == included.Entry.CostProvenance {
		t.Fatal("exported message metadata aliases the pinned snapshot")
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("consolidation data exposed raw content: %s", encoded)
	}

	data.Messages[0].Entry.Tokens.Input = 999
	data.Messages[0].Entry.CostProvenance.Note = "mutated"
	if included.Entry.Tokens.Input != 11 || included.Entry.CostProvenance.Note != "message provenance" {
		t.Fatal("mutating exported metadata changed the pinned snapshot")
	}

	src.bounded = snap
	src.boundedFrom = start.Add(-boundedLoadMargin)
	src.boundedLoadedAt = time.Now()
	if _, err := src.ConsolidationData(context.Background(), pq); err != nil {
		t.Fatalf("ConsolidationData(bounded) error = %v", err)
	}
	if src.bounded != nil || !src.boundedFrom.IsZero() || !src.boundedLoadedAt.IsZero() {
		t.Fatalf("bounded snapshot cache was not released: snapshot=%p from=%v loadedAt=%v", src.bounded, src.boundedFrom, src.boundedLoadedAt)
	}

	newer := &snapshot{}
	newerLoadedAt := time.Now()
	newerBoundedFrom := start.Add(-2 * boundedLoadMargin)
	newerBoundedLoadedAt := newerLoadedAt.Add(-time.Second)
	src.snapshot, src.loadedAt = newer, newerLoadedAt
	src.bounded, src.boundedFrom, src.boundedLoadedAt = newer, newerBoundedFrom, newerBoundedLoadedAt
	src.releaseConsolidationSnapshot(snap)
	if src.snapshot != newer || !src.loadedAt.Equal(newerLoadedAt) || src.bounded != newer ||
		!src.boundedFrom.Equal(newerBoundedFrom) || !src.boundedLoadedAt.Equal(newerBoundedLoadedAt) {
		t.Fatal("releasing an old pinned snapshot cleared a newer cached snapshot")
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := src.ConsolidationData(canceled, pq); !errors.Is(err, context.Canceled) {
		t.Fatalf("ConsolidationData(canceled) error = %v, want context.Canceled", err)
	}
	if src.snapshot != newer || src.bounded != newer {
		t.Fatal("canceled consolidation evicted cached snapshots before snapshot lookup")
	}
}
