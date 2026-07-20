package qwencode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

type parseDiagnostics struct {
	MalformedLines    int64
	UnsupportedEvents int64
}

// usageCounts carries the raw provider token counters shared by chat
// transcript usageMetadata, ui_telemetry api_response events, and the
// token-usage log. Overlap semantics: Cached ⊆ Input always; Thoughts ⊆
// Output on the openai/qwen-oauth paths (OpenAI-compatible accounting,
// qwen-code's openaiContentGenerator maps completion_tokens and its
// reasoning subset verbatim) but additive on gemini/vertex-ai auth.
type usageCounts struct {
	Input    int64
	Output   int64
	Cached   int64
	Thoughts int64
}

func (u usageCounts) isZero() bool {
	return u.Input == 0 && u.Output == 0 && u.Cached == 0 && u.Thoughts == 0
}

// matchKey groups identical request accounting across stores so the
// normalizer can reconcile assistant records, api_response telemetry, and
// usage-log rows without double counting.
func (u usageCounts) matchKey(model string) string {
	return fmt.Sprintf("%s\x00%d\x00%d\x00%d\x00%d", model, u.Input, u.Output, u.Cached, u.Thoughts)
}

type chatPart struct {
	Text             string          `json:"text"`
	Thought          json.RawMessage `json:"thought"`
	FunctionCall     *functionCall   `json:"functionCall"`
	FunctionResponse json.RawMessage `json:"functionResponse"`
}

// thoughtText returns the reasoning text of a part. qwen-code writes thought
// parts either as {"thought": "..."} or (Gemini-style) as
// {"text": "...", "thought": true}.
func (p chatPart) thoughtText() (string, bool) {
	if len(p.FunctionResponse) > 0 || p.FunctionCall != nil {
		return "", false
	}
	if len(p.Thought) == 0 {
		return "", false
	}
	var text string
	if err := json.Unmarshal(p.Thought, &text); err == nil {
		return text, true
	}
	var flag bool
	if err := json.Unmarshal(p.Thought, &flag); err == nil && flag {
		return p.Text, true
	}
	return "", false
}

type functionCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args any    `json:"args"`
}

type toolCallResult struct {
	CallID        string `json:"callId"`
	Status        string `json:"status"`
	ResultDisplay any    `json:"resultDisplay"`
}

type apiResponseEvent struct {
	Model        string
	AuthType     string
	SubagentName string
	Usage        usageCounts
	DurationMs   int64
}

type chatRecord struct {
	Line      int
	Type      string
	UUID      string
	Timestamp time.Time
	CWD       string

	Model      string
	Usage      *usageCounts
	Parts      []chatPart
	ToolResult *toolCallResult
	APIEvent   *apiResponseEvent
}

type parsedChatSession struct {
	File    chatFile
	Records []chatRecord
}

// usageLogRecord is one line of usage/token-usage-YYYY-MM.jsonl: a single
// API request with raw provider token counters.
type usageLogRecord struct {
	ID        string
	Timestamp time.Time
	SessionID string
	Model     string
	AuthType  string
	// Source labels the in-process caller: "main" for the interactive loop,
	// other values for managed auxiliary calls (e.g. the memory extractor).
	Source     string
	Usage      usageCounts
	DurationMs int64
}

// sessionRollup is one line of usage_record.jsonl: a per-session summary the
// CLI appends on exit. Token sums there are unreliable (often zero), so only
// the metadata is consumed.
type sessionRollup struct {
	SessionID string
	Project   string
	StartTime time.Time
	EndTime   time.Time
}

const maxLineBytes = 32 * 1024 * 1024

func parseChatFile(ctx context.Context, file chatFile) (parsedChatSession, parseDiagnostics, error) {
	result := parsedChatSession{File: file}
	var diag parseDiagnostics

	fh, err := os.Open(file.Path)
	if err != nil {
		return parsedChatSession{}, diag, err
	}
	defer fh.Close()

	scanner := bufio.NewScanner(fh)
	scanner.Buffer(make([]byte, 64*1024), maxLineBytes)
	lineNo := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return parsedChatSession{}, diag, err
		}
		lineNo++
		record, ok, malformed, unsupported := parseChatLine(lineNo, scanner.Bytes())
		switch {
		case malformed:
			diag.MalformedLines++
		case unsupported:
			diag.UnsupportedEvents++
		case ok:
			result.Records = append(result.Records, record)
		}
	}
	if err := scanner.Err(); err != nil {
		return parsedChatSession{}, diag, err
	}
	return result, diag, nil
}

func parseChatLine(lineNo int, line []byte) (chatRecord, bool, bool, bool) {
	if strings.TrimSpace(string(line)) == "" {
		return chatRecord{}, false, false, false
	}
	var envelope struct {
		Type      string `json:"type"`
		Subtype   string `json:"subtype"`
		UUID      string `json:"uuid"`
		Timestamp string `json:"timestamp"`
		CWD       string `json:"cwd"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil || envelope.Type == "" {
		return chatRecord{}, false, true, false
	}
	record := chatRecord{
		Line:      lineNo,
		Type:      envelope.Type,
		UUID:      envelope.UUID,
		Timestamp: parseChatTime(envelope.Timestamp),
		CWD:       envelope.CWD,
	}

	switch envelope.Type {
	case "user":
		var value struct {
			Message struct {
				Parts []chatPart `json:"parts"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &value); err != nil {
			return chatRecord{}, false, true, false
		}
		record.Parts = value.Message.Parts
	case "assistant":
		var value struct {
			Model   string `json:"model"`
			Message struct {
				Parts []chatPart `json:"parts"`
			} `json:"message"`
			UsageMetadata *struct {
				PromptTokenCount        int64 `json:"promptTokenCount"`
				CandidatesTokenCount    int64 `json:"candidatesTokenCount"`
				CachedContentTokenCount int64 `json:"cachedContentTokenCount"`
				ThoughtsTokenCount      int64 `json:"thoughtsTokenCount"`
			} `json:"usageMetadata"`
		}
		if err := json.Unmarshal(line, &value); err != nil {
			return chatRecord{}, false, true, false
		}
		record.Model = value.Model
		record.Parts = value.Message.Parts
		if value.UsageMetadata != nil {
			record.Usage = &usageCounts{
				Input:    value.UsageMetadata.PromptTokenCount,
				Output:   value.UsageMetadata.CandidatesTokenCount,
				Cached:   value.UsageMetadata.CachedContentTokenCount,
				Thoughts: value.UsageMetadata.ThoughtsTokenCount,
			}
		}
	case "tool_result":
		var value struct {
			ToolCallResult *toolCallResult `json:"toolCallResult"`
			Message        struct {
				Parts []chatPart `json:"parts"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &value); err != nil {
			return chatRecord{}, false, true, false
		}
		record.ToolResult = value.ToolCallResult
		record.Parts = value.Message.Parts
		if record.ToolResult == nil {
			return record, false, false, false
		}
	case "system":
		// System records carry many benign subtypes (slash_command,
		// file_history_snapshot, chat_compression, notification, ...). Only
		// ui_telemetry api_response events hold usage analytics; everything
		// else is metadata and is skipped without flagging diagnostics.
		if envelope.Subtype != "ui_telemetry" {
			return record, false, false, false
		}
		var value struct {
			SystemPayload struct {
				UIEvent map[string]any `json:"uiEvent"`
			} `json:"systemPayload"`
		}
		if err := json.Unmarshal(line, &value); err != nil {
			return chatRecord{}, false, true, false
		}
		event := value.SystemPayload.UIEvent
		name, _ := event["event.name"].(string)
		if name != "qwen-code.api_response" {
			// tool_call events mirror tool_result records, api_error events
			// carry no tokens, and new telemetry names appear regularly.
			return record, false, false, false
		}
		model, _ := event["model"].(string)
		authType, _ := event["auth_type"].(string)
		subagent, _ := event["subagent_name"].(string)
		record.APIEvent = &apiResponseEvent{
			Model:        model,
			AuthType:     authType,
			SubagentName: subagent,
			Usage: usageCounts{
				Input:    eventInt(event, "input_token_count"),
				Output:   eventInt(event, "output_token_count"),
				Cached:   eventInt(event, "cached_content_token_count"),
				Thoughts: eventInt(event, "thoughts_token_count"),
			},
			DurationMs: eventInt(event, "duration_ms"),
		}
	default:
		return record, false, false, true
	}
	return record, true, false, false
}

func eventInt(event map[string]any, key string) int64 {
	switch v := event[key].(type) {
	case float64:
		return int64(v)
	case json.Number:
		parsed, err := v.Int64()
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}

func parseUsageLogFile(ctx context.Context, path string) ([]usageLogRecord, parseDiagnostics, error) {
	var diag parseDiagnostics
	fh, err := os.Open(path)
	if err != nil {
		return nil, diag, err
	}
	defer fh.Close()

	scanner := bufio.NewScanner(fh)
	scanner.Buffer(make([]byte, 64*1024), maxLineBytes)
	records := make([]usageLogRecord, 0)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, diag, err
		}
		line := scanner.Bytes()
		if strings.TrimSpace(string(line)) == "" {
			continue
		}
		var value struct {
			ID            string `json:"id"`
			Timestamp     string `json:"timestamp"`
			SessionID     string `json:"sessionId"`
			Model         string `json:"model"`
			AuthType      string `json:"authType"`
			Source        string `json:"source"`
			InputTokens   int64  `json:"inputTokens"`
			OutputTokens  int64  `json:"outputTokens"`
			CachedTokens  int64  `json:"cachedTokens"`
			ThoughtsTok   int64  `json:"thoughtsTokens"`
			APIDurationMs int64  `json:"apiDurationMs"`
		}
		if err := json.Unmarshal(line, &value); err != nil || value.SessionID == "" {
			diag.MalformedLines++
			continue
		}
		records = append(records, usageLogRecord{
			ID:        value.ID,
			Timestamp: parseChatTime(value.Timestamp),
			SessionID: value.SessionID,
			Model:     value.Model,
			AuthType:  value.AuthType,
			Source:    value.Source,
			Usage: usageCounts{
				Input:    value.InputTokens,
				Output:   value.OutputTokens,
				Cached:   value.CachedTokens,
				Thoughts: value.ThoughtsTok,
			},
			DurationMs: value.APIDurationMs,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, diag, err
	}
	return records, diag, nil
}

func parseSessionRollups(ctx context.Context, path string) (map[string]sessionRollup, parseDiagnostics, error) {
	var diag parseDiagnostics
	if path == "" {
		return map[string]sessionRollup{}, diag, nil
	}
	fh, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]sessionRollup{}, diag, nil
		}
		return nil, diag, err
	}
	defer fh.Close()

	scanner := bufio.NewScanner(fh)
	scanner.Buffer(make([]byte, 64*1024), maxLineBytes)
	rollups := make(map[string]sessionRollup)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, diag, err
		}
		line := scanner.Bytes()
		if strings.TrimSpace(string(line)) == "" {
			continue
		}
		var value struct {
			SessionID string `json:"sessionId"`
			Project   string `json:"project"`
			Timestamp int64  `json:"timestamp"`
			StartTime int64  `json:"startTime"`
		}
		if err := json.Unmarshal(line, &value); err != nil || value.SessionID == "" {
			diag.MalformedLines++
			continue
		}
		rollup := sessionRollup{
			SessionID: value.SessionID,
			Project:   value.Project,
			StartTime: millisTime(value.StartTime),
			EndTime:   millisTime(value.Timestamp),
		}
		// The CLI can append several rollups for one session (e.g. resumed
		// runs); the last line wins for metadata, keeping the earliest start.
		if existing, ok := rollups[value.SessionID]; ok {
			if !existing.StartTime.IsZero() && (rollup.StartTime.IsZero() || existing.StartTime.Before(rollup.StartTime)) {
				rollup.StartTime = existing.StartTime
			}
			if existing.EndTime.After(rollup.EndTime) {
				rollup.EndTime = existing.EndTime
			}
			if rollup.Project == "" {
				rollup.Project = existing.Project
			}
		}
		rollups[value.SessionID] = rollup
	}
	if err := scanner.Err(); err != nil {
		return nil, diag, err
	}
	return rollups, diag, nil
}

func parseChatTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func millisTime(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(value).UTC()
}

func valueToText(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(encoded)
	}
}
