package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type parseDiagnostics struct {
	MalformedLines    int64
	UnsupportedEvents int64
}

type tokenSnapshot struct {
	Input     int64
	Cached    int64
	Output    int64
	Reasoning int64
	Total     int64
}

type codexRecord struct {
	File      transcriptFile
	Line      int
	Timestamp time.Time
	TopType   string

	SessionMeta *sessionMetaRecord
	TurnContext *turnContextRecord
	Event       *eventMsgRecord
	Response    *responseItemRecord
	Compacted   bool
}

type sessionMetaRecord struct {
	ID            string
	CLIVersion    string
	CWD           string
	Source        string
	ModelProvider string
	ThreadSource  string
}

type turnContextRecord struct {
	TurnID             string
	Model              string
	CWD                string
	ApprovalPolicy     string
	SandboxPolicy      string
	CollaborationMode  string
	ModelContextWindow int64
	Provider           string
}

type eventMsgRecord struct {
	PayloadType        string
	TurnID             string
	Text               string
	CallID             string
	ToolName           string
	Status             string
	ChangedFiles       int64
	Compaction         bool
	LastUsage          tokenSnapshot
	HasLastUsage       bool
	TotalUsage         tokenSnapshot
	HasTotalUsage      bool
	PlanType           string
	ModelContextWindow int64
}

type responseItemRecord struct {
	ItemType string
	TurnID   string
	Role     string
	Text     string
	CallID   string
	ToolName string
	Status   string
	IsError  bool
}

func parseTranscriptFile(ctx context.Context, file transcriptFile) ([]codexRecord, parseDiagnostics, error) {
	fh, err := os.Open(file.Path)
	if err != nil {
		return nil, parseDiagnostics{}, err
	}
	defer fh.Close()

	reader := bufio.NewReaderSize(fh, 128*1024)
	records := make([]codexRecord, 0)
	var diag parseDiagnostics
	lineNo := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, diag, err
		}
		line, err := reader.ReadString('\n')
		if line != "" {
			lineNo++
			record, ok, malformed := parseLine(file, lineNo, line)
			if malformed {
				diag.MalformedLines++
			} else if !ok {
				diag.UnsupportedEvents++
			} else {
				records = append(records, record)
			}
		}
		if err == nil {
			continue
		}
		if err == io.EOF {
			break
		}
		return nil, diag, err
	}
	return records, diag, nil
}

func parseLine(file transcriptFile, lineNo int, line string) (codexRecord, bool, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return codexRecord{}, false, false
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		return codexRecord{}, false, true
	}
	record, ok := normalizeRawRecord(file, lineNo, raw)
	return record, ok, false
}

func normalizeRawRecord(file transcriptFile, lineNo int, raw map[string]any) (codexRecord, bool) {
	topType := firstString(raw, "type")
	record := codexRecord{
		File:      file,
		Line:      lineNo,
		TopType:   topType,
		Timestamp: firstTime(raw, "timestamp", "created_at"),
	}
	payload := mapValue(raw["payload"])
	switch topType {
	case "session_meta":
		record.SessionMeta = &sessionMetaRecord{
			ID:            firstString(payload, "id", "session_id"),
			CLIVersion:    firstString(payload, "cli_version", "cliVersion"),
			CWD:           firstString(payload, "cwd"),
			Source:        firstString(payload, "source"),
			ModelProvider: firstString(payload, "model_provider", "modelProvider"),
			ThreadSource:  firstString(payload, "thread_source", "threadSource"),
		}
	case "turn_context":
		record.TurnContext = &turnContextRecord{
			TurnID:             firstString(payload, "turn_id", "turnId"),
			Model:              firstString(payload, "model", "model_id"),
			CWD:                firstString(payload, "cwd"),
			ApprovalPolicy:     firstString(payload, "approval_policy"),
			SandboxPolicy:      firstString(payload, "sandbox_policy"),
			CollaborationMode:  firstString(payload, "collaboration_mode", "collaboration_mode_kind"),
			ModelContextWindow: intValue(payload["model_context_window"]),
			Provider:           firstString(payload, "model_provider", "provider"),
		}
	case "event_msg":
		record.Event = parseEventPayload(payload)
	case "response_item":
		record.Response = parseResponsePayload(payload)
	case "compacted":
		record.Compacted = true
	default:
		return codexRecord{}, false
	}
	return record, true
}

func parseEventPayload(payload map[string]any) *eventMsgRecord {
	if payload == nil {
		payload = map[string]any{}
	}
	event := &eventMsgRecord{
		PayloadType:  firstString(payload, "type"),
		TurnID:       firstString(payload, "turn_id", "turnId"),
		Text:         firstString(payload, "message", "text", "summary"),
		CallID:       firstString(payload, "call_id", "callId"),
		ToolName:     firstString(payload, "name", "tool", "tool_name"),
		Status:       firstString(payload, "status"),
		ChangedFiles: intValue(payload["changed_files"]),
	}
	if event.PayloadType == "context_compacted" {
		event.Compaction = true
	}
	info := mapValue(payload["info"])
	if info != nil {
		last := mapValue(info["last_token_usage"])
		event.LastUsage = parseTokenSnapshot(last)
		event.HasLastUsage = last != nil
		total := mapValue(info["total_token_usage"])
		event.TotalUsage = parseTokenSnapshot(total)
		event.HasTotalUsage = total != nil
		event.ModelContextWindow = intValue(info["model_context_window"])
		rateLimits := mapValue(info["rate_limits"])
		if rateLimits == nil {
			rateLimits = mapValue(payload["rate_limits"])
		}
		event.PlanType = firstString(rateLimits, "plan_type", "planType")
	}
	return event
}

func parseResponsePayload(payload map[string]any) *responseItemRecord {
	if payload == nil {
		payload = map[string]any{}
	}
	item := mapValue(payload["item"])
	if item == nil {
		item = payload
	}
	itemType := firstString(item, "type")
	response := &responseItemRecord{
		ItemType: itemType,
		TurnID:   firstString(payload, "turn_id", "turnId"),
		Role:     firstString(item, "role"),
		CallID:   firstString(item, "call_id", "callId", "id"),
		ToolName: firstString(item, "name", "tool", "tool_name"),
		Status:   firstString(item, "status"),
		IsError:  boolValue(item["is_error"]),
	}
	switch itemType {
	case "message":
		response.Text = parseContentText(item["content"])
	case "reasoning":
		response.Text = parseReasoningSummary(item["summary"])
	case "function_call", "custom_tool_call":
		response.Text = firstString(item, "arguments", "input")
	case "function_call_output", "custom_tool_call_output", "tool_search_output":
		response.Text = firstString(item, "output", "content")
	case "web_search_call", "tool_search_call":
		response.Text = contentToText(mapValue(item["action"]))
	}
	if response.TurnID == "" {
		response.TurnID = firstString(item, "turn_id", "turnId")
	}
	return response
}

func parseTokenSnapshot(raw map[string]any) tokenSnapshot {
	if raw == nil {
		return tokenSnapshot{}
	}
	return tokenSnapshot{
		Input:     intValue(raw["input_tokens"]),
		Cached:    intValue(raw["cached_input_tokens"]),
		Output:    intValue(raw["output_tokens"]),
		Reasoning: intValue(raw["reasoning_output_tokens"]),
		Total:     intValue(raw["total_tokens"]),
	}
}

func parseContentText(content any) string {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			text := parseContentText(item)
			if text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		if text := firstString(v, "text", "content"); text != "" {
			return text
		}
		return firstString(v, "input_text", "output_text")
	default:
		return fmt.Sprint(v)
	}
}

func parseReasoningSummary(summary any) string {
	text := parseContentText(summary)
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return text
}

func mapValue(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if m == nil {
			return ""
		}
		if value, ok := m[key]; ok {
			switch v := value.(type) {
			case string:
				return v
			case json.Number:
				return v.String()
			}
		}
	}
	return ""
}

func firstTime(m map[string]any, keys ...string) time.Time {
	for _, key := range keys {
		if m == nil {
			return time.Time{}
		}
		value, ok := m[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case string:
			if ts, err := time.Parse(time.RFC3339Nano, v); err == nil {
				return ts.UTC()
			}
		case json.Number:
			if i, err := v.Int64(); err == nil {
				return numericTime(i)
			}
		case float64:
			return numericTime(int64(v))
		}
	}
	return time.Time{}
}

func numericTime(v int64) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	if v > 1_000_000_000_000 {
		return time.UnixMilli(v).UTC()
	}
	return time.Unix(v, 0).UTC()
}

func intValue(v any) int64 {
	switch n := v.(type) {
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return i
		}
		if f, err := n.Float64(); err == nil {
			return int64(f)
		}
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}

func boolValue(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

func contentToText(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(encoded)
}
