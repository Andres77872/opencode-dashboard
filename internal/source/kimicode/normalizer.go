package kimicode

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
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
}

type stepState struct {
	TurnID   string
	Step     int64
	StepUUID string
}

type agentNormalizer struct {
	snap         *snapshot
	pricing      pricingSnapshot
	session      *sessionRecord
	sessionID    string
	project      *projectRecord
	directory    string
	agentID      string
	agentLabel   string
	isSubagent   bool
	file         agentFile
	currentStep  stepState
	lastModel    string
	lastProvider string
	requestSeq   int
	userSeq      int
	pending      *messageRecord
	pendingKey   string
	seenPrompts  map[string]time.Time
	seenTools    map[string]bool
	toolRefs     map[string]toolRef
}

type toolRef struct {
	message *messageRecord
	index   int
}

func normalizeSessions(home string, parsed []parsedSession, pricing pricingSnapshot, diag source.SourceDiagnostics) *snapshot {
	snap := &snapshot{
		home:        home,
		diagnostics: diag,
		projectMap:  make(map[string]*projectRecord),
		sessionMap:  make(map[string]*sessionRecord),
		messageMap:  make(map[string]*messageRecord),
		ordered:     make([]*messageRecord, 0),
	}
	sort.Slice(parsed, func(i, j int) bool { return parsed[i].Files.Dir < parsed[j].Files.Dir })

	for _, item := range parsed {
		workDir := item.State.WorkDir
		directory := redactDisplayPath(workDir)
		projectID, projectName := projectFromPath(workDir)
		project := snap.ensureProject(projectID, projectName, directory)
		session := snap.ensureSession(item.Files.ID, project, directory)
		session.Title = sanitizeTitle(item.State.Title)
		session.Created = parseStateTime(item.State.CreatedAt)
		session.Updated = parseStateTime(item.State.UpdatedAt)
		if session.Created.IsZero() {
			session.Created = item.Files.ModTime
		}
		if session.Updated.IsZero() {
			session.Updated = item.Files.ModTime
		}

		sort.Slice(item.Agents, func(i, j int) bool { return item.Agents[i].File.AgentID < item.Agents[j].File.AgentID })
		for _, agent := range item.Agents {
			normalizer := newAgentNormalizer(snap, pricing, session, project, directory, agent)
			seenRecords := make(map[string]bool, len(agent.Records))
			for _, record := range agent.Records {
				key := stableWireRecordKey(record)
				if seenRecords[key] {
					continue
				}
				seenRecords[key] = true
				normalizer.apply(record)
			}
			normalizer.flushPending()
		}

		if session.Title == "" || isGenericSessionTitle(session.Title) {
			session.Title = derivedSessionTitle(session, item.State.LastPrompt)
		}
	}

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
			session.Title = derivedSessionTitle(session, "")
		}
		for _, message := range session.Messages {
			message.Entry.SessionTitle = session.Title
		}
	}
	return snap
}

func newAgentNormalizer(snap *snapshot, pricing pricingSnapshot, session *sessionRecord, project *projectRecord, directory string, agent parsedAgent) *agentNormalizer {
	isSubagent := agent.File.AgentID != "main" || agent.Meta.ParentAgentID != nil || (agent.Meta.Type != "" && agent.Meta.Type != "main")
	label := ""
	if isSubagent {
		label = strings.TrimSpace(agent.Meta.Type)
		if label == "" || label == "sub" {
			label = agent.File.AgentID
		}
	}
	return &agentNormalizer{
		snap: snap, pricing: pricing, session: session, sessionID: session.ID,
		project: project, directory: directory, agentID: agent.File.AgentID,
		agentLabel: label, isSubagent: isSubagent, file: agent.File,
		seenPrompts: make(map[string]time.Time), seenTools: make(map[string]bool),
		toolRefs: make(map[string]toolRef),
	}
}

func (n *agentNormalizer) apply(record wireRecord) {
	timestamp := record.Time
	if timestamp.IsZero() {
		timestamp = n.file.ModTime
	}
	switch {
	case record.Prompt != nil:
		n.addPrompt(record.Prompt.Input, record.Prompt.Origin, timestamp)
	case record.Message != nil:
		if record.Message.Role == "user" {
			n.addPrompt(record.Message.Content, record.Message.Origin, timestamp)
		}
	case record.Request != nil:
		n.startRequest(record.Request, timestamp)
	case record.Usage != nil:
		n.closeUsage(record.Usage, timestamp)
	case record.Event != nil:
		n.applyLoopEvent(record.Event, timestamp)
	}
	updateSessionTimes(n.session, timestamp)
}

func (n *agentNormalizer) addPrompt(parts []contentPartRecord, origin originRecord, timestamp time.Time) {
	if origin.Kind != "" && origin.Kind != "user" {
		return
	}
	normalized := promptParts(parts)
	if len(normalized) == 0 {
		return
	}
	fingerprint := promptFingerprint(normalized)
	if previous, ok := n.seenPrompts[fingerprint]; ok {
		delta := timestamp.Sub(previous)
		if delta < 0 {
			delta = -delta
		}
		// turn.prompt is followed by context.append_message for the same
		// user input. Their timestamps are usually identical but can differ by
		// one scheduler tick, so collapse only a very tight duplicate window.
		if delta <= 100*time.Millisecond {
			return
		}
	}
	n.seenPrompts[fingerprint] = timestamp
	n.flushPending()

	id := synthesizeUserID(n.sessionID, n.agentID, timestamp, normalized, n.userSeq)
	n.userSeq++
	msg := n.newMessage(id, "user", timestamp)
	for _, part := range normalized {
		msg.TextParts = append(msg.TextParts, redactAndTruncateMessagePart("text", part))
	}
	n.register(msg)
}

func promptParts(parts []contentPartRecord) []string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "text":
			if strings.TrimSpace(part.Text) != "" {
				out = append(out, part.Text)
			}
		case "think":
			// User prompts should not carry thinking parts.
		default:
			if part.Type != "" {
				out = append(out, "["+part.Type+" input]")
			}
		}
	}
	return out
}

func (n *agentNormalizer) startRequest(request *llmRequestRecord, timestamp time.Time) {
	n.flushPending()
	model := strings.TrimSpace(request.ModelAlias)
	if model == "" {
		model = strings.TrimSpace(request.Model)
	}
	if model != "" {
		n.lastModel = model
	}
	if request.Provider != "" {
		n.lastProvider = request.Provider
	}
	key := request.TurnStep
	if key == "" {
		key = n.currentStep.StepUUID
	}
	if key == "" && n.currentStep.TurnID != "" {
		key = n.currentStep.TurnID + "." + strconv.FormatInt(n.currentStep.Step, 10)
	}
	if request.Attempt != "" {
		key += "." + request.Attempt
	}
	if request.Kind == "compaction" {
		key = "compaction." + key
	}
	if key == "" {
		key = strconv.FormatInt(timestamp.UnixMilli(), 10)
	}
	n.pendingKey = key
	n.pending = n.newAssistantMessage(timestamp, key)
}

func (n *agentNormalizer) closeUsage(usage *usageRecord, timestamp time.Time) {
	if n.pending == nil {
		key := n.currentStep.StepUUID
		if key == "" {
			key = strconv.FormatInt(timestamp.UnixMilli(), 10)
		}
		n.pendingKey = key
		n.pending = n.newAssistantMessage(timestamp, key)
	}
	if usage.Model != "" {
		n.pending.Entry.ModelID = usage.Model
		n.lastModel = usage.Model
	}
	if n.pending.Entry.ProviderID == "" {
		n.pending.Entry.ProviderID = inferProvider(n.pending.Entry.ModelID, n.lastProvider)
	}
	tokens := stats.TokenStats{
		Input:  positive(usage.Usage.InputOther),
		Output: positive(usage.Usage.Output),
		Cache: stats.CacheStats{
			Read:  positive(usage.Usage.InputCacheRead),
			Write: positive(usage.Usage.InputCacheCreation),
		},
	}
	n.pending.Entry.Tokens = &tokens
}

func (n *agentNormalizer) applyLoopEvent(event *loopEventRecord, timestamp time.Time) {
	switch event.Type {
	case "step.begin":
		// Kimi's classic engine writes usage.record after step.end, while the
		// rebuilt engine writes it before content/tool events. The next step is
		// the first boundary that is safely after both orderings.
		n.flushPending()
		n.currentStep = stepState{TurnID: event.TurnID, Step: event.Step, StepUUID: event.UUID}
	case "step.end":
		if n.currentStep.StepUUID == "" {
			n.currentStep = stepState{TurnID: event.TurnID, Step: event.Step, StepUUID: event.UUID}
		}
	case "content.part":
		msg := n.ensurePending(timestamp, event.StepUUID)
		switch event.Part.Type {
		case "think":
			if event.Part.Think != "" {
				msg.ReasoningParts = append(msg.ReasoningParts, redactAndTruncateMessagePart("reasoning", event.Part.Think))
			}
		case "text":
			if event.Part.Text != "" {
				msg.TextParts = append(msg.TextParts, redactAndTruncateMessagePart("text", event.Part.Text))
			}
		}
	case "tool.call":
		n.addToolCall(n.ensurePending(timestamp, event.StepUUID), event, timestamp)
	case "tool.result":
		n.applyToolResult(event, timestamp)
	}
}

func (n *agentNormalizer) ensurePending(timestamp time.Time, stepUUID string) *messageRecord {
	if n.pending != nil {
		return n.pending
	}
	key := stepUUID
	if key == "" {
		key = n.currentStep.StepUUID
	}
	if key == "" {
		key = strconv.FormatInt(timestamp.UnixMilli(), 10)
	}
	n.pendingKey = key
	n.pending = n.newAssistantMessage(timestamp, key)
	return n.pending
}

func (n *agentNormalizer) newAssistantMessage(timestamp time.Time, key string) *messageRecord {
	id := synthesizeRequestID(n.sessionID, n.agentID, key, n.requestSeq)
	msg := n.newMessage(id, "assistant", timestamp)
	msg.Entry.ModelID = n.lastModel
	msg.Entry.ProviderID = inferProvider(n.lastModel, n.lastProvider)
	return msg
}

func (n *agentNormalizer) newMessage(id, role string, timestamp time.Time) *messageRecord {
	return &messageRecord{
		Entry: stats.MessageEntry{
			SourceID:    kimiSourceID,
			ID:          id,
			SessionID:   n.sessionID,
			Role:        role,
			TimeCreated: timestamp.UTC(),
			Agent:       n.agentLabel,
			IsSubagent:  n.isSubagent,
		},
		projectID: n.project.ID,
	}
}

func (n *agentNormalizer) addToolCall(msg *messageRecord, event *loopEventRecord, timestamp time.Time) {
	callID := event.ToolCallID
	if callID == "" {
		callID = event.UUID
	}
	if callID == "" {
		callID = fmt.Sprintf("%s:tool:%d", msg.Entry.ID, len(msg.ToolParts)+1)
	}
	key := n.agentID + "\x00" + callID
	if n.seenTools[key] {
		return
	}
	n.seenTools[key] = true
	name := event.Name
	if name == "" {
		name = "unknown"
	}
	input, truncation, redacted := redactToolInput(event.Args)
	title := event.Description
	if title == "" {
		title = name
	}
	title, titleTruncation, titleRedacted := redactToolText(title)
	truncation = mergeTruncation(truncation, titleTruncation)
	redacted = redacted || titleRedacted
	msg.ToolParts = append(msg.ToolParts, stats.ToolPart{
		SourceID: kimiSourceID,
		Type:     "tool",
		CallID:   callID,
		Tool:     name,
		State: stats.ToolState{
			Status:     "partial",
			Input:      input,
			Title:      title,
			Truncation: truncation,
			Redacted:   redacted,
			Time:       &stats.ToolTime{Start: timestamp.UnixMilli()},
		},
	})
	n.toolRefs[key] = toolRef{message: msg, index: len(msg.ToolParts) - 1}
}

func (n *agentNormalizer) applyToolResult(event *loopEventRecord, timestamp time.Time) {
	callID := event.ToolCallID
	if callID == "" {
		return
	}
	ref, ok := n.toolRefs[n.agentID+"\x00"+callID]
	if !ok || ref.message == nil || ref.index < 0 || ref.index >= len(ref.message.ToolParts) {
		return
	}
	tool := &ref.message.ToolParts[ref.index]
	outputValue := event.Result.Output
	if outputValue == nil {
		outputValue = event.Result.Content
	}
	if outputValue == nil {
		outputValue = event.Result.Error
	}
	output, truncation, redacted := redactToolText(valueToText(outputValue))
	tool.State.Output = output
	tool.State.Truncation = mergeTruncation(tool.State.Truncation, truncation)
	tool.State.Redacted = tool.State.Redacted || redacted
	if event.Result.IsError || event.Result.Error != nil {
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

func (n *agentNormalizer) flushPending() {
	if n.pending == nil {
		return
	}
	n.finishPending()
}

func (n *agentNormalizer) finishPending() {
	if n.pending == nil {
		return
	}
	if n.pending.Entry.ModelID == "" {
		n.pending.Entry.ModelID = n.lastModel
	}
	if n.pending.Entry.ProviderID == "" {
		n.pending.Entry.ProviderID = inferProvider(n.pending.Entry.ModelID, n.lastProvider)
	}
	n.pending.recomputeCost(n.pricing)
	n.register(n.pending)
	n.pending = nil
	n.pendingKey = ""
	n.requestSeq++
}

func (n *agentNormalizer) register(msg *messageRecord) {
	if msg == nil {
		return
	}
	n.snap.messageMap[msg.Entry.ID] = msg
	n.snap.ordered = append(n.snap.ordered, msg)
	n.session.Messages = append(n.session.Messages, msg)
	updateSessionTimes(n.session, msg.Entry.TimeCreated)
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

func inferProvider(model, fallback string) string {
	if fallback != "" {
		return fallback
	}
	if strings.HasPrefix(model, "kimi-code/") || strings.HasPrefix(model, "kimi-") || model == "k3" || strings.HasPrefix(model, "k2.") {
		return "kimi"
	}
	return ""
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

func parseStateTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func sanitizeTitle(title string) string {
	title, _ = redactText(strings.TrimSpace(title))
	if len(title) > 120 {
		title, _ = truncateText(title, 120)
	}
	return title
}

func isGenericSessionTitle(title string) bool {
	switch strings.ToLower(strings.TrimSpace(title)) {
	case "", "new session", "untitled":
		return true
	default:
		return false
	}
}

func derivedSessionTitle(session *sessionRecord, fallback string) string {
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
	if fallback, _ = redactText(strings.TrimSpace(fallback)); fallback != "" {
		return shortTitle(fallback)
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

func synthesizeUserID(sessionID, agentID string, timestamp time.Time, parts []string, seq int) string {
	hash := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return fmt.Sprintf("%s:%s:%s:u:%d:%x:%d", kimiSourceID, safeID(sessionID), safeID(agentID), timestamp.UnixMilli(), hash[:4], seq)
}

func synthesizeRequestID(sessionID, agentID, key string, seq int) string {
	return fmt.Sprintf("%s:%s:%s:r:%s:%d", kimiSourceID, safeID(sessionID), safeID(agentID), safeID(key), seq)
}

func sessionIDFromMessageID(id string) (string, bool) {
	parts := strings.Split(id, ":")
	if len(parts) < 5 || parts[0] != kimiSourceID || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}

func promptFingerprint(parts []string) string {
	return strings.Join(parts, "\x00")
}

func stableWireRecordKey(record wireRecord) string {
	encoded, err := json.Marshal(record)
	if err != nil {
		return fmt.Sprintf("%#v", record)
	}
	sum := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", sum[:])
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
