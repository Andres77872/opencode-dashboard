package claudecode

import (
	"encoding/json"
	"strings"
	"testing"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

func usageMap(t *testing.T, raw string) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var out map[string]any
	if err := decoder.Decode(&out); err != nil {
		t.Fatalf("decode usage %s: %v", raw, err)
	}
	return out
}

func TestUsageIsComplete(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{
			// The streaming stub a proxied GPT model writes per content block:
			// input_tokens is the whole prompt and nothing has been emitted yet.
			name: "streaming stub with no cache accounting and no output",
			raw:  `{"input_tokens":15832,"output_tokens":0}`,
			want: false,
		},
		{
			name: "finalizing record carries the cache split",
			raw:  `{"input_tokens":7860,"output_tokens":416,"cache_read_input_tokens":7680,"cache_creation_input_tokens":0,"cache_creation":{"ephemeral_1h_input_tokens":0,"ephemeral_5m_input_tokens":0}}`,
			want: true,
		},
		{
			// Anthropic always reports the cache fields, so an all-zero cache
			// read on a real request stays a measured zero.
			name: "native usage with a genuine zero cache read",
			raw:  `{"input_tokens":1035,"output_tokens":413,"cache_read_input_tokens":0,"cache_creation_input_tokens":23034}`,
			want: true,
		},
		{
			// A provider without prompt caching still reports its output, which
			// is enough to treat the record as a finalized accounting.
			name: "cache-less provider that reported output",
			raw:  `{"input_tokens":1500,"output_tokens":300}`,
			want: true,
		},
		{
			name: "legacy cache_read alias counts as cache accounting",
			raw:  `{"input_tokens":10,"output_tokens":0,"cache_read":5}`,
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := usageIsComplete(usageMap(t, tt.raw)); got != tt.want {
				t.Errorf("usageIsComplete() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestTruncatedUsageReportsUnavailableInsteadOfZeros pins the behaviour that a
// stub must not be presented as a measured zero cache read and zero output.
func TestTruncatedUsageReportsUnavailableInsteadOfZeros(t *testing.T) {
	pricing := pricingSnapshot{
		ID: "test", Currency: "USD",
		Models: map[string]pricingRate{
			"claude-opus-5": {InputPerMillion: 5, OutputPerMillion: 25, CacheReadPerMillion: 0.5, CacheCreatePerMillion: 6.25, CacheCreate1hPerMillion: 10},
		},
	}
	stub := parsedRecord{
		Role: "assistant", Model: "claude-opus-5", HasUsage: true, UsageComplete: false,
		Usage: tokenUsage{Input: 15832},
	}

	var entry stats.MessageEntry
	applyContributionToEntry(&entry, assistantContribution{
		usage: stub.Usage, hasUsage: true, complete: false, cost: computeAssistantCost(stub, pricing),
	})

	if entry.Tokens != nil {
		t.Errorf("Tokens = %+v, want nil so the row reports unknown rather than zeros", *entry.Tokens)
	}
	if entry.UsageStatus != stats.UsageStatusUnavailable {
		t.Errorf("UsageStatus = %q, want %q", entry.UsageStatus, stats.UsageStatusUnavailable)
	}
	if entry.UsageUnavailableReason != stats.UsageUnavailableUnknown {
		t.Errorf("UsageUnavailableReason = %q, want %q", entry.UsageUnavailableReason, stats.UsageUnavailableUnknown)
	}
	if entry.RequestTrace != stats.RequestTraceObserved {
		t.Errorf("RequestTrace = %q, want %q; the request itself was persisted", entry.RequestTrace, stats.RequestTraceObserved)
	}
	if entry.CostStatus != stats.CostMissing || entry.Cost != 0 {
		t.Errorf("cost = %v/%q, want 0/missing; a stub's prompt total cannot be priced", entry.Cost, entry.CostStatus)
	}
}

// TestCompleteUsageStillRecorded guards against the refusal leaking onto real
// records, including one whose cache read is a genuine zero.
func TestCompleteUsageStillRecorded(t *testing.T) {
	pricing := pricingSnapshot{
		ID: "test", Currency: "USD",
		Models: map[string]pricingRate{
			"claude-opus-5": {InputPerMillion: 5, OutputPerMillion: 25, CacheReadPerMillion: 0.5, CacheCreatePerMillion: 6.25, CacheCreate1hPerMillion: 10},
		},
	}
	record := parsedRecord{
		Role: "assistant", Model: "claude-opus-5", HasUsage: true, UsageComplete: true,
		Usage: tokenUsage{Input: 1035, Output: 413, CacheCreate: 23034, CacheCreate5m: 23034},
	}
	var entry stats.MessageEntry
	applyContributionToEntry(&entry, assistantContribution{
		usage: record.Usage, hasUsage: true, complete: true, cost: computeAssistantCost(record, pricing),
	})
	if entry.Tokens == nil {
		t.Fatal("Tokens = nil, want the recorded usage")
	}
	if entry.Tokens.Cache.Read != 0 || entry.Tokens.Cache.Write != 23034 || entry.Tokens.Output != 413 {
		t.Errorf("Tokens = %+v, want a measured zero cache read with the recorded write and output", *entry.Tokens)
	}
	if entry.UsageStatus != stats.UsageStatusRecorded {
		t.Errorf("UsageStatus = %q, want %q", entry.UsageStatus, stats.UsageStatusRecorded)
	}
	if entry.CostStatus != stats.CostComputed {
		t.Errorf("CostStatus = %q, want %q", entry.CostStatus, stats.CostComputed)
	}
}

// TestCompleteUsageWinsRegardlessOfChunkOrder pins that a trailing stub cannot
// erase a finalizing record's cache read, output and cost.
func TestCompleteUsageWinsRegardlessOfChunkOrder(t *testing.T) {
	pricing := pricingSnapshot{
		ID: "test", Currency: "USD",
		Models: map[string]pricingRate{
			"claude-opus-5": {InputPerMillion: 5, OutputPerMillion: 25, CacheReadPerMillion: 0.5, CacheCreatePerMillion: 6.25, CacheCreate1hPerMillion: 10},
		},
	}
	full := assistantContribution{
		usage:    tokenUsage{Input: 500, Output: 300, CacheRead: 20000, CacheCreate: 1000, CacheCreate5m: 1000},
		hasUsage: true, complete: true,
		cost: computeCost("claude-opus-5", "anthropic", tokenUsage{Input: 500, Output: 300, CacheRead: 20000, CacheCreate: 1000, CacheCreate5m: 1000}, true, nil, pricing),
	}
	stub := assistantContribution{
		usage: tokenUsage{Input: 20500}, hasUsage: true, complete: false,
		cost: incompleteUsageCost(pricing),
	}

	for _, tt := range []struct {
		name  string
		order []assistantContribution
	}{
		{"stub then full", []assistantContribution{stub, stub, full}},
		{"full then stub", []assistantContribution{full, stub}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			merged := tt.order[0]
			for _, next := range tt.order[1:] {
				merged = mergeRepeatedBillingContribution(merged, next)
			}
			var entry stats.MessageEntry
			applyContributionToEntry(&entry, merged)
			if entry.Tokens == nil {
				t.Fatal("Tokens = nil, want the finalizing record's usage")
			}
			if entry.Tokens.Cache.Read != 20000 || entry.Tokens.Output != 300 || entry.Tokens.Input != 500 {
				t.Errorf("Tokens = %+v, want input 500 / output 300 / cache read 20000", *entry.Tokens)
			}
			if entry.UsageStatus != stats.UsageStatusRecorded {
				t.Errorf("UsageStatus = %q, want %q", entry.UsageStatus, stats.UsageStatusRecorded)
			}
		})
	}
}

// TestReasoningTokensBillAtOutputRate aligns the Claude adapter with every
// other source, which all bill Output+Reasoning at the output rate.
func TestReasoningTokensBillAtOutputRate(t *testing.T) {
	pricing := pricingSnapshot{
		ID: "test", Currency: "USD",
		Models: map[string]pricingRate{
			"claude-opus-5": {InputPerMillion: 5, OutputPerMillion: 25, CacheReadPerMillion: 0.5, CacheCreatePerMillion: 6.25, CacheCreate1hPerMillion: 10},
		},
	}
	got := computeCost("claude-opus-5", "anthropic", tokenUsage{Reasoning: 1_000_000}, true, nil, pricing)
	if got.Cost != 25.0 {
		t.Errorf("1M reasoning tokens cost %v, want 25 (the output rate)", got.Cost)
	}
}

// TestForeignAliasPricesLongCacheFromBorrowedWriteRate pins that a borrowed
// catalog never gets Anthropic's 2x-input rule applied to its 1h cache tier.
func TestForeignAliasPricesLongCacheFromBorrowedWriteRate(t *testing.T) {
	rate := foreignRate(source.PricingRateSummary{
		InputPerMillion: 5.0, CachedInputPerMillion: 0.5, CacheWritePerMillion: 6.25, OutputPerMillion: 30.0,
	})
	if got := rate.cacheCreate1hPerMillion(); got != 6.25 {
		t.Errorf("1h cache-creation rate = %v, want 6.25 (the borrowed cache-write rate), not %v (2x input)", got, rate.InputPerMillion*2)
	}
}
