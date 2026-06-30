package claudecode

import (
	"math"
	"testing"

	"opencode-dashboard/internal/stats"
)

func TestCostProvenanceForReportedComputedApproximateAndMissingMessages(t *testing.T) {
	src := newFixtureSource(t, "valid_home")
	list := readAllMessages(t, src)

	tests := []struct {
		name              string
		sessionID         string
		wantStatus        stats.CostStatus
		wantCost          float64
		allowAnyPositive  bool
		wantSnapshotID    bool
		wantMissingCount  int64
		wantComputedCount int64
		wantReportedCount int64
	}{
		{
			name:              "reported cost is preserved exactly",
			sessionID:         "reported-session",
			wantStatus:        stats.CostReported,
			wantCost:          0.042,
			wantReportedCount: 1,
		},
		{
			name:              "computed cost uses fixture pricing snapshot",
			sessionID:         "computed-session",
			wantStatus:        stats.CostComputed,
			allowAnyPositive:  true,
			wantSnapshotID:    true,
			wantComputedCount: 1,
		},
		{
			name:              "approximate cost is labeled",
			sessionID:         "approximate-missing-session",
			wantStatus:        stats.CostApproximate,
			allowAnyPositive:  true,
			wantSnapshotID:    true,
			wantComputedCount: 1,
		},
		{
			name:             "missing cost remains unknown with zero compatibility value",
			sessionID:        "approximate-missing-session",
			wantStatus:       stats.CostMissing,
			wantCost:         0,
			wantMissingCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := findMessage(t, list, func(msg stats.MessageEntry) bool {
				if msg.SessionID != tt.sessionID || msg.Role != "assistant" {
					return false
				}
				if tt.sessionID == "approximate-missing-session" {
					if tt.wantStatus == stats.CostApproximate {
						return msg.ModelID != ""
					}
					return msg.ModelID == ""
				}
				return true
			})

			if msg.CostStatus != tt.wantStatus {
				t.Fatalf("message cost status = %q, want %q", msg.CostStatus, tt.wantStatus)
			}
			if msg.CostProvenance == nil {
				t.Fatalf("message cost provenance is nil")
			}
			if msg.CostProvenance.Status != tt.wantStatus {
				t.Errorf("provenance status = %q, want %q", msg.CostProvenance.Status, tt.wantStatus)
			}
			if tt.allowAnyPositive {
				if msg.Cost <= 0 {
					t.Errorf("computed/approximate cost = %v, want positive", msg.Cost)
				}
			} else if math.Abs(msg.Cost-tt.wantCost) > 0.0000001 {
				t.Errorf("cost = %.9f, want %.9f", msg.Cost, tt.wantCost)
			}
			if tt.wantSnapshotID && msg.CostProvenance.PricingSnapshotID != "anthropic-test-2026-01-02" {
				t.Errorf("pricing snapshot = %q, want anthropic-test-2026-01-02", msg.CostProvenance.PricingSnapshotID)
			}
			if msg.CostProvenance.MissingCount != tt.wantMissingCount {
				t.Errorf("missing count = %d, want %d", msg.CostProvenance.MissingCount, tt.wantMissingCount)
			}
			if msg.CostProvenance.ComputedCount != tt.wantComputedCount {
				t.Errorf("computed count = %d, want %d", msg.CostProvenance.ComputedCount, tt.wantComputedCount)
			}
			if msg.CostProvenance.ReportedCount != tt.wantReportedCount {
				t.Errorf("reported count = %d, want %d", msg.CostProvenance.ReportedCount, tt.wantReportedCount)
			}
		})
	}
}

func TestAggregateCostProvenanceIsMixedAndNeverPretendsMissingIsRealZero(t *testing.T) {
	src := newFixtureSource(t, "valid_home")
	overview, err := src.Overview(testContext(t), stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatalf("Overview(all) failed: %v", err)
	}

	assertAllSourceID(t, overview.SourceID)
	if overview.Cost <= 0 {
		t.Errorf("Overview().Cost = %v, want positive sum of known reported/computed/approximate costs", overview.Cost)
	}
	if overview.CostStatus != stats.CostMixed {
		t.Errorf("Overview().CostStatus = %q, want %q", overview.CostStatus, stats.CostMixed)
	}
	if overview.CostProvenance == nil {
		t.Fatalf("Overview().CostProvenance = nil, want aggregate provenance")
	}
	if overview.CostProvenance.Status != stats.CostMixed {
		t.Errorf("aggregate provenance status = %q, want mixed", overview.CostProvenance.Status)
	}
	if overview.CostProvenance.ReportedCount == 0 {
		t.Errorf("aggregate reported count = 0, want reported fixture counted")
	}
	if overview.CostProvenance.ComputedCount == 0 {
		t.Errorf("aggregate computed count = 0, want computed/approximate fixtures counted")
	}
	if overview.CostProvenance.MissingCount == 0 {
		t.Errorf("aggregate missing count = 0, want missing-cost fixture counted")
	}
	if overview.CostProvenance.Currency != "USD" {
		t.Errorf("aggregate currency = %q, want USD", overview.CostProvenance.Currency)
	}
}

func TestBundledPricingSnapshotCoversCurrentClaudeRates(t *testing.T) {
	pricing := loadBundledPricingForTest(t)

	tests := []struct {
		name            string
		key             string
		want            pricingRate
		wantApproximate bool
	}{
		{
			name: "current fable 5 exact rate uses current price",
			key:  "claude-fable-5",
			want: pricingRate{
				InputPerMillion:         10.0,
				OutputPerMillion:        50.0,
				CacheReadPerMillion:     1.0,
				CacheCreatePerMillion:   12.5,
				CacheCreate1hPerMillion: 20.0,
			},
		},
		{
			name: "current opus 4.8 exact rate uses current price",
			key:  "claude-opus-4-8",
			want: pricingRate{
				InputPerMillion:         5.0,
				OutputPerMillion:        25.0,
				CacheReadPerMillion:     0.5,
				CacheCreatePerMillion:   6.25,
				CacheCreate1hPerMillion: 10.0,
			},
		},
		{
			name: "current opus family fallback uses current price",
			key:  "claude-opus-4",
			want: pricingRate{
				InputPerMillion:         5.0,
				OutputPerMillion:        25.0,
				CacheReadPerMillion:     0.5,
				CacheCreatePerMillion:   6.25,
				CacheCreate1hPerMillion: 10.0,
			},
			wantApproximate: true,
		},
		{
			name: "current sonnet 4.6 exact rate",
			key:  "claude-sonnet-4-6",
			want: pricingRate{
				InputPerMillion:         3.0,
				OutputPerMillion:        15.0,
				CacheReadPerMillion:     0.3,
				CacheCreatePerMillion:   3.75,
				CacheCreate1hPerMillion: 6.0,
			},
		},
		{
			name: "current sonnet family fallback rate",
			key:  "claude-sonnet-4",
			want: pricingRate{
				InputPerMillion:         3.0,
				OutputPerMillion:        15.0,
				CacheReadPerMillion:     0.3,
				CacheCreatePerMillion:   3.75,
				CacheCreate1hPerMillion: 6.0,
			},
			wantApproximate: true,
		},
		{
			name: "current haiku 4.5 exact rate",
			key:  "claude-haiku-4-5-20251001",
			want: pricingRate{
				InputPerMillion:         1.0,
				OutputPerMillion:        5.0,
				CacheReadPerMillion:     0.1,
				CacheCreatePerMillion:   1.25,
				CacheCreate1hPerMillion: 2.0,
			},
		},
		{
			name: "current haiku family fallback rate",
			key:  "claude-haiku-4",
			want: pricingRate{
				InputPerMillion:         1.0,
				OutputPerMillion:        5.0,
				CacheReadPerMillion:     0.1,
				CacheCreatePerMillion:   1.25,
				CacheCreate1hPerMillion: 2.0,
			},
			wantApproximate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := pricing.Models[tt.key]
			if !ok {
				t.Fatalf("bundled pricing missing %q", tt.key)
			}
			assertPricingRate(t, tt.key, got, tt.want)
			if got.Approximate != tt.wantApproximate {
				t.Errorf("%s approximate = %v, want %v", tt.key, got.Approximate, tt.wantApproximate)
			}
			if tt.key == "claude-opus-4-8" && (got.InputPerMillion == 15.0 || got.OutputPerMillion == 75.0) {
				t.Errorf("%s uses deprecated Opus input/output pricing %.2f/%.2f, want 5.00/25.00", tt.key, got.InputPerMillion, got.OutputPerMillion)
			}
		})
	}
}

func TestBundledPricingSnapshotKeepsDeprecatedOpusSeparateWhenRepresented(t *testing.T) {
	pricing := loadBundledPricingForTest(t)
	deprecatedRate := pricingRate{
		InputPerMillion:         15.0,
		OutputPerMillion:        75.0,
		CacheReadPerMillion:     1.5,
		CacheCreatePerMillion:   18.75,
		CacheCreate1hPerMillion: 30.0,
	}

	tests := []struct {
		name string
		key  string
	}{
		{name: "original opus 4 dated model remains deprecated", key: "claude-opus-4-20250514"},
		{name: "opus 4.1 dated model remains deprecated", key: "claude-opus-4-1-20250805"},
		{name: "opus 4.1 family fallback remains deprecated", key: "claude-opus-4-1"},
	}

	found := false
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := pricing.Models[tt.key]
			if !ok {
				t.Skipf("deprecated Opus key %q not represented in bundled pricing snapshot", tt.key)
			}
			found = true
			assertPricingRate(t, tt.key, got, deprecatedRate)
			if got.InputPerMillion == 5.0 || got.OutputPerMillion == 25.0 {
				t.Errorf("%s collapsed onto current Opus pricing %.2f/%.2f, want deprecated 15.00/75.00", tt.key, got.InputPerMillion, got.OutputPerMillion)
			}
		})
	}
	if !found {
		t.Skip("no deprecated Opus rate represented in bundled pricing snapshot")
	}
}

func TestBundledPricingRealClaudeModelsComputeNonMissingCosts(t *testing.T) {
	pricing := loadBundledPricingForTest(t)
	usage := tokenUsage{Input: 1_000_000, Output: 1_000_000}

	tests := []struct {
		name     string
		model    string
		wantCost float64
	}{
		{name: "opus 4.8 uses current input output price", model: "claude-opus-4-8", wantCost: 30.0},
		{name: "sonnet 5 computes non-missing", model: "claude-sonnet-5", wantCost: 12.0},
		{name: "sonnet 4.6 computes non-missing", model: "claude-sonnet-4-6", wantCost: 18.0},
		{name: "haiku 4.5 dated model computes non-missing", model: "claude-haiku-4-5-20251001", wantCost: 6.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := computeCost(tt.model, usage, true, nil, pricing)
			if result.Status == stats.CostMissing {
				t.Fatalf("computeCost(%q) status = missing, want computed or approximate", tt.model)
			}
			if result.Status != stats.CostComputed && result.Status != stats.CostApproximate {
				t.Errorf("computeCost(%q) status = %q, want computed or approximate", tt.model, result.Status)
			}
			if result.Cost <= 0 {
				t.Errorf("computeCost(%q) cost = %.9f, want non-zero", tt.model, result.Cost)
			}
			if !approxEqual(result.Cost, tt.wantCost) {
				t.Errorf("computeCost(%q) cost = %.9f, want %.9f", tt.model, result.Cost, tt.wantCost)
			}
			if tt.model == "claude-opus-4-8" && approxEqual(result.Cost, 90.0) {
				t.Errorf("claude-opus-4-8 computed with deprecated 15/75 input/output rates; want current 5/25 rates")
			}
			if result.Provenance == nil {
				t.Fatalf("computeCost(%q) provenance = nil", tt.model)
			}
			if result.Provenance.MissingCount != 0 {
				t.Errorf("computeCost(%q) missing count = %d, want 0", tt.model, result.Provenance.MissingCount)
			}
		})
	}
}

func TestPricingLookupMatchesOnlyBoundaryAwarePrefixes(t *testing.T) {
	pricing := pricingSnapshot{
		Currency: "USD",
		Models: map[string]pricingRate{
			"claude-opus-4":   {InputPerMillion: 5.0},
			"claude-opus-4-1": {InputPerMillion: 15.0},
		},
	}

	tests := []struct {
		name      string
		model     string
		wantOK    bool
		wantExact bool
		wantInput float64
	}{
		{name: "exact family key matches exactly", model: "claude-opus-4", wantOK: true, wantExact: true, wantInput: 5.0},
		{name: "hyphen child matches family prefix", model: "claude-opus-4-8", wantOK: true, wantInput: 5.0},
		{name: "longest hyphen child prefix wins", model: "claude-opus-4-1-20250805", wantOK: true, wantInput: 15.0},
		{name: "adjacent digit does not match family prefix", model: "claude-opus-40", wantOK: false},
		{name: "adjacent letter does not match family prefix", model: "claude-opus-4x", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, exact, ok := pricing.lookup(tt.model)
			if ok != tt.wantOK {
				t.Fatalf("lookup(%q) ok = %v, want %v", tt.model, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if exact != tt.wantExact {
				t.Errorf("lookup(%q) exact = %v, want %v", tt.model, exact, tt.wantExact)
			}
			if got.InputPerMillion != tt.wantInput {
				t.Errorf("lookup(%q) input rate = %.2f, want %.2f", tt.model, got.InputPerMillion, tt.wantInput)
			}
		})
	}
}

func TestComputeCostBillsCacheCreationFiveMinuteAndOneHourSeparately(t *testing.T) {
	pricing := pricingSnapshot{
		ID:       "cache-split-test",
		Currency: "USD",
		Models: map[string]pricingRate{
			"claude-cache-test": {
				CacheCreatePerMillion:   6.25,
				CacheCreate1hPerMillion: 10.0,
			},
		},
	}

	tests := []struct {
		name     string
		usage    tokenUsage
		wantCost float64
	}{
		{
			name:     "nested split bills five minute and one hour cache writes differently",
			usage:    tokenUsage{CacheCreate: 2_000_000, CacheCreate5m: 1_000_000, CacheCreate1h: 1_000_000},
			wantCost: 16.25,
		},
		{
			name:     "aggregate only cache creation remains five minute default",
			usage:    tokenUsage{CacheCreate: 2_000_000},
			wantCost: 12.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := computeCost("claude-cache-test", tt.usage, true, nil, pricing)
			if result.Status != stats.CostComputed {
				t.Errorf("CostStatus = %q, want %q", result.Status, stats.CostComputed)
			}
			if !approxEqual(result.Cost, tt.wantCost) {
				t.Errorf("Cost = %.9f, want %.9f", result.Cost, tt.wantCost)
			}
		})
	}
}

func TestCacheCreationSplitKeepsPublicCacheWriteAggregate(t *testing.T) {
	src := newTempRegressionSource(t, map[string][]string{
		"-repo-fixture-cache/cache-split-session.jsonl": cacheCreationSplitLines(),
	})
	ctx := testContext(t)
	period := stats.PeriodQuery{Period: "all"}

	messages, err := src.Messages(ctx, period, 1, 100, chronologicalMessageSort())
	if err != nil {
		t.Fatalf("Messages(all) failed: %v", err)
	}
	// 1 user prompt row + 1 assistant API-request row.
	if messages.Total != 2 || len(messages.Messages) != 2 {
		t.Fatalf("Messages total/len = %d/%d, want 2/2", messages.Total, len(messages.Messages))
	}
	entry := findMessage(t, messages, func(m stats.MessageEntry) bool { return m.Role == "assistant" })
	if entry.Tokens == nil {
		t.Fatalf("assistant row Tokens = nil, want aggregate cache write tokens")
	}
	if entry.Tokens.Cache.Write != 30 {
		t.Errorf("assistant row Tokens.Cache.Write = %d, want aggregate cache_creation_input_tokens 30", entry.Tokens.Cache.Write)
	}
	wantCost := (float64(10)*3.75 + float64(20)*6.0) / 1_000_000
	if !approxEqual(entry.Cost, wantCost) {
		t.Errorf("assistant row Cost = %.9f, want %.9f from 10 five-minute + 20 one-hour cache write tokens", entry.Cost, wantCost)
	}
	if entry.CostStatus != stats.CostComputed {
		t.Errorf("assistant row CostStatus = %q, want %q", entry.CostStatus, stats.CostComputed)
	}

	overview, err := src.Overview(ctx, period)
	if err != nil {
		t.Fatalf("Overview(all) failed: %v", err)
	}
	if overview.Tokens.Cache.Write != 30 {
		t.Errorf("Overview().Tokens.Cache.Write = %d, want aggregate cache_creation_input_tokens 30", overview.Tokens.Cache.Write)
	}
}

// TestClaudeCostCumulativeTotalCostUSDIsIgnoredInFavorOfPerRequestTokenCost
// documents the per-request model: each assistant record is its own API-request
// row, the cumulative total_cost_usd field is ignored when usage tokens are present
// (to avoid overcounting), per-request cost is computed from tokens, and per-request
// usage sums normally across requests.
func TestClaudeCostCumulativeTotalCostUSDIsIgnoredInFavorOfPerRequestTokenCost(t *testing.T) {
	src := newTempRegressionSource(t, map[string][]string{
		"-home-andres-projects-cost/cumulative-total-cost-session.jsonl": cumulativeTotalCostUSDLines(),
	})
	ctx := testContext(t)
	period := stats.PeriodQuery{Period: "all"}

	// Per-request token cost (cumulative total_cost_usd ignored):
	//   a1 (100/10/cr5/cc7) = (100*3+10*15+5*0.3+7*3.75)/1e6 = 477.75/1e6
	//   a2 (300/30/cr15/cc17) = (300*3+30*15+15*0.3+17*3.75)/1e6 = 1418.25/1e6
	//   a3 (600/60/cr25/cc27) = (600*3+60*15+25*0.3+27*3.75)/1e6 = 2808.75/1e6
	wantCost := (477.75 + 1418.25 + 2808.75) / 1_000_000

	overview, err := src.Overview(ctx, period)
	if err != nil {
		t.Fatalf("Overview(all) failed: %v", err)
	}
	// 1 user prompt + 3 distinct assistant API-request rows (no shared requestId/message id).
	if overview.Messages != 4 {
		t.Errorf("Overview().Messages = %d, want 4 (1 user + 3 assistant API requests)", overview.Messages)
	}
	if !approxEqual(overview.Cost, wantCost) {
		t.Errorf("Overview().Cost = %.9f, want %.9f per-request token cost (cumulative total_cost_usd ignored)", overview.Cost, wantCost)
	}
	assertCumulativeSummedTokens(t, "Overview().Tokens", overview.Tokens)

	messages, err := src.Messages(ctx, period, 1, 100, chronologicalMessageSort())
	if err != nil {
		t.Fatalf("Messages(all) failed: %v", err)
	}
	if messages.Total != 4 || len(messages.Messages) != 4 {
		t.Fatalf("Messages total/len = %d/%d, want 4/4", messages.Total, len(messages.Messages))
	}
	// Each assistant row is computed (not reported) because the cumulative reported
	// value is ignored when usage is present.
	for _, msg := range messages.Messages {
		if msg.Role != "assistant" {
			continue
		}
		if msg.CostStatus != stats.CostComputed {
			t.Errorf("assistant row %q CostStatus = %q, want %q (token-computed)", msg.ID, msg.CostStatus, stats.CostComputed)
		}
		if msg.CostProvenance == nil {
			t.Fatalf("assistant row %q CostProvenance = nil, want computed provenance", msg.ID)
		}
		if msg.CostProvenance.ReportedCount != 0 {
			t.Errorf("assistant row %q ReportedCount = %d, want 0; cumulative total_cost_usd must not be reported", msg.ID, msg.CostProvenance.ReportedCount)
		}
	}

	session, err := src.SessionByID(ctx, "cumulative-total-cost-session")
	if err != nil {
		t.Fatalf("SessionByID(cumulative-total-cost-session) failed: %v", err)
	}
	if session == nil {
		t.Fatalf("SessionByID(cumulative-total-cost-session) = nil, want session detail")
	}
	if !approxEqual(session.TotalCost, wantCost) {
		t.Errorf("SessionByID().TotalCost = %.9f, want %.9f per-request token cost", session.TotalCost, wantCost)
	}
	assertCumulativeSummedTokens(t, "SessionByID().TotalTokens", session.TotalTokens)

	models, err := src.Models(ctx, period)
	if err != nil {
		t.Fatalf("Models(all) failed: %v", err)
	}
	model := findModelEntryByID(t, models, "claude-test-computed")
	if model.Messages != 3 {
		t.Errorf("Models()[claude-test-computed].Messages = %d, want 3 assistant API requests", model.Messages)
	}
	if !approxEqual(model.Cost, wantCost) {
		t.Errorf("Models()[claude-test-computed].Cost = %.9f, want %.9f per-request token cost", model.Cost, wantCost)
	}
	assertCumulativeSummedTokens(t, "Models()[claude-test-computed].Tokens", model.Tokens)
}

func TestClaudeCostExtraAssistantUsageKeysAreNotReportedCost(t *testing.T) {
	tests := []struct {
		name              string
		sessionID         string
		usageJSON         string
		wantStatus        stats.CostStatus
		wantComputedCount int64
		wantMissingCount  int64
		wantPositiveCost  bool
	}{
		{
			name:              "supported token fields with extra usage keys remain computed not reported",
			sessionID:         "extra-usage-computed-session",
			usageJSON:         `{"input_tokens":100,"output_tokens":25,"cache_read_input_tokens":5,"cache_creation_input_tokens":7,"cache_creation":{"ephemeral_5m_input_tokens":999999},"server_tool_use":{"web_search_requests":42},"service_tier":"priority","inference_geo":"eu","iterations":99,"speed":"turbo"}`,
			wantStatus:        stats.CostComputed,
			wantComputedCount: 1,
			wantPositiveCost:  true,
		},
		{
			name:             "only non-token extra usage keys remain missing not reported",
			sessionID:        "extra-usage-missing-session",
			usageJSON:        `{"server_tool_use":{"web_search_requests":42},"service_tier":"priority","inference_geo":"eu","iterations":99,"speed":"turbo"}`,
			wantStatus:       stats.CostMissing,
			wantMissingCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := newTempRegressionSource(t, map[string][]string{
				"-home-andres-projects-cost/" + tt.sessionID + ".jsonl": extraUsageKeyLines(tt.sessionID, tt.usageJSON),
			})
			messages := readAllMessages(t, src)
			// 1 user prompt row + 1 assistant API-request row.
			if messages.Total != 2 || len(messages.Messages) != 2 {
				t.Fatalf("Messages total/len = %d/%d, want 2/2", messages.Total, len(messages.Messages))
			}
			entry := findMessage(t, messages, func(m stats.MessageEntry) bool { return m.Role == "assistant" })
			if entry.CostStatus != tt.wantStatus {
				t.Errorf("CostStatus = %q, want %q", entry.CostStatus, tt.wantStatus)
			}
			if entry.CostProvenance == nil {
				t.Fatalf("CostProvenance = nil, want provenance")
			}
			if entry.CostProvenance.ReportedCount != 0 {
				t.Errorf("ReportedCount = %d, want 0; extra usage keys must not be mistaken for reported cost fields", entry.CostProvenance.ReportedCount)
			}
			if entry.CostProvenance.ComputedCount != tt.wantComputedCount {
				t.Errorf("ComputedCount = %d, want %d", entry.CostProvenance.ComputedCount, tt.wantComputedCount)
			}
			if entry.CostProvenance.MissingCount != tt.wantMissingCount {
				t.Errorf("MissingCount = %d, want %d", entry.CostProvenance.MissingCount, tt.wantMissingCount)
			}
			if tt.wantPositiveCost {
				if entry.Cost <= 0 {
					t.Errorf("Cost = %.9f, want positive computed cost from supported token fields", entry.Cost)
				}
			} else if entry.Cost != 0 {
				t.Errorf("Cost = %.9f, want 0 compatibility value for missing cost", entry.Cost)
			}
		})
	}
}

func loadBundledPricingForTest(t *testing.T) pricingSnapshot {
	t.Helper()
	src := New(Options{
		ClaudeHome: t.TempDir(),
		PathSource: "bundled pricing test fixture",
	})
	pricing := src.loadPricing(testContext(t))
	if pricing.ID == "" {
		t.Fatalf("bundled pricing snapshot ID is empty")
	}
	if len(pricing.Models) == 0 {
		t.Fatalf("bundled pricing snapshot has no models")
	}
	return pricing
}

func assertPricingRate(t *testing.T, key string, got pricingRate, want pricingRate) {
	t.Helper()
	if got.InputPerMillion != want.InputPerMillion {
		t.Errorf("%s input_per_million = %.2f, want %.2f", key, got.InputPerMillion, want.InputPerMillion)
	}
	if got.OutputPerMillion != want.OutputPerMillion {
		t.Errorf("%s output_per_million = %.2f, want %.2f", key, got.OutputPerMillion, want.OutputPerMillion)
	}
	if got.CacheReadPerMillion != want.CacheReadPerMillion {
		t.Errorf("%s cache_read_input_per_million = %.2f, want %.2f", key, got.CacheReadPerMillion, want.CacheReadPerMillion)
	}
	if got.CacheCreatePerMillion != want.CacheCreatePerMillion {
		t.Errorf("%s cache_creation_input_per_million = %.2f, want %.2f", key, got.CacheCreatePerMillion, want.CacheCreatePerMillion)
	}
	if got.cacheCreate1hPerMillion() != want.CacheCreate1hPerMillion {
		t.Errorf("%s cache_creation_input_1h_per_million = %.2f, want %.2f", key, got.cacheCreate1hPerMillion(), want.CacheCreate1hPerMillion)
	}
}

func cacheCreationSplitLines() []string {
	return []string{
		`{"type":"user","uuid":"cache-split-user","session_id":"cache-split-session","timestamp":"2026-04-04T10:00:00Z","cwd":"/repo/fixture/cache","message":{"role":"user","content":"Prove cache creation split pricing keeps public aggregate token semantics."}}`,
		`{"type":"assistant","uuid":"cache-split-assistant","session_id":"cache-split-session","timestamp":"2026-04-04T10:00:01Z","cwd":"/repo/fixture/cache","message":{"role":"assistant","model":"claude-test-computed","content":[{"type":"text","text":"Cache writes split internally but stay aggregate publicly."}],"usage":{"cache_creation_input_tokens":30,"cache_creation":{"ephemeral_5m_input_tokens":10,"ephemeral_1h_input_tokens":20}}}}`,
	}
}

func cumulativeTotalCostUSDLines() []string {
	return []string{
		`{"type":"user","uuid":"cumulative-total-cost-user","session_id":"cumulative-total-cost-session","timestamp":"2026-04-02T10:00:00Z","cwd":"/home/andres/projects/cost","message":{"role":"user","content":"Use cumulative total cost fields without double counting."}}`,
		`{"type":"assistant","uuid":"cumulative-total-cost-assistant-1","session_id":"cumulative-total-cost-session","timestamp":"2026-04-02T10:00:01Z","cwd":"/home/andres/projects/cost","total_cost_usd":1,"message":{"role":"assistant","model":"claude-test-computed","content":[{"type":"text","text":"First cumulative call."}],"usage":{"input_tokens":100,"output_tokens":10,"cache_read_input_tokens":5,"cache_creation_input_tokens":7}}}`,
		`{"type":"assistant","uuid":"cumulative-total-cost-assistant-2","session_id":"cumulative-total-cost-session","timestamp":"2026-04-02T10:00:02Z","cwd":"/home/andres/projects/cost","total_cost_usd":3,"message":{"role":"assistant","model":"claude-test-computed","content":[{"type":"text","text":"Second cumulative call."}],"usage":{"input_tokens":300,"output_tokens":30,"cache_read_input_tokens":15,"cache_creation_input_tokens":17}}}`,
		`{"type":"assistant","uuid":"cumulative-total-cost-assistant-3","session_id":"cumulative-total-cost-session","timestamp":"2026-04-02T10:00:03Z","cwd":"/home/andres/projects/cost","total_cost_usd":6,"message":{"role":"assistant","model":"claude-test-computed","content":[{"type":"text","text":"Final cumulative call."}],"usage":{"input_tokens":600,"output_tokens":60,"cache_read_input_tokens":25,"cache_creation_input_tokens":27}}}`,
	}
}

func extraUsageKeyLines(sessionID string, usageJSON string) []string {
	return []string{
		`{"type":"user","uuid":"` + sessionID + `-user","session_id":"` + sessionID + `","timestamp":"2026-04-03T10:00:00Z","cwd":"/home/andres/projects/cost","message":{"role":"user","content":"Prove extra usage keys are not reported cost fields."}}`,
		`{"type":"assistant","uuid":"` + sessionID + `-assistant","session_id":"` + sessionID + `","timestamp":"2026-04-03T10:00:01Z","cwd":"/home/andres/projects/cost","message":{"role":"assistant","model":"claude-test-computed","content":[{"type":"text","text":"Usage extras tolerated without reported cost."}],"usage":` + usageJSON + `}}`,
	}
}

func assertCumulativeSummedTokens(t *testing.T, label string, got stats.TokenStats) {
	t.Helper()
	if got.Input != 1000 || got.Output != 100 || got.Cache.Read != 45 || got.Cache.Write != 51 {
		t.Errorf("%s = input/output/cache.read/cache.write %d/%d/%d/%d, want summed 1000/100/45/51", label, got.Input, got.Output, got.Cache.Read, got.Cache.Write)
	}
}
