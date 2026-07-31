package stats

import (
	"errors"
	"fmt"
	"strings"
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

// InvalidPeriodError is the canonical rejection for an unresolvable period.
func InvalidPeriodError(period string) error {
	return fmt.Errorf("%w: %q (supported presets: %s)", ErrInvalidPeriod, period, strings.Join(SupportedPeriodPresets(), ", "))
}

// InvalidDimensionError rejects a daily-breakdown dimension. supported is the
// human-readable list for that caller, which differs by source: only Codex
// records a processing mode.
func InvalidDimensionError(dimension, supported string) error {
	return fmt.Errorf("%w %q: supported values are %s", ErrInvalidDimension, dimension, supported)
}
