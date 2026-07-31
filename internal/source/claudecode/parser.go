package claudecode

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

type tokenUsage struct {
	Input         int64
	Output        int64
	Reasoning     int64
	CacheRead     int64
	CacheCreate   int64
	CacheCreate5m int64
	CacheCreate1h int64
}

type parsedToolUse struct {
	ID    string
	Name  string
	Input map[string]any
}

type parsedToolResult struct {
	ToolUseID string
	Content   any
	IsError   bool
	SpillFile string
}

type parsedRecord struct {
	File         transcriptFile
	Line         int
	UUID         string
	RequestID    string
	APIMessageID string
	SessionID    string
	CWD          string
	Role         string
	IsMeta       bool
	// IsSidechain is true for subagent (Task tool) transcript records, which Claude
	// Code marks with "isSidechain": true. These are real API requests and carry
	// their own usage/cost.
	IsSidechain bool
	// Agent names the subagent type for sidechain records (e.g. "Explore", "Plan"),
	// taken from attributionAgent or agentId. Empty for main-conversation records.
	Agent     string
	Timestamp time.Time
	Model     string
	Usage     tokenUsage
	HasUsage  bool
	// UsageComplete reports that the usage object carried the cache-accounting
	// fields, which is what makes its input_tokens a fresh-input count rather
	// than a whole-prompt total. See usageIsComplete.
	UsageComplete bool
	ReportedUSD   *float64
	// ReportedUSDCumulative is true when the transcript field name explicitly
	// indicates a cumulative total rather than a per-call delta. Claude JSONL
	// field semantics are not fully public, so normalization uses this only as a
	// conservative signal to avoid linear overcount for total_* fields.
	ReportedUSDCumulative bool

	TextParts      []string
	ReasoningParts []string
	ToolUses       []parsedToolUse
	ToolResults    []parsedToolResult
}

func parseTranscriptFile(ctx context.Context, file transcriptFile) ([]parsedRecord, parseDiagnostics, error) {
	fh, err := os.Open(file.Path)
	if err != nil {
		return nil, parseDiagnostics{}, err
	}
	defer fh.Close()

	reader := bufio.NewReaderSize(fh, 128*1024)
	records := make([]parsedRecord, 0)
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

func parseLine(file transcriptFile, lineNo int, line string) (parsedRecord, bool, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return parsedRecord{}, false, false
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		return parsedRecord{}, false, true
	}
	record, ok := normalizeRawRecord(file, lineNo, raw)
	return record, ok, false
}

func normalizeRawRecord(file transcriptFile, lineNo int, raw map[string]any) (parsedRecord, bool) {
	message := mapValue(raw["message"])
	record := parsedRecord{File: file, Line: lineNo}
	record.IsMeta = boolValue(raw["isMeta"]) || boolValue(raw["is_meta"])
	record.IsSidechain = boolValue(raw["isSidechain"]) || boolValue(raw["is_sidechain"])
	record.Agent = firstString(raw, "attributionAgent", "attribution_agent", "agentId", "agent_id")
	if record.Agent == "" {
		record.Agent = firstString(message, "attributionAgent", "attribution_agent", "agentId", "agent_id")
	}
	record.SessionID = firstString(raw, "session_id", "sessionId", "sessionID")
	if record.SessionID == "" {
		record.SessionID = firstString(message, "session_id", "sessionId", "sessionID")
	}
	if record.SessionID == "" {
		record.SessionID = file.SessionID
	}
	record.UUID = firstString(raw, "uuid", "id", "message_id", "messageID")
	if record.UUID == "" {
		record.UUID = firstString(message, "uuid", "id", "message_id", "messageID")
	}
	record.RequestID = firstString(raw, "requestId", "request_id", "requestID")
	if record.RequestID == "" {
		record.RequestID = firstString(message, "requestId", "request_id", "requestID")
	}
	record.APIMessageID = firstString(message, "id", "message_id", "messageID")
	if record.APIMessageID == "" {
		record.APIMessageID = firstString(raw, "api_message_id", "apiMessageID")
	}
	record.CWD = firstString(raw, "cwd", "project_cwd", "projectPath")
	if record.CWD == "" {
		record.CWD = firstString(message, "cwd", "project_cwd", "projectPath")
	}
	record.Timestamp = firstTime(raw, "timestamp", "created_at", "time_created")
	if record.Timestamp.IsZero() {
		record.Timestamp = firstTime(message, "timestamp", "created_at", "time_created")
	}
	record.Role = firstString(message, "role")
	if record.Role == "" {
		record.Role = firstString(raw, "role")
	}
	if record.Role == "" {
		switch firstString(raw, "type") {
		case "user", "assistant":
			record.Role = firstString(raw, "type")
		}
	}
	record.Model = firstString(message, "model", "model_id", "modelID")
	if record.Model == "" {
		record.Model = firstString(raw, "model", "model_id", "modelID")
	}

	if usage := mapValue(message["usage"]); usage != nil {
		record.Usage, record.HasUsage = parseUsage(usage)
		record.UsageComplete = usageIsComplete(usage)
	} else if usage := mapValue(raw["usage"]); usage != nil {
		record.Usage, record.HasUsage = parseUsage(usage)
		record.UsageComplete = usageIsComplete(usage)
	}
	if reported, key := firstFloatPointer(raw, "costUSD", "cost_usd", "total_cost_usd", "totalCostUSD", "totalCostUsd", "cost"); reported != nil {
		record.ReportedUSD = reported
		record.ReportedUSDCumulative = isCumulativeCostField(key)
	}
	if record.ReportedUSD == nil {
		if reported, key := firstFloatPointer(message, "costUSD", "cost_usd", "total_cost_usd", "totalCostUSD", "totalCostUsd", "cost"); reported != nil {
			record.ReportedUSD = reported
			record.ReportedUSDCumulative = isCumulativeCostField(key)
		}
	}

	content := message["content"]
	if content == nil {
		content = raw["content"]
	}
	if content == nil && firstString(raw, "prompt") != "" {
		content = firstString(raw, "prompt")
	}
	parseContent(content, &record)

	if record.Role == "" && len(record.TextParts) == 0 && len(record.ReasoningParts) == 0 && len(record.ToolUses) == 0 && len(record.ToolResults) == 0 {
		return parsedRecord{}, false
	}
	if record.Role == "" {
		record.Role = "unknown"
	}
	return record, true
}

func parseContent(content any, record *parsedRecord) {
	switch v := content.(type) {
	case nil:
		return
	case string:
		if v != "" {
			record.TextParts = append(record.TextParts, v)
		}
	case []any:
		for _, item := range v {
			parseContentItem(item, record)
		}
	case map[string]any:
		parseContentItem(v, record)
	default:
		if text := fmt.Sprint(v); text != "" {
			record.TextParts = append(record.TextParts, text)
		}
	}
}

func parseContentItem(item any, record *parsedRecord) {
	m := mapValue(item)
	if m == nil {
		if text, ok := item.(string); ok && text != "" {
			record.TextParts = append(record.TextParts, text)
		}
		return
	}
	switch firstString(m, "type") {
	case "text":
		if text := firstString(m, "text", "content"); text != "" {
			record.TextParts = append(record.TextParts, text)
		}
	case "thinking", "reasoning":
		if text := firstString(m, "text", "thinking", "content"); text != "" {
			record.ReasoningParts = append(record.ReasoningParts, text)
		}
	case "tool_use":
		input := mapValue(m["input"])
		if input == nil {
			input = map[string]any{}
		}
		record.ToolUses = append(record.ToolUses, parsedToolUse{
			ID:    firstString(m, "id", "tool_use_id", "toolUseID"),
			Name:  firstString(m, "name", "tool_name", "toolName"),
			Input: input,
		})
	case "tool_result":
		record.ToolResults = append(record.ToolResults, parsedToolResult{
			ToolUseID: firstString(m, "tool_use_id", "toolUseID", "id"),
			Content:   m["content"],
			IsError:   boolValue(m["is_error"]),
			SpillFile: firstString(m, "spill_file", "spillFile", "content_file", "contentFile"),
		})
	}
}

// usageIsComplete reports whether a usage object is a finalized usage report
// rather than a mid-stream snapshot.
//
// Models proxied through Claude Code (the GPT models) emit one usage stub per
// streamed content block before the request finishes. A stub carries only
// input_tokens and output_tokens, where input_tokens is the whole prompt — the
// value the finalizing record later splits into fresh input plus cache reads —
// and output_tokens is still 0. Treating a stub as finalized usage therefore
// bills cached tokens at the fresh-input rate and asserts an output of zero for
// a request that did produce output.
//
// Two properties together identify a stub, and a record is only refused when
// both hold: it carries no cache-accounting field at all, and it reports no
// output. Requiring both keeps a genuinely cache-less provider's finalized
// usage usable — that record still reports its output — and limits the refusal
// to records that show no evidence of a completed generation. Anthropic's own
// usage always carries the cache fields even when they are zero, so native
// records never reach the second test.
func usageIsComplete(raw map[string]any) bool {
	for _, key := range []string{
		"cache_read_input_tokens", "cache_creation_input_tokens", "cache_creation",
		"cache_read", "cache_write",
	} {
		if _, ok := raw[key]; ok {
			return true
		}
	}
	return intValue(raw["output_tokens"]) != 0
}

func parseUsage(raw map[string]any) (tokenUsage, bool) {
	cacheCreate := intValue(raw["cache_creation_input_tokens"])
	if cacheCreate == 0 {
		cacheCreate = intValue(raw["cache_write"])
	}
	usage := tokenUsage{
		Input:       intValue(raw["input_tokens"]),
		Output:      intValue(raw["output_tokens"]),
		Reasoning:   intValue(raw["reasoning_tokens"]),
		CacheRead:   intValue(raw["cache_read_input_tokens"]),
		CacheCreate: cacheCreate,
	}
	if usage.CacheRead == 0 {
		usage.CacheRead = intValue(raw["cache_read"])
	}
	cacheCreation := mapValue(raw["cache_creation"])
	_, has5m := cacheCreation["ephemeral_5m_input_tokens"]
	_, has1h := cacheCreation["ephemeral_1h_input_tokens"]
	if has5m || has1h {
		usage.CacheCreate5m = intValue(cacheCreation["ephemeral_5m_input_tokens"])
		usage.CacheCreate1h = intValue(cacheCreation["ephemeral_1h_input_tokens"])
		splitTotal := usage.CacheCreate5m + usage.CacheCreate1h
		if usage.CacheCreate == 0 || splitTotal > usage.CacheCreate {
			usage.CacheCreate = splitTotal
		} else if splitTotal < usage.CacheCreate {
			usage.CacheCreate5m += usage.CacheCreate - splitTotal
		}
	} else {
		usage.CacheCreate5m = usage.CacheCreate
	}
	return usage, usage.Input != 0 || usage.Output != 0 || usage.Reasoning != 0 || usage.CacheRead != 0 || usage.CacheCreate != 0
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
			if s, ok := value.(string); ok {
				return s
			}
			if value != nil {
				if n, ok := value.(json.Number); ok {
					return n.String()
				}
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

func firstFloatPointer(m map[string]any, keys ...string) (*float64, string) {
	for _, key := range keys {
		if m == nil {
			return nil, ""
		}
		value, ok := m[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case json.Number:
			if f, err := v.Float64(); err == nil {
				return &f, key
			}
		case float64:
			return &v, key
		case int:
			f := float64(v)
			return &f, key
		}
	}
	return nil, ""
}

func isCumulativeCostField(key string) bool {
	switch key {
	case "total_cost_usd", "totalCostUSD", "totalCostUsd":
		return true
	default:
		return false
	}
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
