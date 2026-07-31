package analyticsagent

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"opencode-dashboard/internal/stats"
)

// Prepare validates and normalizes provider-proposed analytics arguments before
// they are fingerprinted, streamed, persisted, or allowed to reach a source.
// It deliberately checks only the static input contract; source registration
// and availability remain execution-time facts.
func (r *ToolRegistry) Prepare(name string, arguments json.RawMessage) (json.RawMessage, error) {
	now := time.Now().UTC()
	switch name {
	case "list_sources":
		var args struct{}
		if err := decodeStrict(arguments, &args); err != nil {
			return nil, err
		}
		return marshalPrepared(args)
	case "get_overview":
		var args sourcePeriodNoLimitArgs
		if err := decodeStrict(arguments, &args); err != nil {
			return nil, err
		}
		if err := normalizeRequiredSource(&args.Source); err != nil {
			return nil, err
		}
		if err := normalizePeriod(&args.Period, &args.From, &args.To, now); err != nil {
			return nil, err
		}
		return marshalPrepared(args)
	case "get_source_integrity":
		var args integrityArgs
		if err := decodeStrict(arguments, &args); err != nil {
			return nil, err
		}
		args.Source = strings.TrimSpace(args.Source)
		if args.Source != "" && !isSafeSourceID(args.Source) {
			return nil, invalidInput("source must be an exact ID returned by list_sources")
		}
		if err := normalizePeriod(&args.Period, &args.From, &args.To, now); err != nil {
			return nil, err
		}
		return marshalPrepared(args)
	case "get_cross_source_overview":
		var args aggregateArgs
		if err := decodeStrict(arguments, &args); err != nil {
			return nil, err
		}
		if err := normalizePeriod(&args.Period, &args.From, &args.To, now); err != nil {
			return nil, err
		}
		if err := normalizeLimit(&args.Limit, defaultListLimit, maxAggregateTopN, "limit"); err != nil {
			return nil, err
		}
		if err := normalizeLimit(&args.TrendLimit, 90, maxDailyLimit, "trend_limit"); err != nil {
			return nil, err
		}
		if args.IncludeTrend {
			if err := validateBucketWindow(periodArgs{Period: args.Period, From: args.From, To: args.To}, automaticTrendGranularity(args.Period), maxDailyLimit); err != nil {
				return nil, err
			}
		}
		return marshalPrepared(args)
	case "get_daily_usage":
		var args dailyArgs
		if err := decodeStrict(arguments, &args); err != nil {
			return nil, err
		}
		if err := prepareDailyArgs(&args.Source, &args.Period, &args.From, &args.To, &args.Granularity, &args.Limit, now); err != nil {
			return nil, err
		}
		return marshalPrepared(args)
	case "get_usage_trend_by_dimension":
		var args dimensionTrendArgs
		if err := decodeStrict(arguments, &args); err != nil {
			return nil, err
		}
		args.Dimension = strings.TrimSpace(args.Dimension)
		if args.Dimension != "model" && args.Dimension != "tool" && args.Dimension != "project" {
			return nil, invalidInput("dimension must be model, tool, or project")
		}
		if err := prepareDailyArgs(&args.Source, &args.Period, &args.From, &args.To, &args.Granularity, &args.Limit, now); err != nil {
			return nil, err
		}
		return marshalPrepared(args)
	case "get_session_usage":
		var args sessionUsageArgs
		if err := decodeStrict(arguments, &args); err != nil {
			return nil, err
		}
		if err := normalizeRequiredSource(&args.Source); err != nil {
			return nil, err
		}
		if err := normalizePeriod(&args.Period, &args.From, &args.To, now); err != nil {
			return nil, err
		}
		if err := normalizeLimit(&args.Limit, defaultListLimit, maxListLimit, "limit"); err != nil {
			return nil, err
		}
		args.Sort = strings.TrimSpace(args.Sort)
		if args.Sort == "" {
			args.Sort = "newest"
		}
		switch args.Sort {
		case "newest", "oldest", "cost", "messages":
		default:
			return nil, invalidInput("sort must be newest, oldest, cost, or messages")
		}
		return marshalPrepared(args)
	case "get_model_usage", "get_tool_usage", "get_project_usage":
		var args sourcePeriodArgs
		if err := decodeStrict(arguments, &args); err != nil {
			return nil, err
		}
		if err := normalizeRequiredSource(&args.Source); err != nil {
			return nil, err
		}
		if err := normalizePeriod(&args.Period, &args.From, &args.To, now); err != nil {
			return nil, err
		}
		if err := normalizeLimit(&args.Limit, defaultListLimit, maxListLimit, "limit"); err != nil {
			return nil, err
		}
		return marshalPrepared(args)
	default:
		return nil, invalidInput("unknown analytics tool")
	}
}

func prepareDailyArgs(source, period, from, to, granularity *string, limit **int, now time.Time) error {
	if err := normalizeRequiredSource(source); err != nil {
		return err
	}
	if err := normalizePeriod(period, from, to, now); err != nil {
		return err
	}
	if err := normalizeLimit(limit, defaultDailyLimit, maxDailyLimit, "limit"); err != nil {
		return err
	}
	*granularity = strings.TrimSpace(*granularity)
	if *granularity == "" {
		*granularity = automaticTrendGranularity(*period)
	}
	if *granularity != string(stats.GranularityDay) && *granularity != string(stats.GranularityHour) {
		return invalidInput("granularity must be day or hour")
	}
	return validateBucketWindow(periodArgs{Period: *period, From: *from, To: *to}, *granularity, maxDailyLimit)
}

func normalizeRequiredSource(source *string) error {
	*source = strings.TrimSpace(*source)
	if *source == "" {
		return invalidInput("source is required; use an exact ID returned by list_sources")
	}
	if !isSafeSourceID(*source) {
		return invalidInput("source must be an exact ID returned by list_sources")
	}
	return nil
}

func normalizePeriod(period, from, to *string, now time.Time) error {
	pq, err := validatePeriodAt(periodArgs{Period: *period, From: *from, To: *to}, now)
	if err != nil {
		return err
	}
	*period, *from, *to = pq.Period, pq.From, pq.To
	return nil
}

func normalizeLimit(value **int, defaultValue, maximum int, field string) error {
	resolved, err := validatedLimit(*value, defaultValue, maximum)
	if err != nil {
		if field != "limit" {
			return invalidInput(field + " must be between 1 and " + strconv.Itoa(maximum))
		}
		return err
	}
	*value = intPointer(resolved)
	return nil
}

func intPointer(value int) *int { return &value }

func marshalPrepared(value any) (json.RawMessage, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(encoded), nil
}
