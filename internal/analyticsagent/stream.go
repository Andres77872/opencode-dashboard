package analyticsagent

import "encoding/json"

const (
	StreamEventStart          = "start"
	StreamEventRoundStart     = "round_start"
	StreamEventContentDelta   = "content_delta"
	StreamEventContentReset   = "content_reset"
	StreamEventToolStart      = "tool_start"
	StreamEventToolFinish     = "tool_finish"
	StreamEventSubagentStart  = "subagent_start"
	StreamEventSubagentFinish = "subagent_finish"
)

// Subagent run outcomes. A specialist that spends its whole budget without
// concluding is reported as exhausted rather than silently succeeding.
const (
	SubagentStatusComplete  = "complete"
	SubagentStatusExhausted = "budget_exhausted"
	SubagentStatusFailed    = "failed"
)

// StreamEvent is the progress contract between the analytics service and the
// web transport. Executable tool calls carry normalized, validated arguments;
// rejected calls carry {} plus their public
// failure envelope. Results are privacy-scrubbed JSON exchanged with the
// provider. Provider reasoning and raw provider messages deliberately have no
// representation in this type.
type StreamEvent struct {
	Type string `json:"type"`
	// Agent names the role that produced the event. Tool events emitted for a
	// delegated investigation carry the specialist's id and the delegation call
	// id in ParentCallID, so a client can nest them under that delegation.
	Agent        AgentID `json:"agent,omitempty"`
	ParentCallID string  `json:"parent_call_id,omitempty"`
	// Round is the 1-based provider round within the emitting agent's own loop.
	Round      int             `json:"round,omitempty"`
	Delta      string          `json:"delta,omitempty"`
	CallID     string          `json:"call_id,omitempty"`
	Name       string          `json:"name,omitempty"`
	OK         *bool           `json:"ok,omitempty"`
	Model      string          `json:"model,omitempty"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	DurationMS int64           `json:"duration_ms,omitempty"`
	// Subagent is present on subagent_start and subagent_finish events.
	Subagent *SubagentEvent `json:"subagent,omitempty"`
}

// SubagentEvent describes one delegated investigation as it starts and as it
// finishes. Report is the specialist's own written finding, which is derived
// entirely from privacy-scrubbed aggregate tool results.
type SubagentEvent struct {
	Agent     AgentID  `json:"agent"`
	Title     string   `json:"title"`
	Task      string   `json:"task"`
	Status    string   `json:"status,omitempty"`
	Report    string   `json:"report,omitempty"`
	Rounds    int      `json:"rounds,omitempty"`
	ToolsUsed []string `json:"tools_used,omitempty"`
	Usage     *Usage   `json:"usage,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// ToolCallRecord is the canonical record of one analytics tool invocation in a
// completed chat turn: normalized input arguments (or {} for a rejected
// proposal), the safe result envelope returned to the model, and outcome
// metadata. Callers persist and display it.
type ToolCallRecord struct {
	CallID string `json:"call_id"`
	Name   string `json:"name"`
	// Agent is the role that called the tool, and ParentCallID links a
	// specialist's calls to the delegation that started them.
	Agent        AgentID         `json:"agent,omitempty"`
	ParentCallID string          `json:"parent_call_id,omitempty"`
	Round        int             `json:"round,omitempty"`
	Arguments    json.RawMessage `json:"arguments"`
	Result       json.RawMessage `json:"result"`
	OK           bool            `json:"ok"`
	DurationMS   int64           `json:"duration_ms"`
}

// SubagentRunRecord is the canonical record of one delegated investigation,
// including the specialist's finding and what it cost to produce.
type SubagentRunRecord struct {
	CallID     string   `json:"call_id"`
	Agent      AgentID  `json:"agent"`
	Title      string   `json:"title"`
	Task       string   `json:"task"`
	Status     string   `json:"status"`
	Report     string   `json:"report,omitempty"`
	Error      string   `json:"error,omitempty"`
	Rounds     int      `json:"rounds"`
	ToolsUsed  []string `json:"tools_used"`
	Usage      Usage    `json:"usage"`
	DurationMS int64    `json:"duration_ms"`
}
