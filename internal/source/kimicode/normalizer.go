package kimicode

import (
	"crypto/sha256"
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
	mainAgent      bool
}

type stepState struct {
	TurnID   string
	Step     int64
	StepUUID string
}

type agentNormalizer struct {
	snap            *snapshot
	pricing         pricingSnapshot
	session         *sessionRecord
	sessionID       string
	project         *projectRecord
	directory       string
	agentID         string
	agentLabel      string
	isMainAgent     bool
	isSubagent      bool
	file            agentFile
	currentStep     stepState
	lastModel       string
	lastProvider    string
	requestSeq      int
	userSeq         int
	pending         *messageRecord
	pendingKey      string
	pendingTurnStep string
	pendingTurnID   string
	pendingTerminal bool
	promptEcho      string
	seenTools       map[string]bool
	toolRefs        map[string]toolRef
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
		workDir := resolveSessionWorkDir(item.State, item.Files.IndexWorkDir)
		directory := redactDisplayPath(workDir)
		projectID, projectName := projectFromPath(workDir, item.Files.WorkspaceKey)
		project := snap.ensureProject(projectID, projectName, directory)
		session := snap.ensureSession(item.Files.ID, project, directory)
		session.Title = sanitizeTitle(item.State.Title)
		if session.Title == "" {
			session.Title = sanitizeTitle(item.State.Label)
		}
		session.Created = parseStateTime(item.State.CreatedAt)
		session.Updated = parseStateTime(item.State.UpdatedAt)
		if session.Created.IsZero() {
			session.Created = item.Files.ModTime
		}
		if session.Updated.IsZero() {
			session.Updated = item.Files.ModTime
		}

		sort.Slice(item.Agents, func(i, j int) bool {
			leftMain := item.Agents[i].File.AgentID == "main" || strings.EqualFold(item.Agents[i].Meta.Type, "main")
			rightMain := item.Agents[j].File.AgentID == "main" || strings.EqualFold(item.Agents[j].Meta.Type, "main")
			if leftMain != rightMain {
				return leftMain
			}
			return item.Agents[i].File.AgentID < item.Agents[j].File.AgentID
		})
		for _, agent := range item.Agents {
			normalizer := newAgentNormalizer(snap, pricing, session, project, directory, agent)
			seenRecords := make(map[string]bool)
			times := effectiveRecordTimes(agent.Records, session.Created, session.Updated, agent.File.ModTime)
			for index, record := range agent.Records {
				if key := durableWireRecordKey(record); key != "" {
					if seenRecords[key] {
						continue
					}
					seenRecords[key] = true
				}
				normalizer.apply(record, times[index])
			}
			normalizer.finishAtEOF()
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
	agentType := strings.ToLower(strings.TrimSpace(agent.Meta.Type))
	isMain := agent.File.AgentID == "main" || agentType == "main"
	isSubagent := agentType == "sub" || agent.Meta.ParentAgentID != nil
	if !isMain && agentType != "independent" && agentType != "sub" && agent.Meta.ParentAgentID == nil {
		// Metadata-free non-main wires in older releases are subagents.
		isSubagent = true
	}
	label := agentLabelFromMeta(agent.Meta, agent.File.AgentID, isMain)
	return &agentNormalizer{
		snap: snap, pricing: pricing, session: session, sessionID: session.ID,
		project: project, directory: directory, agentID: agent.File.AgentID,
		agentLabel: label, isMainAgent: isMain, isSubagent: isSubagent, file: agent.File,
		seenTools: make(map[string]bool),
		toolRefs:  make(map[string]toolRef),
	}
}

func (n *agentNormalizer) apply(record wireRecord, timestamp time.Time) {
	isPromptRecord := record.Prompt != nil || (record.Message != nil && record.Message.Role == "user")
	if !isPromptRecord && !transparentPromptEchoControl(record) {
		// A request, loop step, usage event, cancellation, compaction, or
		// assistant message is a semantic boundary. Do not retain a content
		// fingerprint beyond it: a later identical prompt is genuine input.
		n.promptEcho = ""
	}
	switch {
	case record.Config != nil:
		n.applyConfig(record.Config)
	case record.Prompt != nil:
		n.addPrompt(record.Prompt.Input, record.Prompt.Origin, timestamp, true)
	case record.Message != nil:
		if record.Message.Role == "user" {
			n.addPrompt(record.Message.Content, record.Message.Origin, timestamp, false)
		}
	case record.Request != nil:
		n.startRequest(record.Request, timestamp)
	case record.Usage != nil:
		n.closeUsage(record.Usage, timestamp)
	case record.Cancel != nil:
		n.applyTurnCancel(record.Cancel)
	case record.Event != nil:
		n.applyLoopEvent(record.Event, timestamp)
	}
	updateSessionTimes(n.session, timestamp)
}

func transparentPromptEchoControl(record wireRecord) bool {
	// Kimi can persist bookkeeping between turn.prompt and the matching
	// context.append_message user row. These records do not represent a second
	// prompt or an outbound attempt, so they must not break the schema echo pair.
	switch record.Type {
	case "metadata", "config.update",
		"permission.set_mode", "permission.record_approval_result",
		"plan_mode.enter", "plan_mode.cancel", "plan_mode.exit",
		"swarm_mode.enter", "swarm_mode.exit",
		"tools.register_user_tool", "tools.unregister_user_tool",
		"tools.set_active_tools", "tools.update_store",
		"context.update_token_count",
		"goal.create", "goal.update", "goal.clear",
		"llm.tools_snapshot", "mcp.tools_discovered":
		return true
	default:
		return false
	}
}

func (n *agentNormalizer) applyConfig(config *configUpdateRecord) {
	if config == nil {
		return
	}
	if label := sanitizeAgentLabel(config.ProfileName); label != "" {
		n.agentLabel = label
	}
	if model := strings.TrimSpace(config.ModelAlias); model != "" {
		n.lastModel = model
	}
}

func (n *agentNormalizer) addPrompt(parts []contentPartRecord, origin originRecord, timestamp time.Time, primary bool) {
	if !visiblePromptOrigin(origin) {
		n.promptEcho = ""
		return
	}
	normalized := promptParts(parts)
	if len(normalized) == 0 {
		n.promptEcho = ""
		return
	}
	fingerprint := promptFingerprint(normalized)
	if !primary && n.promptEcho == fingerprint {
		// Kimi writes the same logical user input first as turn.prompt (or
		// turn.steer) and then immediately as context.append_message. Normalize
		// that documented two-record representation into one transcript row;
		// this is deliberately adjacency-based, not identity-less deduplication.
		n.promptEcho = ""
		return
	}
	n.promptEcho = ""
	n.flushPending()

	id := synthesizeUserID(n.sessionID, n.agentID, timestamp, normalized, n.userSeq)
	n.userSeq++
	msg := n.newMessage(id, "user", timestamp)
	for _, part := range normalized {
		msg.TextParts = append(msg.TextParts, redactAndTruncateMessagePart("text", part))
	}
	n.register(msg)
	if primary {
		n.promptEcho = fingerprint
	}
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
	if n.pending != nil &&
		n.pending.Entry.UsageStatus == stats.UsageStatusUnavailable &&
		request.TurnStep != "" &&
		request.TurnStep == n.pendingTurnStep {
		n.pending.Entry.UsageUnavailableReason = stats.UsageUnavailableFailed
	}
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
	n.pendingTurnStep = strings.TrimSpace(request.TurnStep)
	n.pendingTurnID = n.currentStep.TurnID
	n.pendingTerminal = false
	n.pending = n.newAssistantMessage(timestamp, key)
	n.pending.Entry.RequestTrace = stats.RequestTraceObserved
	n.pending.Entry.UsageStatus = stats.UsageStatusUnavailable
}

func (n *agentNormalizer) applyTurnCancel(cancel *turnCancelRecord) {
	if cancel == nil || n.pending == nil || n.pending.Entry.UsageStatus != stats.UsageStatusUnavailable {
		return
	}
	if cancel.TurnID != "" && cancel.TurnID != n.pendingTurnID {
		return
	}
	n.pending.Entry.UsageUnavailableReason = stats.UsageUnavailableCancelled
}

func (n *agentNormalizer) closeUsage(usage *usageRecord, timestamp time.Time) {
	if n.pending != nil && n.pending.Entry.UsageStatus == stats.UsageStatusRecorded {
		// Identity-less usage records are independently persisted evidence.
		// Once one request already has canonical usage, the next record must
		// become its own inferred successful request rather than overwrite it.
		n.flushPending()
	}
	if n.pending == nil {
		key := n.currentStep.StepUUID
		if key == "" {
			key = strconv.FormatInt(timestamp.UnixMilli(), 10)
		}
		n.pendingKey = key
		n.pending = n.newAssistantMessage(timestamp, key)
	}
	if n.pending.Entry.RequestTrace == "" {
		n.pending.Entry.RequestTrace = stats.RequestTraceInferred
	}
	if usage.Model != "" {
		n.pending.Entry.ModelID = usage.Model
		n.lastModel = usage.Model
	}
	if n.pending.Entry.ProviderID == "" {
		n.pending.Entry.ProviderID = inferProvider(n.pending.Entry.ModelID, n.lastProvider)
	}
	n.setPendingUsage(usage.Usage, stats.UsageStatusRecorded)
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
		if event.Usage != nil {
			n.recoverStepUsage(*event.Usage, timestamp, event.UUID)
		} else if n.pending != nil {
			n.pendingTerminal = true
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

func (n *agentNormalizer) recoverStepUsage(usage tokenUsage, timestamp time.Time, stepUUID string) {
	if n.pending == nil {
		key := strings.TrimSpace(stepUUID)
		if key == "" {
			key = n.currentStep.StepUUID
		}
		if key == "" {
			key = strconv.FormatInt(timestamp.UnixMilli(), 10)
		}
		n.pendingKey = key
		n.pending = n.newAssistantMessage(timestamp, key)
	}
	// Canonical usage.record is stronger evidence and must never be replaced
	// by a duplicate/later step-end fallback.
	if n.pending.Entry.UsageStatus == stats.UsageStatusRecorded {
		return
	}
	if n.pending.Entry.RequestTrace == "" {
		n.pending.Entry.RequestTrace = stats.RequestTraceInferred
	}
	n.setPendingUsage(usage, stats.UsageStatusRecovered)
}

func (n *agentNormalizer) setPendingUsage(usage tokenUsage, status stats.UsageStatus) {
	if n.pending == nil {
		return
	}
	tokens := stats.TokenStats{
		Input: positive(usage.InputOther), Output: positive(usage.Output),
		Cache: stats.CacheStats{
			Read: positive(usage.InputCacheRead), Write: positive(usage.InputCacheCreation),
		},
	}
	n.pending.Entry.Tokens = &tokens
	n.pending.Entry.UsageStatus = status
	n.pending.Entry.UsageUnavailableReason = ""
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
		mainAgent: n.isMainAgent,
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

func (n *agentNormalizer) finishAtEOF() {
	if n.pending != nil &&
		n.pending.Entry.UsageStatus == stats.UsageStatusUnavailable &&
		!n.pendingTerminal &&
		n.pending.Entry.UsageUnavailableReason == "" {
		n.pending.Entry.UsageUnavailableReason = stats.UsageUnavailableInterrupted
	}
	n.finishPending()
}

func (n *agentNormalizer) finishPending() {
	if n.pending == nil {
		return
	}
	if n.pending.Entry.RequestTrace == "" && n.pending.Entry.UsageStatus == "" {
		// Pre-trace content/tool events without persisted usage do not prove
		// that a separately countable outbound attempt existed. Keep request
		// inference limited to durable usage evidence.
		n.pending = nil
		n.pendingKey = ""
		return
	}
	if n.pending.Entry.ModelID == "" {
		n.pending.Entry.ModelID = n.lastModel
	}
	if n.pending.Entry.ProviderID == "" {
		n.pending.Entry.ProviderID = inferProvider(n.pending.Entry.ModelID, n.lastProvider)
	}
	if n.pending.Entry.UsageStatus == stats.UsageStatusUnavailable &&
		n.pending.Entry.UsageUnavailableReason == "" {
		n.pending.Entry.UsageUnavailableReason = stats.UsageUnavailableUnknown
	}
	n.pending.recomputeCost(n.pricing)
	n.register(n.pending)
	n.pending = nil
	n.pendingKey = ""
	n.pendingTurnStep = ""
	n.pendingTurnID = ""
	n.pendingTerminal = false
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
	result := computeCost(m.Entry.ModelID, m.Entry.ProviderID, *m.Entry.Tokens, pricing)
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

func parseStateTime(value any) time.Time {
	switch typed := value.(type) {
	case string:
		text := strings.TrimSpace(typed)
		if parsed, err := time.Parse(time.RFC3339Nano, text); err == nil {
			return parsed.UTC()
		}
		if numeric, err := strconv.ParseFloat(text, 64); err == nil {
			return numericTimeFloat(numeric)
		}
	case float64:
		return numericTimeFloat(typed)
	case float32:
		return numericTimeFloat(float64(typed))
	case int:
		return numericTime(int64(typed))
	case int64:
		return numericTime(typed)
	case int32:
		return numericTime(int64(typed))
	case uint64:
		if typed <= uint64(^uint64(0)>>1) {
			return numericTime(int64(typed))
		}
	case uint:
		return numericTime(int64(typed))
	}
	return time.Time{}
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
		if msg.Entry.Role != "user" || !msg.mainAgent {
			continue
		}
		for _, part := range msg.TextParts {
			if title := shortTitle(part.Text); title != "" {
				return title
			}
		}
	}
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

func projectFromPath(path, workspaceKey string) (string, string) {
	base := filepath.Base(strings.TrimSpace(path))
	if base == "." || base == string(filepath.Separator) || base == "" {
		id := safeID(workspaceKey)
		if id == "unknown" {
			return "unknown", "unknown"
		}
		return id, "unknown"
	}
	cleaned := filepath.Clean(strings.TrimSpace(path))
	hash := sha256.Sum256([]byte(cleaned))
	prefix := safeID(workspaceKey)
	if prefix == "unknown" {
		prefix = safeID(base)
	}
	return fmt.Sprintf("%s-%x", prefix, hash[:6]), base
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

func durableWireRecordKey(record wireRecord) string {
	if strings.TrimSpace(record.DurableID) == "" {
		return ""
	}
	key := record.Type + "\x00" + record.DurableID
	if record.Request != nil {
		// A provider request id can identify the logical request while Kimi's
		// attempt field identifies distinct outbound retries. Preserve those
		// attempts even when they share the same durable request id; only an
		// identical id/attempt tuple proves that the wire event was duplicated.
		key += "\x00" + record.Request.Kind + "\x00" + record.Request.TurnStep + "\x00" + record.Request.Attempt
	}
	return key
}

func effectiveRecordTimes(records []wireRecord, created, updated, fileMod time.Time) []time.Time {
	times := make([]time.Time, len(records))
	var previous time.Time
	for index, record := range records {
		if !record.Time.IsZero() {
			previous = record.Time.UTC()
			times[index] = previous
		} else if !previous.IsZero() {
			times[index] = previous
		}
	}
	var next time.Time
	for index := len(records) - 1; index >= 0; index-- {
		if !records[index].Time.IsZero() {
			next = records[index].Time.UTC()
		} else if times[index].IsZero() && !next.IsZero() {
			times[index] = next
		}
	}
	fallback := created
	if fallback.IsZero() {
		fallback = updated
	}
	if fallback.IsZero() {
		fallback = fileMod.UTC()
	}
	for index := range times {
		if times[index].IsZero() {
			times[index] = fallback
		}
	}
	return times
}

func resolveSessionWorkDir(state sessionState, indexed string) string {
	for _, candidate := range []string{state.Cwd, state.WorkDir, customCwd(state.Custom), indexed} {
		if value := strings.TrimSpace(candidate); value != "" {
			return value
		}
	}
	return ""
}

func customCwd(custom map[string]any) string {
	if custom == nil {
		return ""
	}
	value, _ := custom["cwd"].(string)
	return value
}

func visiblePromptOrigin(origin originRecord) bool {
	kind := strings.ToLower(strings.TrimSpace(origin.Kind))
	switch kind {
	case "", "user":
		return true
	case "skill_activation", "plugin_activation":
		return strings.EqualFold(strings.TrimSpace(origin.Trigger), "user-slash")
	default:
		return false
	}
}

func agentLabelFromMeta(meta agentMeta, agentID string, isMain bool) string {
	for _, key := range []string{"profileName", "profile_name", "name", "label"} {
		if label := sanitizeAgentLabel(meta.Labels[key]); label != "" {
			return label
		}
	}
	agentType := strings.ToLower(strings.TrimSpace(meta.Type))
	if isMain {
		return ""
	}
	if agentType != "" && agentType != "sub" {
		return sanitizeAgentLabel(agentType)
	}
	return sanitizeAgentLabel(agentID)
}

func sanitizeAgentLabel(label string) string {
	label, _ = redactText(strings.TrimSpace(label))
	if len(label) > 80 {
		label, _ = truncateText(label, 80)
	}
	return label
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
