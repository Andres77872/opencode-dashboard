package analyticsagent

import "encoding/json"

const (
	StreamEventStart        = "start"
	StreamEventContentDelta = "content_delta"
	StreamEventContentReset = "content_reset"
	StreamEventToolStart    = "tool_start"
	StreamEventToolFinish   = "tool_finish"
)

// StreamEvent is the progress contract between the analytics service and the
// web transport. Tool arguments and results carry only the validated,
// privacy-scrubbed JSON that is already exchanged with the provider, so the
// local dashboard can show exactly what each analytics tool received and
// returned. Provider reasoning and raw provider messages deliberately have no
// representation in this type.
type StreamEvent struct {
	Type       string          `json:"type"`
	Delta      string          `json:"delta,omitempty"`
	CallID     string          `json:"call_id,omitempty"`
	Name       string          `json:"name,omitempty"`
	OK         *bool           `json:"ok,omitempty"`
	Model      string          `json:"model,omitempty"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	DurationMS int64           `json:"duration_ms,omitempty"`
}

// ToolCallRecord is the canonical record of one analytics tool invocation in a
// completed chat turn: the validated input arguments, the safe result envelope
// returned to the model, and outcome metadata. Callers persist and display it.
type ToolCallRecord struct {
	CallID     string          `json:"call_id"`
	Name       string          `json:"name"`
	Arguments  json.RawMessage `json:"arguments"`
	Result     json.RawMessage `json:"result"`
	OK         bool            `json:"ok"`
	DurationMS int64           `json:"duration_ms"`
}
