package analyticsagent

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

const (
	integritySourceTimeout  = 3 * time.Second
	integrityMaxConcurrency = 4
)

// CacheIntegrityProvider is the narrow, privacy-safe cache boundary available
// to analytics tools. Implementations must normalize internal cache state and
// must never include paths, timestamps, or raw errors.
type CacheIntegrityProvider interface {
	AnalyticsCacheIntegrity(context.Context) (CacheIntegritySnapshot, error)
}

type CacheIntegritySnapshot struct {
	Enabled     bool
	SyncRunning bool
	Sources     []CacheIntegritySource
}

type CacheIntegritySource struct {
	SourceID               string
	Cached                 bool
	NeedsSync              bool
	Status                 string
	FillFailed             bool
	RecentWindowIncomplete bool
}

type integrityArgs struct {
	Source string `json:"source,omitempty"`
	Period string `json:"period,omitempty"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
}

type safeIntegrityPeriod struct {
	Period string `json:"period,omitempty"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
}

type safeSourceScanIntegrity struct {
	Status            string `json:"status"`
	ScannedFiles      int64  `json:"scanned_files,omitempty"`
	MalformedLines    int64  `json:"malformed_lines,omitempty"`
	UnsupportedEvents int64  `json:"unsupported_events,omitempty"`
}

type safeCostEvidence struct {
	Status       stats.CostStatus `json:"status"`
	MissingCount int64            `json:"missing_count,omitempty"`
}

type safeCacheIntegrity struct {
	Enabled                bool   `json:"enabled"`
	Status                 string `json:"status"`
	Cached                 bool   `json:"cached,omitempty"`
	NeedsSync              bool   `json:"needs_sync,omitempty"`
	SyncRunning            bool   `json:"sync_running,omitempty"`
	FillFailed             bool   `json:"fill_failed,omitempty"`
	RecentWindowIncomplete bool   `json:"recent_window_incomplete,omitempty"`
}

type safeIntegrityFinding struct {
	Code          string `json:"code"`
	Severity      string `json:"severity"`
	Category      string `json:"category"`
	Scope         string `json:"scope"`
	AffectedCount int64  `json:"affected_count,omitempty"`
	TotalCount    int64  `json:"total_count,omitempty"`
	Unit          string `json:"unit,omitempty"`
}

type safeSourceIntegrity struct {
	SourceID          string                   `json:"source_id"`
	Available         bool                     `json:"available"`
	Status            string                   `json:"status"`
	AssessedSignals   []string                 `json:"assessed_signals"`
	Scan              *safeSourceScanIntegrity `json:"scan,omitempty"`
	RequestAccounting *safeRequestAccounting   `json:"request_accounting,omitempty"`
	CostEvidence      *safeCostEvidence        `json:"cost_evidence,omitempty"`
	Cache             *safeCacheIntegrity      `json:"cache,omitempty"`
	Findings          []safeIntegrityFinding   `json:"findings"`
}

type safeIntegrityReport struct {
	Period  safeIntegrityPeriod   `json:"period"`
	Sources []safeSourceIntegrity `json:"sources"`
}

type integrityQueryResult struct {
	overview *stats.OverviewStats
	failed   bool
}

func (r *ToolRegistry) sourceIntegrity(ctx context.Context, args integrityArgs) (safeIntegrityReport, error) {
	pq, err := validatePeriod(periodArgs{Period: args.Period, From: args.From, To: args.To})
	if err != nil {
		return safeIntegrityReport{}, err
	}
	requestedSource := strings.TrimSpace(args.Source)
	if requestedSource != "" && !isSafeSourceID(requestedSource) {
		return safeIntegrityReport{}, invalidInput("source is invalid or unavailable")
	}
	infos := r.registry.List(ctx)
	safeInfos := make([]source.SourceInfo, 0, len(infos))
	for _, info := range infos {
		if isSafeSourceID(string(info.ID)) {
			safeInfos = append(safeInfos, info)
		}
	}
	infos = safeInfos
	if requestedSource != "" {
		filtered := make([]source.SourceInfo, 0, 1)
		for _, info := range infos {
			if string(info.ID) == requestedSource {
				filtered = append(filtered, info)
				break
			}
		}
		if len(filtered) == 0 {
			return safeIntegrityReport{}, invalidInput("source is invalid or unavailable")
		}
		infos = filtered
	}

	results := make([]integrityQueryResult, len(infos))
	var wg sync.WaitGroup
	slots := make(chan struct{}, integrityMaxConcurrency)
	for i := range infos {
		if !infos[i].Available {
			continue
		}
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				results[index].failed = true
				return
			}
			queryCtx, cancel := context.WithTimeout(ctx, integritySourceTimeout)
			defer cancel()
			selected, resolveErr := r.registry.Resolve(string(infos[index].ID))
			if resolveErr != nil {
				results[index].failed = true
				return
			}
			overview, overviewErr := selected.Overview(queryCtx, pq)
			if overviewErr != nil {
				results[index].failed = true
				return
			}
			results[index].overview = &overview
		}(i)
	}
	wg.Wait()

	cacheBySource := make(map[string]CacheIntegritySource)
	var cacheSnapshot CacheIntegritySnapshot
	cacheAssessed := r.cacheIntegrity != nil
	cacheFailed := false
	if cacheAssessed {
		cacheSnapshot, err = r.cacheIntegrity.AnalyticsCacheIntegrity(ctx)
		if err != nil {
			cacheFailed = true
		} else {
			for _, item := range cacheSnapshot.Sources {
				if isSafeSourceID(item.SourceID) {
					cacheBySource[item.SourceID] = item
				}
			}
		}
	}

	report := safeIntegrityReport{
		Period:  safeIntegrityPeriod{Period: pq.Period, From: pq.From, To: pq.To},
		Sources: make([]safeSourceIntegrity, 0, len(infos)),
	}
	for i, info := range infos {
		report.Sources = append(report.Sources, buildSafeSourceIntegrity(
			info, results[i], cacheAssessed, cacheFailed, cacheSnapshot, cacheBySource[string(info.ID)],
		))
	}
	return report, nil
}

func buildSafeSourceIntegrity(
	info source.SourceInfo,
	query integrityQueryResult,
	cacheAssessed, cacheFailed bool,
	cacheSnapshot CacheIntegritySnapshot,
	cacheSource CacheIntegritySource,
) safeSourceIntegrity {
	result := safeSourceIntegrity{
		SourceID:        string(info.ID),
		Available:       info.Available,
		Status:          "ok",
		AssessedSignals: []string{"availability"},
		Findings:        make([]safeIntegrityFinding, 0),
	}
	if !info.Available {
		result.Status = "unavailable"
		result.Findings = append(result.Findings, integrityFinding("source_unavailable", "error", "availability", "source", 0, 0, ""))
	}

	if scan, assessed := safeScanIntegrity(info); assessed {
		result.AssessedSignals = append(result.AssessedSignals, "ingestion")
		result.Scan = &scan
		switch scan.Status {
		case "empty":
			result.Findings = append(result.Findings, integrityFinding("source_empty", "info", "ingestion", "source", 0, 0, "records"))
		case "partial":
			result.Findings = append(result.Findings, integrityFinding("source_ingestion_partial", "warning", "ingestion", "source", 0, 0, ""))
		}
		if scan.MalformedLines > 0 {
			result.Findings = append(result.Findings, integrityFinding("malformed_records_skipped", "warning", "ingestion", "source", scan.MalformedLines, 0, "records"))
		}
		if scan.UnsupportedEvents > 0 {
			result.Findings = append(result.Findings, integrityFinding("unsupported_events_skipped", "info", "ingestion", "source", scan.UnsupportedEvents, 0, "events"))
		}
	}

	if info.Available {
		result.AssessedSignals = append(result.AssessedSignals, "request_accounting", "cost_evidence")
		if query.failed || query.overview == nil {
			result.Findings = append(result.Findings, integrityFinding("source_query_failed", "error", "accounting", "period", 0, 0, ""))
		} else {
			overview := *query.overview
			result.RequestAccounting = safeRequestAccountingFrom(overview.RequestAccounting)
			addAccountingFindings(&result, overview)
			if cost := safeCostEvidenceFrom(overview); cost != nil {
				result.CostEvidence = cost
				addCostFindings(&result, *cost, overview.Requests)
			} else {
				result.AssessedSignals = removeSignal(result.AssessedSignals, "cost_evidence")
			}
		}
	}

	if cacheAssessed {
		result.AssessedSignals = append(result.AssessedSignals, "cache")
		result.Cache = safeCacheIntegrityFrom(cacheFailed, cacheSnapshot, cacheSource)
		addCacheFindings(&result)
	}
	if result.Status == "ok" && findingsNeedAttention(result.Findings) {
		result.Status = "attention"
	}
	sort.Strings(result.AssessedSignals)
	return result
}

func safeScanIntegrity(info source.SourceInfo) (safeSourceScanIntegrity, bool) {
	diag := info.Diagnostics
	status := strings.ToLower(strings.TrimSpace(diag.Status))
	switch status {
	case "ok", "partial", "empty":
	default:
		status = "unknown"
	}
	malformed := nonNegative(diag.MalformedLines)
	unsupported := nonNegative(diag.UnsupportedEvents)
	scanned := nonNegative(diag.ScannedFiles)
	assessed := diag.Status != "" || scanned > 0 || malformed > 0 || unsupported > 0
	if !assessed {
		return safeSourceScanIntegrity{}, false
	}
	if malformed > 0 && status == "ok" {
		status = "partial"
	}
	return safeSourceScanIntegrity{
		Status: status, ScannedFiles: scanned,
		MalformedLines: malformed, UnsupportedEvents: unsupported,
	}, true
}

func addAccountingFindings(result *safeSourceIntegrity, overview stats.OverviewStats) {
	accounting := overview.RequestAccounting
	if accounting == nil {
		result.AssessedSignals = removeSignal(result.AssessedSignals, "request_accounting")
		return
	}
	normalized := stats.NormalizeUsageUnavailableReasons(accounting.UsageUnavailable, accounting.UsageUnavailableReasons)
	if accounting.UsageUnavailableReasons.Total() != accounting.UsageUnavailable ||
		normalized != accounting.UsageUnavailableReasons {
		result.Findings = append(result.Findings, integrityFinding(
			"request_accounting_inconsistent", "error", "accounting", "period",
			accounting.UsageUnavailable, overview.Requests, "requests",
		))
	}
	if accounting.UsageUnavailable > 0 {
		result.Findings = append(result.Findings, integrityFinding(
			"request_usage_unavailable", "warning", "accounting", "period",
			accounting.UsageUnavailable, overview.Requests, "requests",
		))
	}
	switch accounting.TraceCoverage {
	case stats.TraceCoverageComplete:
	case stats.TraceCoverageMixed:
		result.Findings = append(result.Findings, integrityFinding("request_trace_mixed", "warning", "accounting", "period", 0, overview.Requests, "requests"))
	case stats.TraceCoverageSuccessfulOnly:
		result.Findings = append(result.Findings, integrityFinding("request_trace_successful_only", "warning", "accounting", "period", 0, overview.Requests, "requests"))
	default:
		result.Findings = append(result.Findings, integrityFinding("request_trace_unknown", "warning", "accounting", "period", 0, overview.Requests, "requests"))
	}
}

func safeCostEvidenceFrom(overview stats.OverviewStats) *safeCostEvidence {
	status := safeCostStatus(overview.CostStatus)
	missing := int64(0)
	if overview.CostProvenance != nil {
		missing = nonNegative(overview.CostProvenance.MissingCount)
		if status == "" {
			status = safeCostStatus(overview.CostProvenance.Status)
		}
	}
	if status == "" {
		return nil
	}
	return &safeCostEvidence{Status: status, MissingCount: missing}
}

func addCostFindings(result *safeSourceIntegrity, cost safeCostEvidence, total int64) {
	switch {
	case cost.Status == stats.CostMissing:
		result.Findings = append(result.Findings, integrityFinding("cost_evidence_missing", "warning", "cost", "period", maxInt64(cost.MissingCount, total), total, "requests"))
	case cost.MissingCount > 0:
		result.Findings = append(result.Findings, integrityFinding("cost_evidence_partial", "warning", "cost", "period", cost.MissingCount, total, "requests"))
	}
}

func safeCacheIntegrityFrom(failed bool, snapshot CacheIntegritySnapshot, item CacheIntegritySource) *safeCacheIntegrity {
	if failed {
		return &safeCacheIntegrity{Enabled: true, Status: "unknown"}
	}
	if !snapshot.Enabled {
		return &safeCacheIntegrity{Enabled: false, Status: "disabled"}
	}
	status := strings.ToLower(strings.TrimSpace(item.Status))
	switch status {
	case "ready", "syncing", "pending", "error", "unavailable":
	default:
		status = "unknown"
	}
	return &safeCacheIntegrity{
		Enabled: true, Status: status, Cached: item.Cached, NeedsSync: item.NeedsSync,
		SyncRunning: snapshot.SyncRunning, FillFailed: item.FillFailed,
		RecentWindowIncomplete: item.RecentWindowIncomplete,
	}
}

func addCacheFindings(result *safeSourceIntegrity) {
	cache := result.Cache
	if cache == nil || !cache.Enabled {
		return
	}
	if cache.Status == "unknown" {
		result.Findings = append(result.Findings, integrityFinding("cache_status_unavailable", "warning", "freshness", "cache_window", 0, 0, ""))
	}
	if cache.SyncRunning {
		result.Findings = append(result.Findings, integrityFinding("cache_sync_in_progress", "info", "freshness", "cache_window", 0, 0, ""))
	}
	if !cache.Cached || cache.NeedsSync {
		result.Findings = append(result.Findings, integrityFinding("cache_not_ready", "info", "freshness", "cache_window", 0, 0, ""))
	}
	if cache.FillFailed || cache.Status == "error" {
		result.Findings = append(result.Findings, integrityFinding("cache_sync_failed", "warning", "freshness", "cache_window", 0, 0, ""))
	}
	if cache.RecentWindowIncomplete {
		result.Findings = append(result.Findings, integrityFinding("recent_window_incomplete", "warning", "freshness", "cache_window", 0, 0, ""))
	}
}

func integrityFinding(code, severity, category, scope string, affected, total int64, unit string) safeIntegrityFinding {
	return safeIntegrityFinding{
		Code: code, Severity: severity, Category: category, Scope: scope,
		AffectedCount: nonNegative(affected), TotalCount: nonNegative(total), Unit: unit,
	}
}

func findingsNeedAttention(findings []safeIntegrityFinding) bool {
	for _, finding := range findings {
		if finding.Severity == "warning" || finding.Severity == "error" ||
			finding.Code == "source_empty" {
			return true
		}
	}
	return false
}

func removeSignal(values []string, target string) []string {
	out := values[:0]
	for _, value := range values {
		if value != target {
			out = append(out, value)
		}
	}
	return out
}

func nonNegative(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
