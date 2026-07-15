package analyticsagent

const (
	StreamEventStart        = "start"
	StreamEventContentDelta = "content_delta"
	StreamEventContentReset = "content_reset"
	StreamEventToolStart    = "tool_start"
	StreamEventToolFinish   = "tool_finish"
)

// StreamEvent is the privacy-bounded progress contract between the analytics
// service and the web transport. Tool arguments, tool results, provider
// reasoning, and raw provider messages deliberately have no representation in
// this type.
type StreamEvent struct {
	Type   string `json:"type"`
	Delta  string `json:"delta,omitempty"`
	CallID string `json:"call_id,omitempty"`
	Name   string `json:"name,omitempty"`
	OK     *bool  `json:"ok,omitempty"`
	Model  string `json:"model,omitempty"`
}
