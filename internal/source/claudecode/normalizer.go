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

type interactionState struct {
	msg                        *messageRecord
	session                    *sessionRecord
	startedByUser              bool
	rawAssistantRecords        int64
	assistantCalls             []assistantContribution
	billingContributionIndexes map[string]int
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
	activeInteractions := make(map[string]*interactionState)
	seenRecords := make(map[string]struct{}, len(records))
	for _, record := range records {
		sessionID := recordSessionID(record)
		timestamp := recordTimestamp(record)

		// Deduplicate before aggregation so copied transcript files or repeated JSONL
		// records do not double-count cost/tokens/interactions. UUID-bearing records
		// are keyed by session/message identity plus equivalent semantic content; this
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
			interaction := snap.startInteraction(sessionID, record, timestamp, true, pricing)
			activeInteractions[sessionID] = interaction
			interaction.msg.appendTextParts(record.TextParts)
			interaction.msg.appendReasoningParts(record.ReasoningParts)
			updateSessionTimes(interaction.session, timestamp)

		case "assistant":
			interaction := activeInteractions[sessionID]
			if interaction == nil || !interaction.startedByUser {
				interaction = snap.startInteraction(sessionID, record, timestamp, false, pricing)
				activeInteractions[sessionID] = interaction
			}
			interaction.addAssistantRecord(sessionID, record, timestamp, pricing, pendingTools)
			snap.applyToolResults(sessionID, record.ToolResults, pendingTools, timestamp)
			updateSessionTimes(interaction.session, timestamp)
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

func (s *snapshot) startInteraction(sessionID string, record parsedRecord, timestamp time.Time, startedByUser bool, pricing pricingSnapshot) *interactionState {
	projectID := record.File.ProjectID
	projectPath := record.File.ProjectPath
	if record.CWD != "" {
		projectPath = record.CWD
	}
	project := s.ensureProject(projectID, projectPath)
	session := s.ensureSession(sessionID, project, projectPath)
	role := "assistant"
	if startedByUser {
		role = "user"
	}
	missing := missingCost(defaultCurrency(pricing))
	msg := &messageRecord{
		Entry: stats.MessageEntry{
			SourceID:       claudeSourceID,
			ID:             synthesizeMessageID(sessionID, record.UUID, record.Line),
			SessionID:      sessionID,
			Role:           role,
			TimeCreated:    timestamp.UTC(),
			CostStatus:     missing.Status,
			CostProvenance: missing.Provenance,
		},
		projectID: project.ID,
		line:      record.Line,
	}
	interaction := &interactionState{msg: msg, session: session, startedByUser: startedByUser}
	s.messageMap[msg.Entry.ID] = msg
	s.ordered = append(s.ordered, msg)
	session.Messages = append(session.Messages, msg)
	updateSessionTimes(session, timestamp)
	return interaction
}

func defaultCurrency(pricing pricingSnapshot) string {
	if pricing.Currency != "" {
		return pricing.Currency
	}
	return "USD"
}

func (i *interactionState) addAssistantRecord(sessionID string, record parsedRecord, timestamp time.Time, pricing pricingSnapshot, pendingTools map[string]pendingToolRef) {
	if len(i.assistantCalls) == 0 {
		i.msg.Entry.Role = "assistant"
		// Interaction rows carry the time of the first assistant/API response when
		// one exists, which keeps cost/day/model rollups tied to usage-producing
		// records while still grouping under the user prompt text in detail/title.
		i.msg.Entry.TimeCreated = timestamp.UTC()
	}
	if record.Model != "" {
		i.msg.Entry.ModelID = record.Model
		i.msg.Entry.ProviderID = "anthropic"
	}
	i.msg.appendTextParts(record.TextParts)
	i.msg.appendReasoningParts(record.ReasoningParts)
	i.addToolUses(sessionID, record, timestamp, pendingTools)

	cost := computeCost(record.Model, record.Usage, record.HasUsage, record.ReportedUSD, pricing)
	contribution := assistantContribution{
		usage:      record.Usage,
		hasUsage:   record.HasUsage,
		cost:       cost,
		cumulative: record.ReportedUSDCumulative,
	}
	i.rawAssistantRecords++
	i.upsertAssistantContribution(sessionID, record, contribution)
	i.msg.Entry.FoldedAssistantCalls = i.rawAssistantRecords
	i.recomputeCostAndTokens()
}

func (i *interactionState) upsertAssistantContribution(sessionID string, record parsedRecord, contribution assistantContribution) {
	key := assistantBillingKey(sessionID, record)
	if key == "" {
		i.assistantCalls = append(i.assistantCalls, contribution)
		return
	}
	if i.billingContributionIndexes == nil {
		i.billingContributionIndexes = make(map[string]int)
	}
	if index, ok := i.billingContributionIndexes[key]; ok {
		i.assistantCalls[index] = mergeRepeatedBillingContribution(i.assistantCalls[index], contribution)
		return
	}
	i.billingContributionIndexes[key] = len(i.assistantCalls)
	i.assistantCalls = append(i.assistantCalls, contribution)
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

func (i *interactionState) addToolUses(sessionID string, record parsedRecord, timestamp time.Time, pendingTools map[string]pendingToolRef) {
	for _, toolUse := range record.ToolUses {
		input, truncation, redacted := redactAndTruncateToolInput(toolUse.Input)
		name := toolUse.Name
		if name == "" {
			name = "unknown"
		}
		callID := toolUse.ID
		if callID == "" {
			callID = fmt.Sprintf("%s:tool:%d:%d", sessionID, record.Line, len(i.msg.ToolParts)+1)
		}
		i.msg.ToolParts = append(i.msg.ToolParts, stats.ToolPart{
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
		i.msg.Entry.FoldedToolCalls++
		pendingTools[toolKey(sessionID, callID)] = pendingToolRef{MessageID: i.msg.Entry.ID, Index: len(i.msg.ToolParts) - 1}
	}
}

func (i *interactionState) recomputeCostAndTokens() {
	cost, tokens, status, provenance := combineInteractionContributions(i.assistantCalls)
	i.msg.Entry.Cost = cost
	i.msg.Entry.Tokens = tokens
	i.msg.Entry.CostStatus = status
	i.msg.Entry.CostProvenance = provenance
}

func combineInteractionContributions(contributions []assistantContribution) (float64, *stats.TokenStats, stats.CostStatus, *stats.CostProvenance) {
	if len(contributions) == 0 {
		missing := missingCost("USD")
		return 0, nil, missing.Status, missing.Provenance
	}
	for _, contribution := range contributions {
		if contribution.cumulative {
			return combineCumulativeInteractionContributions(contributions)
		}
	}
	return combineDeltaInteractionContributions(contributions)
}

func combineDeltaInteractionContributions(contributions []assistantContribution) (float64, *stats.TokenStats, stats.CostStatus, *stats.CostProvenance) {
	var totalCost float64
	var tokens stats.TokenStats
	hasTokens := false
	prov := &stats.CostProvenance{Currency: "USD"}
	statuses := make(map[stats.CostStatus]bool)
	for _, contribution := range contributions {
		totalCost += contribution.cost.Cost
		if contribution.hasUsage {
			hasTokens = true
			addUsageToTokens(&tokens, contribution.usage)
		}
		status := contribution.cost.Status
		if status == "" {
			status = stats.CostMissing
		}
		statuses[status] = true
		mergeCostProvenance(prov, contribution.cost.Provenance)
	}
	status := combineStatuses(statuses)
	prov.Status = status
	if status == stats.CostMixed {
		prov.Note = "interaction mixes reported, computed, approximate, or missing Claude Code costs"
	} else if status == stats.CostMissing {
		prov.Note = "interaction cost is unknown because Claude Code cost data is missing"
	}
	return totalCost, tokenPointer(tokens, hasTokens), status, prov
}

func combineCumulativeInteractionContributions(contributions []assistantContribution) (float64, *stats.TokenStats, stats.CostStatus, *stats.CostProvenance) {
	var tokens stats.TokenStats
	hasTokens := false
	chosen := contributions[0]
	for _, contribution := range contributions {
		if contribution.hasUsage {
			hasTokens = true
			maxUsageIntoTokens(&tokens, contribution.usage)
		}
		if contribution.cost.Cost >= chosen.cost.Cost {
			chosen = contribution
		}
	}
	status := chosen.cost.Status
	if status == "" {
		status = stats.CostMissing
	}
	provenance := cloneProvenance(chosen.cost.Provenance)
	if provenance == nil {
		provenance = &stats.CostProvenance{Status: status, Currency: "USD"}
		if status == stats.CostMissing {
			provenance.MissingCount = 1
		}
	}
	provenance.Status = status
	if provenance.Currency == "" {
		provenance.Currency = "USD"
	}
	cumulativeNote := "grouped Claude Code interaction uses final/max cumulative cost and token values to avoid linear overcount"
	if provenance.Note == "" {
		provenance.Note = cumulativeNote
	} else if !strings.Contains(provenance.Note, cumulativeNote) {
		provenance.Note += "; " + cumulativeNote
	}
	return chosen.cost.Cost, tokenPointer(tokens, hasTokens), status, provenance
}

func addUsageToTokens(tokens *stats.TokenStats, usage tokenUsage) {
	tokens.Input += usage.Input
	tokens.Output += usage.Output
	tokens.Reasoning += usage.Reasoning
	tokens.Cache.Read += usage.CacheRead
	tokens.Cache.Write += usage.CacheCreate
}

func maxUsageIntoTokens(tokens *stats.TokenStats, usage tokenUsage) {
	if usage.Input > tokens.Input {
		tokens.Input = usage.Input
	}
	if usage.Output > tokens.Output {
		tokens.Output = usage.Output
	}
	if usage.Reasoning > tokens.Reasoning {
		tokens.Reasoning = usage.Reasoning
	}
	if usage.CacheRead > tokens.Cache.Read {
		tokens.Cache.Read = usage.CacheRead
	}
	if usage.CacheCreate > tokens.Cache.Write {
		tokens.Cache.Write = usage.CacheCreate
	}
}

func tokenPointer(tokens stats.TokenStats, ok bool) *stats.TokenStats {
	if !ok {
		return nil
	}
	out := tokens
	return &out
}

func mergeCostProvenance(dst *stats.CostProvenance, src *stats.CostProvenance) {
	if src == nil {
		dst.MissingCount++
		return
	}
	dst.MissingCount += src.MissingCount
	dst.ComputedCount += src.ComputedCount
	dst.ReportedCount += src.ReportedCount
	if dst.PricingSnapshotID == "" {
		dst.PricingSnapshotID = src.PricingSnapshotID
	}
	if dst.PricingSource == "" {
		dst.PricingSource = src.PricingSource
	}
	if src.Currency != "" {
		dst.Currency = src.Currency
	}
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
	for _, msg := range session.Messages {
		if len(msg.TextParts) == 0 {
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
	return session.ID
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
