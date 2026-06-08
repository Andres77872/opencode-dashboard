package claudecode

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
	Entry          stats.MessageEntry
	TextParts      []stats.MessagePart
	ReasoningParts []stats.MessagePart
	ToolParts      []stats.ToolPart
	projectID      string
	line           int
}

type pendingToolRef struct {
	MessageID string
	Index     int
}

// assistantMessageState tracks an in-progress assistant message so that streaming
// transcript chunks sharing one request id merge into a single API-request row
// instead of being counted as separate messages.
type assistantMessageState struct {
	msg          *messageRecord
	contribution assistantContribution
}

type assistantContribution struct {
	usage      tokenUsage
	hasUsage   bool
	cost       costResult
	cumulative bool
}

func normalizeRecords(home string, records []parsedRecord, pricing pricingSnapshot, diag source.SourceDiagnostics) *snapshot {
	snap := &snapshot{
		home:       home,
		projectMap: make(map[string]*projectRecord),
		sessionMap: make(map[string]*sessionRecord),
		messageMap: make(map[string]*messageRecord),
		ordered:    make([]*messageRecord, 0, len(records)),
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

	pendingTools := make(map[string]pendingToolRef)
	// requestMessages keys an in-progress assistant message by its API request so
	// streaming transcript chunks (same requestId / API message id) collapse into a
	// single API-request row rather than separate messages.
	requestMessages := make(map[string]*assistantMessageState)
	seenRecords := make(map[string]struct{}, len(records))
	for _, record := range records {
		sessionID := recordSessionID(record)
		timestamp := recordTimestamp(record)

		// Deduplicate before aggregation so copied transcript files or repeated JSONL
		// records do not double-count cost/tokens/messages. UUID-bearing records are
		// keyed by session/message identity plus equivalent semantic content; this
		// skips exact duplicate copies without suppressing distinct assistant usage if
		// a future Claude schema reuses an ID for different usage/content. UUID-less
		// records use the same semantic fingerprint without file path/line so exact
		// copied records collapse while genuinely different records remain counted.
		dedupeKey := stableRecordDedupeKey(sessionID, record)
		if _, exists := seenRecords[dedupeKey]; exists {
			continue
		}
		seenRecords[dedupeKey] = struct{}{}

		if len(record.ToolResults) > 0 && record.Role != "assistant" {
			snap.applyToolResults(sessionID, record.ToolResults, pendingTools, timestamp)
			if session := snap.sessionMap[sessionID]; session != nil {
				updateSessionTimes(session, timestamp)
			}
		}

		if isInternalMetadataRecord(record) {
			diag.UnsupportedEvents++
			continue
		}

		switch record.Role {
		case "user":
			if !hasUserPromptText(record) {
				continue
			}
			// One row per user prompt (main or subagent). Cost/token rollups ignore
			// user rows; they exist for the conversation view and session titles.
			msg := snap.newMessage(sessionID, record, timestamp, "user")
			msg.appendTextParts(record.TextParts)
			msg.appendReasoningParts(record.ReasoningParts)

		case "assistant":
			// One row per assistant API request. Subagent (sidechain) requests roll
			// into the same session as the parent, flagged via Agent/IsSubagent.
			snap.addAssistantRequest(sessionID, record, timestamp, pricing, pendingTools, requestMessages)
			snap.applyToolResults(sessionID, record.ToolResults, pendingTools, timestamp)
		}
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

func recordSessionID(record parsedRecord) string {
	if record.SessionID != "" {
		return record.SessionID
	}
	if record.File.SessionID != "" {
		return record.File.SessionID
	}
	return "unknown-session"
}

func recordTimestamp(record parsedRecord) time.Time {
	if !record.Timestamp.IsZero() {
		return record.Timestamp
	}
	return record.File.ModTime
}

// newMessage creates a single dashboard message row (one user prompt or one
// assistant API request) and registers it on the project, session and snapshot
// ordering. Cost/token fields are left zero here; assistant rows are populated by
// addAssistantRequest, and user rows carry no cost.
func (s *snapshot) newMessage(sessionID string, record parsedRecord, timestamp time.Time, role string) *messageRecord {
	projectID := record.File.ProjectID
	projectPath := record.File.ProjectPath
	if record.CWD != "" {
		projectPath = record.CWD
	}
	project := s.ensureProject(projectID, projectPath)
	session := s.ensureSession(sessionID, project, projectPath)
	msg := &messageRecord{
		Entry: stats.MessageEntry{
			SourceID:    claudeSourceID,
			ID:          synthesizeMessageID(sessionID, record.UUID, record.Line),
			SessionID:   sessionID,
			Role:        role,
			TimeCreated: timestamp.UTC(),
			Agent:       record.Agent,
			IsSubagent:  record.IsSidechain,
		},
		projectID: project.ID,
		line:      record.Line,
	}
	s.messageMap[msg.Entry.ID] = msg
	s.ordered = append(s.ordered, msg)
	session.Messages = append(session.Messages, msg)
	updateSessionTimes(session, timestamp)
	return msg
}

// addAssistantRequest records one assistant API request as its own message. When
// a later transcript chunk shares the same request id (streaming), it merges into
// the existing row instead of creating a new message or double-counting usage.
func (s *snapshot) addAssistantRequest(sessionID string, record parsedRecord, timestamp time.Time, pricing pricingSnapshot, pendingTools map[string]pendingToolRef, requestMessages map[string]*assistantMessageState) {
	contribution := assistantContribution{
		usage:      record.Usage,
		hasUsage:   record.HasUsage,
		cost:       computeAssistantCost(record, pricing),
		cumulative: record.ReportedUSDCumulative,
	}

	key := assistantBillingKey(sessionID, record)
	if key != "" {
		if state, ok := requestMessages[key]; ok {
			state.contribution = mergeRepeatedBillingContribution(state.contribution, contribution)
			s.appendAssistantContent(state.msg, sessionID, record, timestamp, pendingTools)
			if record.Model != "" && state.msg.Entry.ModelID == "" {
				state.msg.Entry.ModelID = record.Model
				state.msg.Entry.ProviderID = "anthropic"
			}
			applyContributionToEntry(&state.msg.Entry, state.contribution)
			updateSessionTimes(s.sessionMap[sessionID], timestamp)
			return
		}
	}

	msg := s.newMessage(sessionID, record, timestamp, "assistant")
	if record.Model != "" {
		msg.Entry.ModelID = record.Model
		msg.Entry.ProviderID = "anthropic"
	}
	s.appendAssistantContent(msg, sessionID, record, timestamp, pendingTools)
	applyContributionToEntry(&msg.Entry, contribution)
	if key != "" {
		requestMessages[key] = &assistantMessageState{msg: msg, contribution: contribution}
	}
}

// computeAssistantCost derives the per-request cost. Cumulative reported totals
// (total_cost_usd) cannot be summed across requests without overcounting, so when
// usage tokens are present they are preferred over the cumulative reported value.
func computeAssistantCost(record parsedRecord, pricing pricingSnapshot) costResult {
	reported := record.ReportedUSD
	if record.ReportedUSDCumulative && record.HasUsage {
		reported = nil
	}
	return computeCost(record.Model, record.Usage, record.HasUsage, reported, pricing)
}

// applyContributionToEntry writes a single API request's cost and tokens onto its
// message row (replacing, not accumulating — one request == one usage).
func applyContributionToEntry(entry *stats.MessageEntry, c assistantContribution) {
	entry.Cost = c.cost.Cost
	status := c.cost.Status
	if status == "" {
		status = stats.CostMissing
	}
	entry.CostStatus = status
	entry.CostProvenance = c.cost.Provenance
	if c.hasUsage {
		var tokens stats.TokenStats
		addUsageToTokens(&tokens, c.usage)
		entry.Tokens = tokenPointer(tokens, true)
	} else {
		entry.Tokens = nil
	}
}

func assistantBillingKey(sessionID string, record parsedRecord) string {
	if record.RequestID != "" {
		return "request\x00" + sessionID + "\x00" + record.RequestID
	}
	if record.APIMessageID != "" {
		return "message\x00" + sessionID + "\x00" + record.APIMessageID
	}
	return ""
}

func mergeRepeatedBillingContribution(previous, current assistantContribution) assistantContribution {
	if !current.hasUsage && current.cost.Status == stats.CostMissing && current.cost.Cost == 0 {
		return previous
	}
	if current.cost.Status == stats.CostMissing && previous.cost.Status != stats.CostMissing {
		if current.hasUsage {
			previous.usage = current.usage
			previous.hasUsage = true
		}
		return previous
	}
	return current
}

// appendAssistantContent appends one transcript chunk's text, reasoning and tool
// uses onto an assistant message row.
func (s *snapshot) appendAssistantContent(msg *messageRecord, sessionID string, record parsedRecord, timestamp time.Time, pendingTools map[string]pendingToolRef) {
	msg.appendTextParts(record.TextParts)
	msg.appendReasoningParts(record.ReasoningParts)
	s.addToolUses(msg, sessionID, record, timestamp, pendingTools)
}

func (s *snapshot) addToolUses(msg *messageRecord, sessionID string, record parsedRecord, timestamp time.Time, pendingTools map[string]pendingToolRef) {
	for _, toolUse := range record.ToolUses {
		input, truncation, redacted := redactAndTruncateToolInput(toolUse.Input)
		name := toolUse.Name
		if name == "" {
			name = "unknown"
		}
		callID := toolUse.ID
		if callID == "" {
			callID = fmt.Sprintf("%s:tool:%d:%d", sessionID, record.Line, len(msg.ToolParts)+1)
		}
		msg.ToolParts = append(msg.ToolParts, stats.ToolPart{
			SourceID: claudeSourceID,
			Type:     "tool",
			CallID:   callID,
			Tool:     name,
			State: stats.ToolState{
				Status:     "partial",
				Input:      input,
				Title:      name,
				Truncation: truncation,
				Redacted:   redacted,
				Time:       &stats.ToolTime{Start: timestamp.UnixMilli()},
			},
		})
		pendingTools[toolKey(sessionID, callID)] = pendingToolRef{MessageID: msg.Entry.ID, Index: len(msg.ToolParts) - 1}
	}
}

func addUsageToTokens(tokens *stats.TokenStats, usage tokenUsage) {
	tokens.Input += usage.Input
	tokens.Output += usage.Output
	tokens.Reasoning += usage.Reasoning
	tokens.Cache.Read += usage.CacheRead
	tokens.Cache.Write += usage.CacheCreate
}

func tokenPointer(tokens stats.TokenStats, ok bool) *stats.TokenStats {
	if !ok {
		return nil
	}
	out := tokens
	return &out
}

func (m *messageRecord) appendTextParts(texts []string) {
	for _, text := range texts {
		truncated, info := truncateText(text, messageTextMaxBytes)
		m.TextParts = append(m.TextParts, stats.MessagePart{Type: "text", Text: truncated, Truncation: info})
	}
}

func (m *messageRecord) appendReasoningParts(texts []string) {
	for _, text := range texts {
		truncated, info := truncateText(text, messageTextMaxBytes)
		m.ReasoningParts = append(m.ReasoningParts, stats.MessagePart{Type: "reasoning", Text: truncated, Truncation: info})
	}
}

func (s *snapshot) applyToolResults(sessionID string, results []parsedToolResult, pendingTools map[string]pendingToolRef, timestamp time.Time) {
	for _, result := range results {
		if ref, ok := pendingTools[toolKey(sessionID, result.ToolUseID)]; ok {
			s.applyToolResult(ref, result, timestamp)
		}
	}
}

func hasUserPromptText(record parsedRecord) bool {
	return len(record.TextParts) > 0 || len(record.ReasoningParts) > 0
}

func isInternalMetadataRecord(record parsedRecord) bool {
	return record.IsMeta || (record.Role != "user" && record.Role != "assistant")
}

func stableRecordDedupeKey(sessionID string, record parsedRecord) string {
	semantic := recordSemanticFingerprint(sessionID, record)
	role := record.Role
	if role == "" {
		role = "unknown"
	}
	if record.UUID != "" {
		return "uuid\x00" + sessionID + "\x00" + role + "\x00" + record.UUID + "\x00" + semantic
	}
	return "semantic\x00" + semantic
}

func recordSemanticFingerprint(sessionID string, record parsedRecord) string {
	timestamp := ""
	if !record.Timestamp.IsZero() {
		timestamp = record.Timestamp.UTC().Format(time.RFC3339Nano)
	}
	fingerprint := struct {
		SessionID      string
		Role           string
		IsMeta         bool
		RequestID      string
		APIMessageID   string
		Timestamp      string
		CWD            string
		Model          string
		Usage          tokenUsage
		HasUsage       bool
		ReportedUSD    *float64
		CumulativeCost bool
		TextParts      []string
		ReasoningParts []string
		ToolUses       []parsedToolUse
		ToolResults    []parsedToolResult
	}{
		SessionID:      sessionID,
		Role:           record.Role,
		IsMeta:         record.IsMeta,
		RequestID:      record.RequestID,
		APIMessageID:   record.APIMessageID,
		Timestamp:      timestamp,
		CWD:            record.CWD,
		Model:          record.Model,
		Usage:          record.Usage,
		HasUsage:       record.HasUsage,
		ReportedUSD:    record.ReportedUSD,
		CumulativeCost: record.ReportedUSDCumulative,
		TextParts:      record.TextParts,
		ReasoningParts: record.ReasoningParts,
		ToolUses:       record.ToolUses,
		ToolResults:    record.ToolResults,
	}
	encoded, err := json.Marshal(fingerprint)
	if err != nil {
		encoded = []byte(fmt.Sprintf("%#v", fingerprint))
	}
	sum := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", sum[:])
}

func isToolResultOnlyRecord(record parsedRecord) bool {
	return record.Role != "assistant" &&
		len(record.ToolResults) > 0 &&
		len(record.TextParts) == 0 &&
		len(record.ReasoningParts) == 0 &&
		len(record.ToolUses) == 0
}

func (s *snapshot) ensureProject(id, worktree string) *projectRecord {
	if id == "" {
		id = "unknown"
	}
	project, ok := s.projectMap[id]
	if !ok {
		project = &projectRecord{ID: id, Worktree: worktree, Name: projectName(id, worktree)}
		s.projectMap[id] = project
	}
	if project.Worktree == "" && worktree != "" {
		project.Worktree = worktree
		project.Name = projectName(id, worktree)
	}
	return project
}

func (s *snapshot) ensureSession(id string, project *projectRecord, directory string) *sessionRecord {
	if id == "" {
		id = "unknown-session"
	}
	session, ok := s.sessionMap[id]
	if !ok {
		session = &sessionRecord{ID: id, ProjectID: project.ID, ProjectName: project.Name, Directory: directory}
		s.sessionMap[id] = session
	}
	if session.Directory == "" && directory != "" {
		session.Directory = directory
	}
	return session
}

func updateSessionTimes(session *sessionRecord, timestamp time.Time) {
	if timestamp.IsZero() {
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
	// Prefer the first human (non-subagent) user prompt, then any user prompt, then
	// any text-bearing row, so subagent prompts never displace the real session title.
	if text := firstPromptText(session, func(m *messageRecord) bool { return m.Entry.Role == "user" && !m.Entry.IsSubagent }); text != "" {
		return text
	}
	if text := firstPromptText(session, func(m *messageRecord) bool { return m.Entry.Role == "user" }); text != "" {
		return text
	}
	if text := firstPromptText(session, func(m *messageRecord) bool { return true }); text != "" {
		return text
	}
	return session.ID
}

func firstPromptText(session *sessionRecord, accept func(*messageRecord) bool) string {
	for _, msg := range session.Messages {
		if !accept(msg) || len(msg.TextParts) == 0 {
			continue
		}
		text := strings.TrimSpace(msg.TextParts[0].Text)
		if text == "" {
			continue
		}
		if len(text) > 80 {
			return text[:80] + "..."
		}
		return text
	}
	return ""
}

func synthesizeMessageID(sessionID, uuid string, line int) string {
	if uuid == "" {
		uuid = fmt.Sprintf("line-%d", line)
	}
	replacer := strings.NewReplacer("/", "_", " ", "_", "?", "_", "#", "_")
	return claudeSourceID + ":" + replacer.Replace(sessionID) + ":" + replacer.Replace(uuid)
}

func toolKey(sessionID, callID string) string {
	return sessionID + "\x00" + callID
}

func (s *snapshot) applyToolResult(ref pendingToolRef, result parsedToolResult, timestamp time.Time) {
	msg := s.messageMap[ref.MessageID]
	if msg == nil || ref.Index < 0 || ref.Index >= len(msg.ToolParts) {
		return
	}
	tool := &msg.ToolParts[ref.Index]
	if result.IsError {
		tool.State.Status = "error"
	} else {
		tool.State.Status = "completed"
	}
	output, redacted := stringifyToolContent(result.Content)
	output, truncation := truncateText(output, toolTextMaxBytes)
	tool.State.Output = output
	tool.State.Redacted = tool.State.Redacted || redacted
	tool.State.Truncation = mergeTruncation(tool.State.Truncation, truncation)
	if tool.State.Time == nil {
		tool.State.Time = &stats.ToolTime{}
	}
	tool.State.Time.End = timestamp.UnixMilli()
	if result.SpillFile != "" {
		if tool.State.Metadata == nil {
			tool.State.Metadata = map[string]any{}
		}
		tool.State.Metadata["deferred"] = true
		tool.State.Metadata["spill_file"] = true
		tool.State.Metadata["spill_path"] = filepath.Base(result.SpillFile)
	}
}

func finalizeDiagnostics(diag source.SourceDiagnostics) source.SourceDiagnostics {
	if diag.Status == "unavailable" {
		return diag
	}
	if diag.ScannedFiles == 0 {
		diag.Status = "empty"
		if diag.Reason == "" {
			diag.Reason = "no persisted Claude Code JSONL transcripts found"
		}
		return diag
	}
	if diag.MalformedLines > 0 || diag.UnsupportedEvents > 0 || diag.Reason != "" {
		diag.Status = "partial"
		if diag.Reason == "" {
			diag.Reason = "some Claude Code JSONL records were skipped"
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
	entry := m.Entry
	return &stats.MessageDetail{
		MessageEntry: entry,
		Content: stats.MessageContent{
			TextParts:      text,
			ReasoningParts: reasoning,
			ToolParts:      tools,
		},
	}
}

func cloneProvenance(in *stats.CostProvenance) *stats.CostProvenance {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneTokens(in *stats.TokenStats) *stats.TokenStats {
	if in == nil {
		return nil
	}
	out := *in
	return &out
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
