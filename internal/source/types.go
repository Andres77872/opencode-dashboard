package source

import (
	"context"

	"opencode-dashboard/internal/stats"
)

type SourceID string

const (
	SourceOpenCode   SourceID = "opencode"
	SourceClaudeCode SourceID = "claude_code"
	SourceCodex      SourceID = "codex"
	SourceKimiCode   SourceID = "kimi_code"
	SourceQwenCode   SourceID = "qwen_code"
)

type Source interface {
	Info(context.Context) SourceInfo

	Overview(context.Context, stats.PeriodQuery) (stats.OverviewStats, error)
	Daily(context.Context, stats.PeriodQuery, ...stats.Granularity) (stats.DailyStats, error)
	DailyDimension(context.Context, string, stats.PeriodQuery, ...stats.Granularity) (stats.DailyDimensionStats, error)
	Models(context.Context, stats.PeriodQuery) (stats.ModelStats, error)
	Tools(context.Context, stats.PeriodQuery) (stats.ToolStats, error)
	Projects(context.Context, stats.PeriodQuery) (stats.ProjectStats, error)
	ProjectByID(context.Context, string, stats.PeriodQuery, int, int) (*stats.ProjectDetail, error)
	Sessions(context.Context, stats.SessionQuery) (stats.SessionList, error)
	SessionByID(context.Context, string) (*stats.SessionDetail, error)
	Messages(context.Context, stats.PeriodQuery, int, int, stats.MessageSort) (stats.MessageList, error)
	MessageByID(context.Context, string) (*stats.MessageDetail, error)
	Config(context.Context) (stats.ConfigView, error)
}

// ConsolidationSource is an optional fast path for sources that already build
// an in-memory snapshot. It lets the cache consume one stable snapshot instead
// of paginating the source and resolving every message detail individually.
//
// ConsolidationData deliberately carries only aggregate-safe metadata. Raw
// message text, reasoning, tool input, and tool output must never cross this
// boundary because the dashboard cache does not persist conversation content.
type ConsolidationSource interface {
	ConsolidationData(context.Context, stats.PeriodQuery) (ConsolidationData, error)
}

type ConsolidationData struct {
	Sessions       []stats.SessionEntry
	Messages       []ConsolidationMessage
	CostStatus     stats.CostStatus
	CostProvenance *stats.CostProvenance
}

type ConsolidationMessage struct {
	Entry stats.MessageEntry
	Tools []ConsolidationTool
	// ModelTokens optionally carries the source's canonical token accounting
	// for model analytics when it differs from the message row's display and
	// overview tokens. OpenCode uses this for messages with step-finish parts:
	// every step is additive, while message.data.tokens only contains the last
	// upstream update. Nil means model analytics should use Entry.Tokens.
	ModelTokens *stats.TokenStats
}

type ConsolidationTool struct {
	Name   string
	Status string
}

type SourceInfo struct {
	ID           SourceID          `json:"id"`
	Label        string            `json:"label"`
	Kind         string            `json:"kind"`
	Available    bool              `json:"available"`
	Default      bool              `json:"default"`
	Selected     bool              `json:"selected,omitempty"`
	Path         string            `json:"path,omitempty"`
	PathSource   string            `json:"path_source,omitempty"`
	ReadOnly     bool              `json:"read_only"`
	LocalOnly    bool              `json:"local_only"`
	Capabilities []string          `json:"capabilities"`
	Warnings     []string          `json:"warnings,omitempty"`
	Diagnostics  SourceDiagnostics `json:"diagnostics,omitempty"`
	CostPolicy   CostPolicy        `json:"cost_policy,omitempty"`
	Privacy      PrivacyInfo       `json:"privacy,omitempty"`
}

type SourceDiagnostics struct {
	Status            string `json:"status,omitempty"`
	Reason            string `json:"reason,omitempty"`
	ScannedFiles      int64  `json:"scanned_files,omitempty"`
	MalformedLines    int64  `json:"malformed_lines,omitempty"`
	UnsupportedEvents int64  `json:"unsupported_events,omitempty"`
}

type CostPolicy struct {
	Status            string `json:"status,omitempty"`
	Currency          string `json:"currency,omitempty"`
	PricingSnapshotID string `json:"pricing_snapshot_id,omitempty"`
	Note              string `json:"note,omitempty"`
}

type PrivacyInfo struct {
	PlaintextTranscripts bool     `json:"plaintext_transcripts,omitempty"`
	ReadOnly             bool     `json:"read_only,omitempty"`
	LocalOnly            bool     `json:"local_only,omitempty"`
	Redaction            bool     `json:"redaction,omitempty"`
	Warnings             []string `json:"warnings,omitempty"`
}

type SourceListResponse struct {
	DefaultSourceID SourceID     `json:"default_source_id"`
	StartupSourceID SourceID     `json:"startup_source_id,omitempty"`
	Sources         []SourceInfo `json:"sources"`
}
