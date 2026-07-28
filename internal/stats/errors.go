package stats

import (
	"errors"
	"fmt"
)

// Sentinels for the request-shaping failures every aggregator can raise. The
// web layer maps these to 4xx responses; anything else is a 500. They live here
// because every source and the cache already depend on this package, and
// because classifying by errors.Is is the only way for that mapping to survive
// a reworded message.
var (
	// ErrInvalidPeriod marks a period string no aggregator can resolve.
	ErrInvalidPeriod = errors.New("invalid period")
	// ErrInvalidDimension marks an unsupported daily-breakdown dimension.
	ErrInvalidDimension = errors.New("invalid dimension")
	// ErrFilterUnavailable marks a query whose model/provider filter cannot be
	// served yet because the source has no consolidated cache to filter over.
	// It describes a temporary state, not a malformed request.
	ErrFilterUnavailable = errors.New("filter unavailable")
)

// supportedPeriods is echoed by InvalidPeriodError so the list users see is
// defined once rather than repeated at each aggregator that validates a period.
const supportedPeriods = "1d, 7d, 14d, 30d, 1y, all, plus hour presets 1h, 6h, 12h, 24h, 72h"

// InvalidPeriodError is the canonical rejection for an unresolvable period.
func InvalidPeriodError(period string) error {
	return fmt.Errorf("%w: %q (supported: %s)", ErrInvalidPeriod, period, supportedPeriods)
}

// InvalidDimensionError rejects a daily-breakdown dimension. supported is the
// human-readable list for that caller, which differs by source: only Codex
// records a processing mode.
func InvalidDimensionError(dimension, supported string) error {
	return fmt.Errorf("%w %q: supported values are %s", ErrInvalidDimension, dimension, supported)
}
