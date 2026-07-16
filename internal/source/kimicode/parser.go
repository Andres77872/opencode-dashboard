package kimicode

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

type sessionState struct {
	CreatedAt     string               `json:"createdAt"`
	UpdatedAt     string               `json:"updatedAt"`
	Title         string               `json:"title"`
	IsCustomTitle bool                 `json:"isCustomTitle"`
	LastPrompt    string               `json:"lastPrompt"`
	ForkedFrom    string               `json:"forkedFrom"`
	WorkDir       string               `json:"workDir"`
	Agents        map[string]agentMeta `json:"agents"`
	Custom        map[string]any       `json:"custom"`
}

type agentMeta struct {
	Homedir       string  `json:"homedir"`
	Type          string  `json:"type"`
	ParentAgentID *string `json:"parentAgentId"`
	SwarmItem     string  `json:"swarmItem"`
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
	Line int
	Time time.Time
	Type string

	Prompt  *promptRecord
	Message *contextMessageRecord
	Event   *loopEventRecord
	Request *llmRequestRecord
	Usage   *usageRecord
}

type originRecord struct {
	Kind    string `json:"kind"`
	Variant string `json:"variant"`
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

var ignoredRecordTypes = map[string]bool{
	"metadata":                          true,
	"forked":                            true,
	"turn.cancel":                       true,
	"config.update":                     true,
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

	content, err := os.ReadFile(files.StatePath)
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
		result.Agents = append(result.Agents, parsedAgent{
			File:    file,
			Meta:    result.State.Agents[file.AgentID],
			Records: records,
		})
	}
	return result, diag, nil
}

func parseWireFile(ctx context.Context, file agentFile) ([]wireRecord, parseDiagnostics, error) {
	boundary, boundaryDiag, err := findLastForkBoundary(ctx, file.Path)
	if err != nil {
		return nil, boundaryDiag, err
	}

	fh, err := os.Open(file.Path)
	if err != nil {
		return nil, boundaryDiag, err
	}
	defer fh.Close()

	scanner := bufio.NewScanner(fh)
	scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)
	records := make([]wireRecord, 0)
	diag := boundaryDiag
	lineNo := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, diag, err
		}
		lineNo++
		if lineNo <= boundary {
			continue
		}
		record, ok, malformed, unsupported := parseWireLine(lineNo, scanner.Bytes())
		switch {
		case malformed:
			diag.MalformedLines++
		case unsupported:
			diag.UnsupportedEvents++
		case ok:
			records = append(records, record)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, diag, err
	}
	return records, diag, nil
}

// findLastForkBoundary returns the last durable "forked" marker. Kimi Code
// clones an agent's complete wire and appends this marker, so only records after
// the last marker belong to the derived session. A fork of a fork can contain
// several markers, making "last" essential.
func findLastForkBoundary(ctx context.Context, path string) (int, parseDiagnostics, error) {
	fh, err := os.Open(path)
	if err != nil {
		return 0, parseDiagnostics{}, err
	}
	defer fh.Close()

	scanner := bufio.NewScanner(fh)
	scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)
	last := 0
	lineNo := 0
	var diag parseDiagnostics
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return 0, diag, err
		}
		lineNo++
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &envelope); err != nil {
			// Count malformed data in the real parse pass only, avoiding a
			// doubled diagnostic for the marker pre-scan.
			continue
		}
		if envelope.Type == "forked" {
			last = lineNo
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, diag, err
	}
	return last, diag, nil
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
		return wireRecord{}, false, true, false
	}
	record := wireRecord{Line: lineNo, Type: recordType, Time: rawTime(raw["time"])}

	switch recordType {
	case "turn.prompt", "turn.steer":
		var value struct {
			Input  []contentPartRecord `json:"input"`
			Origin originRecord        `json:"origin"`
		}
		if err := json.Unmarshal(line, &value); err != nil {
			return wireRecord{}, false, true, false
		}
		record.Prompt = &promptRecord{Input: value.Input, Origin: value.Origin}
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
			}
		default:
			return record, false, false, true
		}
	case "llm.request":
		var value struct {
			Kind       string `json:"kind"`
			Provider   string `json:"provider"`
			Model      string `json:"model"`
			ModelAlias string `json:"modelAlias"`
			TurnStep   string `json:"turnStep"`
			Attempt    string `json:"attempt"`
		}
		if err := json.Unmarshal(line, &value); err != nil {
			return wireRecord{}, false, true, false
		}
		record.Request = &llmRequestRecord{
			Kind: value.Kind, Provider: value.Provider, Model: value.Model,
			ModelAlias: value.ModelAlias, TurnStep: value.TurnStep, Attempt: value.Attempt,
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
			return record, false, false, false
		}
		return record, false, false, true
	}
	return record, true, false, false
}

func rawTime(raw json.RawMessage) time.Time {
	if len(raw) == 0 {
		return time.Time{}
	}
	var millis int64
	if err := json.Unmarshal(raw, &millis); err == nil && millis > 0 {
		return numericTime(millis)
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if parsed, err := time.Parse(time.RFC3339Nano, text); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
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
