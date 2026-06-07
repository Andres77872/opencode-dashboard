package stats

import (
	"strings"
	"time"
)

type CacheStats struct {
	Read  int64 `json:"read"`
	Write int64 `json:"write"`
}

type CostStatus string

const (
	CostReported               CostStatus = "reported"
	CostComputed               CostStatus = "computed"
	CostApproximate            CostStatus = "approximate"
	CostEstimatedAPIEquivalent CostStatus = "estimated_api_equivalent"
	CostMixed                  CostStatus = "mixed"
	CostMissing                CostStatus = "missing"
)

type CostProvenance struct {
	Status            CostStatus `json:"status"`
	Currency          string     `json:"currency,omitempty"`
	PricingSnapshotID string     `json:"pricing_snapshot_id,omitempty"`
	PricingSource     string     `json:"pricing_source,omitempty"`
	MissingCount      int64      `json:"missing_count,omitempty"`
	ComputedCount     int64      `json:"computed_count,omitempty"`
	ReportedCount     int64      `json:"reported_count,omitempty"`
	Note              string     `json:"note,omitempty"`
}

type TruncationInfo struct {
	Truncated     bool  `json:"truncated,omitempty"`
	OriginalBytes int64 `json:"original_bytes,omitempty"`
	DisplayBytes  int64 `json:"display_bytes,omitempty"`
}

// NOTE: TokenStats uses nested cache: {read, write} for daily/model aggregate tokens.
type TokenStats struct {
	Input     int64      `json:"input"`
	Output    int64      `json:"output"`
	Reasoning int64      `json:"reasoning"`
	Cache     CacheStats `json:"cache"`
}

type OverviewStats struct {
	SourceID       string          `json:"source_id,omitempty"`
	Sessions       int64           `json:"sessions"`
	Messages       int64           `json:"messages"`
	Cost           float64         `json:"cost"`
	Tokens         TokenStats      `json:"tokens"`
	CostPerDay     float64         `json:"cost_per_day"`
	Days           int             `json:"days"`
	CostStatus     CostStatus      `json:"cost_status,omitempty"`
	CostProvenance *CostProvenance `json:"cost_provenance,omitempty"`
}

type DayStats struct {
	SourceID       string          `json:"source_id,omitempty"`
	Date           string          `json:"date"`
	Sessions       int64           `json:"sessions"`
	Messages       int64           `json:"messages"`
	Cost           float64         `json:"cost"`
	Tokens         TokenStats      `json:"tokens"`
	CostStatus     CostStatus      `json:"cost_status,omitempty"`
	CostProvenance *CostProvenance `json:"cost_provenance,omitempty"`
}

type Granularity string

const (
	GranularityDay  Granularity = "day"
	GranularityHour Granularity = "hour"
)

type DailyStats struct {
	SourceID       string          `json:"source_id,omitempty"`
	Days           []DayStats      `json:"days"`
	Granularity    Granularity     `json:"granularity"`
	CostStatus     CostStatus      `json:"cost_status,omitempty"`
	CostProvenance *CostProvenance `json:"cost_provenance,omitempty"`
}

// DimensionDayStats represents a single day's data for a specific dimension value.
type DimensionDayStats struct {
	SourceID       string          `json:"source_id,omitempty"`
	Date           string          `json:"date"`
	Dimension      string          `json:"dimension_key"`
	Sessions       int64           `json:"sessions"`
	Messages       int64           `json:"messages"`
	Cost           float64         `json:"cost"`
	Tokens         TokenStats      `json:"tokens"`
	CostStatus     CostStatus      `json:"cost_status,omitempty"`
	CostProvenance *CostProvenance `json:"cost_provenance,omitempty"`
}

// DailyDimensionStats is the response type for the dimension-grouped daily endpoint.
type DailyDimensionStats struct {
	SourceID       string              `json:"source_id,omitempty"`
	Days           []DimensionDayStats `json:"days"`
	Dimension      string              `json:"dimension"`
	Period         string              `json:"period"`
	CostStatus     CostStatus          `json:"cost_status,omitempty"`
	CostProvenance *CostProvenance     `json:"cost_provenance,omitempty"`
}

// NOTE: AvgTokenStats uses flat cache_read, cache_write for per-unit averages.
type AvgTokenStats struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	Reasoning  float64 `json:"reasoning"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
}

type ModelEntry struct {
	SourceID       string          `json:"source_id,omitempty"`
	ModelID        string          `json:"model_id"`
	ProviderID     string          `json:"provider_id"`
	Sessions       int64           `json:"sessions"`
	Messages       int64           `json:"messages"`
	Cost           float64         `json:"cost"`
	Tokens         TokenStats      `json:"tokens"`
	CostStatus     CostStatus      `json:"cost_status,omitempty"`
	CostProvenance *CostProvenance `json:"cost_provenance,omitempty"`

	// Added: nullable pointers with omitempty for backward-compatible API expansion
	AvgTokensPerMessage *AvgTokenStats `json:"avg_tokens_per_message,omitempty"`
	AvgTokensPerSession *AvgTokenStats `json:"avg_tokens_per_session,omitempty"`
}

type ModelStats struct {
	SourceID       string          `json:"source_id,omitempty"`
	Models         []ModelEntry    `json:"models"`
	CostStatus     CostStatus      `json:"cost_status,omitempty"`
	CostProvenance *CostProvenance `json:"cost_provenance,omitempty"`
}

type ToolEntry struct {
	SourceID    string `json:"source_id,omitempty"`
	Name        string `json:"name"`
	Invocations int64  `json:"invocations"`
	Successes   int64  `json:"successes"`
	Failures    int64  `json:"failures"`
	Sessions    int64  `json:"sessions"`
}

type ToolStats struct {
	SourceID string      `json:"source_id,omitempty"`
	Tools    []ToolEntry `json:"tools"`
}

type ProjectEntry struct {
	SourceID       string          `json:"source_id,omitempty"`
	ProjectID      string          `json:"project_id"`
	ProjectName    string          `json:"project_name"`
	Sessions       int64           `json:"sessions"`
	Messages       int64           `json:"messages"`
	Cost           float64         `json:"cost"`
	Tokens         TokenStats      `json:"tokens"`
	CostStatus     CostStatus      `json:"cost_status,omitempty"`
	CostProvenance *CostProvenance `json:"cost_provenance,omitempty"`
}

type ProjectStats struct {
	SourceID       string          `json:"source_id,omitempty"`
	Projects       []ProjectEntry  `json:"projects"`
	CostStatus     CostStatus      `json:"cost_status,omitempty"`
	CostProvenance *CostProvenance `json:"cost_provenance,omitempty"`
}

// ProjectDetail is the response type for the project drilldown endpoint.
type ProjectDetail struct {
	SourceID       string          `json:"source_id,omitempty"`
	ProjectID      string          `json:"project_id"`
	ProjectName    string          `json:"project_name"`
	Worktree       string          `json:"worktree,omitempty"`
	Sessions       int64           `json:"sessions"`
	Messages       int64           `json:"messages"`
	Cost           float64         `json:"cost"`
	Tokens         TokenStats      `json:"tokens"`
	RecentSessions []SessionEntry  `json:"recent_sessions,omitempty"`
	TotalSessions  int64           `json:"total_sessions"`
	CostStatus     CostStatus      `json:"cost_status,omitempty"`
	CostProvenance *CostProvenance `json:"cost_provenance,omitempty"`
}

type SessionEntry struct {
	SourceID       string          `json:"source_id,omitempty"`
	ID             string          `json:"id"`
	Title          string          `json:"title"`
	ProjectID      string          `json:"project_id"`
	ProjectName    string          `json:"project_name"`
	TimeCreated    time.Time       `json:"time_created"`
	TimeUpdated    time.Time       `json:"time_updated"`
	MessageCount   int64           `json:"message_count"`
	Cost           float64         `json:"cost"`
	CostStatus     CostStatus      `json:"cost_status,omitempty"`
	CostProvenance *CostProvenance `json:"cost_provenance,omitempty"`
}

type SessionList struct {
	SourceID       string          `json:"source_id,omitempty"`
	Sessions       []SessionEntry  `json:"sessions"`
	Total          int64           `json:"total"`
	Page           int             `json:"page"`
	PageSize       int             `json:"page_size"`
	CostStatus     CostStatus      `json:"cost_status,omitempty"`
	CostProvenance *CostProvenance `json:"cost_provenance,omitempty"`
}

type SessionSortMode string

const (
	SessionSortNewest   SessionSortMode = "newest"
	SessionSortOldest   SessionSortMode = "oldest"
	SessionSortCost     SessionSortMode = "cost"
	SessionSortMessages SessionSortMode = "messages"
)

type SessionQuery struct {
	Page      int
	PageSize  int
	Filter    string
	ProjectID string // exact project ID filter, empty = no filter
	Sort      SessionSortMode
	Period    string // "1d", "7d", "30d", "1y", "all" — filters by message activity time
	From      string // ISO 8601 date "2006-01-02" for custom range
	To        string // ISO 8601 date "2006-01-02" for custom range (optional)
}

// PeriodQuery carries either a preset period string OR explicit from/to dates.
// Exactly one mode should be active:
//   - Period != ""  → preset mode (from/to ignored)
//   - From != ""    → explicit range mode (Period ignored)
type PeriodQuery struct {
	Period string // preset: "1h", "6h", "12h", "24h", "72h", "1d", "7d", "14d", "30d", "1y", "all"
	From   string // ISO 8601 date "2006-01-02". When From is set, Period is ignored.
	To     string // ISO 8601 date "2006-01-02". Optional — empty = now in server timezone.
}

type MessageSortField string

const (
	MessageSortTime   MessageSortField = "time"
	MessageSortCost   MessageSortField = "cost"
	MessageSortTokens MessageSortField = "tokens"
	MessageSortModel  MessageSortField = "model"
	MessageSortRole   MessageSortField = "role"
)

type MessageSortDirection string

const (
	MessageSortAsc  MessageSortDirection = "asc"
	MessageSortDesc MessageSortDirection = "desc"
)

type MessageSort struct {
	Field     MessageSortField
	Direction MessageSortDirection
}

func (ms MessageSort) OrderByClause() string {
	dir := "DESC"
	if ms.Direction == MessageSortAsc {
		dir = "ASC"
	}

	switch ms.Field {
	case MessageSortCost:
		return "cost " + dir
	case MessageSortTokens:
		return "(COALESCE(JSON_EXTRACT(m.data, '$.tokens.input'), 0) + COALESCE(JSON_EXTRACT(m.data, '$.tokens.output'), 0) + COALESCE(JSON_EXTRACT(m.data, '$.tokens.reasoning'), 0) + COALESCE(JSON_EXTRACT(m.data, '$.tokens.cache.read'), 0) + COALESCE(JSON_EXTRACT(m.data, '$.tokens.cache.write'), 0)) " + dir
	case MessageSortModel:
		return "model_id " + dir
	case MessageSortRole:
		return "role " + dir
	default:
		return "m.time_created " + dir
	}
}

func ParseMessageSort(s string) MessageSort {
	if s == "" {
		return DefaultMessageSort()
	}

	parts := strings.SplitN(s, ":", 2)
	field := parseMessageSortField(parts[0])

	if len(parts) == 2 {
		switch parts[1] {
		case "asc":
			return MessageSort{Field: field, Direction: MessageSortAsc}
		case "desc":
			return MessageSort{Field: field, Direction: MessageSortDesc}
		}
	}

	return MessageSort{Field: field, Direction: defaultMessageSortDirection(field)}
}

func DefaultMessageSort() MessageSort {
	return MessageSort{Field: MessageSortTime, Direction: MessageSortDesc}
}

func defaultMessageSortDirection(f MessageSortField) MessageSortDirection {
	switch f {
	case MessageSortModel, MessageSortRole:
		return MessageSortAsc
	default:
		return MessageSortDesc
	}
}

func parseMessageSortField(s string) MessageSortField {
	switch s {
	case "cost":
		return MessageSortCost
	case "tokens":
		return MessageSortTokens
	case "model":
		return MessageSortModel
	case "role":
		return MessageSortRole
	case "time":
		return MessageSortTime
	default:
		return MessageSortTime
	}
}

type SessionMessage struct {
	SourceID       string          `json:"source_id,omitempty"`
	ID             string          `json:"id"`
	Role           string          `json:"role"`
	TimeCreated    time.Time       `json:"time_created"`
	Cost           float64         `json:"cost,omitempty"`
	Tokens         *TokenStats     `json:"tokens,omitempty"`
	ModelID        string          `json:"model_id,omitempty"`
	ProviderID     string          `json:"provider_id,omitempty"`
	Agent          string          `json:"agent,omitempty"`
	CostStatus     CostStatus      `json:"cost_status,omitempty"`
	CostProvenance *CostProvenance `json:"cost_provenance,omitempty"`
}

type SessionDetail struct {
	SourceID       string           `json:"source_id,omitempty"`
	ID             string           `json:"id"`
	Title          string           `json:"title"`
	ProjectID      string           `json:"project_id"`
	ProjectName    string           `json:"project_name"`
	Directory      string           `json:"directory"`
	TimeCreated    time.Time        `json:"time_created"`
	TimeUpdated    time.Time        `json:"time_updated"`
	Messages       []SessionMessage `json:"messages"`
	TotalCost      float64          `json:"total_cost"`
	TotalTokens    TokenStats       `json:"total_tokens"`
	MessageCount   int64            `json:"message_count"`
	CostStatus     CostStatus       `json:"cost_status,omitempty"`
	CostProvenance *CostProvenance  `json:"cost_provenance,omitempty"`
}

type ConfigView struct {
	SourceID string         `json:"source_id,omitempty"`
	Path     string         `json:"path"`
	Exists   bool           `json:"exists"`
	Content  map[string]any `json:"content,omitempty"`
	Redacted bool           `json:"redacted,omitempty"`
}

type DateRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type Pagination struct {
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}

func DefaultPagination() Pagination {
	return Pagination{Page: 1, PageSize: 20}
}

func (p Pagination) Offset() int {
	if p.Page < 1 {
		p.Page = 1
	}
	if p.PageSize < 1 {
		return 0
	}
	return (p.Page - 1) * p.PageSize
}

// MessageEntry represents a single message row in the requests history list.
type MessageEntry struct {
	SourceID       string          `json:"source_id,omitempty"`
	ID             string          `json:"id"`
	SessionID      string          `json:"session_id"`
	SessionTitle   string          `json:"session_title"`
	Role           string          `json:"role"`
	TimeCreated    time.Time       `json:"time_created"`
	Cost           float64         `json:"cost,omitempty"`
	Tokens         *TokenStats     `json:"tokens,omitempty"`
	ModelID        string          `json:"model_id,omitempty"`
	ProviderID     string          `json:"provider_id,omitempty"`
	CostStatus     CostStatus      `json:"cost_status,omitempty"`
	CostProvenance *CostProvenance `json:"cost_provenance,omitempty"`

	// Claude Code interactions may fold multiple raw assistant/tool events into
	// one user-facing row. These additive counts make that folding discoverable
	// without exposing raw transcript events as separate dashboard messages.
	FoldedAssistantCalls int64 `json:"folded_assistant_calls,omitempty"`
	FoldedToolCalls      int64 `json:"folded_tool_calls,omitempty"`
	FoldedTokenUpdates   int64 `json:"folded_token_updates,omitempty"`
}

// MessageList is a paginated list of messages for a date range.
type MessageList struct {
	SourceID       string          `json:"source_id,omitempty"`
	Messages       []MessageEntry  `json:"messages"`
	Total          int64           `json:"total"`
	Page           int             `json:"page"`
	PageSize       int             `json:"page_size"`
	CostStatus     CostStatus      `json:"cost_status,omitempty"`
	CostProvenance *CostProvenance `json:"cost_provenance,omitempty"`
}

// MessagePart represents a single text or reasoning part from the part table.
type MessagePart struct {
	Type       string          `json:"type"` // "text" or "reasoning"
	Text       string          `json:"text"` // Actual content
	Truncation *TruncationInfo `json:"truncation,omitempty"`
	Redacted   bool            `json:"redacted,omitempty"`
}

type ToolTime struct {
	Start     int64 `json:"start,omitempty"`
	End       int64 `json:"end,omitempty"`
	Compacted int64 `json:"compacted,omitempty"`
}

type ToolState struct {
	Status     string                 `json:"status"` // pending, running, completed, error
	Input      map[string]interface{} `json:"input,omitempty"`
	Output     string                 `json:"output,omitempty"`
	Title      string                 `json:"title,omitempty"`
	Error      string                 `json:"error,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	Time       *ToolTime              `json:"time,omitempty"`
	Truncation *TruncationInfo        `json:"truncation,omitempty"`
	Redacted   bool                   `json:"redacted,omitempty"`
}

type ToolPart struct {
	SourceID string    `json:"source_id,omitempty"`
	Type     string    `json:"type"` // always "tool"
	CallID   string    `json:"call_id"`
	Tool     string    `json:"tool"`
	State    ToolState `json:"state"`
}

type MessageContent struct {
	TextParts      []MessagePart `json:"text_parts"`
	ReasoningParts []MessagePart `json:"reasoning_parts"`
	ToolParts      []ToolPart    `json:"tool_parts"`
}

type MessageDetail struct {
	MessageEntry
	Content MessageContent `json:"content"`
}
