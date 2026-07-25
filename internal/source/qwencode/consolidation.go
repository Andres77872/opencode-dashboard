package qwencode

import (
	"context"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

var _ source.ConsolidationSource = (*Source)(nil)

// ConsolidationData exports the cache-safe metadata in one pinned snapshot.
// In particular, tool payloads and message content never cross this boundary.
// The snapshot is deliberately kept cached afterwards: a consolidation runs
// concurrently with live reads of the recent window, which need the very same
// snapshot — releasing it here forced those reads into cold full re-parses.
// It expires via the normal snapshot TTL instead.
func (s *Source) ConsolidationData(ctx context.Context, pq stats.PeriodQuery) (source.ConsolidationData, error) {
	if err := ctx.Err(); err != nil {
		return source.ConsolidationData{}, err
	}
	snap, err := s.snapshotFor(ctx, pq)
	if err != nil {
		return source.ConsolidationData{}, err
	}
	return snap.consolidationData(ctx, pq)
}

func (s *snapshot) consolidationData(ctx context.Context, pq stats.PeriodQuery) (source.ConsolidationData, error) {
	if err := ctx.Err(); err != nil {
		return source.ConsolidationData{}, err
	}
	window, err := s.window(pq)
	if err != nil {
		return source.ConsolidationData{}, err
	}

	sessions := make([]stats.SessionEntry, 0, len(s.sessionMap))
	for _, session := range s.sessionMap {
		if err := ctx.Err(); err != nil {
			return source.ConsolidationData{}, err
		}
		entry := session.entry()
		// JSONL session titles can be derived from prompt text. The cache uses
		// its own safe title, so do not carry this field across the boundary.
		entry.Title = ""
		if err := ctx.Err(); err != nil {
			return source.ConsolidationData{}, err
		}
		entry.CostProvenance = cloneProvenance(entry.CostProvenance)
		sessions = append(sessions, entry)
	}

	filtered := make([]*messageRecord, 0, len(s.ordered))
	messages := make([]source.ConsolidationMessage, 0, len(s.ordered))
	for _, message := range s.ordered {
		if err := ctx.Err(); err != nil {
			return source.ConsolidationData{}, err
		}
		if !window.all && !inWindow(message.Entry.TimeCreated, window) {
			continue
		}

		filtered = append(filtered, message)
		entry := message.Entry
		entry.SessionTitle = ""
		entry.Tokens = cloneTokens(entry.Tokens)
		entry.CostProvenance = cloneProvenance(entry.CostProvenance)
		tools := make([]source.ConsolidationTool, 0, len(message.ToolParts))
		for _, tool := range message.ToolParts {
			if err := ctx.Err(); err != nil {
				return source.ConsolidationData{}, err
			}
			tools = append(tools, source.ConsolidationTool{
				Name:   tool.Tool,
				Status: tool.State.Status,
			})
		}
		messages = append(messages, source.ConsolidationMessage{Entry: entry, Tools: tools})
	}

	_, _, status, provenance := aggregateCostProvenance(filtered)
	if err := ctx.Err(); err != nil {
		return source.ConsolidationData{}, err
	}
	return source.ConsolidationData{
		Sessions:       sessions,
		Messages:       messages,
		CostStatus:     status,
		CostProvenance: provenance,
	}, nil
}
