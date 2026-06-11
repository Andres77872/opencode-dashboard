package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

type closeable interface {
	Close() error
}

// consolidationStaleness is how old the last successful consolidation may be
// before a read triggers a new background one: the 6h finality delay plus one
// hour, so there is at least one full newly finalizable clock hour. Between
// consolidations reads never touch the cache write path — the recent window
// is read live and merged.
const consolidationStaleness = DefaultSyncSafetyDelay + time.Hour

// fillRetryBackoff paces staleness re-checks and retries after a failed
// consolidation, so a persistently failing source is retried at most once a
// minute instead of on every read.
const fillRetryBackoff = time.Minute

// fillTimeout caps a background consolidation; it is detached from request
// contexts so a slow consolidation finishes instead of dying with the first
// request.
const fillTimeout = 10 * time.Minute

// CachedSource implements source.Source by serving aggregate and list endpoints
// as a merge of the dashboard-owned SQLite cache (finalized hours, strictly
// before the hour-aligned cutoff) and a live read of the recent gap
// [cutoff, now). Detail/config endpoints delegate to the original live source
// so raw conversation content is never persisted.
//
// Reads never wait on consolidation: when the last successful sync is older
// than consolidationStaleness, a single-flight background consolidation is
// spawned and the read proceeds immediately with the cache+gap merge.
type CachedSource struct {
	store *Store
	live  source.Source
	info  source.SourceInfo

	fillCtx    context.Context
	fillCancel context.CancelFunc

	freshMu       sync.Mutex
	fillDone      chan struct{} // non-nil while a fill is in flight
	nextFillCheck time.Time
}

func WrapSource(store *Store, live source.Source) *CachedSource {
	info := source.SourceInfo{}
	if live != nil {
		info = live.Info(context.Background())
	}
	fillCtx, fillCancel := context.WithCancel(context.Background())
	return &CachedSource{store: store, live: live, info: info, fillCtx: fillCtx, fillCancel: fillCancel}
}

func (s *CachedSource) Info(ctx context.Context) source.SourceInfo {
	if s == nil {
		return source.SourceInfo{}
	}
	info := s.info
	if s.live != nil {
		info = s.live.Info(ctx)
	}
	if s.store != nil {
		if status, ok, err := s.store.SourceStatus(ctx, string(info.ID)); err == nil && ok {
			if status.Status == "ready" {
				info.Diagnostics.Status = "ok"
			} else if status.Status != "" {
				info.Diagnostics.Status = status.Status
				if status.Reason != "" {
					info.Diagnostics.Reason = status.Reason
				}
			}
		}
	}
	return info
}

// ensureFresh triggers consolidation when it is due — it NEVER blocks the
// read. If the source's last successful sync is older than
// consolidationStaleness, a single-flight background consolidation is spawned
// (detached from the request context, so per-source request timeouts cannot
// kill it). The read proceeds immediately: the recent window is read live and
// merged, so nothing depends on the consolidation finishing. A consolidation
// never fails the read — errors roll back and surface via the cache status
// API.
func (s *CachedSource) ensureFresh(ctx context.Context) {
	if s == nil || s.store == nil || s.live == nil {
		return
	}
	s.freshMu.Lock()
	defer s.freshMu.Unlock()
	if s.fillDone != nil {
		return // consolidation already in flight
	}
	now := time.Now()
	if now.Before(s.nextFillCheck) {
		return
	}
	s.nextFillCheck = now.Add(fillRetryBackoff)
	lastSynced, err := s.store.LastSyncedMS(ctx, s.sourceID())
	if err == nil && lastSynced > 0 && now.Sub(time.UnixMilli(lastSynced)) < consolidationStaleness {
		return
	}
	done := make(chan struct{})
	s.fillDone = done
	go s.runFill(done)
}

func (s *CachedSource) runFill(done chan struct{}) {
	ctx, cancel := context.WithTimeout(s.fillCtx, fillTimeout)
	defer cancel()
	_, _ = s.store.SyncSourceWithOptions(ctx, s.live, SyncOptions{
		Mode:          SyncModeIncremental,
		Cutoff:        DefaultSafeCutoff(time.Now().UTC()),
		ReadTriggered: true,
	})
	s.freshMu.Lock()
	s.fillDone = nil
	close(done)
	s.freshMu.Unlock()
}

func (s *CachedSource) sourceID() string {
	if s == nil {
		return ""
	}
	if s.info.ID != "" {
		return string(s.info.ID)
	}
	if s.live != nil {
		return string(s.live.Info(context.Background()).ID)
	}
	return ""
}

// split resolves the request window and divides it at the source's finality
// cutoff: the finalized part is served from the cache, the recent gap live.
func (s *CachedSource) split(ctx context.Context, pq stats.PeriodQuery) (splitWindows, error) {
	cutoff, err := s.store.LastSafeCutoff(ctx, s.sourceID())
	if err != nil {
		return splitWindows{}, err
	}
	return splitPeriod(pq, cutoff, time.Now().UTC())
}

// degradeGap handles a failed live read of the recent gap: the failure is
// logged, surfaced through the fill-state status API, and the read proceeds
// with the consolidated cache data only — it never fails the request.
func (s *CachedSource) degradeGap(err error) gapData {
	s.store.logger.Warn("cache gap read failed; serving consolidated data only", "source", s.sourceID(), "error", err)
	s.store.recordFillState(s.sourceID(), fmt.Errorf("recent-window live read failed: %w", err))
	return gapData{}
}

func (s *CachedSource) Overview(ctx context.Context, pq stats.PeriodQuery) (stats.OverviewStats, error) {
	s.ensureFresh(ctx)
	sp, err := s.split(ctx, pq)
	if err != nil {
		return stats.OverviewStats{}, err
	}
	if !sp.hasCache {
		return s.live.Overview(ctx, sp.livePQ)
	}
	c, err := s.store.Overview(ctx, s.sourceID(), sp.cachePQ)
	if err != nil || !sp.hasLive {
		return c, err
	}
	gap, err := fetchGapMessages(ctx, s.live, sp.livePQ)
	if err != nil {
		gap = s.degradeGap(err)
	}
	return s.mergeOverview(ctx, sp, c, gap)
}

func (s *CachedSource) Daily(ctx context.Context, pq stats.PeriodQuery, granularity ...stats.Granularity) (stats.DailyStats, error) {
	s.ensureFresh(ctx)
	// Resolve granularity once so the cache and live halves agree (mirrors
	// Store.Daily's auto-hour rule).
	gran := stats.GranularityDay
	if len(granularity) > 0 && granularity[0] != "" {
		gran = granularity[0]
	} else if pq.Period == "1d" || isHourPeriod(pq.Period) {
		gran = stats.GranularityHour
	}
	sp, err := s.split(ctx, pq)
	if err != nil {
		return stats.DailyStats{}, err
	}
	if !sp.hasCache {
		return s.live.Daily(ctx, sp.livePQ, gran)
	}
	c, err := s.store.Daily(ctx, s.sourceID(), sp.cachePQ, gran)
	if err != nil || !sp.hasLive {
		return c, err
	}
	gap, err := fetchGapMessages(ctx, s.live, sp.livePQ)
	if err != nil {
		gap = s.degradeGap(err)
	}
	return s.mergeDaily(ctx, sp, gran, c, gap)
}

func (s *CachedSource) DailyDimension(ctx context.Context, dimension string, pq stats.PeriodQuery) (stats.DailyDimensionStats, error) {
	s.ensureFresh(ctx)
	sp, err := s.split(ctx, pq)
	if err != nil {
		return stats.DailyDimensionStats{}, err
	}
	if !sp.hasCache {
		return s.live.DailyDimension(ctx, dimension, sp.livePQ)
	}
	c, err := s.store.DailyDimension(ctx, s.sourceID(), dimension, sp.cachePQ)
	if err != nil || !sp.hasLive {
		return c, err
	}
	label := periodLabel(pq)
	if dimension == "tool" {
		gapDim, err := s.live.DailyDimension(ctx, dimension, sp.livePQ)
		if err != nil {
			s.degradeGap(err)
			gapDim = stats.DailyDimensionStats{}
		}
		return mergeDailyDimension(s.sourceID(), dimension, label, c, gapDim.Days, gapDim.CostStatus, gapDim.CostProvenance), nil
	}
	gap, err := fetchGapMessages(ctx, s.live, sp.livePQ)
	if err != nil {
		gap = s.degradeGap(err)
	}
	var projectOf map[string]stats.SessionEntry
	if dimension == "project" && len(gap.msgs) > 0 {
		gapSessions, err := fetchGapSessions(ctx, s.live, stats.SessionQuery{FromTime: sp.livePQ.FromTime, ToTime: sp.livePQ.ToTime})
		if err != nil {
			gap = s.degradeGap(err)
		} else {
			projectOf = make(map[string]stats.SessionEntry, len(gapSessions))
			for _, sess := range gapSessions {
				projectOf[sess.ID] = sess
			}
		}
	}
	rows := gapDimensionRows(s.sourceID(), dimension, gap.msgs, projectOf)
	gapAgg := newBucketAgg()
	for _, entry := range gap.msgs {
		gapAgg.cost2.add(entry)
	}
	gapStatus, gapProv := gapAgg.cost2.result()
	return mergeDailyDimension(s.sourceID(), dimension, label, c, rows, gapStatus, gapProv), nil
}

func (s *CachedSource) Models(ctx context.Context, pq stats.PeriodQuery) (stats.ModelStats, error) {
	s.ensureFresh(ctx)
	sp, err := s.split(ctx, pq)
	if err != nil {
		return stats.ModelStats{}, err
	}
	if !sp.hasCache {
		return s.live.Models(ctx, sp.livePQ)
	}
	c, err := s.store.Models(ctx, s.sourceID(), sp.cachePQ)
	if err != nil || !sp.hasLive {
		return c, err
	}
	gap, err := fetchGapMessages(ctx, s.live, sp.livePQ)
	if err != nil {
		gap = s.degradeGap(err)
	}
	return s.mergeModels(ctx, sp, c, gap)
}

func (s *CachedSource) Tools(ctx context.Context, pq stats.PeriodQuery) (stats.ToolStats, error) {
	s.ensureFresh(ctx)
	sp, err := s.split(ctx, pq)
	if err != nil {
		return stats.ToolStats{}, err
	}
	if !sp.hasCache {
		return s.live.Tools(ctx, sp.livePQ)
	}
	c, err := s.store.Tools(ctx, s.sourceID(), sp.cachePQ)
	if err != nil || !sp.hasLive {
		return c, err
	}
	gapTools, err := s.live.Tools(ctx, sp.livePQ)
	if err != nil {
		s.degradeGap(err)
		gapTools = stats.ToolStats{}
	}
	return mergeTools(s.sourceID(), c, gapTools), nil
}

func (s *CachedSource) Projects(ctx context.Context, pq stats.PeriodQuery) (stats.ProjectStats, error) {
	s.ensureFresh(ctx)
	sp, err := s.split(ctx, pq)
	if err != nil {
		return stats.ProjectStats{}, err
	}
	if !sp.hasCache {
		return s.live.Projects(ctx, sp.livePQ)
	}
	c, err := s.store.Projects(ctx, s.sourceID(), sp.cachePQ)
	if err != nil || !sp.hasLive {
		return c, err
	}
	gap, err := fetchGapMessages(ctx, s.live, sp.livePQ)
	var gapSessions []stats.SessionEntry
	if err == nil && len(gap.msgs) > 0 {
		gapSessions, err = fetchGapSessions(ctx, s.live, stats.SessionQuery{FromTime: sp.livePQ.FromTime, ToTime: sp.livePQ.ToTime})
	}
	if err != nil {
		gap = s.degradeGap(err)
		gapSessions = nil
	}
	return s.mergeProjects(ctx, sp, c, gap, gapSessions)
}

func (s *CachedSource) ProjectByID(ctx context.Context, id string, pq stats.PeriodQuery, page, limit int) (*stats.ProjectDetail, error) {
	s.ensureFresh(ctx)
	page, limit = normalizeProjectPage(page, limit)
	sp, err := s.split(ctx, pq)
	if err != nil {
		return nil, err
	}
	if !sp.hasCache {
		return s.live.ProjectByID(ctx, id, sp.livePQ, page, limit)
	}
	if !sp.hasLive {
		return s.store.ProjectByID(ctx, s.sourceID(), id, sp.cachePQ, page, limit)
	}
	// Aggregate part only; the merged recent-sessions page is rebuilt below.
	cacheDetail, err := s.store.ProjectByID(ctx, s.sourceID(), id, sp.cachePQ, 1, 1)
	if err != nil {
		return nil, err
	}
	gapSessions, err := fetchGapSessions(ctx, s.live, stats.SessionQuery{ProjectID: id, FromTime: sp.livePQ.FromTime, ToTime: sp.livePQ.ToTime})
	var gap gapData
	if err == nil {
		gap, err = fetchGapMessages(ctx, s.live, sp.livePQ)
	}
	if err != nil {
		s.degradeGap(err)
		return s.store.ProjectByID(ctx, s.sourceID(), id, sp.cachePQ, page, limit)
	}
	return s.mergeProjectDetail(ctx, sp, id, page, limit, cacheDetail, gap, gapSessions)
}

func (s *CachedSource) Sessions(ctx context.Context, query stats.SessionQuery) (stats.SessionList, error) {
	s.ensureFresh(ctx)
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 {
		query.PageSize = 20
	}
	if query.PageSize > 100 {
		query.PageSize = 100
	}
	if query.Sort == "" {
		query.Sort = stats.SessionSortNewest
	}
	pq := stats.PeriodQuery{Period: query.Period, From: query.From, To: query.To, FromTime: query.FromTime, ToTime: query.ToTime}
	sp, err := s.split(ctx, pq)
	if err != nil {
		return stats.SessionList{}, err
	}
	if !sp.hasCache {
		liveQuery := query
		liveQuery.FromTime = sp.livePQ.FromTime
		liveQuery.ToTime = sp.livePQ.ToTime
		return s.live.Sessions(ctx, liveQuery)
	}
	cacheQuery := query
	cacheQuery.ToTime = sp.cachePQ.ToTime
	if !sp.hasLive {
		return s.store.Sessions(ctx, s.sourceID(), cacheQuery)
	}
	gapQuery := query
	gapQuery.FromTime = sp.livePQ.FromTime
	gapQuery.ToTime = sp.livePQ.ToTime
	gapSessions, err := fetchGapSessions(ctx, s.live, gapQuery)
	var gap gapData
	if err == nil {
		gap, err = fetchGapMessages(ctx, s.live, sp.livePQ)
	}
	if err != nil {
		s.degradeGap(err)
		return s.store.Sessions(ctx, s.sourceID(), cacheQuery)
	}
	return s.mergeSessions(ctx, sp, query, cacheQuery, gapSessions, gap)
}

func (s *CachedSource) SessionByID(ctx context.Context, id string) (*stats.SessionDetail, error) {
	if s.live != nil {
		if detail, err := s.live.SessionByID(ctx, id); err != nil || detail != nil {
			return detail, err
		}
	}
	return s.store.SessionByID(ctx, s.sourceID(), id)
}

func (s *CachedSource) Messages(ctx context.Context, pq stats.PeriodQuery, page, limit int, sort stats.MessageSort) (stats.MessageList, error) {
	s.ensureFresh(ctx)
	page, limit = normalizeMessagePage(page, limit)
	sp, err := s.split(ctx, pq)
	if err != nil {
		return stats.MessageList{}, err
	}
	if !sp.hasCache {
		return s.live.Messages(ctx, sp.livePQ, page, limit, sort)
	}
	if !sp.hasLive {
		return s.store.Messages(ctx, s.sourceID(), sp.cachePQ, page, limit, sort)
	}
	gap, err := fetchGapMessages(ctx, s.live, sp.livePQ)
	if err != nil {
		s.degradeGap(err)
		return s.store.Messages(ctx, s.sourceID(), sp.cachePQ, page, limit, sort)
	}
	return s.mergeMessages(ctx, sp, page, limit, sort, gap)
}

func (s *CachedSource) MessageByID(ctx context.Context, id string) (*stats.MessageDetail, error) {
	if s.live != nil {
		if detail, err := s.live.MessageByID(ctx, id); err != nil || detail != nil {
			return detail, err
		}
	}
	entry, err := s.store.MessageByID(ctx, s.sourceID(), id)
	if err != nil || entry == nil {
		return nil, err
	}
	return &stats.MessageDetail{
		MessageEntry: *entry,
		Content: stats.MessageContent{
			TextParts:      []stats.MessagePart{},
			ReasoningParts: []stats.MessagePart{},
			ToolParts:      []stats.ToolPart{},
		},
	}, nil
}

func (s *CachedSource) Config(ctx context.Context) (stats.ConfigView, error) {
	return s.live.Config(ctx)
}

func (s *CachedSource) Close() error {
	if s == nil {
		return nil
	}
	// Cancel and reap any in-flight background fill before closing the store,
	// so the fill goroutine never races a closing database.
	if s.fillCancel != nil {
		s.fillCancel()
	}
	s.freshMu.Lock()
	done := s.fillDone
	s.freshMu.Unlock()
	if done != nil {
		<-done
	}
	var err error
	if s.live != nil {
		if closer, ok := s.live.(closeable); ok {
			err = closer.Close()
		}
	}
	if s.store != nil {
		if closeErr := s.store.Close(); err == nil {
			err = closeErr
		}
	}
	return err
}
