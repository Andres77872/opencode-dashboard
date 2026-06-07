package source

import (
	"context"
	"sort"
	"sync"
	"time"

	"opencode-dashboard/internal/stats"
)

const (
	defaultAggregateTopN         = 10
	defaultPerSourceFetchTimeout = 10 * time.Second
)

// AllSourcesOverview is the cross-source dashboard payload that powers the
// Overview view in both the TUI and the web frontend. Unlike every other view
// (which is scoped to a single selected source), the Overview merges data across
// all available sources.
//
// Cost is intentionally NOT presented as a single combined number by the UIs:
// OpenCode reports real spend, Codex reports an estimated API-equivalent, and
// Claude Code is mixed — summing them would be misleading. Total.Cost holds the
// arithmetic sum for API completeness, but consumers should show per-source costs
// (SourceOverview.Overview.Cost) with each source's own provenance.
type AllSourcesOverview struct {
	// Total holds combined, additive metrics (sessions, messages, tokens, days).
	Total stats.OverviewStats `json:"total"`
	// Sources is the per-source breakdown, one entry per source queried.
	Sources []SourceOverview `json:"sources"`
	// MessagesPerSession and TokensPerMessage are derived over the combined totals.
	MessagesPerSession float64             `json:"messages_per_session"`
	TokensPerMessage   stats.AvgTokenStats `json:"tokens_per_message"`
	// TokenDistribution mirrors Total.Tokens, surfaced flat for the distribution widget.
	TokenDistribution stats.TokenStats `json:"token_distribution"`
	// Top signals merged across sources, ranked by a cost-neutral metric (tokens /
	// invocations) so real vs estimated dollars are never compared. Each entry keeps
	// its own SourceID and its own cost.
	TopModels   []stats.ModelEntry   `json:"top_models"`
	TopProjects []stats.ProjectEntry `json:"top_projects"`
	TopTools    []stats.ToolEntry    `json:"top_tools"`
	// Errors records sources that failed to load; the response still succeeds.
	Errors []SourceLoadError `json:"errors,omitempty"`
}

// SourceOverview is a single source's contribution to the aggregate.
type SourceOverview struct {
	SourceID string              `json:"source_id"`
	Label    string              `json:"label,omitempty"`
	Overview stats.OverviewStats `json:"overview"` // carries this source's own cost + provenance
	// MessageShare and TokenShare are this source's fraction of the combined totals (0..1).
	MessageShare       float64             `json:"message_share"`
	TokenShare         float64             `json:"token_share"`
	MessagesPerSession float64             `json:"messages_per_session"`
	TokensPerMessage   stats.AvgTokenStats `json:"tokens_per_message"`
	// Trend is this source's per-day series for the window, present when IncludeTrend.
	Trend []stats.DayStats `json:"trend,omitempty"`
}

// SourceLoadError records a per-source failure without failing the whole response.
type SourceLoadError struct {
	SourceID string `json:"source_id"`
	Message  string `json:"message"`
}

// AggregateOptions controls how AggregateOverview fans out.
type AggregateOptions struct {
	IncludeTrend     bool          // also fetch Daily() per source and attach to SourceOverview.Trend
	TopN             int           // cap for top signals; <=0 -> defaultAggregateTopN
	PerSourceTimeout time.Duration // per-source fetch bound; <=0 -> defaultPerSourceFetchTimeout
}

// AggregateOverview fans out to every available source, calls its existing
// interface methods for the given period, and merges the results into combined
// totals plus a per-source breakdown. A single source erroring is recorded in
// Errors and excluded from totals; it never fails the whole response.
func AggregateOverview(ctx context.Context, reg *Registry, pq stats.PeriodQuery, opts AggregateOptions) (AllSourcesOverview, error) {
	result := AllSourcesOverview{
		Sources:     []SourceOverview{},
		TopModels:   []stats.ModelEntry{},
		TopProjects: []stats.ProjectEntry{},
		TopTools:    []stats.ToolEntry{},
	}
	if reg == nil {
		return result, nil
	}

	topN := opts.TopN
	if topN <= 0 {
		topN = defaultAggregateTopN
	}
	perSourceTimeout := opts.PerSourceTimeout
	if perSourceTimeout <= 0 {
		perSourceTimeout = defaultPerSourceFetchTimeout
	}

	srcs := reg.Available(ctx)
	if len(srcs) == 0 {
		return result, nil
	}

	type perSourceRaw struct {
		info     SourceInfo
		overview stats.OverviewStats
		models   []stats.ModelEntry
		tools    []stats.ToolEntry
		projects []stats.ProjectEntry
		trend    []stats.DayStats
		err      error
	}

	// Sources are queried concurrently (each writes only its own index slot, so no
	// mutex is needed). Within a source the calls run SEQUENTIALLY, each with its
	// OWN fresh timeout — sources commonly share one SQLite connection, so issuing
	// the calls concurrently would just queue them while their deadlines ran down.
	// A per-call timeout (rather than one shared per-source budget) stops a heavy
	// early call (e.g. Tools) from starving a later one (e.g. Daily/trend).
	raws := make([]perSourceRaw, len(srcs))
	var wg sync.WaitGroup
	for i, src := range srcs {
		wg.Add(1)
		go func(i int, src Source) {
			defer wg.Done()
			raws[i].info = src.Info(ctx)

			call := func(fn func(context.Context) error) error {
				cctx, cancel := context.WithTimeout(ctx, perSourceTimeout)
				defer cancel()
				return fn(cctx)
			}

			if err := call(func(c context.Context) error {
				ov, err := src.Overview(c, pq)
				if err == nil {
					raws[i].overview = ov
				}
				return err
			}); err != nil {
				raws[i].err = err
				return
			}

			// Top-signal sources are best-effort: a failure here degrades to no
			// rows for that dimension rather than dropping the whole source.
			_ = call(func(c context.Context) error {
				models, err := src.Models(c, pq)
				if err == nil {
					raws[i].models = models.Models
				}
				return err
			})
			_ = call(func(c context.Context) error {
				tools, err := src.Tools(c, pq)
				if err == nil {
					raws[i].tools = tools.Tools
				}
				return err
			})
			_ = call(func(c context.Context) error {
				projects, err := src.Projects(c, pq)
				if err == nil {
					raws[i].projects = projects.Projects
				}
				return err
			})
			if opts.IncludeTrend {
				_ = call(func(c context.Context) error {
					daily, err := src.Daily(c, pq)
					if err == nil {
						raws[i].trend = daily.Days
					}
					return err
				})
			}
		}(i, src)
	}
	wg.Wait()

	// First pass: combined totals + collect top-signal candidates.
	var allModels []stats.ModelEntry
	var allTools []stats.ToolEntry
	var allProjects []stats.ProjectEntry
	dayDates := make(map[string]struct{})

	for i := range raws {
		raw := &raws[i]
		id := string(raw.info.ID)
		if raw.err != nil {
			result.Errors = append(result.Errors, SourceLoadError{SourceID: id, Message: raw.err.Error()})
			continue
		}
		ov := raw.overview
		result.Total.Sessions += ov.Sessions
		result.Total.Messages += ov.Messages
		result.Total.Cost += ov.Cost
		result.Total.Tokens.Input += ov.Tokens.Input
		result.Total.Tokens.Output += ov.Tokens.Output
		result.Total.Tokens.Reasoning += ov.Tokens.Reasoning
		result.Total.Tokens.Cache.Read += ov.Tokens.Cache.Read
		result.Total.Tokens.Cache.Write += ov.Tokens.Cache.Write
		if ov.Days > result.Total.Days {
			result.Total.Days = ov.Days
		}
		for _, d := range raw.trend {
			dayDates[d.Date] = struct{}{}
		}

		// Stamp SourceID defensively so top-signal rows are always source-tagged,
		// independent of whether each source annotated its own entries.
		for j := range raw.models {
			raw.models[j].SourceID = id
		}
		for j := range raw.tools {
			raw.tools[j].SourceID = id
		}
		for j := range raw.projects {
			raw.projects[j].SourceID = id
		}
		allModels = append(allModels, raw.models...)
		allTools = append(allTools, raw.tools...)
		allProjects = append(allProjects, raw.projects...)
	}

	// When trend data is present we can count distinct calendar days across all
	// sources (more accurate than the per-source max used otherwise).
	if len(dayDates) > 0 {
		result.Total.Days = len(dayDates)
	}

	totalMessages := result.Total.Messages
	totalTok := sumTokens(result.Total.Tokens)

	// Second pass: per-source breakdown (skip errored sources).
	for i := range raws {
		raw := &raws[i]
		if raw.err != nil {
			continue
		}
		ov := raw.overview
		so := SourceOverview{
			SourceID:           string(raw.info.ID),
			Label:              raw.info.Label,
			Overview:           ov,
			MessagesPerSession: messagesPerSession(ov.Messages, ov.Sessions),
			TokensPerMessage:   avgTokens(ov.Tokens, ov.Messages),
			Trend:              raw.trend,
		}
		if totalMessages > 0 {
			so.MessageShare = float64(ov.Messages) / float64(totalMessages)
		}
		if totalTok > 0 {
			so.TokenShare = float64(sumTokens(ov.Tokens)) / float64(totalTok)
		}
		result.Sources = append(result.Sources, so)
	}

	// Derived combined ratios + distribution.
	result.MessagesPerSession = messagesPerSession(result.Total.Messages, result.Total.Sessions)
	result.TokensPerMessage = avgTokens(result.Total.Tokens, result.Total.Messages)
	result.TokenDistribution = result.Total.Tokens

	// Top signals, ranked by a cost-neutral metric.
	result.TopModels = topModels(allModels, topN)
	result.TopProjects = topProjects(allProjects, topN)
	result.TopTools = topTools(allTools, topN)

	return result, nil
}

func sumTokens(t stats.TokenStats) int64 {
	return t.Input + t.Output + t.Reasoning + t.Cache.Read + t.Cache.Write
}

func messagesPerSession(messages, sessions int64) float64 {
	if sessions <= 0 {
		return 0
	}
	return float64(messages) / float64(sessions)
}

func avgTokens(t stats.TokenStats, divisor int64) stats.AvgTokenStats {
	if divisor <= 0 {
		return stats.AvgTokenStats{}
	}
	d := float64(divisor)
	return stats.AvgTokenStats{
		Input:      float64(t.Input) / d,
		Output:     float64(t.Output) / d,
		Reasoning:  float64(t.Reasoning) / d,
		CacheRead:  float64(t.Cache.Read) / d,
		CacheWrite: float64(t.Cache.Write) / d,
	}
}

// topModels ranks merged model entries by total tokens (cost-neutral) so real
// and estimated costs are never compared across sources.
func topModels(models []stats.ModelEntry, n int) []stats.ModelEntry {
	sort.SliceStable(models, func(i, j int) bool {
		ti, tj := sumTokens(models[i].Tokens), sumTokens(models[j].Tokens)
		if ti != tj {
			return ti > tj
		}
		if models[i].Messages != models[j].Messages {
			return models[i].Messages > models[j].Messages
		}
		return models[i].ModelID < models[j].ModelID
	})
	if n > 0 && len(models) > n {
		models = models[:n]
	}
	if models == nil {
		return []stats.ModelEntry{}
	}
	return models
}

// topProjects ranks merged project entries by total tokens (cost-neutral).
func topProjects(projects []stats.ProjectEntry, n int) []stats.ProjectEntry {
	sort.SliceStable(projects, func(i, j int) bool {
		ti, tj := sumTokens(projects[i].Tokens), sumTokens(projects[j].Tokens)
		if ti != tj {
			return ti > tj
		}
		if projects[i].Messages != projects[j].Messages {
			return projects[i].Messages > projects[j].Messages
		}
		return projects[i].ProjectID < projects[j].ProjectID
	})
	if n > 0 && len(projects) > n {
		projects = projects[:n]
	}
	if projects == nil {
		return []stats.ProjectEntry{}
	}
	return projects
}

// topTools ranks merged tool entries by invocation count.
func topTools(tools []stats.ToolEntry, n int) []stats.ToolEntry {
	sort.SliceStable(tools, func(i, j int) bool {
		if tools[i].Invocations != tools[j].Invocations {
			return tools[i].Invocations > tools[j].Invocations
		}
		return tools[i].Name < tools[j].Name
	})
	if n > 0 && len(tools) > n {
		tools = tools[:n]
	}
	if tools == nil {
		return []stats.ToolEntry{}
	}
	return tools
}
