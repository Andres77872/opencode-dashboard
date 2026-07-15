package codex

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
	// seenUsage tracks every cumulative total_token_usage vector observed per
	// logical session. Forked/resumed rollout files replay the parent thread's
	// token_count ladder verbatim (with fresh timestamps); a vector that was
	// already observed is such a replay and must only seed the new thread's
	// baseline, never produce usage again. tokenSnapshot must stay comparable
	// (fixed-size value type) for this map key.
	seenUsage map[string]map[tokenSnapshot]struct{}
	// seenUserText tracks user prompt texts per logical session for the same
	// reason: some fork replays re-emit the parent's user_message events, and
	// only a prompt never seen before marks the derived thread's own activity.
	seenUserText map[string]map[string]struct{}
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

// threadState is the accounting state of one rollout file. A rollout file is a
// single thread's append-only event stream with its own session-cumulative
// token counter, so token deltas must be computed per file: forked threads run
// parallel divergent counters under one logical session id, and mixing them in
// one state would collapse their usage to the envelope maximum.
type threadState struct {
	threadID    string // the file's own thread id (first session_meta in file order)
	sessionID   string // logical session the thread belongs to (parent for forks)
	metaLine    int    // line of the thread's own session_meta
	provider    string
	model       string
	projectID   string
	projectName string
	directory   string
	turnID      string
	requestSeq  int
	userSeq     int
	// turnSeqs preserves each turn's row counters across revisits: transcripts
	// can return to an earlier turn id, and restarting its counters would
	// synthesize duplicate message ids (which the cache's primary key would
	// then silently collapse).
	turnSeqs map[string]turnSeq
	pending  *messageRecord
	tokenMax tokenSnapshot
	// replay marks a derived (forked/resumed) thread that is still inside the
	// replayed copy of its parent's history. Replayed content must not become
	// rows; replayed token_counts only advance tokenMax to the fork baseline.
	// Replay ends at the thread's first own signal: a user prompt or a
	// token_count whose cumulative vector was never seen in the session.
	replay     bool
	isSubagent bool
}

func normalizeRecords(home string, records []codexRecord, pricing pricingSnapshot, diag source.SourceDiagnostics) *snapshot {
	snap := &snapshot{
		home:         home,
		projectMap:   make(map[string]*projectRecord),
		sessionMap:   make(map[string]*sessionRecord),
		messageMap:   make(map[string]*messageRecord),
		ordered:      make([]*messageRecord, 0),
		seenUsage:    make(map[string]map[tokenSnapshot]struct{}),
		seenUserText: make(map[string]map[string]struct{}),
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

	threads := fileThreadsByPath(records)
	states := make(map[string]*threadState)
	seenRecords := make(map[string]struct{}, len(records))
	for _, record := range records {
		info := threadForFile(threads, record.File)
		key := stableRecordDedupeKey(record, info.sessionID)
		if _, exists := seenRecords[key]; exists {
			continue
		}
		seenRecords[key] = struct{}{}

		state := states[record.File.Path]
		if state == nil {
			state = &threadState{
				threadID:   info.threadID,
				sessionID:  info.sessionID,
				metaLine:   info.metaLine,
				replay:     info.derived,
				isSubagent: info.isSubagent,
			}
			states[record.File.Path] = state
		}
		timestamp := recordTimestamp(record)

		switch {
		case record.SessionMeta != nil:
			// The thread's own meta binds provider/cwd. Replayed parent metas
			// (forks) and repeated resume metas only fill gaps and never rebind
			// the thread's identity.
			own := record.Line == state.metaLine
			if record.SessionMeta.ModelProvider != "" && (own || state.provider == "") {
				state.provider = record.SessionMeta.ModelProvider
			}
			if record.SessionMeta.CWD != "" && (own || state.directory == "") {
				state.directory = redactDisplayPath(record.SessionMeta.CWD)
				state.projectID, state.projectName = projectFromPath(record.SessionMeta.CWD)
			} else if state.projectID == "" {
				state.projectID, state.projectName = projectFromPath(state.directory)
			}
			snap.ensureSession(state.sessionID, snap.ensureProject(state.projectID, state.projectName, state.directory), state.directory)
			updateSessionTimes(snap.sessionMap[state.sessionID], timestamp)

		case record.TurnContext != nil:
			snap.syncTurn(state, record.TurnContext.TurnID)
			if record.TurnContext.Model != "" {
				state.model = record.TurnContext.Model
			}
			if record.TurnContext.Provider != "" {
				state.provider = record.TurnContext.Provider
			}
			if state.directory == "" && record.TurnContext.CWD != "" {
				state.directory = redactDisplayPath(record.TurnContext.CWD)
				state.projectID, state.projectName = projectFromPath(record.TurnContext.CWD)
			}
			snap.ensureSession(state.sessionID, snap.ensureProject(state.projectID, state.projectName, state.directory), state.directory)
			updateSessionTimes(snap.sessionMap[state.sessionID], timestamp)

		case record.Event != nil:
			snap.applyEvent(state, record.Event, timestamp, pricing)

		case record.Response != nil:
			snap.applyResponse(state, record.Response, timestamp, pricing)

		case record.Compacted:
			// Compaction records are metadata only. They must not create rows or
			// replay developer/user/assistant content into details.
			if session := snap.sessionMap[state.sessionID]; session != nil {
				updateSessionTimes(session, timestamp)
			}
		}
	}

	// Trailing assistant content that never received a token_count still
	// surfaces as its own row (unless the thread was still replaying parent
	// history). Deterministic order: iterate states by file path.
	for _, path := range sortedKeys(states) {
		snap.flushPending(states[path])
	}
	sort.SliceStable(snap.ordered, func(i, j int) bool {
		if snap.ordered[i].Entry.TimeCreated.Equal(snap.ordered[j].Entry.TimeCreated) {
			return snap.ordered[i].Entry.ID < snap.ordered[j].Entry.ID
		}
		return snap.ordered[i].Entry.TimeCreated.Before(snap.ordered[j].Entry.TimeCreated)
	})

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

// fileThread is the identity of one rollout file resolved from its first
// session_meta in file order: that record is always the thread's own meta,
// while later metas are fork replays or resume repetitions.
type fileThread struct {
	threadID   string
	sessionID  string
	metaLine   int
	derived    bool
	isSubagent bool
}

func fileThreadsByPath(records []codexRecord) map[string]fileThread {
	threads := make(map[string]fileThread)
	for _, record := range records {
		meta := record.SessionMeta
		if meta == nil || meta.ID == "" {
			continue
		}
		if existing, ok := threads[record.File.Path]; ok && existing.metaLine <= record.Line {
			continue
		}
		sessionID := meta.SessionID
		if sessionID == "" {
			sessionID = meta.ID
		}
		threads[record.File.Path] = fileThread{
			threadID:  meta.ID,
			sessionID: sessionID,
			metaLine:  record.Line,
			// A derived thread starts with a replayed copy of its parent's
			// history: forks say so explicitly; resumes carry the original
			// session id under a fresh thread id. Sub-agent threads have their
			// own session (session_id empty) and start fresh.
			derived:    meta.ForkedFromID != "" || (meta.SessionID != "" && meta.SessionID != meta.ID),
			isSubagent: meta.AgentRole != "",
		}
	}
	return threads
}

func threadForFile(threads map[string]fileThread, file transcriptFile) fileThread {
	if info, ok := threads[file.Path]; ok {
		return info
	}
	id := file.SessionID
	if id == "" {
		id = "unknown-session"
	}
	return fileThread{threadID: id, sessionID: id}
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

func (s *snapshot) applyEvent(state *threadState, event *eventMsgRecord, timestamp time.Time, pricing pricingSnapshot) {
	s.syncTurn(state, event.TurnID)
	switch event.PayloadType {
	case "task_started":
		// Marks the turn; the user prompt (user_message) and assistant API requests
		// (token_count) create the rows. Fork replays also re-emit task_started, so
		// it must not end replay mode.
		s.ensureSession(state.sessionID, s.ensureProject(state.projectID, state.projectName, state.directory), state.directory)
	case "user_message":
		// A new user prompt closes any open assistant request and starts its own
		// row. Fork replays can re-emit the parent's user_message events: while
		// replaying, a prompt text already seen in the session is such a copy
		// and stays suppressed; an unseen prompt is the thread's own activity
		// and ends replay mode.
		seenText := s.seenUserTextFor(state.sessionID)
		if state.replay {
			if _, replayed := seenText[event.Text]; replayed {
				s.discardPending(state)
				break
			}
			state.replay = false
		}
		s.flushPending(state)
		msg := s.buildMessage(state, "user", timestamp)
		if event.Text != "" {
			msg.TextParts = append(msg.TextParts, redactAndTruncateMessagePart("text", event.Text))
		}
		s.registerMessage(state, msg)
		seenText[event.Text] = struct{}{}
		state.userSeq++
	case "agent_message":
		// Assistant mirrors are intentionally ignored when response_item.message is
		// available, preventing duplicate assistant text and row counts.
	case "token_count":
		s.applyTokenCount(state, event, timestamp)
	case "patch_apply_end", "web_search_end", "mcp_tool_call_end":
		if event.CallID != "" && state.pending != nil {
			state.pending.applyToolStatus(event.CallID, event.Status, timestamp)
		}
	case "task_complete":
		// Trailing assistant content with no token_count stays as its own row.
		s.flushPending(state)
	case "turn_aborted":
		// An aborted turn never gets a token_count; surface whatever content
		// arrived (unless it was replayed history) and keep ids advancing.
		s.flushPending(state)
	case "context_compacted":
		// Metadata only.
	}
	if session := s.sessionMap[state.sessionID]; session != nil {
		updateSessionTimes(session, timestamp)
	}
}

// applyTokenCount closes one model API request. The per-request usage is the
// delta of the thread-cumulative total_token_usage (which clamps spikes and
// regressions and, for well-formed transcripts, equals last_token_usage). Fall
// back to last_token_usage only when no running total is present.
//
// Derived (forked/resumed) threads replay their parent's token_count ladder
// first: while replaying, a cumulative vector already seen in the logical
// session only advances this thread's baseline, and the first unseen vector is
// the thread's own first request, ending replay mode.
func (s *snapshot) applyTokenCount(state *threadState, event *eventMsgRecord, timestamp time.Time) {
	switch {
	case event.HasTotalUsage:
		seen := s.seenUsageFor(state.sessionID)
		if state.replay {
			if _, replayed := seen[event.TotalUsage]; replayed {
				state.tokenMax = maxSnapshot(state.tokenMax, event.TotalUsage)
				s.discardPending(state)
				return
			}
			state.replay = false
		}
		usage := positiveDelta(state.tokenMax, event.TotalUsage)
		state.tokenMax = maxSnapshot(state.tokenMax, event.TotalUsage)
		seen[event.TotalUsage] = struct{}{}
		s.closeRequest(state, timestamp, usage, true, requestRawInput(event, usage))
	case event.HasLastUsage:
		// Advance the running total too, so a later cumulative vector that
		// already includes this request does not count it twice.
		state.tokenMax = addSnapshot(state.tokenMax, event.LastUsage)
		if state.replay {
			s.discardPending(state)
			return
		}
		s.closeRequest(state, timestamp, event.LastUsage, true, event.LastUsage.Input)
	default:
		s.closeRequest(state, timestamp, tokenSnapshot{}, false, 0)
	}
}

// requestRawInput is the raw (cached-inclusive) prompt size of one API request,
// the signal long-context pricing thresholds compare against. last_token_usage
// reports it directly; the cumulative delta only approximates it when events
// were merged by clamping.
func requestRawInput(event *eventMsgRecord, usage tokenSnapshot) int64 {
	if event.HasLastUsage && event.LastUsage.Input > 0 {
		return event.LastUsage.Input
	}
	return usage.Input
}

func (s *snapshot) seenUsageFor(sessionID string) map[tokenSnapshot]struct{} {
	seen := s.seenUsage[sessionID]
	if seen == nil {
		seen = make(map[tokenSnapshot]struct{})
		s.seenUsage[sessionID] = seen
	}
	return seen
}

func (s *snapshot) seenUserTextFor(sessionID string) map[string]struct{} {
	seen := s.seenUserText[sessionID]
	if seen == nil {
		seen = make(map[string]struct{})
		s.seenUserText[sessionID] = seen
	}
	return seen
}

func (s *snapshot) applyResponse(state *threadState, response *responseItemRecord, timestamp time.Time, pricing pricingSnapshot) {
	s.syncTurn(state, response.TurnID)
	switch response.ItemType {
	case "message":
		if response.Role != "assistant" {
			return
		}
		req := s.ensurePending(state, timestamp)
		if response.Text != "" && !req.seenAssistant[response.Text] {
			req.seenAssistant[response.Text] = true
			req.TextParts = append(req.TextParts, redactAndTruncateMessagePart("text", response.Text))
		}
	case "reasoning":
		text := response.Text
		if strings.TrimSpace(text) == "" {
			text = "[Codex reasoning event redacted or encrypted]"
		}
		req := s.ensurePending(state, timestamp)
		if len(req.ReasoningParts) == 0 {
			part := redactAndTruncateMessagePart("reasoning", text)
			part.Redacted = true
			req.ReasoningParts = append(req.ReasoningParts, part)
		}
	case "function_call", "custom_tool_call", "web_search_call", "tool_search_call":
		req := s.ensurePending(state, timestamp)
		req.addToolCall(response, timestamp)
	case "function_call_output", "custom_tool_call_output", "tool_search_output":
		if state.pending != nil {
			state.pending.applyToolOutput(response, timestamp)
		}
	}
	if session := s.sessionMap[state.sessionID]; session != nil {
		updateSessionTimes(session, timestamp)
	}
}

type turnSeq struct {
	request int
	user    int
}

// syncTurn advances the thread to a new turn, flushing any open assistant
// request and swapping in that turn's row counters. A freshly seen turn starts
// at zero; a revisited turn resumes where it left off so message ids stay
// unique as well as stable.
func (s *snapshot) syncTurn(state *threadState, turnID string) {
	if turnID == "" || turnID == state.turnID {
		return
	}
	s.flushPending(state)
	if state.turnSeqs == nil {
		state.turnSeqs = make(map[string]turnSeq)
	}
	state.turnSeqs[state.turnID] = turnSeq{request: state.requestSeq, user: state.userSeq}
	seq := state.turnSeqs[turnID]
	state.turnID = turnID
	state.requestSeq = seq.request
	state.userSeq = seq.user
}

// buildMessage creates one dashboard row (a user prompt or an assistant API
// request) without registering it; registration happens on emit so replayed
// content in derived threads can be discarded. Cost is left missing here and
// computed once at the end.
func (s *snapshot) buildMessage(state *threadState, role string, timestamp time.Time) *messageRecord {
	if state.projectID == "" {
		state.projectID, state.projectName = projectFromPath(state.directory)
	}
	project := s.ensureProject(state.projectID, state.projectName, state.directory)
	s.ensureSession(state.sessionID, project, state.directory)
	turnID := state.turnID
	if turnID == "" {
		turnID = "turn"
	}
	var id string
	if role == "user" {
		id = synthesizeRequestID(state.threadID, turnID, "u", state.userSeq)
	} else {
		id = synthesizeRequestID(state.threadID, turnID, "r", state.requestSeq)
	}
	msg := &messageRecord{
		Entry: stats.MessageEntry{
			SourceID:    codexSourceID,
			ID:          id,
			SessionID:   state.sessionID,
			Role:        role,
			TimeCreated: timestamp.UTC(),
			IsSubagent:  state.isSubagent,
		},
		projectID:     project.ID,
		seenAssistant: map[string]bool{},
		seenTools:     map[string]bool{},
	}
	if role == "assistant" {
		msg.Entry.ModelID = state.model
		msg.Entry.ProviderID = state.provider
	}
	return msg
}

func (s *snapshot) registerMessage(state *threadState, msg *messageRecord) {
	session := s.ensureSession(state.sessionID, s.ensureProject(state.projectID, state.projectName, state.directory), state.directory)
	s.messageMap[msg.Entry.ID] = msg
	s.ordered = append(s.ordered, msg)
	session.Messages = append(session.Messages, msg)
	updateSessionTimes(session, msg.Entry.TimeCreated)
}

// ensurePending returns the in-progress assistant API-request row, creating it on
// first content. The row is registered when it closes or flushes, so trailing
// content without a token_count still surfaces (with missing cost).
func (s *snapshot) ensurePending(state *threadState, timestamp time.Time) *messageRecord {
	if state.pending == nil {
		state.pending = s.buildMessage(state, "assistant", timestamp)
	}
	return state.pending
}

// closeRequest finalizes one API request when its token_count arrives, attaching the
// per-request usage. A token_count with usage but no buffered content still yields a
// usage-only assistant row.
func (s *snapshot) closeRequest(state *threadState, timestamp time.Time, usage tokenSnapshot, hasUsage bool, rawInput int64) {
	req := state.pending
	if req == nil {
		if !hasUsage || usageEmpty(usage) {
			return
		}
		req = s.buildMessage(state, "assistant", timestamp)
	}
	if hasUsage {
		req.setTokens(usage)
		if rawInput > 0 {
			req.maxInputSnapshot = rawInput
		}
	}
	if req.Entry.ModelID == "" {
		req.Entry.ModelID = state.model
	}
	if req.Entry.ProviderID == "" {
		req.Entry.ProviderID = state.provider
	}
	req.Entry.Role = "assistant"
	s.registerMessage(state, req)
	state.pending = nil
	state.requestSeq++
}

// flushPending releases an assistant request that never received a token_count
// (e.g. trailing content at task_complete), registering it as a content-only row.
// While a derived thread is replaying parent history the buffered content is a
// replayed copy and is discarded instead. Either way the counter advances so the
// next request id stays unique.
func (s *snapshot) flushPending(state *threadState) {
	if state.pending == nil {
		return
	}
	if !state.replay {
		s.registerMessage(state, state.pending)
	}
	state.pending = nil
	state.requestSeq++
}

// discardPending drops a replayed pending row without registering it.
func (s *snapshot) discardPending(state *threadState) {
	if state.pending == nil {
		return
	}
	state.pending = nil
	state.requestSeq++
}

func usageEmpty(u tokenSnapshot) bool {
	return u.Input == 0 && u.Cached == 0 && u.Output == 0 && u.Reasoning == 0
}

func (m *messageRecord) setTokens(u tokenSnapshot) {
	if usageEmpty(u) {
		return
	}
	// Rollout usage buckets overlap (cached_input ⊆ input, reasoning ⊆ output);
	// stats.TokenStats buckets are disjoint, so subtract the subsets out.
	tokens := &stats.TokenStats{
		Input:     positive(u.Input - u.Cached),
		Output:    positive(u.Output - u.Reasoning),
		Reasoning: u.Reasoning,
	}
	tokens.Cache.Read = u.Cached
	tokens.Cache.Write = 0
	m.Entry.Tokens = tokens
	// Long-context detection needs the raw (cached-inclusive) request input.
	m.maxInputSnapshot = u.Input
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

func addSnapshot(a, b tokenSnapshot) tokenSnapshot {
	return tokenSnapshot{
		Input:     a.Input + b.Input,
		Cached:    a.Cached + b.Cached,
		Output:    a.Output + b.Output,
		Reasoning: a.Reasoning + b.Reasoning,
		Total:     a.Total + b.Total,
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
	// Prefer the first user prompt's text, then any text-bearing row.
	if text := firstSessionText(session, func(m *messageRecord) bool { return m.Entry.Role == "user" }); text != "" {
		return text
	}
	if text := firstSessionText(session, func(m *messageRecord) bool { return true }); text != "" {
		return text
	}
	return session.ID
}

func firstSessionText(session *sessionRecord, accept func(*messageRecord) bool) string {
	for _, msg := range session.Messages {
		if !accept(msg) {
			continue
		}
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
	return ""
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

func synthesizeRequestID(sessionID, turnID, kind string, seq int) string {
	return codexSourceID + ":" + safeID(sessionID) + ":" + safeID(turnID) + ":" + kind + strconv.Itoa(seq)
}

// threadIDFromMessageID reverses synthesizeRequestID enough to recover the
// owning thread id, so a single message can be resolved by parsing only its
// thread's transcript instead of the whole corpus. Ids are
// codex:<threadID>:<turnID>:<kind><seq>; threadID/turnID are UUIDs (no colons),
// so parts[1] is the thread id even if a later segment carried a colon.
func threadIDFromMessageID(id string) (string, bool) {
	parts := strings.Split(id, ":")
	if len(parts) < 4 || parts[0] != codexSourceID || parts[1] == "" {
		return "", false
	}
	return parts[1], true
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
