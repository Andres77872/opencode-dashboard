package kimicode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

const maxWireLineBytes = 32 * 1024 * 1024

type parseDiagnostics struct {
	MalformedLines    int64
	UnsupportedEvents int64
	Reason            string
}

type sessionState struct {
	ID            string               `json:"id"`
	Version       int                  `json:"version"`
	CreatedAt     any                  `json:"createdAt"`
	UpdatedAt     any                  `json:"updatedAt"`
	Title         string               `json:"title"`
	Label         string               `json:"label"`
	IsCustomTitle bool                 `json:"isCustomTitle"`
	LastPrompt    string               `json:"lastPrompt"`
	ForkedFrom    string               `json:"forkedFrom"`
	Cwd           string               `json:"cwd"`
	WorkDir       string               `json:"workDir"`
	Archived      bool                 `json:"archived"`
	Agents        map[string]agentMeta `json:"agents"`
	Custom        map[string]any       `json:"custom"`
}

type agentMeta struct {
	Homedir       string            `json:"homedir"`
	Type          string            `json:"type"`
	ParentAgentID *string           `json:"parentAgentId"`
	ForkedFrom    string            `json:"forkedFrom"`
	Labels        map[string]string `json:"labels"`
	SwarmItem     string            `json:"swarmItem"`
}

type parsedSession struct {
	Files  sessionFiles
	State  sessionState
	Agents []parsedAgent
}

type parsedAgent struct {
	File    agentFile
	Meta    agentMeta
	Records []wireRecord
}

type wireRecord struct {
	Line      int
	Time      time.Time
	Type      string
	DurableID string

	Prompt  *promptRecord
	Message *contextMessageRecord
	Event   *loopEventRecord
	Request *llmRequestRecord
	Usage   *usageRecord
	Cancel  *turnCancelRecord
	Config  *configUpdateRecord
}

type originRecord struct {
	Kind    string `json:"kind"`
	Variant string `json:"variant"`
	Trigger string `json:"trigger"`
}

type contentPartRecord struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Think string `json:"think"`
}

type promptRecord struct {
	Input  []contentPartRecord
	Origin originRecord
}

type contextMessageRecord struct {
	Role    string
	Content []contentPartRecord
	Origin  originRecord
}

type loopEventRecord struct {
	Type        string
	UUID        string
	TurnID      string
	Step        int64
	StepUUID    string
	Part        contentPartRecord
	ToolCallID  string
	Name        string
	Args        any
	Description string
	Result      toolResultRecord
	Usage       *tokenUsage
}

type toolResultRecord struct {
	Output  any  `json:"output"`
	Content any  `json:"content"`
	Error   any  `json:"error"`
	IsError bool `json:"isError"`
}

type llmRequestRecord struct {
	Kind       string
	Provider   string
	Model      string
	ModelAlias string
	TurnStep   string
	Attempt    string
}

type configUpdateRecord struct {
	ProfileName string
	ModelAlias  string
	Cwd         string
}

type tokenUsage struct {
	InputOther         int64 `json:"inputOther"`
	InputCacheRead     int64 `json:"inputCacheRead"`
	InputCacheCreation int64 `json:"inputCacheCreation"`
	Output             int64 `json:"output"`
}

type usageRecord struct {
	Model      string
	Usage      tokenUsage
	UsageScope string
}

type turnCancelRecord struct {
	TurnID string
}

var ignoredRecordTypes = map[string]bool{
	"forked":                            true,
	"permission.set_mode":               true,
	"permission.record_approval_result": true,
	"full_compaction.begin":             true,
	"full_compaction.cancel":            true,
	"full_compaction.complete":          true,
	"micro_compaction.apply":            true,
	"plan_mode.enter":                   true,
	"plan_mode.cancel":                  true,
	"plan_mode.exit":                    true,
	"swarm_mode.enter":                  true,
	"swarm_mode.exit":                   true,
	"tools.register_user_tool":          true,
	"tools.unregister_user_tool":        true,
	"tools.set_active_tools":            true,
	"tools.update_store":                true,
	"context.update_token_count":        true,
	"context.clear":                     true,
	"context.apply_compaction":          true,
	"context.undo":                      true,
	"goal.create":                       true,
	"goal.update":                       true,
	"goal.clear":                        true,
	"llm.tools_snapshot":                true,
	"mcp.tools_discovered":              true,
}

func parseSession(ctx context.Context, files sessionFiles) (parsedSession, parseDiagnostics, error) {
	result := parsedSession{Files: files}
	var diag parseDiagnostics

	statePath := files.StatePath
	if statePath == "" {
		statePath = files.LegacyStatePath
	}
	var content []byte
	var err error
	if statePath != "" {
		content, err = os.ReadFile(statePath)
	} else {
		err = os.ErrNotExist
	}
	switch {
	case err == nil:
		if jsonErr := json.Unmarshal(content, &result.State); jsonErr != nil {
			diag.MalformedLines++
		}
	case os.IsNotExist(err):
		// A wire can briefly outlive a missing state file during concurrent
		// session operations; retain the transcript with filesystem metadata.
	case err != nil:
		return parsedSession{}, diag, err
	}
	if result.State.Agents == nil {
		result.State.Agents = map[string]agentMeta{}
	}
	if strings.TrimSpace(result.State.ID) != "" {
		result.Files.ID = strings.TrimSpace(result.State.ID)
	}

	hasMain := false
	for _, file := range files.Agents {
		if err := ctx.Err(); err != nil {
			return parsedSession{}, diag, err
		}
		records, wireDiag, err := parseWireFile(ctx, file)
		if err != nil {
			return parsedSession{}, diag, err
		}
		diag.MalformedLines += wireDiag.MalformedLines
		diag.UnsupportedEvents += wireDiag.UnsupportedEvents
		diag.Reason = appendReason(diag.Reason, wireDiag.Reason)
		result.Agents = append(result.Agents, parsedAgent{
			File:    file,
			Meta:    result.State.Agents[file.AgentID],
			Records: records,
		})
		if file.AgentID == "main" {
			hasMain = true
		}
	}

	if files.RootWire != nil {
		if hasMain {
			diag.Reason = appendReason(diag.Reason, "ignored a shadowed Kimi Code root wire because agents/main is authoritative")
		} else {
			records, wireDiag, err := parseWireFile(ctx, *files.RootWire)
			if err != nil {
				return parsedSession{}, diag, err
			}
			diag.MalformedLines += wireDiag.MalformedLines
			diag.UnsupportedEvents += wireDiag.UnsupportedEvents
			diag.Reason = appendReason(diag.Reason, wireDiag.Reason)
			if hasCanonicalAgentRecords(records) {
				result.Agents = append(result.Agents, parsedAgent{
					File: *files.RootWire, Meta: result.State.Agents["main"], Records: records,
				})
			} else {
				diag.Reason = appendReason(diag.Reason, "ignored a legacy UI-only Kimi Code root wire without canonical agent records")
			}
		}
	}
	return result, diag, nil
}

func parseWireFile(ctx context.Context, file agentFile) ([]wireRecord, parseDiagnostics, error) {
	return parseWireFileWithLimit(ctx, file, maxWireLineBytes)
}

func parseWireFileWithLimit(ctx context.Context, file agentFile, maxLineBytes int) ([]wireRecord, parseDiagnostics, error) {
	records := make([]wireRecord, 0)
	var diag parseDiagnostics
	lineDiag, err := readBoundedLines(ctx, file.Path, maxLineBytes, func(lineNo int, line []byte, oversized bool) {
		if oversized {
			return
		}
		record, ok, malformed, unsupported := parseWireLine(lineNo, line)
		if record.Type == "forked" && !malformed {
			// A fork clones the complete parent wire and appends a durable
			// marker. Reset immediately so memory stays bounded and only the
			// records after the last marker survive this single pass.
			records = records[:0]
			return
		}
		switch {
		case malformed:
			diag.MalformedLines++
		case unsupported:
			diag.UnsupportedEvents++
		case ok:
			records = append(records, record)
		}
	})
	diag.MalformedLines += lineDiag.MalformedLines
	if lineDiag.MalformedLines > 0 {
		diag.Reason = appendReason(diag.Reason, "skipped an oversized Kimi Code JSONL record and continued scanning")
	}
	if err != nil {
		return nil, diag, err
	}
	return records, diag, nil
}

// readBoundedLines streams a JSONL-like file without allowing one malformed or
// torn line to retain unbounded memory. Oversized lines are discarded through
// their newline and reported to the callback, after which scanning continues.
func readBoundedLines(ctx context.Context, path string, maxBytes int, visit func(int, []byte, bool)) (parseDiagnostics, error) {
	fh, err := os.Open(path)
	if err != nil {
		return parseDiagnostics{}, err
	}
	defer fh.Close()
	if maxBytes <= 0 {
		maxBytes = maxWireLineBytes
	}
	reader := bufio.NewReaderSize(fh, 64*1024)
	line := make([]byte, 0, 64*1024)
	lineNo := 0
	var diag parseDiagnostics
	oversized := false
	hasData := false
	for {
		if err := ctx.Err(); err != nil {
			return diag, err
		}
		fragment, readErr := reader.ReadSlice('\n')
		if len(fragment) > 0 {
			hasData = true
			if !oversized {
				if len(line)+len(fragment) > maxBytes {
					line = nil
					oversized = true
				} else {
					line = append(line, fragment...)
				}
			}
		}
		if readErr == bufio.ErrBufferFull {
			continue
		}
		if readErr != nil && readErr != io.EOF {
			return diag, readErr
		}
		if hasData {
			lineNo++
			if oversized {
				diag.MalformedLines++
				visit(lineNo, nil, true)
			} else {
				line = bytes.TrimSuffix(line, []byte{'\n'})
				line = bytes.TrimSuffix(line, []byte{'\r'})
				visit(lineNo, line, false)
			}
		}
		line = make([]byte, 0, 64*1024)
		oversized = false
		hasData = false
		if readErr == io.EOF {
			break
		}
	}
	return diag, nil
}

func parseWireLine(lineNo int, line []byte) (wireRecord, bool, bool, bool) {
	if strings.TrimSpace(string(line)) == "" {
		return wireRecord{}, false, false, false
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(line, &raw); err != nil {
		return wireRecord{}, false, true, false
	}
	var recordType string
	if err := json.Unmarshal(raw["type"], &recordType); err != nil || recordType == "" {
		if len(raw["message"]) > 0 {
			return wireRecord{}, false, false, true
		}
		return wireRecord{}, false, true, false
	}
	record := wireRecord{
		Line: lineNo, Type: recordType, Time: rawTime(raw["time"]),
		DurableID: firstRawScalar(raw["id"], raw["uuid"], raw["requestId"]),
	}

	switch recordType {
	case "metadata":
		if record.Time.IsZero() {
			record.Time = rawTime(raw["created_at"])
		}
	case "config.update":
		var value struct {
			ProfileName string `json:"profileName"`
			ModelAlias  string `json:"modelAlias"`
			Cwd         string `json:"cwd"`
		}
		if err := json.Unmarshal(line, &value); err != nil {
			return wireRecord{}, false, true, false
		}
		record.Config = &configUpdateRecord{ProfileName: value.ProfileName, ModelAlias: value.ModelAlias, Cwd: value.Cwd}
	case "turn.prompt", "turn.steer":
		var value struct {
			Input  []contentPartRecord `json:"input"`
			Origin originRecord        `json:"origin"`
		}
		if err := json.Unmarshal(line, &value); err != nil {
			return wireRecord{}, false, true, false
		}
		record.Prompt = &promptRecord{Input: value.Input, Origin: value.Origin}
	case "turn.cancel":
		var value struct {
			TurnID string `json:"turnId"`
		}
		if err := json.Unmarshal(line, &value); err != nil {
			return wireRecord{}, false, true, false
		}
		record.Cancel = &turnCancelRecord{TurnID: strings.TrimSpace(value.TurnID)}
	case "context.append_message":
		var value struct {
			Message struct {
				Role    string              `json:"role"`
				Content []contentPartRecord `json:"content"`
				Origin  originRecord        `json:"origin"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &value); err != nil {
			return wireRecord{}, false, true, false
		}
		record.Message = &contextMessageRecord{
			Role: value.Message.Role, Content: value.Message.Content, Origin: value.Message.Origin,
		}
	case "context.append_loop_event":
		var value struct {
			Event struct {
				Type        string            `json:"type"`
				UUID        string            `json:"uuid"`
				TurnID      string            `json:"turnId"`
				Step        int64             `json:"step"`
				StepUUID    string            `json:"stepUuid"`
				Part        contentPartRecord `json:"part"`
				ToolCallID  string            `json:"toolCallId"`
				Name        string            `json:"name"`
				Args        any               `json:"args"`
				Description string            `json:"description"`
				Result      toolResultRecord  `json:"result"`
				Usage       *tokenUsage       `json:"usage"`
			} `json:"event"`
		}
		if err := json.Unmarshal(line, &value); err != nil {
			return wireRecord{}, false, true, false
		}
		event := value.Event
		switch event.Type {
		case "step.begin", "step.end", "content.part", "tool.call", "tool.result":
			record.Event = &loopEventRecord{
				Type: event.Type, UUID: event.UUID, TurnID: event.TurnID, Step: event.Step,
				StepUUID: event.StepUUID, Part: event.Part, ToolCallID: event.ToolCallID,
				Name: event.Name, Args: event.Args, Description: event.Description, Result: event.Result,
				Usage: event.Usage,
			}
			if event.UUID != "" {
				record.DurableID = event.Type + ":" + event.UUID
			}
		default:
			return record, false, false, true
		}
	case "llm.request":
		var value struct {
			Kind       string          `json:"kind"`
			Provider   string          `json:"provider"`
			Model      string          `json:"model"`
			ModelAlias string          `json:"modelAlias"`
			TurnStep   string          `json:"turnStep"`
			Attempt    json.RawMessage `json:"attempt"`
		}
		if err := json.Unmarshal(line, &value); err != nil {
			return wireRecord{}, false, true, false
		}
		record.Request = &llmRequestRecord{
			Kind: value.Kind, Provider: value.Provider, Model: value.Model,
			ModelAlias: value.ModelAlias, TurnStep: value.TurnStep, Attempt: rawScalar(value.Attempt),
		}
	case "usage.record":
		var value struct {
			Model      string     `json:"model"`
			Usage      tokenUsage `json:"usage"`
			UsageScope string     `json:"usageScope"`
		}
		if err := json.Unmarshal(line, &value); err != nil {
			return wireRecord{}, false, true, false
		}
		record.Usage = &usageRecord{Model: value.Model, Usage: value.Usage, UsageScope: value.UsageScope}
	default:
		if ignoredRecordTypes[recordType] {
			return record, true, false, false
		}
		return record, false, false, true
	}
	return record, true, false, false
}

func rawTime(raw json.RawMessage) time.Time {
	if len(raw) == 0 {
		return time.Time{}
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		if value, err := strconv.ParseFloat(number.String(), 64); err == nil && value > 0 {
			return numericTimeFloat(value)
		}
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if parsed, err := time.Parse(time.RFC3339Nano, text); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

func numericTimeFloat(value float64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	if value > 1_000_000_000_000 {
		seconds := value / 1000
		whole := int64(seconds)
		return time.Unix(whole, int64((seconds-float64(whole))*float64(time.Second))).UTC()
	}
	whole := int64(value)
	return time.Unix(whole, int64((value-float64(whole))*float64(time.Second))).UTC()
}

func numericTime(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	if value > 1_000_000_000_000 {
		return time.UnixMilli(value).UTC()
	}
	return time.Unix(value, 0).UTC()
}

func rawScalar(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String()
	}
	return ""
}

func firstRawScalar(values ...json.RawMessage) string {
	for _, value := range values {
		if text := rawScalar(value); text != "" {
			return text
		}
	}
	return ""
}

func hasCanonicalAgentRecords(records []wireRecord) bool {
	for _, record := range records {
		if record.Prompt != nil || record.Message != nil || record.Event != nil || record.Request != nil || record.Usage != nil || record.Cancel != nil {
			return true
		}
	}
	return false
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
