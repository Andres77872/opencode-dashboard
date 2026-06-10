package codex

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"opencode-dashboard/internal/stats"
)

type periodWindow struct {
	start time.Time
	end   time.Time
	all   bool
}

func (s *snapshot) overview(pq stats.PeriodQuery) (stats.OverviewStats, error) {
	messages, err := s.filteredMessages(pq)
	if err != nil {
		return stats.OverviewStats{}, err
	}
	cost, tokens, status, provenance := aggregateCostProvenance(messages)
	days := uniqueDays(messages)
	result := stats.OverviewStats{SourceID: codexSourceID, Sessions: int64(len(uniqueSessions(messages))), Messages: int64(len(messages)), Cost: cost, Tokens: tokens, Days: len(days), CostStatus: status, CostProvenance: provenance}
	if result.Days > 0 {
		result.CostPerDay = cost / float64(result.Days)
	}
	return result, nil
}

func (s *snapshot) daily(pq stats.PeriodQuery, granularity ...stats.Granularity) (stats.DailyStats, error) {
	gran := stats.GranularityDay
	if len(granularity) > 0 && granularity[0] != "" {
		gran = granularity[0]
	} else if pq.Period == "1d" || isHourPreset(pq.Period) {
		gran = stats.GranularityHour
	}
	window, err := s.window(pq)
	if err != nil {
		return stats.DailyStats{}, err
	}
	messages, err := s.filteredMessages(pq)
	if err != nil {
		return stats.DailyStats{}, err
	}
	groups := make(map[string][]*messageRecord)
	for _, msg := range messages {
		key := msg.Entry.TimeCreated.UTC().Format("2006-01-02")
		if gran == stats.GranularityHour {
			key = msg.Entry.TimeCreated.UTC().Truncate(time.Hour).Format("2006-01-02T15:04:05Z")
		}
		groups[key] = append(groups[key], msg)
	}
	fillEmptyBuckets(groups, window, gran, messages)
	keys := sortedKeys(groups)
	days := make([]stats.DayStats, 0, len(keys))
	for _, key := range keys {
		group := groups[key]
		if len(group) == 0 {
			days = append(days, stats.DayStats{SourceID: codexSourceID, Date: key})
			continue
		}
		cost, tokens, status, provenance := aggregateCostProvenance(group)
		days = append(days, stats.DayStats{SourceID: codexSourceID, Date: key, Sessions: int64(len(uniqueSessions(group))), Messages: int64(len(group)), Cost: cost, Tokens: tokens, CostStatus: status, CostProvenance: provenance})
	}
	_, _, status, provenance := aggregateCostProvenance(messages)
	return stats.DailyStats{SourceID: codexSourceID, Days: days, Granularity: gran, CostStatus: status, CostProvenance: provenance}, nil
}

func (s *snapshot) dailyDimension(dimension string, pq stats.PeriodQuery) (stats.DailyDimensionStats, error) {
	if dimension != "model" && dimension != "tool" && dimension != "project" {
		return stats.DailyDimensionStats{}, fmt.Errorf("invalid dimension %q: supported values are model, tool, project", dimension)
	}
	messages, err := s.filteredMessages(pq)
	if err != nil {
		return stats.DailyDimensionStats{}, err
	}
	type key struct{ day, dim string }
	groups := make(map[key][]*messageRecord)
	for _, msg := range messages {
		// Dimension breakdowns count assistant API-request rows only, matching
		// the other sources; user-prompt rows carry no model/cost anyway.
		if msg.Entry.Role != "assistant" {
			continue
		}
		day := msg.Entry.TimeCreated.UTC().Format("2006-01-02")
		switch dimension {
		case "model":
			if msg.Entry.ModelID != "" {
				groups[key{day: day, dim: msg.Entry.ModelID}] = append(groups[key{day: day, dim: msg.Entry.ModelID}], msg)
			}
		case "project":
			groups[key{day: day, dim: msg.projectID}] = append(groups[key{day: day, dim: msg.projectID}], msg)
		case "tool":
			seen := make(map[string]bool)
			for _, tool := range msg.ToolParts {
				if tool.Tool == "" || seen[tool.Tool] {
					continue
				}
				seen[tool.Tool] = true
				groups[key{day: day, dim: tool.Tool}] = append(groups[key{day: day, dim: tool.Tool}], msg)
			}
		}
	}
	keys := make([]key, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].day != keys[j].day {
			return keys[i].day < keys[j].day
		}
		return keys[i].dim < keys[j].dim
	})
	days := make([]stats.DimensionDayStats, 0, len(keys))
	for _, key := range keys {
		group := groups[key]
		cost, tokens, status, provenance := aggregateCostProvenance(group)
		days = append(days, stats.DimensionDayStats{SourceID: codexSourceID, Date: key.day, Dimension: key.dim, Sessions: int64(len(uniqueSessions(group))), Messages: int64(len(group)), Cost: cost, Tokens: tokens, CostStatus: status, CostProvenance: provenance})
	}
	periodLabel := pq.Period
	if periodLabel == "" && pq.From != "" {
		periodLabel = "from_" + pq.From
	}
	_, _, status, provenance := aggregateCostProvenance(messages)
	return stats.DailyDimensionStats{SourceID: codexSourceID, Days: days, Dimension: dimension, Period: periodLabel, CostStatus: status, CostProvenance: provenance}, nil
}

func (s *snapshot) models(pq stats.PeriodQuery) (stats.ModelStats, error) {
	messages, err := s.filteredMessages(pq)
	if err != nil {
		return stats.ModelStats{}, err
	}
	type aggregate struct {
		messages []*messageRecord
		sessions map[string]bool
	}
	aggs := make(map[string]*aggregate)
	for _, msg := range messages {
		if msg.Entry.ModelID == "" {
			continue
		}
		key := msg.Entry.ProviderID + "\x00" + msg.Entry.ModelID
		agg := aggs[key]
		if agg == nil {
			agg = &aggregate{sessions: map[string]bool{}}
			aggs[key] = agg
		}
		agg.messages = append(agg.messages, msg)
		agg.sessions[msg.Entry.SessionID] = true
	}
	models := make([]stats.ModelEntry, 0, len(aggs))
	for key, agg := range aggs {
		parts := strings.SplitN(key, "\x00", 2)
		cost, tokens, status, provenance := aggregateCostProvenance(agg.messages)
		entry := stats.ModelEntry{SourceID: codexSourceID, ProviderID: parts[0], ModelID: parts[1], Sessions: int64(len(agg.sessions)), Messages: int64(len(agg.messages)), Cost: cost, Tokens: tokens, CostStatus: status, CostProvenance: provenance}
		if entry.Messages > 0 {
			entry.AvgTokensPerMessage = &stats.AvgTokenStats{Input: float64(tokens.Input) / float64(entry.Messages), Output: float64(tokens.Output) / float64(entry.Messages), Reasoning: float64(tokens.Reasoning) / float64(entry.Messages), CacheRead: float64(tokens.Cache.Read) / float64(entry.Messages), CacheWrite: float64(tokens.Cache.Write) / float64(entry.Messages)}
		}
		if entry.Sessions > 0 {
			entry.AvgTokensPerSession = &stats.AvgTokenStats{Input: float64(tokens.Input) / float64(entry.Sessions), Output: float64(tokens.Output) / float64(entry.Sessions), Reasoning: float64(tokens.Reasoning) / float64(entry.Sessions), CacheRead: float64(tokens.Cache.Read) / float64(entry.Sessions), CacheWrite: float64(tokens.Cache.Write) / float64(entry.Sessions)}
		}
		models = append(models, entry)
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].Cost != models[j].Cost {
			return models[i].Cost > models[j].Cost
		}
		return models[i].ModelID < models[j].ModelID
	})
	_, _, status, provenance := aggregateCostProvenance(messages)
	return stats.ModelStats{SourceID: codexSourceID, Models: models, CostStatus: status, CostProvenance: provenance}, nil
}

func (s *snapshot) tools(pq stats.PeriodQuery) (stats.ToolStats, error) {
	messages, err := s.filteredMessages(pq)
	if err != nil {
		return stats.ToolStats{}, err
	}
	type aggregate struct {
		entry    stats.ToolEntry
		sessions map[string]bool
	}
	aggs := make(map[string]*aggregate)
	for _, msg := range messages {
		for _, tool := range msg.ToolParts {
			if tool.Tool == "" {
				continue
			}
			agg := aggs[tool.Tool]
			if agg == nil {
				agg = &aggregate{entry: stats.ToolEntry{SourceID: codexSourceID, Name: tool.Tool}, sessions: map[string]bool{}}
				aggs[tool.Tool] = agg
			}
			agg.entry.Invocations++
			agg.sessions[msg.Entry.SessionID] = true
			switch tool.State.Status {
			case "completed":
				agg.entry.Successes++
			case "error":
				agg.entry.Failures++
			}
		}
	}
	tools := make([]stats.ToolEntry, 0, len(aggs))
	for _, agg := range aggs {
		agg.entry.Sessions = int64(len(agg.sessions))
		tools = append(tools, agg.entry)
	}
	sort.Slice(tools, func(i, j int) bool {
		if tools[i].Invocations != tools[j].Invocations {
			return tools[i].Invocations > tools[j].Invocations
		}
		return tools[i].Name < tools[j].Name
	})
	return stats.ToolStats{SourceID: codexSourceID, Tools: tools}, nil
}

func (s *snapshot) projects(pq stats.PeriodQuery) (stats.ProjectStats, error) {
	messages, err := s.filteredMessages(pq)
	if err != nil {
		return stats.ProjectStats{}, err
	}
	byProject := make(map[string][]*messageRecord)
	for _, msg := range messages {
		byProject[msg.projectID] = append(byProject[msg.projectID], msg)
	}
	projects := make([]stats.ProjectEntry, 0, len(byProject))
	for id, group := range byProject {
		project := s.projectMap[id]
		if project == nil {
			project = &projectRecord{ID: id, Name: id}
		}
		cost, tokens, status, provenance := aggregateCostProvenance(group)
		projects = append(projects, stats.ProjectEntry{SourceID: codexSourceID, ProjectID: id, ProjectName: project.Name, Sessions: int64(len(uniqueSessions(group))), Messages: int64(len(group)), Cost: cost, Tokens: tokens, CostStatus: status, CostProvenance: provenance})
	}
	sort.Slice(projects, func(i, j int) bool {
		if projects[i].Cost != projects[j].Cost {
			return projects[i].Cost > projects[j].Cost
		}
		return projects[i].ProjectID < projects[j].ProjectID
	})
	_, _, status, provenance := aggregateCostProvenance(messages)
	return stats.ProjectStats{SourceID: codexSourceID, Projects: projects, CostStatus: status, CostProvenance: provenance}, nil
}

func (s *snapshot) projectByID(id string, pq stats.PeriodQuery, page, limit int) (*stats.ProjectDetail, error) {
	project := s.projectMap[id]
	if project == nil {
		return nil, nil
	}
	messages, err := s.filteredMessages(pq)
	if err != nil {
		return nil, err
	}
	projectMessages := make([]*messageRecord, 0)
	for _, msg := range messages {
		if msg.projectID == id {
			projectMessages = append(projectMessages, msg)
		}
	}
	allSessions := make([]*sessionRecord, 0)
	for _, session := range s.sessionMap {
		if session.ProjectID == id {
			allSessions = append(allSessions, session)
		}
	}
	sortSessions(allSessions, stats.SessionSortNewest)
	page, limit = normalizePage(page, limit, 10)
	recent := paginateSessions(allSessions, page, limit)
	recentEntries := make([]stats.SessionEntry, 0, len(recent))
	for _, session := range recent {
		recentEntries = append(recentEntries, session.entry())
	}
	cost, tokens, status, provenance := aggregateCostProvenance(projectMessages)
	return &stats.ProjectDetail{SourceID: codexSourceID, ProjectID: id, ProjectName: project.Name, Worktree: project.Worktree, Sessions: int64(len(uniqueSessions(projectMessages))), Messages: int64(len(projectMessages)), Cost: cost, Tokens: tokens, RecentSessions: recentEntries, TotalSessions: int64(len(allSessions)), CostStatus: status, CostProvenance: provenance}, nil
}

func (s *snapshot) sessions(query stats.SessionQuery) (stats.SessionList, error) {
	pq := stats.PeriodQuery{Period: query.Period, From: query.From, To: query.To}
	window, err := s.window(pq)
	if err != nil {
		return stats.SessionList{}, err
	}
	page, pageSize := normalizePage(query.Page, query.PageSize, 20)
	filter := strings.ToLower(strings.TrimSpace(query.Filter))
	sessions := make([]*sessionRecord, 0, len(s.sessionMap))
	for _, session := range s.sessionMap {
		if query.ProjectID != "" && session.ProjectID != query.ProjectID {
			continue
		}
		if filter != "" && !strings.Contains(strings.ToLower(session.Title), filter) && !strings.Contains(strings.ToLower(session.ProjectName), filter) && !strings.Contains(strings.ToLower(session.Directory), filter) {
			continue
		}
		if !window.all && !sessionHasMessageInWindow(session, window) {
			continue
		}
		sessions = append(sessions, session)
	}
	sortSessions(sessions, query.Sort)
	pageSessions := paginateSessions(sessions, page, pageSize)
	entries := make([]stats.SessionEntry, 0, len(pageSessions))
	for _, session := range pageSessions {
		entries = append(entries, session.entry())
	}
	msgs := messagesForSessions(sessions)
	_, _, status, provenance := aggregateCostProvenance(msgs)
	return stats.SessionList{SourceID: codexSourceID, Sessions: entries, Total: int64(len(sessions)), Page: page, PageSize: pageSize, CostStatus: status, CostProvenance: provenance}, nil
}

func (s *snapshot) sessionByID(id string) *stats.SessionDetail {
	session := s.sessionMap[id]
	if session == nil {
		return nil
	}
	messages := make([]stats.SessionMessage, 0, len(session.Messages))
	for _, msg := range session.Messages {
		entry := stats.SessionMessage{SourceID: codexSourceID, ID: msg.Entry.ID, Role: msg.Entry.Role, TimeCreated: msg.Entry.TimeCreated, Cost: msg.Entry.Cost, Tokens: cloneTokens(msg.Entry.Tokens), ModelID: msg.Entry.ModelID, ProviderID: msg.Entry.ProviderID, CostStatus: msg.Entry.CostStatus, CostProvenance: cloneProvenance(msg.Entry.CostProvenance)}
		messages = append(messages, entry)
	}
	cost, tokens, status, provenance := aggregateCostProvenance(session.Messages)
	return &stats.SessionDetail{SourceID: codexSourceID, ID: session.ID, Title: session.Title, ProjectID: session.ProjectID, ProjectName: session.ProjectName, Directory: session.Directory, TimeCreated: session.Created, TimeUpdated: session.Updated, Messages: messages, TotalCost: cost, TotalTokens: tokens, MessageCount: int64(len(messages)), CostStatus: status, CostProvenance: provenance}
}

func (s *snapshot) messages(pq stats.PeriodQuery, page, limit int, sortSpec stats.MessageSort) (stats.MessageList, error) {
	messages, err := s.filteredMessages(pq)
	if err != nil {
		return stats.MessageList{}, err
	}
	sortMessages(messages, sortSpec)
	page, limit = normalizePage(page, limit, 50)
	if limit > 100 {
		limit = 100
	}
	start := (page - 1) * limit
	end := start + limit
	if start > len(messages) {
		start = len(messages)
	}
	if end > len(messages) {
		end = len(messages)
	}
	entries := make([]stats.MessageEntry, 0, end-start)
	for _, msg := range messages[start:end] {
		entry := msg.Entry
		entry.Tokens = cloneTokens(entry.Tokens)
		entry.CostProvenance = cloneProvenance(entry.CostProvenance)
		entries = append(entries, entry)
	}
	_, _, status, provenance := aggregateCostProvenance(messages)
	return stats.MessageList{SourceID: codexSourceID, Messages: entries, Total: int64(len(messages)), Page: page, PageSize: limit, CostStatus: status, CostProvenance: provenance}, nil
}

func (s *snapshot) messageByID(id string) *stats.MessageDetail {
	msg := s.messageMap[id]
	return msg.detail()
}

func (s *snapshot) filteredMessages(pq stats.PeriodQuery) ([]*messageRecord, error) {
	window, err := s.window(pq)
	if err != nil {
		return nil, err
	}
	result := make([]*messageRecord, 0, len(s.ordered))
	for _, msg := range s.ordered {
		if window.all || inWindow(msg.Entry.TimeCreated, window) {
			result = append(result, msg)
		}
	}
	return result, nil
}

func (s *snapshot) window(pq stats.PeriodQuery) (periodWindow, error) {
	if pq.From != "" {
		from, err := time.ParseInLocation("2006-01-02", pq.From, time.UTC)
		if err != nil {
			return periodWindow{}, fmt.Errorf("invalid from date %q: expected YYYY-MM-DD format", pq.From)
		}
		to := time.Now().UTC()
		if pq.To != "" {
			parsed, err := time.ParseInLocation("2006-01-02", pq.To, time.UTC)
			if err != nil {
				return periodWindow{}, fmt.Errorf("invalid to date %q: expected YYYY-MM-DD format", pq.To)
			}
			to = parsed.AddDate(0, 0, 1)
		}
		return periodWindow{start: from, end: to}, nil
	}
	period := pq.Period
	if period == "" {
		period = "7d"
	}
	if period == "all" {
		return periodWindow{all: true}, nil
	}
	if hours, ok := parseHourPreset(period); ok {
		now := time.Now().UTC()
		return periodWindow{start: now.Add(-time.Duration(hours) * time.Hour), end: now}, nil
	}
	days, ok := map[string]int{"1d": 1, "7d": 7, "14d": 14, "30d": 30, "1y": 365}[period]
	if !ok {
		return periodWindow{}, fmt.Errorf("invalid period: %q (supported: 1d, 7d, 14d, 30d, 1y, all, plus hour presets 1h, 6h, 12h, 24h, 72h)", period)
	}
	now := time.Now().UTC()
	endDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
	startDate := endDate.AddDate(0, 0, -days)
	return periodWindow{start: startDate, end: endDate}, nil
}

func inWindow(t time.Time, window periodWindow) bool {
	return !t.Before(window.start) && t.Before(window.end)
}

// fillEmptyBuckets adds zero-value bucket keys for every day (or hour) in the
// window so gaps stay visible, matching the OpenCode SQL daily path. The "all"
// window has no bounds, so it spans the observed message times instead.
func fillEmptyBuckets(groups map[string][]*messageRecord, window periodWindow, gran stats.Granularity, messages []*messageRecord) {
	start, end := window.start, window.end
	if window.all {
		if len(messages) == 0 {
			return
		}
		start, end = messages[0].Entry.TimeCreated, messages[0].Entry.TimeCreated
		for _, msg := range messages {
			if msg.Entry.TimeCreated.Before(start) {
				start = msg.Entry.TimeCreated
			}
			if msg.Entry.TimeCreated.After(end) {
				end = msg.Entry.TimeCreated
			}
		}
		end = end.Add(time.Nanosecond)
	}
	if gran == stats.GranularityHour {
		for t := start.UTC().Truncate(time.Hour); t.Before(end); t = t.Add(time.Hour) {
			key := t.Format("2006-01-02T15:04:05Z")
			if _, ok := groups[key]; !ok {
				groups[key] = nil
			}
		}
		return
	}
	first := start.UTC()
	first = time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, time.UTC)
	for t := first; t.Before(end); t = t.AddDate(0, 0, 1) {
		key := t.Format("2006-01-02")
		if _, ok := groups[key]; !ok {
			groups[key] = nil
		}
	}
}

func parseHourPreset(period string) (int, bool) {
	switch period {
	case "1h":
		return 1, true
	case "6h":
		return 6, true
	case "12h":
		return 12, true
	case "24h":
		return 24, true
	case "72h":
		return 72, true
	default:
		return 0, false
	}
}

func isHourPreset(period string) bool { _, ok := parseHourPreset(period); return ok }

func uniqueSessions(messages []*messageRecord) map[string]bool {
	result := make(map[string]bool)
	for _, msg := range messages {
		result[msg.Entry.SessionID] = true
	}
	return result
}

func uniqueDays(messages []*messageRecord) map[string]bool {
	result := make(map[string]bool)
	for _, msg := range messages {
		result[msg.Entry.TimeCreated.UTC().Format("2006-01-02")] = true
	}
	return result
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func normalizePage(page, limit, defaultLimit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = defaultLimit
	}
	return page, limit
}

func sessionHasMessageInWindow(session *sessionRecord, window periodWindow) bool {
	for _, msg := range session.Messages {
		if inWindow(msg.Entry.TimeCreated, window) {
			return true
		}
	}
	return false
}

func sortSessions(sessions []*sessionRecord, mode stats.SessionSortMode) {
	sort.Slice(sessions, func(i, j int) bool {
		switch mode {
		case stats.SessionSortOldest:
			return sessions[i].Created.Before(sessions[j].Created)
		case stats.SessionSortCost:
			ci, _, _, _ := aggregateCostProvenance(sessions[i].Messages)
			cj, _, _, _ := aggregateCostProvenance(sessions[j].Messages)
			if ci != cj {
				return ci > cj
			}
		case stats.SessionSortMessages:
			if len(sessions[i].Messages) != len(sessions[j].Messages) {
				return len(sessions[i].Messages) > len(sessions[j].Messages)
			}
		}
		return sessions[i].Created.After(sessions[j].Created)
	})
}

func paginateSessions(sessions []*sessionRecord, page, limit int) []*sessionRecord {
	start := (page - 1) * limit
	end := start + limit
	if start > len(sessions) {
		start = len(sessions)
	}
	if end > len(sessions) {
		end = len(sessions)
	}
	return sessions[start:end]
}

func (s *sessionRecord) entry() stats.SessionEntry {
	cost, _, status, provenance := aggregateCostProvenance(s.Messages)
	return stats.SessionEntry{SourceID: codexSourceID, ID: s.ID, Title: s.Title, ProjectID: s.ProjectID, ProjectName: s.ProjectName, TimeCreated: s.Created, TimeUpdated: s.Updated, MessageCount: int64(len(s.Messages)), Cost: cost, CostStatus: status, CostProvenance: provenance}
}

func messagesForSessions(sessions []*sessionRecord) []*messageRecord {
	var messages []*messageRecord
	for _, session := range sessions {
		messages = append(messages, session.Messages...)
	}
	return messages
}

func sortMessages(messages []*messageRecord, sortSpec stats.MessageSort) {
	if sortSpec.Field == "" {
		sortSpec = stats.DefaultMessageSort()
	}
	sort.SliceStable(messages, func(i, j int) bool {
		cmp := 0
		switch sortSpec.Field {
		case stats.MessageSortCost:
			cmp = compareFloat(messages[i].Entry.Cost, messages[j].Entry.Cost)
		case stats.MessageSortTokens:
			cmp = compareInt(tokenTotal(messages[i]), tokenTotal(messages[j]))
		case stats.MessageSortModel:
			cmp = strings.Compare(messages[i].Entry.ModelID, messages[j].Entry.ModelID)
		case stats.MessageSortRole:
			cmp = strings.Compare(messages[i].Entry.Role, messages[j].Entry.Role)
		default:
			if messages[i].Entry.TimeCreated.Before(messages[j].Entry.TimeCreated) {
				cmp = -1
			} else if messages[i].Entry.TimeCreated.After(messages[j].Entry.TimeCreated) {
				cmp = 1
			}
		}
		if cmp == 0 {
			return messages[i].Entry.ID < messages[j].Entry.ID
		}
		if sortSpec.Direction == stats.MessageSortDesc {
			return cmp > 0
		}
		return cmp < 0
	})
}

func compareFloat(a, b float64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
func compareInt(a, b int64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func tokenTotal(msg *messageRecord) int64 {
	if msg.Entry.Tokens == nil {
		return 0
	}
	return msg.Entry.Tokens.Input + msg.Entry.Tokens.Output + msg.Entry.Tokens.Reasoning + msg.Entry.Tokens.Cache.Read + msg.Entry.Tokens.Cache.Write
}
