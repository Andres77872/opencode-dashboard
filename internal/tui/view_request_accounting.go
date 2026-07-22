package tui

import (
	"fmt"

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
	return s.BannerWarn.Render(fmt.Sprintf(
		"Kimi accounting • usage-unavailable requests: %s • trace coverage: %s • tokens/cost are unknown (not zero)",
		formatInt(accounting.UsageUnavailable),
		coverage,
	))
}
