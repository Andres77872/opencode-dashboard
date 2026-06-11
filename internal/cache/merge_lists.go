package cache

import (
	"context"
	"sort"
	"strings"

	"opencode-dashboard/internal/stats"
)

// messageLess replicates messageOrderBy semantics in Go, including the
// message_id ASC tiebreak that applies in both directions.
func messageLess(a, b stats.MessageEntry, sortSpec stats.MessageSort) bool {
	asc := sortSpec.Direction == stats.MessageSortAsc
	cmp := 0
	switch sortSpec.Field {
	case stats.MessageSortCost:
		switch {
		case a.Cost < b.Cost:
			cmp = -1
		case a.Cost > b.Cost:
			cmp = 1
		}
	case stats.MessageSortTokens:
		ta, tb := tokenSum(a), tokenSum(b)
		switch {
		case ta < tb:
			cmp = -1
		case ta > tb:
			cmp = 1
		}
	case stats.MessageSortModel:
		cmp = strings.Compare(a.ModelID, b.ModelID)
	case stats.MessageSortRole:
		cmp = strings.Compare(a.Role, b.Role)
	default:
		am, bm := a.TimeCreated.UnixMilli(), b.TimeCreated.UnixMilli()
		switch {
		case am < bm:
			cmp = -1
		case am > bm:
			cmp = 1
		}
	}
	if cmp == 0 {
		return a.ID < b.ID
	}
	if asc {
		return cmp < 0
	}
	return cmp > 0
}

func tokenSum(e stats.MessageEntry) int64 {
	if e.Tokens == nil {
		return 0
	}
	return e.Tokens.Input + e.Tokens.Output + e.Tokens.Reasoning + e.Tokens.Cache.Read + e.Tokens.Cache.Write
}

// mergeMessages paginates over the merged cache + gap message lists. Windows
// are disjoint at the cutoff, so Total is a plain sum. For time sorts the gap
// rows are strictly newer than every cache row, so the merged list is a
// virtual concatenation and pages map to slices of each half; for other sorts
// the merged top offset+limit rows are always contained in (cache top
// offset+limit) ∪ gap, so an in-memory merge of those is exact.
func (s *CachedSource) mergeMessages(ctx context.Context, sp splitWindows, page, limit int, sortSpec stats.MessageSort, gap gapData) (stats.MessageList, error) {
	sourceID := s.sourceID()
	cacheTotal, err := s.store.messagesTotal(ctx, sourceID, sp.cachePQ)
	if err != nil {
		return stats.MessageList{}, err
	}
	gapSorted := append([]stats.MessageEntry(nil), gap.msgs...)
	sort.SliceStable(gapSorted, func(i, j int) bool { return messageLess(gapSorted[i], gapSorted[j], sortSpec) })

	total := cacheTotal + int64(len(gapSorted))
	offset := (page - 1) * limit
	var pageRows []stats.MessageEntry

	timeSort := sortSpec.Field == stats.MessageSortTime || sortSpec.Field == ""
	switch {
	case timeSort && sortSpec.Direction != stats.MessageSortAsc:
		// Virtual list: gap (all newer) then cache.
		gapCount := len(gapSorted)
		switch {
		case offset+limit <= gapCount:
			pageRows = gapSorted[offset : offset+limit]
		case offset >= gapCount:
			pageRows, err = s.store.messagesSlice(ctx, sourceID, sp.cachePQ, offset-gapCount, limit, sortSpec)
		default:
			head := gapSorted[offset:]
			tail, sliceErr := s.store.messagesSlice(ctx, sourceID, sp.cachePQ, 0, limit-len(head), sortSpec)
			err = sliceErr
			pageRows = append(append([]stats.MessageEntry(nil), head...), tail...)
		}
	case timeSort:
		// Ascending: cache first, then gap.
		cacheCount := int(cacheTotal)
		switch {
		case offset+limit <= cacheCount:
			pageRows, err = s.store.messagesSlice(ctx, sourceID, sp.cachePQ, offset, limit, sortSpec)
		case offset >= cacheCount:
			lo := offset - cacheCount
			hi := lo + limit
			if lo > len(gapSorted) {
				lo = len(gapSorted)
			}
			if hi > len(gapSorted) {
				hi = len(gapSorted)
			}
			pageRows = gapSorted[lo:hi]
		default:
			head, sliceErr := s.store.messagesSlice(ctx, sourceID, sp.cachePQ, offset, cacheCount-offset, sortSpec)
			err = sliceErr
			hi := limit - len(head)
			if hi > len(gapSorted) {
				hi = len(gapSorted)
			}
			pageRows = append(append([]stats.MessageEntry(nil), head...), gapSorted[:hi]...)
		}
	default:
		cacheTop, sliceErr := s.store.messagesSlice(ctx, sourceID, sp.cachePQ, 0, offset+limit, sortSpec)
		if sliceErr != nil {
			return stats.MessageList{}, sliceErr
		}
		merged := append(append([]stats.MessageEntry(nil), cacheTop...), gapSorted...)
		sort.SliceStable(merged, func(i, j int) bool { return messageLess(merged[i], merged[j], sortSpec) })
		lo, hi := offset, offset+limit
		if lo > len(merged) {
			lo = len(merged)
		}
		if hi > len(merged) {
			hi = len(merged)
		}
		pageRows = merged[lo:hi]
	}
	if err != nil {
		return stats.MessageList{}, err
	}
	if pageRows == nil {
		pageRows = []stats.MessageEntry{}
	}

	cacheStatus, cacheProv, err := s.store.costSummaryForPQ(ctx, sourceID, sp.cachePQ)
	if err != nil {
		return stats.MessageList{}, err
	}
	gapAgg := newBucketAgg()
	for _, entry := range gap.msgs {
		gapAgg.cost2.add(entry)
	}
	gapStatus, gapProv := gapAgg.cost2.result()
	status, prov := mergeCost(cacheStatus, cacheProv, gapStatus, gapProv)
	return stats.MessageList{SourceID: sourceID, Messages: pageRows, Total: total, Page: page, PageSize: limit, CostStatus: status, CostProvenance: prov}, nil
}

// gapSessionRollup is the gap-window-scoped activity of one session, derived
// from gap messages (live SessionEntry rollups cover the whole session and
// must not be summed with window-scoped cache rollups).
type gapSessionRollup struct {
	count int64
	cost  float64
	cost2 costAgg
}

func gapSessionRollups(msgs []stats.MessageEntry, keep map[string]bool) map[string]*gapSessionRollup {
	rollups := make(map[string]*gapSessionRollup)
	for _, entry := range msgs {
		if entry.SessionID == "" || (keep != nil && !keep[entry.SessionID]) {
			continue
		}
		r := rollups[entry.SessionID]
		if r == nil {
			r = &gapSessionRollup{}
			rollups[entry.SessionID] = r
		}
		r.count++
		r.cost += entry.Cost
		r.cost2.add(entry)
	}
	return rollups
}

func sessionLess(a, b stats.SessionEntry, mode stats.SessionSortMode) bool {
	tiebreak := func() bool {
		if !a.TimeCreated.Equal(b.TimeCreated) {
			return a.TimeCreated.After(b.TimeCreated)
		}
		return a.ID < b.ID
	}
	switch mode {
	case stats.SessionSortOldest:
		if !a.TimeCreated.Equal(b.TimeCreated) {
			return a.TimeCreated.Before(b.TimeCreated)
		}
		return a.ID < b.ID
	case stats.SessionSortCost:
		if a.Cost != b.Cost {
			return a.Cost > b.Cost
		}
		return tiebreak()
	case stats.SessionSortMessages:
		if a.MessageCount != b.MessageCount {
			return a.MessageCount > b.MessageCount
		}
		return tiebreak()
	default: // newest
		return tiebreak()
	}
}

// mergeSessions merges the windowed cache session list with the live gap.
// Candidate completeness: merging only ever raises the sort keys of
// gap-active sessions (their cache rows are fetched by id regardless of rank)
// and adds gap-only sessions; other cache sessions keep their relative order
// and can only move down, so the merged first page*pageSize rows are covered
// by (cache top page*pageSize) ∪ (cache rows for gap ids) ∪ gap sessions.
func (s *CachedSource) mergeSessions(ctx context.Context, sp splitWindows, query, cacheQuery stats.SessionQuery, gapSessions []stats.SessionEntry, gap gapData) (stats.SessionList, error) {
	sourceID := s.sourceID()
	gapIDs := make([]string, 0, len(gapSessions))
	keep := make(map[string]bool, len(gapSessions))
	for _, entry := range gapSessions {
		gapIDs = append(gapIDs, entry.ID)
		keep[entry.ID] = true
	}
	cacheTotal, ranked, byID, err := s.store.sessionWindowRows(ctx, sourceID, cacheQuery, query.Page*query.PageSize, gapIDs)
	if err != nil {
		return stats.SessionList{}, err
	}
	rollups := gapSessionRollups(gap.msgs, keep)

	candidates := make(map[string]stats.SessionEntry, len(ranked)+len(gapSessions))
	for _, entry := range ranked {
		candidates[entry.ID] = entry
	}
	for id, entry := range byID {
		if _, ok := candidates[id]; !ok {
			candidates[id] = entry
		}
	}
	for _, gapEntry := range gapSessions {
		rollup := rollups[gapEntry.ID]
		var count int64
		var cost float64
		var gapStatus stats.CostStatus
		var gapProv *stats.CostProvenance
		if rollup != nil {
			count, cost = rollup.count, rollup.cost
			gapStatus, gapProv = rollup.cost2.result()
		}
		if cacheEntry, ok := candidates[gapEntry.ID]; ok {
			cacheEntry.MessageCount += count
			cacheEntry.Cost += cost
			if gapEntry.TimeCreated.Before(cacheEntry.TimeCreated) && !gapEntry.TimeCreated.IsZero() {
				cacheEntry.TimeCreated = gapEntry.TimeCreated
			}
			if gapEntry.TimeUpdated.After(cacheEntry.TimeUpdated) {
				cacheEntry.TimeUpdated = gapEntry.TimeUpdated
			}
			if gapEntry.Title != "" {
				cacheEntry.Title = gapEntry.Title
			}
			if gapEntry.ProjectID != "" {
				cacheEntry.ProjectID = gapEntry.ProjectID
				cacheEntry.ProjectName = gapEntry.ProjectName
			}
			cacheEntry.CostStatus, cacheEntry.CostProvenance = mergeCost(cacheEntry.CostStatus, cacheEntry.CostProvenance, gapStatus, gapProv)
			candidates[gapEntry.ID] = cacheEntry
			continue
		}
		entry := stats.SessionEntry{
			SourceID:     sourceID,
			ID:           gapEntry.ID,
			Title:        gapEntry.Title,
			ProjectID:    gapEntry.ProjectID,
			ProjectName:  gapEntry.ProjectName,
			TimeCreated:  gapEntry.TimeCreated,
			TimeUpdated:  gapEntry.TimeUpdated,
			MessageCount: count,
			Cost:         cost,
			CostStatus:   gapStatus,
		}
		entry.CostProvenance = gapProv
		candidates[gapEntry.ID] = entry
	}

	merged := make([]stats.SessionEntry, 0, len(candidates))
	for _, entry := range candidates {
		merged = append(merged, entry)
	}
	sort.SliceStable(merged, func(i, j int) bool { return sessionLess(merged[i], merged[j], query.Sort) })
	lo := (query.Page - 1) * query.PageSize
	hi := lo + query.PageSize
	if lo > len(merged) {
		lo = len(merged)
	}
	if hi > len(merged) {
		hi = len(merged)
	}
	pageRows := merged[lo:hi]
	if pageRows == nil {
		pageRows = []stats.SessionEntry{}
	}

	total := cacheTotal + int64(len(gapSessions)) - int64(len(byID))
	cachePQ := stats.PeriodQuery{Period: cacheQuery.Period, From: cacheQuery.From, To: cacheQuery.To, FromTime: cacheQuery.FromTime, ToTime: cacheQuery.ToTime}
	cacheStatus, cacheProv, err := s.store.costSummaryForPQ(ctx, sourceID, cachePQ)
	if err != nil {
		return stats.SessionList{}, err
	}
	gapAgg := newBucketAgg()
	for _, entry := range gap.msgs {
		gapAgg.cost2.add(entry)
	}
	gapStatus, gapProv := gapAgg.cost2.result()
	status, prov := mergeCost(cacheStatus, cacheProv, gapStatus, gapProv)
	return stats.SessionList{SourceID: sourceID, Sessions: pageRows, Total: total, Page: query.Page, PageSize: query.PageSize, CostStatus: status, CostProvenance: prov}, nil
}

// mergeProjectDetail merges the project aggregate and its recent-sessions
// page. Recent-session rollups in the cache come from the sessions table
// (finalized whole-session values), so adding the gap part yields complete
// whole-session counts.
func (s *CachedSource) mergeProjectDetail(ctx context.Context, sp splitWindows, projectID string, page, limit int, cacheDetail *stats.ProjectDetail, gap gapData, gapSessions []stats.SessionEntry) (*stats.ProjectDetail, error) {
	sourceID := s.sourceID()
	keep := make(map[string]bool, len(gapSessions))
	gapIDs := make([]string, 0, len(gapSessions))
	for _, entry := range gapSessions {
		keep[entry.ID] = true
		gapIDs = append(gapIDs, entry.ID)
	}
	projectMsgs := make([]stats.MessageEntry, 0)
	for _, entry := range gap.msgs {
		if keep[entry.SessionID] {
			projectMsgs = append(projectMsgs, entry)
		}
	}
	if cacheDetail == nil && len(gapSessions) == 0 {
		return nil, nil
	}

	detail := stats.ProjectDetail{SourceID: sourceID, ProjectID: projectID}
	if cacheDetail != nil {
		detail = *cacheDetail
	} else {
		for _, entry := range gapSessions {
			if entry.ProjectName != "" {
				detail.ProjectName = entry.ProjectName
				break
			}
		}
		if detail.ProjectName == "" {
			detail.ProjectName = projectID
		}
	}

	agg := newBucketAgg()
	for _, entry := range projectMsgs {
		agg.add(entry)
	}
	detail.Messages += agg.messages
	detail.Cost += agg.cost
	addTokens(&detail.Tokens, agg.tokens)
	startMs, endMs := sp.cacheWindowMs()
	gapSessionCount := int64(len(agg.sessions))
	if cacheDetail != nil && detail.Sessions > 0 {
		overlap, err := s.store.distinctSessionOverlap(ctx, sourceID, startMs, endMs, agg.sessionIDs(), "AND COALESCE(project_id, '') = ?", []any{projectID})
		if err != nil {
			return nil, err
		}
		detail.Sessions = detail.Sessions + gapSessionCount - overlap
	} else {
		detail.Sessions += gapSessionCount
	}
	gapStatus, gapProv := agg.cost2.result()
	detail.CostStatus, detail.CostProvenance = mergeCost(detail.CostStatus, detail.CostProvenance, gapStatus, gapProv)

	cacheTotal, ranked, byID, err := s.store.projectSessionRows(ctx, sourceID, projectID, page*limit, gapIDs)
	if err != nil {
		return nil, err
	}
	rollups := gapSessionRollups(projectMsgs, keep)
	candidates := make(map[string]stats.SessionEntry, len(ranked)+len(gapSessions))
	for _, entry := range ranked {
		candidates[entry.ID] = entry
	}
	for id, entry := range byID {
		if _, ok := candidates[id]; !ok {
			candidates[id] = entry
		}
	}
	gapOnly := int64(0)
	for _, gapEntry := range gapSessions {
		rollup := rollups[gapEntry.ID]
		var count int64
		var cost float64
		var rollupStatus stats.CostStatus
		var rollupProv *stats.CostProvenance
		if rollup != nil {
			count, cost = rollup.count, rollup.cost
			rollupStatus, rollupProv = rollup.cost2.result()
		}
		if _, inCache := byID[gapEntry.ID]; !inCache {
			gapOnly++
		}
		if cacheEntry, ok := candidates[gapEntry.ID]; ok {
			cacheEntry.MessageCount += count
			cacheEntry.Cost += cost
			if gapEntry.TimeUpdated.After(cacheEntry.TimeUpdated) {
				cacheEntry.TimeUpdated = gapEntry.TimeUpdated
			}
			if gapEntry.Title != "" {
				cacheEntry.Title = gapEntry.Title
			}
			cacheEntry.CostStatus, cacheEntry.CostProvenance = mergeCost(cacheEntry.CostStatus, cacheEntry.CostProvenance, rollupStatus, rollupProv)
			candidates[gapEntry.ID] = cacheEntry
			continue
		}
		entry := stats.SessionEntry{
			SourceID:     sourceID,
			ID:           gapEntry.ID,
			Title:        gapEntry.Title,
			ProjectID:    gapEntry.ProjectID,
			ProjectName:  gapEntry.ProjectName,
			TimeCreated:  gapEntry.TimeCreated,
			TimeUpdated:  gapEntry.TimeUpdated,
			MessageCount: count,
			Cost:         cost,
			CostStatus:   rollupStatus,
		}
		entry.CostProvenance = rollupProv
		candidates[gapEntry.ID] = entry
	}
	merged := make([]stats.SessionEntry, 0, len(candidates))
	for _, entry := range candidates {
		merged = append(merged, entry)
	}
	sort.SliceStable(merged, func(i, j int) bool {
		if !merged[i].TimeCreated.Equal(merged[j].TimeCreated) {
			return merged[i].TimeCreated.After(merged[j].TimeCreated)
		}
		return merged[i].ID < merged[j].ID
	})
	lo := (page - 1) * limit
	hi := lo + limit
	if lo > len(merged) {
		lo = len(merged)
	}
	if hi > len(merged) {
		hi = len(merged)
	}
	pageRows := merged[lo:hi]
	if pageRows == nil {
		pageRows = []stats.SessionEntry{}
	}
	detail.RecentSessions = pageRows
	detail.TotalSessions = cacheTotal + gapOnly
	return &detail, nil
}

// normalizeMessagePage mirrors Store.Messages page/limit normalization.
func normalizeMessagePage(page, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	return page, limit
}

// normalizeProjectPage mirrors recentProjectSessions normalization.
func normalizeProjectPage(page, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	return page, limit
}
