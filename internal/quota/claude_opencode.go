package quota

import (
	"context"
	"encoding/json"
	"math"
	"sync"
	"time"

	"opencode-dashboard/internal/stats"
	"opencode-dashboard/internal/store"
)

const (
	// claudeNativeTTL is how long a completed scan answers later calls. The
	// dashboard polls quotas about once a minute, and several clients may.
	claudeNativeTTL = 30 * time.Second

	// claudeNativeScanTimeout bounds one scan. It is deliberately longer than
	// the per-request provider timeout: the first scan reads every assistant
	// message, which on a multi-gigabyte database outlasts a request. The scan
	// keeps running after its request gives up and serves the next one.
	claudeNativeScanTimeout = 45 * time.Second

	// claudeNativeOverlap is how far before the previous scan a follow-up scan
	// looks. A streaming response keeps updating a message created earlier, so
	// "created since the last scan" alone would miss its final quota event.
	claudeNativeOverlap = 6 * time.Hour
)

// claudeNativeReader serves the latest Claude Agent quota observation without
// rescanning OpenCode's database on every poll.
//
// The observation lives in message JSON with no index to find it by, so the
// first lookup is a full scan. After that only messages created since the
// previous scan (less an overlap) can hold something newer, and that range is
// indexed. One scan runs at a time; callers wait for it only as long as their
// own context allows and are otherwise answered from the last completed scan,
// so a slow read never turns into a different, older quota source.
type claudeNativeReader struct {
	now  func() time.Time
	scan func(ctx context.Context, path string, sinceMS int64) (*ClaudeSnapshot, error)

	mu        sync.Mutex
	path      string
	snap      *ClaudeSnapshot
	scanned   bool          // a scan of path has completed
	scannedMS int64         // when that scan started
	checked   time.Time     // when the last scan finished, successfully or not
	lastErr   error         // outcome of the last scan
	running   chan struct{} // closed when the in-flight scan finishes
}

func newClaudeNativeReader(now func() time.Time) *claudeNativeReader {
	return &claudeNativeReader{now: now, scan: scanOpenCodeClaudeSnapshot}
}

func (r *claudeNativeReader) latest(ctx context.Context, path string) (*ClaudeSnapshot, error) {
	r.mu.Lock()
	if r.path != path {
		r.path, r.snap, r.scanned, r.scannedMS, r.checked, r.lastErr = path, nil, false, 0, time.Time{}, nil
	}
	if r.scanned && r.now().Sub(r.checked) < claudeNativeTTL {
		snap := r.snap
		r.mu.Unlock()
		return snap, nil
	}
	done := r.running
	if done == nil {
		done = make(chan struct{})
		r.running = done
		var since int64
		if r.scanned {
			since = r.scannedMS - claudeNativeOverlap.Milliseconds()
		}
		go r.run(path, since, done)
	}
	r.mu.Unlock()

	select {
	case <-done:
	case <-ctx.Done():
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.path == path && r.scanned {
		return r.snap, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, r.lastErr
}

func (r *claudeNativeReader) run(path string, sinceMS int64, done chan struct{}) {
	started := r.now()
	ctx, cancel := context.WithTimeout(context.Background(), claudeNativeScanTimeout)
	snap, err := r.scan(ctx, path, sinceMS)
	cancel()

	r.mu.Lock()
	if r.path == path {
		if err == nil {
			// An incremental scan sees only recent messages; an empty result
			// there must not discard an older observation that still stands.
			if snap != nil && (r.snap == nil || snap.SavedAtMS >= r.snap.SavedAtMS) {
				r.snap = snap
			}
			r.scanned, r.scannedMS = true, started.UnixMilli()
		}
		r.lastErr, r.checked = err, r.now()
	}
	r.running = nil
	r.mu.Unlock()
	close(done)
}

type claudeNativeWindow struct {
	Utilization *float64  `json:"utilization"`
	ResetsAt    epochTime `json:"resetsAt"`
}

type claudeNativeRateLimit struct {
	claudeNativeWindow
	RateLimitType  string                        `json:"rateLimitType"`
	UnifiedWindows map[string]claudeNativeWindow `json:"unifiedWindows"`
}

// latestOpenCodeClaudeSnapshot reads the original Claude Code rate-limit event
// saved by OpenCode, independently of the accounting relation (which excludes
// this provider). It never starts a CLI, reads credentials, or writes OpenCode.
func latestOpenCodeClaudeSnapshot(ctx context.Context, path string) (*ClaudeSnapshot, error) {
	return scanOpenCodeClaudeSnapshot(ctx, path, 0)
}

// scanOpenCodeClaudeSnapshot is latestOpenCodeClaudeSnapshot restricted to
// messages created at or after sinceMS (0 scans everything).
func scanOpenCodeClaudeSnapshot(ctx context.Context, path string, sinceMS int64) (*ClaudeSnapshot, error) {
	st, err := store.Connect(ctx, path)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	if !st.IsValidSchema() {
		return nil, store.ErrInvalidSchema
	}
	table, role, provider := "message", `json_extract(data, '$.role')`, `json_extract(data, '$.providerID')`
	// Quota is current provider metadata, so read 2.x even while analytics
	// deliberately falls back to 1.x during an unfinished history import.
	if schema := st.Schema(); schema.HasSessionV2 && schema.HasSessionMessage {
		table, role, provider = "session_message", "type", `json_extract(data, '$.model.providerID')`
	}
	// A streaming response can receive a newer quota event after another
	// session creates its next message. Order by update time, not creation.
	// Status-only events must not hide an earlier measured utilization or
	// turn into a false zero. The caller bounds this read with its timeout.
	// A follow-up scan is bounded by creation time: a plain (and, on 2.x,
	// indexed) column, so older rows are rejected before any JSON is parsed.
	// The full scan carries no bound, leaving it a sequential table read.
	recent, args := "", []any{stats.ClaudeAgentProvider}
	if sinceMS > 0 {
		recent, args = "time_created >= ? AND ", []any{sinceMS, stats.ClaudeAgentProvider}
	}
	rows, err := st.DB().QueryContext(ctx, `SELECT time_updated, json_extract(data, '$.native.rateLimit')
		FROM `+table+` WHERE `+recent+role+` = 'assistant' AND `+provider+` = ?
		AND json_type(data, '$.native.rateLimit') = 'object'
		AND (
			(json_type(data, '$.native.rateLimit.unifiedWindows.five_hour.utilization') IN ('integer', 'real')
			 AND json_extract(data, '$.native.rateLimit.unifiedWindows.five_hour.utilization') >= 0)
			OR (json_type(data, '$.native.rateLimit.unifiedWindows.seven_day.utilization') IN ('integer', 'real')
			 AND json_extract(data, '$.native.rateLimit.unifiedWindows.seven_day.utilization') >= 0)
			OR (json_extract(data, '$.native.rateLimit.rateLimitType') IN ('five_hour', 'seven_day')
			 AND json_type(data, '$.native.rateLimit.utilization') IN ('integer', 'real')
			 AND json_extract(data, '$.native.rateLimit.utilization') >= 0)
		)
		ORDER BY time_updated DESC, time_created DESC LIMIT 1`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var latest *ClaudeSnapshot
	for rows.Next() {
		var observed int64
		var data string
		if err := rows.Scan(&observed, &data); err != nil {
			return nil, err
		}
		snap := parseClaudeNativeSnapshot(data, observed)
		if snap != nil && (latest == nil || snap.SavedAtMS > latest.SavedAtMS) {
			latest = snap
		}
	}
	return latest, rows.Err()
}

func parseClaudeNativeSnapshot(data string, observed int64) *ClaudeSnapshot {
	var event claudeNativeRateLimit
	if observed <= 0 || json.Unmarshal([]byte(data), &event) != nil {
		return nil
	}
	window := func(id string) *ClaudeWindow {
		w, ok := event.UnifiedWindows[id]
		if !ok && event.RateLimitType == id {
			w = event.claudeNativeWindow
		}
		if w.Utilization == nil || math.IsNaN(*w.Utilization) || math.IsInf(*w.Utilization, 0) || *w.Utilization < 0 {
			return nil
		}
		percent := *w.Utilization * 100
		if math.IsInf(percent, 0) {
			return nil
		}
		return &ClaudeWindow{UsedPercentage: percent, ResetsAt: int64(w.ResetsAt)}
	}
	snap := &ClaudeSnapshot{SavedAtMS: observed, FiveHour: window("five_hour"), SevenDay: window("seven_day")}
	if snap.FiveHour == nil && snap.SevenDay == nil {
		return nil
	}
	return snap
}
