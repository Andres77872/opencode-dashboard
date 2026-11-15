package quota

import (
	"context"
	"encoding/json"
	"math"

	"opencode-dashboard/internal/stats"
	"opencode-dashboard/internal/store"
)

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
	rows, err := st.DB().QueryContext(ctx, `SELECT time_updated, json_extract(data, '$.native.rateLimit')
		FROM `+table+` WHERE `+role+` = 'assistant' AND `+provider+` = ?
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
		ORDER BY time_updated DESC, time_created DESC LIMIT 1`, stats.ClaudeAgentProvider)
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
