package tui

import (
	"fmt"
	"strings"

	"opencode-dashboard/internal/stats"
)

// renderKimiAccountingDisclosure makes incomplete persisted usage explicit.
// Unknown token/cost evidence must never be presented as a recorded zero.
func renderKimiAccountingDisclosure(s styles, accounting *stats.RequestAccounting) string {
	if accounting == nil || (accounting.UsageUnavailable == 0 && accounting.TraceCoverage == stats.TraceCoverageComplete) {
		return ""
	}

	coverage := accounting.TraceCoverage
	if coverage == "" {
		coverage = stats.TraceCoverageUnknown
	}
	reasons := stats.NormalizeUsageUnavailableReasons(accounting.UsageUnavailable, accounting.UsageUnavailableReasons)
	reasonParts := make([]string, 0, 4)
	if reasons.Cancelled > 0 {
		reasonParts = append(reasonParts, fmt.Sprintf("%s cancelled", formatInt(reasons.Cancelled)))
	}
	if reasons.Interrupted > 0 {
		reasonParts = append(reasonParts, fmt.Sprintf("%s log-ended-open", formatInt(reasons.Interrupted)))
	}
	if reasons.Failed > 0 {
		reasonParts = append(reasonParts, fmt.Sprintf("%s failed/retried", formatInt(reasons.Failed)))
	}
	if reasons.Unknown > 0 {
		reasonParts = append(reasonParts, fmt.Sprintf("%s unexplained", formatInt(reasons.Unknown)))
	}
	reasonSummary := ""
	if len(reasonParts) > 0 {
		reasonSummary = " (" + strings.Join(reasonParts, ", ") + ")"
	}
	return s.BannerWarn.Render(fmt.Sprintf(
		"Kimi accounting • canonical usage: %s • recovery-only: %s • usage-unavailable requests: %s%s • trace coverage: %s • tokens/cost are unknown (not zero)",
		formatInt(accounting.UsageRecorded),
		formatInt(accounting.UsageRecovered),
		formatInt(accounting.UsageUnavailable),
		reasonSummary,
		coverage,
	))
}
