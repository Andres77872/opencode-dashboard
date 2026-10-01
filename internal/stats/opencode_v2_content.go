package stats

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// messageContentV2 extracts display content from one OpenCode 2 transcript
// row. User prompts keep their text in data.text, settled compactions their
// summary in data.summary, and assistant steps embed text, reasoning, and tool
// calls in data.content; there is no part table to join.
func messageContentV2(ctx context.Context, db *sql.DB, id string) (MessageContent, error) {
	content := MessageContent{
		TextParts:      []MessagePart{},
		ReasoningParts: []MessagePart{},
		ToolParts:      []ToolPart{},
	}

	var messageType, data string
	err := db.QueryRowContext(ctx, messageDataV2Query, id).Scan(&messageType, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return content, nil
	}
	if err != nil {
		return content, err
	}

	var payload struct {
		Text    string            `json:"text"`
		Summary string            `json:"summary"`
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		// Like malformed 1.x parts, an unreadable payload hides content but
		// keeps the message's metadata visible.
		return content, nil
	}

	addPart := func(parts *[]MessagePart, partType, text string) {
		if text == "" {
			return
		}
		text, truncation := truncateContentWithInfo(text, 1000)
		*parts = append(*parts, MessagePart{Type: partType, Text: text, Truncation: truncation})
	}

	switch messageType {
	case "user":
		addPart(&content.TextParts, "text", payload.Text)
	case "compaction":
		addPart(&content.TextParts, "text", payload.Summary)
	case "assistant":
		for _, raw := range payload.Content {
			var item struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if err := json.Unmarshal(raw, &item); err != nil {
				continue
			}
			switch item.Type {
			case "text":
				addPart(&content.TextParts, "text", item.Text)
			case "reasoning":
				addPart(&content.ReasoningParts, "reasoning", item.Text)
			case "tool":
				if tool, err := parseToolPartV2(raw); err == nil {
					content.ToolParts = append(content.ToolParts, tool)
				}
			}
		}
	}
	return content, nil
}

// parseToolPartV2 maps an OpenCode 2 assistant tool call onto the 1.x tool
// part shape the API exposes. Tool results are a list of text and file items
// instead of one output string, errors are structured, and timestamps live
// on the call rather than its state.
func parseToolPartV2(raw json.RawMessage) (ToolPart, error) {
	var call struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		State struct {
			Status  string          `json:"status"`
			Input   json.RawMessage `json:"input"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
				Mime string `json:"mime"`
				Name string `json:"name"`
			} `json:"content"`
			Error    json.RawMessage        `json:"error"`
			Metadata map[string]interface{} `json:"metadata"`
		} `json:"state"`
		Time struct {
			Created   *float64 `json:"created"`
			Completed *float64 `json:"completed"`
		} `json:"time"`
	}
	if err := json.Unmarshal(raw, &call); err != nil {
		return ToolPart{}, err
	}

	state := ToolState{Status: call.State.Status, Metadata: call.State.Metadata}

	// Settled calls carry an input object; a call still streaming its
	// arguments carries the raw argument text, which is not displayable input.
	var input map[string]interface{}
	if len(call.State.Input) > 0 && json.Unmarshal(call.State.Input, &input) == nil && input != nil {
		var truncation *TruncationInfo
		state.Input, truncation = truncateToolInputWithInfo(input)
		state.Truncation = mergeTruncation(state.Truncation, truncation)
	}

	output := make([]string, 0, len(call.State.Content))
	for _, item := range call.State.Content {
		switch item.Type {
		case "text":
			output = append(output, item.Text)
		case "file":
			// File results usually inline their bytes as a data: URI; only the
			// label is displayable.
			label := item.Name
			if label == "" {
				label = item.Mime
			}
			output = append(output, "[file: "+label+"]")
		}
	}
	if len(output) > 0 {
		var truncation *TruncationInfo
		state.Output, truncation = truncateContentWithInfo(strings.Join(output, "\n"), toolContentMaxChars)
		state.Truncation = mergeTruncation(state.Truncation, truncation)
	}

	if len(call.State.Error) > 0 {
		var structured struct {
			Message string `json:"message"`
		}
		var message string
		if json.Unmarshal(call.State.Error, &structured) == nil && structured.Message != "" {
			state.Error = structured.Message
		} else if json.Unmarshal(call.State.Error, &message) == nil {
			state.Error = message
		}
	}

	if call.Time.Created != nil || call.Time.Completed != nil {
		state.Time = &ToolTime{}
		if call.Time.Created != nil {
			state.Time.Start = int64(*call.Time.Created)
		}
		if call.Time.Completed != nil {
			state.Time.End = int64(*call.Time.Completed)
		}
	}

	return ToolPart{
		Type:   "tool",
		CallID: call.ID,
		Tool:   call.Name,
		State:  state,
	}, nil
}
