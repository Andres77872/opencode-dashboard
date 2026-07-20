package qwencode

import (
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

	// One API call can be recorded up to three times: as an assistant
	// transcript record, as a ui_telemetry api_response echo, and as a
	// token-usage log row. transcriptCovered counts assistant tuples not yet
	// matched by a telemetry echo; requestCovered counts materialized request
	// rows (assistant or telemetry-synthesized) not yet matched by a
	// usage-log row. Keeping them separate is what prevents a call recorded
	// in all three stores from producing two rows.
	transcriptCovered map[string]int
	requestCovered    map[string]int
}

type messageRecord struct {
	Entry          stats.MessageEntry
	TextParts      []stats.MessagePart
	ReasoningParts []stats.MessagePart
	ToolParts      []stats.ToolPart
	projectID      string
}

func normalizeData(home string, chats []parsedChatSession, usageLog []usageLogRecord, rollups map[string]sessionRollup, pricing pricingSnapshot, diag source.SourceDiagnostics) *snapshot {
	snap := &snapshot{
		home:        home,
		diagnostics: diag,
		projectMap:  make(map[string]*projectRecord),
		sessionMap:  make(map[string]*sessionRecord),
		messageMap:  make(map[string]*messageRecord),
		ordered:     make([]*messageRecord, 0),
	}

	sort.Slice(chats, func(i, j int) bool { return chats[i].File.Path < chats[j].File.Path })
	for _, chat := range chats {
		normalizeChatSession(snap, pricing, chat)
	}
	normalizeUsageLog(snap, pricing, usageLog, rollups)

	sort.SliceStable(snap.ordered, func(i, j int) bool {
		if snap.ordered[i].Entry.TimeCreated.Equal(snap.ordered[j].Entry.TimeCreated) {
			return snap.ordered[i].Entry.ID < snap.ordered[j].Entry.ID
		}
		return snap.ordered[i].Entry.TimeCreated.Before(snap.ordered[j].Entry.TimeCreated)
	})
	for _, session := range snap.sessionMap {
		sort.SliceStable(session.Messages, func(i, j int) bool {
			if session.Messages[i].Entry.TimeCreated.Equal(session.Messages[j].Entry.TimeCreated) {
				return session.Messages[i].Entry.ID < session.Messages[j].Entry.ID
			}
			return session.Messages[i].Entry.TimeCreated.Before(session.Messages[j].Entry.TimeCreated)
		})
		if session.Title == "" {
			session.Title = derivedSessionTitle(session)
		}
		for _, message := range session.Messages {
			message.Entry.SessionTitle = session.Title
		}
	}
	return snap
}

func normalizeChatSession(snap *snapshot, pricing pricingSnapshot, chat parsedChatSession) {
	workDir := firstCWD(chat.Records)
	projectID, projectName := projectIdentity(workDir, chat.File.ProjectDir)
	directory := redactDisplayPath(workDir)
	project := snap.ensureProject(projectID, projectName, directory)
	session := snap.ensureSession(chat.File.SessionID, project, directory)
	if session.Created.IsZero() {
		session.Created = chat.File.ModTime
	}
	if session.Updated.IsZero() {
		session.Updated = chat.File.ModTime
	}

	// Assistant records carry no auth type, but the telemetry echoes of the
	// same requests do; the session-level hint selects the right reasoning
	// token overlap semantics (additive on gemini/vertex-ai, subset otherwise).
	authType := ""
	for _, record := range chat.Records {
		if record.APIEvent != nil && record.APIEvent.AuthType != "" {
			authType = record.APIEvent.AuthType
			break
		}
	}

	// Pass 1: transcript rows. Assistant records are the canonical request
	// rows; their usage tuples mark telemetry echoes as covered.
	toolRefs := make(map[string]toolRef)
	var events []chatRecord
	for _, record := range chat.Records {
		timestamp := record.Timestamp
		if timestamp.IsZero() {
			timestamp = chat.File.ModTime
		}
		switch record.Type {
		case "user":
			addUserMessage(snap, session, project, record, timestamp)
		case "assistant":
			addAssistantMessage(snap, pricing, session, project, record, timestamp, authType, toolRefs)
		case "tool_result":
			applyToolResult(record, timestamp, toolRefs)
		case "system":
			if record.APIEvent != nil {
				events = append(events, record)
			}
		}
		updateSessionTimes(session, timestamp)
	}

	// Pass 2: api_response telemetry. Events matching an assistant record are
	// echoes of rows already created; the rest are auxiliary requests (for
	// example subagents like the memory extractor) that only telemetry saw.
	for _, record := range events {
		event := record.APIEvent
		key := event.Usage.matchKey(event.Model)
		if session.transcriptCovered[key] > 0 {
			session.transcriptCovered[key]--
			continue
		}
		timestamp := record.Timestamp
		if timestamp.IsZero() {
			timestamp = chat.File.ModTime
		}
		id := messageID(session.ID, "e", record.UUID, record.Line)
		msg := newMessage(session, project, id, "assistant", timestamp)
		msg.Entry.ModelID = event.Model
		msg.Entry.ProviderID = inferProvider(event.Model, authTypeProvider(event.AuthType))
		msg.Entry.Agent = event.SubagentName
		msg.Entry.IsSubagent = event.SubagentName != ""
		msg.Entry.Tokens = tokensFromCounts(event.Usage, event.AuthType)
		msg.recomputeCost(pricing)
		register(snap, session, msg)
		session.coverRequest(key)
	}
}

// normalizeUsageLog reconciles the token-usage request log against rows the
// transcripts already produced, then materializes the remainder: requests of
// transcript-less sessions and auxiliary calls no transcript recorded.
func normalizeUsageLog(snap *snapshot, pricing pricingSnapshot, usageLog []usageLogRecord, rollups map[string]sessionRollup) {
	for _, record := range usageLog {
		session := snap.sessionMap[record.SessionID]
		if session == nil {
			session = ensureUsageSession(snap, record.SessionID, rollups)
		}
		key := record.Usage.matchKey(record.Model)
		if session.requestCovered[key] > 0 {
			session.requestCovered[key]--
			continue
		}
		timestamp := record.Timestamp
		if timestamp.IsZero() {
			timestamp = session.Updated
		}
		project := snap.projectMap[session.ProjectID]
		id := messageID(session.ID, "g", record.ID, 0)
		msg := newMessage(session, project, id, "assistant", timestamp)
		msg.Entry.ModelID = record.Model
		msg.Entry.ProviderID = inferProvider(record.Model, authTypeProvider(record.AuthType))
		if record.Source != "" && record.Source != "main" {
			msg.Entry.Agent = record.Source
			msg.Entry.IsSubagent = true
		}
		msg.Entry.Tokens = tokensFromCounts(record.Usage, record.AuthType)
		msg.recomputeCost(pricing)
		register(snap, session, msg)
		updateSessionTimes(session, timestamp)
	}
}

func ensureUsageSession(snap *snapshot, sessionID string, rollups map[string]sessionRollup) *sessionRecord {
	rollup := rollups[sessionID]
	projectID, projectName := projectIdentity(rollup.Project, "")
	directory := redactDisplayPath(rollup.Project)
	project := snap.ensureProject(projectID, projectName, directory)
	session := snap.ensureSession(sessionID, project, directory)
	if session.Created.IsZero() {
		session.Created = rollup.StartTime
	}
	if session.Updated.IsZero() {
		session.Updated = rollup.EndTime
	}
	return session
}

func addUserMessage(snap *snapshot, session *sessionRecord, project *projectRecord, record chatRecord, timestamp time.Time) {
	parts := make([]string, 0, len(record.Parts))
	for _, part := range record.Parts {
		if _, isThought := part.thoughtText(); isThought || part.FunctionCall != nil || len(part.FunctionResponse) > 0 {
			continue
		}
		if strings.TrimSpace(part.Text) != "" {
			parts = append(parts, part.Text)
		}
	}
	if len(parts) == 0 {
		return
	}
	id := messageID(session.ID, "t", record.UUID, record.Line)
	msg := newMessage(session, project, id, "user", timestamp)
	for _, part := range parts {
		msg.TextParts = append(msg.TextParts, redactAndTruncateMessagePart("text", part))
	}
	register(snap, session, msg)
}

func addAssistantMessage(snap *snapshot, pricing pricingSnapshot, session *sessionRecord, project *projectRecord, record chatRecord, timestamp time.Time, authType string, toolRefs map[string]toolRef) {
	id := messageID(session.ID, "t", record.UUID, record.Line)
	msg := newMessage(session, project, id, "assistant", timestamp)
	msg.Entry.ModelID = record.Model
	msg.Entry.ProviderID = inferProvider(record.Model, authTypeProvider(authType))
	if record.Usage != nil {
		msg.Entry.Tokens = tokensFromCounts(*record.Usage, authType)
		key := record.Usage.matchKey(record.Model)
		session.coverTranscript(key)
		session.coverRequest(key)
	}
	for _, part := range record.Parts {
		if thought, ok := part.thoughtText(); ok {
			if thought != "" {
				msg.ReasoningParts = append(msg.ReasoningParts, redactAndTruncateMessagePart("reasoning", thought))
			}
			continue
		}
		if part.FunctionCall != nil {
			addToolCall(msg, part.FunctionCall, timestamp, toolRefs)
			continue
		}
		if part.Text != "" {
			msg.TextParts = append(msg.TextParts, redactAndTruncateMessagePart("text", part.Text))
		}
	}
	msg.recomputeCost(pricing)
	register(snap, session, msg)
}

type toolRef struct {
	message *messageRecord
	index   int
}

func addToolCall(msg *messageRecord, call *functionCall, timestamp time.Time, toolRefs map[string]toolRef) {
	callID := call.ID
	if callID == "" {
		callID = fmt.Sprintf("%s:tool:%d", msg.Entry.ID, len(msg.ToolParts)+1)
	}
	if _, exists := toolRefs[callID]; exists {
		return
	}
	name := call.Name
	if name == "" {
		name = "unknown"
	}
	input, truncation, redacted := redactToolInput(call.Args)
	msg.ToolParts = append(msg.ToolParts, stats.ToolPart{
		SourceID: qwenSourceID,
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
	toolRefs[callID] = toolRef{message: msg, index: len(msg.ToolParts) - 1}
}

func applyToolResult(record chatRecord, timestamp time.Time, toolRefs map[string]toolRef) {
	result := record.ToolResult
	if result == nil || result.CallID == "" {
		return
	}
	ref, ok := toolRefs[result.CallID]
	if !ok || ref.message == nil || ref.index < 0 || ref.index >= len(ref.message.ToolParts) {
		return
	}
	tool := &ref.message.ToolParts[ref.index]
	output, truncation, redacted := redactToolText(valueToText(result.ResultDisplay))
	tool.State.Output = output
	tool.State.Truncation = mergeTruncation(tool.State.Truncation, truncation)
	tool.State.Redacted = tool.State.Redacted || redacted
	if result.Status == "error" {
		tool.State.Status = "error"
		if tool.State.Error == "" {
			tool.State.Error = output
		}
	} else {
		tool.State.Status = "completed"
	}
	if tool.State.Time == nil {
		tool.State.Time = &stats.ToolTime{}
	}
	tool.State.Time.End = timestamp.UnixMilli()
}

func newMessage(session *sessionRecord, project *projectRecord, id, role string, timestamp time.Time) *messageRecord {
	projectID := "unknown"
	if project != nil {
		projectID = project.ID
	}
	return &messageRecord{
		Entry: stats.MessageEntry{
			SourceID:    qwenSourceID,
			ID:          id,
			SessionID:   session.ID,
			Role:        role,
			TimeCreated: timestamp.UTC(),
		},
		projectID: projectID,
	}
}

func register(snap *snapshot, session *sessionRecord, msg *messageRecord) {
	if msg == nil {
		return
	}
	snap.messageMap[msg.Entry.ID] = msg
	snap.ordered = append(snap.ordered, msg)
	session.Messages = append(session.Messages, msg)
	updateSessionTimes(session, msg.Entry.TimeCreated)
}

func (m *messageRecord) recomputeCost(pricing pricingSnapshot) {
	if m.Entry.Role != "assistant" || m.Entry.Tokens == nil {
		result := missingCost(defaultCurrency(pricing))
		m.Entry.CostStatus = result.Status
		m.Entry.CostProvenance = result.Provenance
		return
	}
	result := computeCost(m.Entry.ModelID, *m.Entry.Tokens, pricing)
	m.Entry.Cost = result.Cost
	m.Entry.CostStatus = result.Status
	m.Entry.CostProvenance = result.Provenance
}

// tokensFromCounts converts raw provider counters to the dashboard's
// non-overlapping accounting. Cached ⊆ Input on every auth path, so cached
// reads leave Input. Thoughts ⊆ Output on the OpenAI-compatible paths
// (openai, qwen-oauth, anthropic) but are additive Gemini-native counters on
// gemini/vertex-ai auth. Qwen bills no separate cache-write tier.
func tokensFromCounts(u usageCounts, authType string) *stats.TokenStats {
	output := u.Output
	if !additiveThoughts(authType) {
		output -= u.Thoughts
	}
	tokens := stats.TokenStats{
		Input:     positive(u.Input - u.Cached),
		Output:    positive(output),
		Reasoning: positive(u.Thoughts),
	}
	tokens.Cache.Read = positive(u.Cached)
	return &tokens
}

func additiveThoughts(authType string) bool {
	switch authType {
	case "gemini", "vertex-ai":
		return true
	default:
		return false
	}
}

func (s *sessionRecord) coverTranscript(key string) {
	if s.transcriptCovered == nil {
		s.transcriptCovered = make(map[string]int)
	}
	s.transcriptCovered[key]++
}

func (s *sessionRecord) coverRequest(key string) {
	if s.requestCovered == nil {
		s.requestCovered = make(map[string]int)
	}
	s.requestCovered[key]++
}

func inferProvider(model, fallback string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "qwen") {
		return "qwen"
	}
	return fallback
}

func authTypeProvider(authType string) string {
	switch authType {
	case "qwen-oauth":
		return "qwen"
	case "":
		return ""
	default:
		return authType
	}
}

func firstCWD(records []chatRecord) string {
	for _, record := range records {
		if strings.TrimSpace(record.CWD) != "" {
			return record.CWD
		}
	}
	return ""
}

// projectIdentity resolves the project from the real working directory when
// the transcript records one, else falls back to the sanitized project
// directory name qwen-code derives from the path (not reversible, but stable).
func projectIdentity(workDir, projectDir string) (string, string) {
	if strings.TrimSpace(workDir) != "" {
		return projectFromPath(workDir)
	}
	base := filepath.Base(strings.TrimSpace(projectDir))
	if base == "." || base == string(filepath.Separator) || base == "" {
		return "unknown", "unknown"
	}
	segments := strings.Split(strings.Trim(base, "-"), "-")
	name := segments[len(segments)-1]
	if name == "" {
		name = base
	}
	return safeID(base), name
}

func projectFromPath(path string) (string, string) {
	base := filepath.Base(strings.TrimSpace(path))
	if base == "." || base == string(filepath.Separator) || base == "" {
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
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			out.WriteRune(r)
		} else {
			out.WriteRune('-')
		}
	}
	return out.String()
}

func messageID(sessionID, kind, uuid string, line int) string {
	suffix := safeID(uuid)
	if strings.TrimSpace(uuid) == "" {
		suffix = fmt.Sprintf("l%d", line)
	}
	return fmt.Sprintf("%s:%s:%s:%s", qwenSourceID, safeID(sessionID), kind, suffix)
}

func sessionIDFromMessageID(id string) (string, bool) {
	parts := strings.Split(id, ":")
	if len(parts) < 4 || parts[0] != qwenSourceID || parts[1] == "" {
		return "", false
	}
	return parts[1], true
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
		session = &sessionRecord{
			ID: id, ProjectID: project.ID, ProjectName: project.Name, Directory: directory,
		}
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

func derivedSessionTitle(session *sessionRecord) string {
	for _, msg := range session.Messages {
		if msg.Entry.Role != "user" {
			continue
		}
		for _, part := range msg.TextParts {
			if title := shortTitle(part.Text); title != "" {
				return title
			}
		}
	}
	return session.ID
}

func shortTitle(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) > 80 {
		value, _ = truncateText(value, 80)
	}
	return value
}

func positive(value int64) int64 {
	if value > 0 {
		return value
	}
	return 0
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
	entry.Tokens = cloneTokens(entry.Tokens)
	entry.CostProvenance = cloneProvenance(entry.CostProvenance)
	return &stats.MessageDetail{
		MessageEntry: entry,
		Content: stats.MessageContent{
			TextParts: text, ReasoningParts: reasoning, ToolParts: tools,
		},
	}
}
