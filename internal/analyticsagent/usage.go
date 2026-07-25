package analyticsagent

import (
	"bytes"
	"encoding/json"
)

// Usage is normalized token accounting for one or more provider requests made
// while answering a question. It is local telemetry about this dashboard's own
// assistant: it never contains prompt or completion text, so it is safe to
// stream to the browser and persist with the conversation.
//
// Providers report token counters inconsistently, so every field is optional
// evidence rather than a guarantee. Zero means "not reported", never "free":
// callers must not present a missing counter as a measured zero.
type Usage struct {
	// Requests counts completed provider round trips, including rounds that
	// only produced tool calls.
	Requests int64 `json:"requests"`
	// InputTokens and OutputTokens are the provider's prompt and completion
	// counters. CachedInputTokens and ReasoningTokens are subsets of them and
	// are reported only when the provider breaks them out.
	InputTokens       int64 `json:"input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
	CachedInputTokens int64 `json:"cached_input_tokens,omitempty"`
	ReasoningTokens   int64 `json:"reasoning_tokens,omitempty"`
	TotalTokens       int64 `json:"total_tokens"`
}

// Add accumulates another usage sample. It is the only supported way to build
// per-turn or per-session totals so rounding and missing-counter handling stay
// in one place.
func (u Usage) Add(other Usage) Usage {
	return Usage{
		Requests:          u.Requests + other.Requests,
		InputTokens:       u.InputTokens + other.InputTokens,
		OutputTokens:      u.OutputTokens + other.OutputTokens,
		CachedInputTokens: u.CachedInputTokens + other.CachedInputTokens,
		ReasoningTokens:   u.ReasoningTokens + other.ReasoningTokens,
		TotalTokens:       u.TotalTokens + other.TotalTokens,
	}
}

// IsZero reports whether nothing at all was recorded.
func (u Usage) IsZero() bool {
	return u == Usage{}
}

// HasTokens reports whether any token counter was observed, which is what the
// UI needs to decide between showing counts and showing "not reported".
func (u Usage) HasTokens() bool {
	return u.InputTokens > 0 || u.OutputTokens > 0 || u.TotalTokens > 0
}

// normalize repairs the common provider inconsistencies: negative counters are
// dropped, and a missing total is derived from the parts it is defined as.
func (u Usage) normalize() Usage {
	clamp := func(value int64) int64 {
		if value < 0 {
			return 0
		}
		return value
	}
	u.Requests = clamp(u.Requests)
	u.InputTokens = clamp(u.InputTokens)
	u.OutputTokens = clamp(u.OutputTokens)
	u.CachedInputTokens = clamp(u.CachedInputTokens)
	u.ReasoningTokens = clamp(u.ReasoningTokens)
	u.TotalTokens = clamp(u.TotalTokens)
	if u.TotalTokens == 0 {
		u.TotalTokens = u.InputTokens + u.OutputTokens
	}
	// A provider that reports a subset larger than its parent is reporting
	// something this code does not model; keep the subset bounded instead of
	// publishing an impossible ratio.
	if u.CachedInputTokens > u.InputTokens {
		u.CachedInputTokens = u.InputTokens
	}
	if u.ReasoningTokens > u.OutputTokens {
		u.ReasoningTokens = u.OutputTokens
	}
	return u
}

// wireUsage is the OpenAI-compatible usage envelope MiniMax returns on both the
// buffered response and the terminal streaming chunk.
type wireUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
	// MiniMax has also used input/output naming in some deployments.
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	PromptTokensDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

// parseUsage decodes one provider usage envelope. Absent, null, or malformed
// usage yields a zero value rather than an error: token accounting is
// observability, and losing it must never fail a report the user asked for.
func parseUsage(raw json.RawMessage) Usage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return Usage{}
	}
	var wire wireUsage
	if err := json.Unmarshal(trimmed, &wire); err != nil {
		return Usage{}
	}
	usage := Usage{
		InputTokens:       max64(wire.PromptTokens, wire.InputTokens),
		OutputTokens:      max64(wire.CompletionTokens, wire.OutputTokens),
		CachedInputTokens: wire.PromptTokensDetails.CachedTokens,
		ReasoningTokens:   wire.CompletionTokensDetails.ReasoningTokens,
		TotalTokens:       wire.TotalTokens,
	}
	if usage.HasTokens() {
		usage.Requests = 1
	}
	return usage.normalize()
}

func max64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
