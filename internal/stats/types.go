package stats

import (
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
