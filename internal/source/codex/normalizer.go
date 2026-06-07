package codex

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

type snapshot struct {
	home        string
	diagnostics source.SourceDiagnostics
	projectMap  map[string]*projectRecord
	sessionMap  map[string]*sessionRecord
	messageMap  map[string]*messageRecord
	ordered     []*messageRecord
}

type projectRecord struct {
	ID       string
	Name     string
	Worktree string
}

type sessionRecord struct {
	ID          string
	Title       string
	ProjectID   string
	ProjectName string
	Directory   string
	Created     time.Time
	Updated     time.Time
	Messages    []*messageRecord
}

type messageRecord struct {
	Entry            stats.MessageEntry
	TextParts        []stats.MessagePart
	ReasoningParts   []stats.MessagePart
	ToolParts        []stats.ToolPart
	projectID        string
	maxInputSnapshot int64
	seenAssistant    map[string]bool
	seenTools        map[string]bool
}

type sessionState struct {
	id              string
	provider        string
	projectID       string
	projectName     string
	directory       string
	activeTurnID    string
	tokenMax        tokenSnapshot
	fileSessionID   string
	fallbackCounter int
}

func normalizeRecords(home string, records []codexRecord, pricing pricingSnapshot, diag source.SourceDiagnostics) *snapshot {
	snap := &snapshot{
		home:       home,
		projectMap: make(map[string]*projectRecord),
		sessionMap: make(map[string]*sessionRecord),
		messageMap: make(map[string]*messageRecord),
		ordered:    make([]*messageRecord, 0),
	}
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].Timestamp.Equal(records[j].Timestamp) {
			if records[i].File.Path == records[j].File.Path {
				return records[i].Line < records[j].Line
			}
			return records[i].File.Path < records[j].File.Path
		}
		if records[i].Timestamp.IsZero() {
			return false
		}
		if records[j].Timestamp.IsZero() {
			return true
		}
		return records[i].Timestamp.Before(records[j].Timestamp)
	})

	fileSessionIDs := sessionIDsByFile(records)
	states := make(map[string]*sessionState)
	seenRecords := make(map[string]struct{}, len(records))
	for _, record := range records {
		key := stableRecordDedupeKey(record, fileSessionIDs[record.File.Path])
		if _, exists := seenRecords[key]; exists {
			continue
		}
		seenRecords[key] = struct{}{}

		sessionID := recordSessionID(record, fileSessionIDs)
		state := states[sessionID]
		if state == nil {
			state = &sessionState{id: sessionID, fileSessionID: fileSessionIDs[record.File.Path]}
			states[sessionID] = state
		}
		timestamp := recordTimestamp(record)

		switch {
		case record.SessionMeta != nil:
			state.id = nonEmpty(record.SessionMeta.ID, state.id)
			state.provider = nonEmpty(record.SessionMeta.ModelProvider, state.provider)
			if record.SessionMeta.CWD != "" {
				state.directory = redactDisplayPath(record.SessionMeta.CWD)
				state.projectID, state.projectName = projectFromPath(record.SessionMeta.CWD)
			} else if state.projectID == "" {
				state.projectID, state.projectName = projectFromPath(state.directory)
			}
			snap.ensureSession(state.id, snap.ensureProject(state.projectID, state.projectName, state.directory), state.directory)
			updateSessionTimes(snap.sessionMap[state.id], timestamp)

		case record.TurnContext != nil:
			turn := snap.ensureTurn(state, record.TurnContext.TurnID, timestamp, pricing)
			if record.TurnContext.Model != "" {
				turn.Entry.ModelID = record.TurnContext.Model
			}
			if record.TurnContext.Provider != "" {
				turn.Entry.ProviderID = record.TurnContext.Provider
			} else if turn.Entry.ProviderID == "" && state.provider != "" {
				turn.Entry.ProviderID = state.provider
			}
			if state.directory == "" && record.TurnContext.CWD != "" {
				state.directory = redactDisplayPath(record.TurnContext.CWD)
				state.projectID, state.projectName = projectFromPath(record.TurnContext.CWD)
				turn.projectID = state.projectID
			}
			updateSessionTimes(snap.sessionMap[state.id], timestamp)

		case record.Event != nil:
			snap.applyEvent(state, record.Event, timestamp, pricing)

		case record.Response != nil:
			snap.applyResponse(state, record.Response, timestamp, pricing)

		case record.Compacted:
			// Compaction records are metadata only. They must not create rows or
			// replay developer/user/assistant content into details.
			if session := snap.sessionMap[state.id]; session != nil {
				updateSessionTimes(session, timestamp)
			}
		}
	}

	for _, msg := range snap.ordered {
		msg.recomputeCost(pricing)
	}
	for _, session := range snap.sessionMap {
		sort.SliceStable(session.Messages, func(i, j int) bool {
			return session.Messages[i].Entry.TimeCreated.Before(session.Messages[j].Entry.TimeCreated)
		})
		session.Title = sessionTitle(session)
		for _, msg := range session.Messages {
			msg.Entry.SessionTitle = session.Title
		}
	}
	snap.diagnostics = finalizeDiagnostics(diag)
	return snap
}

func sessionIDsByFile(records []codexRecord) map[string]string {
	ids := make(map[string]string)
	for _, record := range records {
		if record.SessionMeta != nil && record.SessionMeta.ID != "" {
			ids[record.File.Path] = record.SessionMeta.ID
		}
	}
	return ids
}

func recordSessionID(record codexRecord, fileIDs map[string]string) string {
	if record.SessionMeta != nil && record.SessionMeta.ID != "" {
		return record.SessionMeta.ID
	}
	if id := fileIDs[record.File.Path]; id != "" {
		return id
	}
	if record.File.SessionID != "" {
		return record.File.SessionID
	}
	return "unknown-session"
}

func recordTimestamp(record codexRecord) time.Time {
	if !record.Timestamp.IsZero() {
		return record.Timestamp
	}
	if !record.File.ModTime.IsZero() {
		return record.File.ModTime
	}
	return time.Unix(0, 0).UTC()
}

func (s *snapshot) applyEvent(state *sessionState, event *eventMsgRecord, timestamp time.Time, pricing pricingSnapshot) {
	switch event.PayloadType {
	case "task_started":
		turn := s.ensureTurn(state, event.TurnID, timestamp, pricing)
		turn.Entry.Role = "user"
	case "user_message":
		turn := s.ensureTurn(state, event.TurnID, timestamp, pricing)
		if event.Text != "" {
			turn.TextParts = append(turn.TextParts, redactAndTruncateMessagePart("text", event.Text))
		}
	case "agent_message":
		// Assistant mirrors are intentionally ignored when response_item.message is
		// available, preventing duplicate assistant text and row counts.
	case "token_count":
		turn := s.ensureTurn(state, event.TurnID, timestamp, pricing)
		turn.Entry.FoldedTokenUpdates++
		if event.PlanType != "" {
			// Plan metadata affects provenance wording, but all current derived Codex
			// costs remain API-equivalent estimates unless a future reported cost exists.
		}
		if event.HasTotalUsage {
			delta := positiveDelta(state.tokenMax, event.TotalUsage)
			state.tokenMax = maxSnapshot(state.tokenMax, event.TotalUsage)
			turn.addTokenDelta(delta)
			if event.TotalUsage.Input > turn.maxInputSnapshot {
				turn.maxInputSnapshot = event.TotalUsage.Input
			}
		}
	case "patch_apply_end", "web_search_end":
		turn := s.ensureTurn(state, event.TurnID, timestamp, pricing)
		if event.CallID != "" {
			turn.applyToolStatus(event.CallID, event.Status, timestamp)
		}
	case "task_complete":
		turn := s.ensureTurn(state, event.TurnID, timestamp, pricing)
		if turn.Entry.Role == "user" && (turn.Entry.Tokens != nil || len(turn.TextParts) > 0 || len(turn.ToolParts) > 0) {
			turn.Entry.Role = "assistant"
		}
		state.activeTurnID = ""
	case "context_compacted":
		// Metadata only.
	}
	if session := s.sessionMap[state.id]; session != nil {
		updateSessionTimes(session, timestamp)
	}
}

func (s *snapshot) applyResponse(state *sessionState, response *responseItemRecord, timestamp time.Time, pricing pricingSnapshot) {
	turn := s.ensureTurn(state, response.TurnID, timestamp, pricing)
	switch response.ItemType {
	case "message":
		if response.Role != "assistant" {
			return
		}
		if response.Text != "" && !turn.seenAssistant[response.Text] {
			turn.seenAssistant[response.Text] = true
			turn.TextParts = append(turn.TextParts, redactAndTruncateMessagePart("text", response.Text))
			turn.Entry.FoldedAssistantCalls++
		}
		turn.Entry.Role = "assistant"
		if turn.Entry.ProviderID == "" {
			turn.Entry.ProviderID = state.provider
		}
	case "reasoning":
		text := response.Text
		if strings.TrimSpace(text) == "" {
			text = "[Codex reasoning event redacted or encrypted]"
		}
		if len(turn.ReasoningParts) == 0 {
			part := redactAndTruncateMessagePart("reasoning", text)
			part.Redacted = true
			turn.ReasoningParts = append(turn.ReasoningParts, part)
		}
	case "function_call", "custom_tool_call", "web_search_call", "tool_search_call":
		turn.addToolCall(response, timestamp)
	case "function_call_output", "custom_tool_call_output", "tool_search_output":
		turn.applyToolOutput(response, timestamp)
	}
	if session := s.sessionMap[state.id]; session != nil {
		updateSessionTimes(session, timestamp)
	}
}

func (s *snapshot) ensureTurn(state *sessionState, turnID string, timestamp time.Time, pricing pricingSnapshot) *messageRecord {
	if turnID == "" {
		turnID = state.activeTurnID
	}
	if turnID == "" {
		state.fallbackCounter++
		turnID = fmt.Sprintf("fallback:%s:%d", timestamp.UTC().Format("20060102T150405Z"), state.fallbackCounter)
	}
	state.activeTurnID = turnID
	projectID := state.projectID
	projectName := state.projectName
	if projectID == "" {
		projectID, projectName = projectFromPath(state.directory)
		state.projectID, state.projectName = projectID, projectName
	}
	project := s.ensureProject(projectID, projectName, state.directory)
	session := s.ensureSession(state.id, project, state.directory)
	messageID := synthesizeMessageID(state.id, turnID)
	if msg := s.messageMap[messageID]; msg != nil {
		return msg
	}
	missing := missingCost(defaultCurrency(pricing))
	msg := &messageRecord{
		Entry: stats.MessageEntry{
			SourceID:       codexSourceID,
			ID:             messageID,
			SessionID:      state.id,
			Role:           "user",
			TimeCreated:    timestamp.UTC(),
			ProviderID:     state.provider,
			CostStatus:     missing.Status,
			CostProvenance: missing.Provenance,
		},
		projectID:     project.ID,
		seenAssistant: map[string]bool{},
		seenTools:     map[string]bool{},
	}
	s.messageMap[messageID] = msg
	s.ordered = append(s.ordered, msg)
	session.Messages = append(session.Messages, msg)
	updateSessionTimes(session, timestamp)
	return msg
}

func (m *messageRecord) addTokenDelta(delta tokenSnapshot) {
	if delta.Input == 0 && delta.Cached == 0 && delta.Output == 0 && delta.Reasoning == 0 {
		return
	}
	if m.Entry.Tokens == nil {
		m.Entry.Tokens = &stats.TokenStats{}
	}
	m.Entry.Tokens.Input += delta.Input
	m.Entry.Tokens.Cache.Read += delta.Cached
	m.Entry.Tokens.Output += delta.Output
	m.Entry.Tokens.Reasoning += delta.Reasoning
	m.Entry.Tokens.Cache.Write = 0
	m.Entry.Role = "assistant"
}

func (m *messageRecord) addToolCall(response *responseItemRecord, timestamp time.Time) {
	callID := response.CallID
	if callID == "" {
		callID = fmt.Sprintf("tool:%d:%d", timestamp.UnixMilli(), len(m.ToolParts)+1)
	}
	if m.seenTools[callID] {
		return
	}
	m.seenTools[callID] = true
	name := response.ToolName
	if name == "" {
		name = response.ItemType
	}
	input, truncation, redacted := redactToolInput(response.Text)
	status := "partial"
	if response.Status == "completed" || response.Status == "success" {
		status = "completed"
	}
	m.ToolParts = append(m.ToolParts, stats.ToolPart{
		SourceID: codexSourceID,
		Type:     "tool",
		CallID:   callID,
		Tool:     name,
		State: stats.ToolState{
			Status:     status,
			Input:      input,
			Title:      name,
			Truncation: truncation,
			Redacted:   redacted,
			Time:       &stats.ToolTime{Start: timestamp.UnixMilli()},
		},
	})
	m.Entry.FoldedToolCalls++
}

func (m *messageRecord) applyToolOutput(response *responseItemRecord, timestamp time.Time) {
	if response.CallID == "" {
		return
	}
	for i := range m.ToolParts {
		tool := &m.ToolParts[i]
		if tool.CallID != response.CallID {
			continue
		}
		if response.IsError {
			tool.State.Status = "error"
		} else {
			tool.State.Status = "completed"
		}
		output, truncation, redacted := redactToolText(response.Text)
		tool.State.Output = output
		tool.State.Truncation = mergeTruncation(tool.State.Truncation, truncation)
		tool.State.Redacted = tool.State.Redacted || redacted
		if tool.State.Time == nil {
			tool.State.Time = &stats.ToolTime{}
		}
		tool.State.Time.End = timestamp.UnixMilli()
		return
	}
}

func (m *messageRecord) applyToolStatus(callID, status string, timestamp time.Time) {
	for i := range m.ToolParts {
		tool := &m.ToolParts[i]
		if tool.CallID != callID {
			continue
		}
		switch status {
		case "success", "completed":
			tool.State.Status = "completed"
		case "error", "failed", "failure":
			tool.State.Status = "error"
		}
		if tool.State.Time == nil {
			tool.State.Time = &stats.ToolTime{}
		}
		tool.State.Time.End = timestamp.UnixMilli()
		return
	}
}

func (m *messageRecord) recomputeCost(pricing pricingSnapshot) {
	if m.Entry.Tokens == nil {
		missing := missingCost(defaultCurrency(pricing))
		m.Entry.Cost = 0
		m.Entry.CostStatus = missing.Status
		m.Entry.CostProvenance = missing.Provenance
		return
	}
	result := computeCost(m.Entry.ModelID, *m.Entry.Tokens, m.maxInputSnapshot, pricing)
	m.Entry.Cost = result.Cost
	m.Entry.CostStatus = result.Status
	m.Entry.CostProvenance = result.Provenance
}

func positiveDelta(previous, current tokenSnapshot) tokenSnapshot {
	return tokenSnapshot{
		Input:     positive(current.Input - previous.Input),
		Cached:    positive(current.Cached - previous.Cached),
		Output:    positive(current.Output - previous.Output),
		Reasoning: positive(current.Reasoning - previous.Reasoning),
		Total:     positive(current.Total - previous.Total),
	}
}

func maxSnapshot(previous, current tokenSnapshot) tokenSnapshot {
	return tokenSnapshot{
		Input:     maxInt(previous.Input, current.Input),
		Cached:    maxInt(previous.Cached, current.Cached),
		Output:    maxInt(previous.Output, current.Output),
		Reasoning: maxInt(previous.Reasoning, current.Reasoning),
		Total:     maxInt(previous.Total, current.Total),
	}
}

func positive(value int64) int64 {
	if value > 0 {
		return value
	}
	return 0
}

func maxInt(a, b int64) int64 {
	if b > a {
		return b
	}
	return a
}

func (s *snapshot) ensureProject(id, name, worktree string) *projectRecord {
	if id == "" {
		id = "unknown"
	}
	if name == "" {
		name = id
	}
	project := s.projectMap[id]
	if project == nil {
		project = &projectRecord{ID: id, Name: name, Worktree: worktree}
		s.projectMap[id] = project
	}
	if project.Worktree == "" && worktree != "" {
		project.Worktree = worktree
	}
	return project
}

func (s *snapshot) ensureSession(id string, project *projectRecord, directory string) *sessionRecord {
	if id == "" {
		id = "unknown-session"
	}
	if project == nil {
		project = s.ensureProject("unknown", "unknown", directory)
	}
	session := s.sessionMap[id]
	if session == nil {
		session = &sessionRecord{ID: id, ProjectID: project.ID, ProjectName: project.Name, Directory: directory}
		s.sessionMap[id] = session
	}
	if session.Directory == "" && directory != "" {
		session.Directory = directory
	}
	return session
}

func updateSessionTimes(session *sessionRecord, timestamp time.Time) {
	if session == nil || timestamp.IsZero() {
		return
	}
	if session.Created.IsZero() || timestamp.Before(session.Created) {
		session.Created = timestamp.UTC()
	}
	if session.Updated.IsZero() || timestamp.After(session.Updated) {
		session.Updated = timestamp.UTC()
	}
}

func sessionTitle(session *sessionRecord) string {
	for _, msg := range session.Messages {
		for _, part := range msg.TextParts {
			text := strings.TrimSpace(part.Text)
			if text == "" {
				continue
			}
			if len(text) > 80 {
				return text[:80] + "..."
			}
			return text
		}
	}
	return session.ID
}

func projectFromPath(path string) (string, string) {
	base := filepath.Base(path)
	if base == "." || base == string(filepath.Separator) || base == "" || base == "[REDACTED_PATH]" {
		return "unknown", "unknown"
	}
	return safeID(base), base
}

func safeID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var out strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' || r == ':' {
			out.WriteRune(r)
		} else {
			out.WriteRune('-')
		}
	}
	return out.String()
}

func synthesizeMessageID(sessionID, turnID string) string {
	return codexSourceID + ":" + safeID(sessionID) + ":" + safeID(turnID)
}

func defaultCurrency(pricing pricingSnapshot) string {
	if pricing.Currency != "" {
		return pricing.Currency
	}
	return "USD"
}

func nonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func stableRecordDedupeKey(record codexRecord, fileSessionID string) string {
	fingerprint := recordSemanticFingerprint(record, fileSessionID)
	sum := sha256.Sum256([]byte(fingerprint))
	return fmt.Sprintf("%x", sum[:])
}

func recordSemanticFingerprint(record codexRecord, fileSessionID string) string {
	timestamp := ""
	if !record.Timestamp.IsZero() {
		timestamp = record.Timestamp.UTC().Format(time.RFC3339Nano)
	}
	fingerprint := struct {
		Session      string
		Timestamp    string
		TopType      string
		SessionMeta  *sessionMetaRecord
		TurnContext  *turnContextRecord
		Event        *eventMsgRecord
		Response     *responseItemRecord
		Compacted    bool
		FileFallback string
	}{
		Session:      nonEmpty(fileSessionID, record.File.SessionID),
		Timestamp:    timestamp,
		TopType:      record.TopType,
		SessionMeta:  record.SessionMeta,
		TurnContext:  record.TurnContext,
		Event:        record.Event,
		Response:     record.Response,
		Compacted:    record.Compacted,
		FileFallback: nonEmpty(fileSessionID, record.File.SessionID),
	}
	encoded, err := json.Marshal(fingerprint)
	if err != nil {
		return fmt.Sprintf("%#v", fingerprint)
	}
	return string(encoded)
}

func finalizeDiagnostics(diag source.SourceDiagnostics) source.SourceDiagnostics {
	if diag.Status == "unavailable" || diag.Status == "empty" {
		return diag
	}
	if diag.ScannedFiles == 0 {
		diag.Status = "empty"
		if diag.Reason == "" {
			diag.Reason = "no Codex rollout JSONL transcripts found"
		}
		return diag
	}
	if diag.MalformedLines > 0 || diag.UnsupportedEvents > 0 || diag.Reason != "" {
		diag.Status = "partial"
		if diag.Reason == "" {
			diag.Reason = "some Codex JSONL records were skipped"
		}
		return diag
	}
	diag.Status = "ok"
	return diag
}

func (m *messageRecord) detail() *stats.MessageDetail {
	if m == nil {
		return nil
	}
	text := append([]stats.MessagePart{}, m.TextParts...)
	reasoning := append([]stats.MessagePart{}, m.ReasoningParts...)
	tools := append([]stats.ToolPart{}, m.ToolParts...)
	if text == nil {
		text = []stats.MessagePart{}
	}
	if reasoning == nil {
		reasoning = []stats.MessagePart{}
	}
	if tools == nil {
		tools = []stats.ToolPart{}
	}
	return &stats.MessageDetail{
		MessageEntry: m.Entry,
		Content: stats.MessageContent{
			TextParts:      text,
			ReasoningParts: reasoning,
			ToolParts:      tools,
		},
	}
}
