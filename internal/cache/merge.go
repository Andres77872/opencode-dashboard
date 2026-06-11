package cache

import (
	"context"
	"fmt"
	"sort"
	"time"

	"opencode-dashboard/internal/stats"
)

const hourKeyFormat = "2006-01-02T15:04:05Z"

// splitWindows describes how one request window divides at the finality
// cutoff: [start, cutEnd) is served from the cache, [gapStart, end) live.
type splitWindows struct {
	cachePQ  stats.PeriodQuery
	livePQ   stats.PeriodQuery
	hasCache bool
	hasLive  bool
	start    time.Time // zero when the window has no explicit lower bound ("all")
	cutoff   time.Time
	end      time.Time
}

// cacheWindowMs is the [start, end) millisecond window the cache half covers,
// for dedup queries against message_index.
func (sp splitWindows) cacheWindowMs() (int64, int64) {
	end := sp.end
	if !sp.cutoff.IsZero() && sp.cutoff.Before(end) {
		end = sp.cutoff
	}
	return timeToMillis(sp.start), end.UnixMilli()
}

func (sp splitWindows) gapStart() time.Time {
	gapStart := sp.cutoff
	if sp.start.After(gapStart) {
		gapStart = sp.start
	}
	return gapStart
}

// splitPeriod resolves pq to [start, end) the same way the cache and live
// window code does, then splits it at the cutoff. A zero cutoff (source never
// consolidated) yields live-only. now is a parameter for testability.
func splitPeriod(pq stats.PeriodQuery, cutoff, now time.Time) (splitWindows, error) {
	now = now.UTC()
	sp := splitWindows{cutoff: cutoff}
	var start, end time.Time
	switch {
	case !pq.FromTime.IsZero():
		start = pq.FromTime.UTC()
		end = now
		if !pq.ToTime.IsZero() {
			end = pq.ToTime.UTC()
		}
	case pq.From != "":
		from, err := time.ParseInLocation("2006-01-02", pq.From, time.UTC)
		if err != nil {
			return splitWindows{}, fmt.Errorf("invalid from date %q: expected YYYY-MM-DD format", pq.From)
		}
		start = from
		end = now
		if pq.To != "" {
			parsed, err := time.ParseInLocation("2006-01-02", pq.To, time.UTC)
			if err != nil {
				return splitWindows{}, fmt.Errorf("invalid to date %q: expected YYYY-MM-DD format", pq.To)
			}
			end = parsed.AddDate(0, 0, 1)
		}
		if !pq.ToTime.IsZero() && pq.ToTime.UTC().Before(end) {
			end = pq.ToTime.UTC()
		}
	default:
		period := pq.Period
		if period == "" {
			period = "7d"
		}
		if period == "all" {
			end = now
		} else if hours, ok := parseHourPeriod(period); ok {
			start = now.Add(-time.Duration(hours) * time.Hour)
			end = now
		} else {
			n, ok := map[string]int{"1d": 1, "7d": 7, "14d": 14, "30d": 30, "1y": 365}[period]
			if !ok {
				return splitWindows{}, fmt.Errorf("invalid period: %q (supported: 1d, 7d, 14d, 30d, 1y, all, plus hour presets 1h, 6h, 12h, 24h, 72h)", period)
			}
			end = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
			start = end.AddDate(0, 0, -n)
		}
		if !pq.ToTime.IsZero() && pq.ToTime.UTC().Before(end) {
			end = pq.ToTime.UTC()
		}
	}
	sp.start, sp.end = start, end

	if cutoff.IsZero() {
		sp.hasLive = true
		sp.livePQ = pq
		return sp, nil
	}
	cacheEnd := end
	if cutoff.Before(cacheEnd) {
		cacheEnd = cutoff
	}
	sp.hasCache = start.Before(cacheEnd)
	sp.hasLive = end.After(cutoff)
	if !sp.hasCache && !sp.hasLive {
		// Degenerate/empty window: serve it from the cache so the response
		// keeps the cache-backed shape (empty results).
		sp.hasCache = true
	}
	if sp.hasCache {
		sp.cachePQ = pq
		if sp.cachePQ.ToTime.IsZero() || cutoff.Before(sp.cachePQ.ToTime) {
			sp.cachePQ.ToTime = cutoff
		}
	}
	if sp.hasLive {
		sp.livePQ = pq
		sp.livePQ.FromTime = sp.gapStart()
		sp.livePQ.ToTime = end
	}
	return sp, nil
}

// gapData is the recent window fetched live: every message in [gapStart, end),
// with list-level cost status patched into entries that lack one (mirroring
// collectMessagesAndTools). A failed live fetch degrades to an empty gap.
type gapData struct {
	msgs       []stats.MessageEntry
	listStatus stats.CostStatus
	listProv   *stats.CostProvenance
	total      int64
}

const gapPageSize = 100

func fetchGapMessages(ctx context.Context, live sourceReader, pq stats.PeriodQuery) (gapData, error) {
	var g gapData
	for page := 1; ; page++ {
		list, err := live.Messages(ctx, pq, page, gapPageSize, syncSort)
		if err != nil {
			return gapData{}, err
		}
		if page == 1 {
			g.listStatus, g.listProv = list.CostStatus, list.CostProvenance
			g.total = list.Total
		}
		for _, entry := range list.Messages {
			if entry.CostStatus == "" && entry.Role == "assistant" {
				entry.CostStatus = list.CostStatus
				entry.CostProvenance = list.CostProvenance
			}
			g.msgs = append(g.msgs, entry)
		}
		if len(list.Messages) == 0 || int64(page*list.PageSize) >= list.Total {
			break
		}
	}
	g.total = int64(len(g.msgs))
	return g, nil
}

func fetchGapSessions(ctx context.Context, live sourceReader, query stats.SessionQuery) ([]stats.SessionEntry, error) {
	query.PageSize = gapPageSize
	if query.Sort == "" {
		query.Sort = stats.SessionSortOldest
	}
	entries := make([]stats.SessionEntry, 0)
	for page := 1; ; page++ {
		query.Page = page
		list, err := live.Sessions(ctx, query)
		if err != nil {
			return nil, err
		}
		entries = append(entries, list.Sessions...)
		if len(list.Sessions) == 0 || int64(page*list.PageSize) >= list.Total {
			break
		}
	}
	return entries, nil
}

// sourceReader is the subset of source.Source the merge layer reads from.
type sourceReader interface {
	Messages(ctx context.Context, pq stats.PeriodQuery, page, limit int, sort stats.MessageSort) (stats.MessageList, error)
	Sessions(ctx context.Context, query stats.SessionQuery) (stats.SessionList, error)
}

// ---- cost status / provenance merging ----

type costAgg struct {
	reported int64
	computed int64
	missing  int64
	statuses map[stats.CostStatus]int64
}

func (c *costAgg) add(entry stats.MessageEntry) {
	if entry.Role != "assistant" || entry.CostStatus == "" {
		return
	}
	if c.statuses == nil {
		c.statuses = make(map[stats.CostStatus]int64)
	}
	c.statuses[entry.CostStatus]++
	switch entry.CostStatus {
	case stats.CostReported:
		c.reported++
	case stats.CostMissing:
		c.missing++
	default:
		c.computed++
	}
}

// result mirrors costSummaryWhere: single status passes through, several mix.
func (c *costAgg) result() (stats.CostStatus, *stats.CostProvenance) {
	total := c.reported + c.computed + c.missing
	if total == 0 {
		return "", nil
	}
	status := stats.CostMixed
	if len(c.statuses) == 1 {
		for only := range c.statuses {
			status = only
		}
	}
	return status, &stats.CostProvenance{
		Status:        status,
		Currency:      "USD",
		MissingCount:  c.missing,
		ComputedCount: c.computed,
		ReportedCount: c.reported,
	}
}

func mergeCost(aStatus stats.CostStatus, aProv *stats.CostProvenance, bStatus stats.CostStatus, bProv *stats.CostProvenance) (stats.CostStatus, *stats.CostProvenance) {
	if bStatus == "" && bProv == nil {
		return aStatus, aProv
	}
	if aStatus == "" && aProv == nil {
		return bStatus, bProv
	}
	status := aStatus
	if aStatus != bStatus {
		status = stats.CostMixed
	}
	prov := &stats.CostProvenance{Status: status, Currency: "USD"}
	for _, p := range []*stats.CostProvenance{aProv, bProv} {
		if p == nil {
			continue
		}
		prov.MissingCount += p.MissingCount
		prov.ComputedCount += p.ComputedCount
		prov.ReportedCount += p.ReportedCount
	}
	return status, prov
}

// ---- gap aggregation ----

type bucketAgg struct {
	messages int64
	cost     float64
	tokens   stats.TokenStats
	sessions map[string]bool
	cost2    costAgg
}

func newBucketAgg() *bucketAgg {
	return &bucketAgg{sessions: make(map[string]bool)}
}

func (b *bucketAgg) add(entry stats.MessageEntry) {
	b.messages++
	b.cost += entry.Cost
	if entry.Tokens != nil {
		addTokens(&b.tokens, *entry.Tokens)
	}
	if entry.SessionID != "" {
		b.sessions[entry.SessionID] = true
	}
	b.cost2.add(entry)
}

func (b *bucketAgg) sessionIDs() []string {
	ids := make([]string, 0, len(b.sessions))
	for id := range b.sessions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func addTokens(dst *stats.TokenStats, src stats.TokenStats) {
	dst.Input += src.Input
	dst.Output += src.Output
	dst.Reasoning += src.Reasoning
	dst.Cache.Read += src.Cache.Read
	dst.Cache.Write += src.Cache.Write
}

func dayKey(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

func hourKey(t time.Time) string {
	return t.UTC().Truncate(time.Hour).Format(hourKeyFormat)
}

// aggregateGap groups gap messages by key (use a constant key for totals).
func aggregateGap(msgs []stats.MessageEntry, keyOf func(stats.MessageEntry) (string, bool)) map[string]*bucketAgg {
	buckets := make(map[string]*bucketAgg)
	for _, entry := range msgs {
		key, ok := keyOf(entry)
		if !ok {
			continue
		}
		b := buckets[key]
		if b == nil {
			b = newBucketAgg()
			buckets[key] = b
		}
		b.add(entry)
	}
	return buckets
}

func allKey(stats.MessageEntry) (string, bool) { return "", true }

// ---- per-endpoint merges ----

func (s *CachedSource) mergeOverview(ctx context.Context, sp splitWindows, c stats.OverviewStats, gap gapData) (stats.OverviewStats, error) {
	out := c
	if len(gap.msgs) == 0 {
		return out, nil
	}
	agg := newBucketAgg()
	gapDays := make(map[string]bool)
	for _, entry := range gap.msgs {
		agg.add(entry)
		gapDays[dayKey(entry.TimeCreated)] = true
	}
	out.Messages += agg.messages
	out.Cost += agg.cost
	addTokens(&out.Tokens, agg.tokens)

	startMs, endMs := sp.cacheWindowMs()
	overlap, err := s.store.distinctSessionOverlap(ctx, s.sourceID(), startMs, endMs, agg.sessionIDs(), "", nil)
	if err != nil {
		return out, err
	}
	out.Sessions = c.Sessions + int64(len(agg.sessions)) - overlap

	// Only the cutoff's calendar day can be active on both sides of the split.
	addDays := len(gapDays)
	boundary := dayKey(sp.cutoff)
	if gapDays[boundary] {
		lo := dayStartMs(boundary)
		if startMs > lo {
			lo = startMs
		}
		active, err := s.store.hasActivity(ctx, s.sourceID(), lo, endMs)
		if err != nil {
			return out, err
		}
		if active {
			addDays--
		}
	}
	out.Days = c.Days + addDays
	if out.Days > 0 {
		out.CostPerDay = out.Cost / float64(out.Days)
	} else {
		out.CostPerDay = 0
	}
	gapStatus, gapProv := agg.cost2.result()
	out.CostStatus, out.CostProvenance = mergeCost(c.CostStatus, c.CostProvenance, gapStatus, gapProv)
	return out, nil
}

func (s *CachedSource) mergeDaily(ctx context.Context, sp splitWindows, gran stats.Granularity, c stats.DailyStats, gap gapData) (stats.DailyStats, error) {
	sourceID := s.sourceID()
	out := stats.DailyStats{SourceID: sourceID, Granularity: gran}

	if gran == stats.GranularityHour {
		// The cutoff is hour-aligned in steady state, so buckets come whole
		// from one half. A pre-v4 cache can briefly carry a non-aligned
		// cutoff; its single spanning bucket merges additively (cache rows
		// are strictly before the cutoff, gap rows at/after it, so messages
		// and cost cannot double count; sessions may transiently over-count
		// in that one bucket).
		byKey := make(map[string]stats.DayStats, len(c.Days))
		for _, d := range c.Days {
			byKey[d.Date] = d
		}
		gapBuckets := aggregateGap(gap.msgs, func(e stats.MessageEntry) (string, bool) {
			return hourKey(e.TimeCreated), true
		})
		rangeStart := sp.start
		if rangeStart.IsZero() {
			if len(c.Days) > 0 {
				if t, err := time.Parse(hourKeyFormat, c.Days[0].Date); err == nil {
					rangeStart = t
				}
			}
			if rangeStart.IsZero() {
				rangeStart = sp.gapStart()
			}
		}
		rangeStart = rangeStart.UTC().Truncate(time.Hour)
		end := sp.end
		if !end.After(rangeStart) {
			end = rangeStart.Add(time.Hour)
		}
		days := make([]stats.DayStats, 0)
		for t := rangeStart; t.Before(end); t = t.Add(time.Hour) {
			key := t.Format(hourKeyFormat)
			bucketEnd := t.Add(time.Hour)
			switch {
			case !bucketEnd.After(sp.cutoff): // bucket entirely consolidated
				if d, ok := byKey[key]; ok {
					days = append(days, d)
				} else {
					days = append(days, stats.DayStats{SourceID: sourceID, Date: key})
				}
			case !t.Before(sp.cutoff): // bucket entirely in the live gap
				if b, ok := gapBuckets[key]; ok {
					days = append(days, dayStatsFromAgg(sourceID, key, b))
				} else {
					days = append(days, stats.DayStats{SourceID: sourceID, Date: key})
				}
			default: // bucket spans a non-aligned legacy cutoff
				day := stats.DayStats{SourceID: sourceID, Date: key}
				if d, ok := byKey[key]; ok {
					day = d
				}
				if b, ok := gapBuckets[key]; ok {
					day.Sessions += int64(len(b.sessions))
					day.Messages += b.messages
					day.Cost += b.cost
					addTokens(&day.Tokens, b.tokens)
					gapStatus, gapProv := b.cost2.result()
					day.CostStatus, day.CostProvenance = mergeCost(day.CostStatus, day.CostProvenance, gapStatus, gapProv)
				}
				days = append(days, day)
			}
		}
		out.Days = days
		gapAgg := newBucketAgg()
		for _, entry := range gap.msgs {
			gapAgg.cost2.add(entry)
			_ = entry
		}
		gapStatus, gapProv := gapAgg.cost2.result()
		out.CostStatus, out.CostProvenance = mergeCost(c.CostStatus, c.CostProvenance, gapStatus, gapProv)
		return out, nil
	}

	// Day granularity: merge per date; only the cutoff's date can span both
	// halves and needs the distinct-session dedup.
	byDate := make(map[string]stats.DayStats, len(c.Days))
	for _, d := range c.Days {
		byDate[d.Date] = d
	}
	gapBuckets := aggregateGap(gap.msgs, func(e stats.MessageEntry) (string, bool) {
		return dayKey(e.TimeCreated), true
	})
	startMs, endMs := sp.cacheWindowMs()
	boundary := dayKey(sp.cutoff)
	merged := make(map[string]stats.DayStats)
	for date, d := range byDate {
		merged[date] = d
	}
	for date, b := range gapBuckets {
		cacheDay, hadCache := merged[date]
		day := cacheDay
		if !hadCache {
			day = stats.DayStats{SourceID: sourceID, Date: date}
		}
		day.Messages += b.messages
		day.Cost += b.cost
		addTokens(&day.Tokens, b.tokens)
		gapSessions := int64(len(b.sessions))
		if date == boundary && hadCache {
			lo := dayStartMs(date)
			if startMs > lo {
				lo = startMs
			}
			overlap, err := s.store.distinctSessionOverlap(ctx, sourceID, lo, endMs, b.sessionIDs(), "", nil)
			if err != nil {
				return out, err
			}
			day.Sessions = cacheDay.Sessions + gapSessions - overlap
		} else {
			day.Sessions += gapSessions
		}
		gapStatus, gapProv := b.cost2.result()
		day.CostStatus, day.CostProvenance = mergeCost(cacheDay.CostStatus, cacheDay.CostProvenance, gapStatus, gapProv)
		merged[date] = day
	}

	rangeStart := sp.start
	if rangeStart.IsZero() {
		rangeStart = earliestDate(merged)
		if rangeStart.IsZero() {
			rangeStart = sp.gapStart()
		}
	}
	days := make([]stats.DayStats, 0)
	for t := utcDay(rangeStart); t.Before(sp.end); t = t.AddDate(0, 0, 1) {
		key := t.Format("2006-01-02")
		if d, ok := merged[key]; ok {
			days = append(days, d)
		} else {
			days = append(days, stats.DayStats{SourceID: sourceID, Date: key})
		}
	}
	out.Days = days
	gapAgg := newBucketAgg()
	for _, entry := range gap.msgs {
		gapAgg.cost2.add(entry)
	}
	gapStatus, gapProv := gapAgg.cost2.result()
	out.CostStatus, out.CostProvenance = mergeCost(c.CostStatus, c.CostProvenance, gapStatus, gapProv)
	return out, nil
}

func dayStatsFromAgg(sourceID, date string, b *bucketAgg) stats.DayStats {
	d := stats.DayStats{SourceID: sourceID, Date: date, Sessions: int64(len(b.sessions)), Messages: b.messages, Cost: b.cost, Tokens: b.tokens}
	d.CostStatus, d.CostProvenance = b.cost2.result()
	return d
}

func earliestDate(byDate map[string]stats.DayStats) time.Time {
	var min time.Time
	for date := range byDate {
		t, err := time.ParseInLocation("2006-01-02", date, time.UTC)
		if err != nil {
			continue
		}
		if min.IsZero() || t.Before(min) {
			min = t
		}
	}
	return min
}

func (s *CachedSource) mergeModels(ctx context.Context, sp splitWindows, c stats.ModelStats, gap gapData) (stats.ModelStats, error) {
	out := stats.ModelStats{SourceID: s.sourceID()}
	type modelKey struct{ provider, model string }
	gapByKey := make(map[modelKey]*bucketAgg)
	for _, entry := range gap.msgs {
		if entry.Role != "assistant" || entry.ModelID == "" {
			continue
		}
		key := modelKey{provider: entry.ProviderID, model: entry.ModelID}
		b := gapByKey[key]
		if b == nil {
			b = newBucketAgg()
			gapByKey[key] = b
		}
		b.add(entry)
	}

	startMs, endMs := sp.cacheWindowMs()
	merged := make(map[modelKey]stats.ModelEntry, len(c.Models))
	for _, entry := range c.Models {
		merged[modelKey{provider: entry.ProviderID, model: entry.ModelID}] = entry
	}
	for key, b := range gapByKey {
		entry, hadCache := merged[key]
		if !hadCache {
			entry = stats.ModelEntry{SourceID: s.sourceID(), ModelID: key.model, ProviderID: key.provider}
		}
		entry.Messages += b.messages
		entry.Cost += b.cost
		addTokens(&entry.Tokens, b.tokens)
		gapSessions := int64(len(b.sessions))
		if hadCache && entry.Sessions > 0 {
			overlap, err := s.store.distinctSessionOverlap(ctx, s.sourceID(), startMs, endMs, b.sessionIDs(),
				"AND role = 'assistant' AND COALESCE(model_id, '') = ? AND COALESCE(provider_id, '') = ?", []any{key.model, key.provider})
			if err != nil {
				return out, err
			}
			entry.Sessions = entry.Sessions + gapSessions - overlap
		} else {
			entry.Sessions += gapSessions
		}
		gapStatus, gapProv := b.cost2.result()
		entry.CostStatus, entry.CostProvenance = mergeCost(entry.CostStatus, entry.CostProvenance, gapStatus, gapProv)
		entry.AvgTokensPerMessage, entry.AvgTokensPerSession = nil, nil
		setModelAverages(&entry)
		merged[key] = entry
	}
	models := make([]stats.ModelEntry, 0, len(merged))
	for _, entry := range merged {
		models = append(models, entry)
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].Cost != models[j].Cost {
			return models[i].Cost > models[j].Cost
		}
		if models[i].Messages != models[j].Messages {
			return models[i].Messages > models[j].Messages
		}
		return models[i].ModelID < models[j].ModelID
	})
	out.Models = models
	gapAgg := newBucketAgg()
	for _, entry := range gap.msgs {
		gapAgg.cost2.add(entry)
	}
	gapStatus, gapProv := gapAgg.cost2.result()
	out.CostStatus, out.CostProvenance = mergeCost(c.CostStatus, c.CostProvenance, gapStatus, gapProv)
	return out, nil
}

// mergeTools merges by tool name. Per-tool session counts are summed — a
// session whose tool calls span the cutoff counts once per half (documented
// over-count; per-tool gap session ids are not cheaply obtainable).
func mergeTools(sourceID string, c, gap stats.ToolStats) stats.ToolStats {
	merged := make(map[string]stats.ToolEntry, len(c.Tools))
	for _, entry := range c.Tools {
		merged[entry.Name] = entry
	}
	for _, entry := range gap.Tools {
		existing, ok := merged[entry.Name]
		if !ok {
			entry.SourceID = sourceID
			merged[entry.Name] = entry
			continue
		}
		existing.Invocations += entry.Invocations
		existing.Successes += entry.Successes
		existing.Failures += entry.Failures
		existing.Sessions += entry.Sessions
		merged[entry.Name] = existing
	}
	tools := make([]stats.ToolEntry, 0, len(merged))
	for _, entry := range merged {
		tools = append(tools, entry)
	}
	sort.Slice(tools, func(i, j int) bool {
		if tools[i].Invocations != tools[j].Invocations {
			return tools[i].Invocations > tools[j].Invocations
		}
		return tools[i].Name < tools[j].Name
	})
	return stats.ToolStats{SourceID: sourceID, Tools: tools}
}

func (s *CachedSource) mergeProjects(ctx context.Context, sp splitWindows, c stats.ProjectStats, gap gapData, gapSessions []stats.SessionEntry) (stats.ProjectStats, error) {
	out := stats.ProjectStats{SourceID: s.sourceID()}
	projectOf := make(map[string]stats.SessionEntry, len(gapSessions))
	for _, sess := range gapSessions {
		projectOf[sess.ID] = sess
	}
	gapByProject := aggregateGap(gap.msgs, func(e stats.MessageEntry) (string, bool) {
		return projectOf[e.SessionID].ProjectID, true
	})

	startMs, endMs := sp.cacheWindowMs()
	merged := make(map[string]stats.ProjectEntry, len(c.Projects))
	for _, entry := range c.Projects {
		merged[entry.ProjectID] = entry
	}
	for projectID, b := range gapByProject {
		entry, hadCache := merged[projectID]
		if !hadCache {
			name := projectID
			for _, sess := range gapSessions {
				if sess.ProjectID == projectID && sess.ProjectName != "" {
					name = sess.ProjectName
					break
				}
			}
			entry = stats.ProjectEntry{SourceID: s.sourceID(), ProjectID: projectID, ProjectName: name}
		}
		entry.Messages += b.messages
		entry.Cost += b.cost
		addTokens(&entry.Tokens, b.tokens)
		gapSessionCount := int64(len(b.sessions))
		if hadCache && entry.Sessions > 0 {
			overlap, err := s.store.distinctSessionOverlap(ctx, s.sourceID(), startMs, endMs, b.sessionIDs(),
				"AND COALESCE(project_id, '') = ?", []any{projectID})
			if err != nil {
				return out, err
			}
			entry.Sessions = entry.Sessions + gapSessionCount - overlap
		} else {
			entry.Sessions += gapSessionCount
		}
		gapStatus, gapProv := b.cost2.result()
		entry.CostStatus, entry.CostProvenance = mergeCost(entry.CostStatus, entry.CostProvenance, gapStatus, gapProv)
		merged[projectID] = entry
	}
	projects := make([]stats.ProjectEntry, 0, len(merged))
	for _, entry := range merged {
		projects = append(projects, entry)
	}
	sort.Slice(projects, func(i, j int) bool {
		if projects[i].Cost != projects[j].Cost {
			return projects[i].Cost > projects[j].Cost
		}
		if projects[i].Messages != projects[j].Messages {
			return projects[i].Messages > projects[j].Messages
		}
		return projects[i].ProjectID < projects[j].ProjectID
	})
	out.Projects = projects
	gapAgg := newBucketAgg()
	for _, entry := range gap.msgs {
		gapAgg.cost2.add(entry)
	}
	gapStatus, gapProv := gapAgg.cost2.result()
	out.CostStatus, out.CostProvenance = mergeCost(c.CostStatus, c.CostProvenance, gapStatus, gapProv)
	return out, nil
}

// mergeDailyDimension merges per (date, dimension) rows. Session counts are
// summed: only the boundary date can over-count, and per-(day,dimension)
// dedup is not worth a query per row (documented over-count).
func mergeDailyDimension(sourceID, dimension, period string, c stats.DailyDimensionStats, gapRows []stats.DimensionDayStats, gapStatus stats.CostStatus, gapProv *stats.CostProvenance) stats.DailyDimensionStats {
	type rowKey struct{ date, dim string }
	merged := make(map[rowKey]stats.DimensionDayStats, len(c.Days))
	for _, row := range c.Days {
		merged[rowKey{date: row.Date, dim: row.Dimension}] = row
	}
	for _, row := range gapRows {
		key := rowKey{date: row.Date, dim: row.Dimension}
		existing, ok := merged[key]
		if !ok {
			row.SourceID = sourceID
			merged[key] = row
			continue
		}
		existing.Sessions += row.Sessions
		existing.Messages += row.Messages
		existing.Cost += row.Cost
		addTokens(&existing.Tokens, row.Tokens)
		existing.CostStatus, existing.CostProvenance = mergeCost(existing.CostStatus, existing.CostProvenance, row.CostStatus, row.CostProvenance)
		merged[key] = existing
	}
	rows := make([]stats.DimensionDayStats, 0, len(merged))
	for _, row := range merged {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Date != rows[j].Date {
			return rows[i].Date < rows[j].Date
		}
		if rows[i].Messages != rows[j].Messages {
			return rows[i].Messages > rows[j].Messages
		}
		return rows[i].Dimension < rows[j].Dimension
	})
	status, prov := mergeCost(c.CostStatus, c.CostProvenance, gapStatus, gapProv)
	return stats.DailyDimensionStats{SourceID: sourceID, Days: rows, Dimension: dimension, Period: period, CostStatus: status, CostProvenance: prov}
}

// gapDimensionRows builds per (date, dimension) rows from gap messages for
// the model and project dimensions (tool rows come from the live source).
func gapDimensionRows(sourceID, dimension string, msgs []stats.MessageEntry, projectOf map[string]stats.SessionEntry) []stats.DimensionDayStats {
	keyOf := func(e stats.MessageEntry) (string, bool) {
		if e.Role != "assistant" {
			return "", false
		}
		switch dimension {
		case "model":
			if e.ModelID == "" {
				return "", false
			}
			return e.ModelID, true
		case "project":
			projectID := projectOf[e.SessionID].ProjectID
			if projectID == "" {
				return "", false
			}
			return projectID, true
		}
		return "", false
	}
	type rowKey struct{ date, dim string }
	buckets := make(map[rowKey]*bucketAgg)
	for _, entry := range msgs {
		dim, ok := keyOf(entry)
		if !ok {
			continue
		}
		key := rowKey{date: dayKey(entry.TimeCreated), dim: dim}
		b := buckets[key]
		if b == nil {
			b = newBucketAgg()
			buckets[key] = b
		}
		b.add(entry)
	}
	rows := make([]stats.DimensionDayStats, 0, len(buckets))
	for key, b := range buckets {
		row := stats.DimensionDayStats{SourceID: sourceID, Date: key.date, Dimension: key.dim, Sessions: int64(len(b.sessions)), Messages: b.messages, Cost: b.cost, Tokens: b.tokens}
		row.CostStatus, row.CostProvenance = b.cost2.result()
		rows = append(rows, row)
	}
	return rows
}
