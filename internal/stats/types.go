package stats

import (
	"strings"
	"time"
)

type CacheStats struct {
	Read  int64 `json:"read"`
	Write int64 `json:"write"`
}

type TokenStats struct {
	Input     int64      `json:"input"`
	Output    int64      `json:"output"`
	Reasoning int64      `json:"reasoning"`
	Cache     CacheStats `json:"cache"`
}

type OverviewStats struct {
	Sessions   int64      `json:"sessions"`
	Messages   int64      `json:"messages"`
	Cost       float64    `json:"cost"`
	Tokens     TokenStats `json:"tokens"`
	CostPerDay float64    `json:"cost_per_day"`
	Days       int        `json:"days"`
}

type DayStats struct {
	Date     string     `json:"date"`
	Sessions int64      `json:"sessions"`
	Messages int64      `json:"messages"`
	Cost     float64    `json:"cost"`
	Tokens   TokenStats `json:"tokens"`
}

type Granularity string

const (
	GranularityDay  Granularity = "day"
	GranularityHour Granularity = "hour"
)

type DailyStats struct {
	Days        []DayStats  `json:"days"`
	Granularity Granularity `json:"granularity"`
}

type ModelEntry struct {
	ModelID    string     `json:"model_id"`
	ProviderID string     `json:"provider_id"`
	Sessions   int64      `json:"sessions"`
	Messages   int64      `json:"messages"`
	Cost       float64    `json:"cost"`
	Tokens     TokenStats `json:"tokens"`
}

type ModelStats struct {
	Models []ModelEntry `json:"models"`
}

type ToolEntry struct {
	Name        string `json:"name"`
	Invocations int64  `json:"invocations"`
	Successes   int64  `json:"successes"`
	Failures    int64  `json:"failures"`
	Sessions    int64  `json:"sessions"`
}

type ToolStats struct {
	Tools []ToolEntry `json:"tools"`
}

type ProjectEntry struct {
	ProjectID   string     `json:"project_id"`
	ProjectName string     `json:"project_name"`
	Sessions    int64      `json:"sessions"`
	Messages    int64      `json:"messages"`
	Cost        float64    `json:"cost"`
	Tokens      TokenStats `json:"tokens"`
}

type ProjectStats struct {
	Projects []ProjectEntry `json:"projects"`
}

type SessionEntry struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	ProjectID    string    `json:"project_id"`
	ProjectName  string    `json:"project_name"`
	TimeCreated  time.Time `json:"time_created"`
	TimeUpdated  time.Time `json:"time_updated"`
	MessageCount int64     `json:"message_count"`
	Cost         float64   `json:"cost"`
}

type SessionList struct {
	Sessions []SessionEntry `json:"sessions"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}

type SessionSortMode string

const (
	SessionSortNewest   SessionSortMode = "newest"
	SessionSortOldest   SessionSortMode = "oldest"
	SessionSortCost     SessionSortMode = "cost"
	SessionSortMessages SessionSortMode = "messages"
)

type SessionQuery struct {
	Page     int
	PageSize int
	Filter   string
	Sort     SessionSortMode
	Period   string // "1d", "7d", "30d", "1y", "all" — filters by message activity time
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
	ID          string      `json:"id"`
	Role        string      `json:"role"`
	TimeCreated time.Time   `json:"time_created"`
	Cost        float64     `json:"cost,omitempty"`
	Tokens      *TokenStats `json:"tokens,omitempty"`
	ModelID     string      `json:"model_id,omitempty"`
	ProviderID  string      `json:"provider_id,omitempty"`
	Agent       string      `json:"agent,omitempty"`
}

type SessionDetail struct {
	ID           string           `json:"id"`
	Title        string           `json:"title"`
	ProjectID    string           `json:"project_id"`
	ProjectName  string           `json:"project_name"`
	Directory    string           `json:"directory"`
	TimeCreated  time.Time        `json:"time_created"`
	TimeUpdated  time.Time        `json:"time_updated"`
	Messages     []SessionMessage `json:"messages"`
	TotalCost    float64          `json:"total_cost"`
	TotalTokens  TokenStats       `json:"total_tokens"`
	MessageCount int64            `json:"message_count"`
}

type ConfigView struct {
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	Content string `json:"content,omitempty"`
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
	ID           string      `json:"id"`
	SessionID    string      `json:"session_id"`
	SessionTitle string      `json:"session_title"`
	Role         string      `json:"role"`
	TimeCreated  time.Time   `json:"time_created"`
	Cost         float64     `json:"cost"`
	Tokens       *TokenStats `json:"tokens,omitempty"`
	ModelID      string      `json:"model_id,omitempty"`
	ProviderID   string      `json:"provider_id,omitempty"`
}

// MessageList is a paginated list of messages for a date range.
type MessageList struct {
	Messages []MessageEntry `json:"messages"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}

// MessagePart represents a single text or reasoning part from the part table.
type MessagePart struct {
	Type string `json:"type"` // "text" or "reasoning"
	Text string `json:"text"` // Actual content
}

type ToolTime struct {
	Start     int64 `json:"start,omitempty"`
	End       int64 `json:"end,omitempty"`
	Compacted int64 `json:"compacted,omitempty"`
}

type ToolState struct {
	Status   string                 `json:"status"` // pending, running, completed, error
	Input    map[string]interface{} `json:"input,omitempty"`
	Output   string                 `json:"output,omitempty"`
	Title    string                 `json:"title,omitempty"`
	Error    string                 `json:"error,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
	Time     *ToolTime              `json:"time,omitempty"`
}

type ToolPart struct {
	Type   string    `json:"type"` // always "tool"
	CallID string    `json:"call_id"`
	Tool   string    `json:"tool"`
	State  ToolState `json:"state"`
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
