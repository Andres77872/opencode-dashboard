package cache

import (
	"context"
	"testing"
	"time"

	"opencode-dashboard/internal/source"
)

type pricingCapableLiveSource struct {
	*syncFakeSource
	catalog       source.PricingCatalog
	resolution    source.PricingResolution
	invalidations int
}

func (s *pricingCapableLiveSource) PricingCatalog(context.Context) source.PricingCatalog {
	return s.catalog
}

func (s *pricingCapableLiveSource) ResolvePricing(context.Context, string, string) source.PricingResolution {
	return s.resolution
}

func (s *pricingCapableLiveSource) Invalidate() {
	s.invalidations++
}

func TestCachedSourceForwardsPricingInterfacesAndClearsGapMemo(t *testing.T) {
	live := &pricingCapableLiveSource{
		syncFakeSource: &syncFakeSource{id: "pricing-forward-test"},
		catalog: source.PricingCatalog{
			SourceID:   "pricing-forward-test",
			SnapshotID: "pricing-v1+aliases-rev",
			Currency:   "USD",
			Models:     []source.PricingCatalogModel{{ModelID: "canonical"}},
		},
		resolution: source.PricingResolution{
			SourceID:      "pricing-forward-test",
			ProviderID:    "provider",
			ModelID:       "observed",
			TargetModelID: "canonical",
			Kind:          source.PricingResolutionUserAlias,
		},
	}
	cached := WrapSource(nil, live)
	cached.gapRaw = gapData{msgs: nil}
	cached.gapFrom = time.Now().Add(-time.Hour)
	cached.gapAt = time.Now()
	generation := cached.gapGeneration

	if got := cached.PricingCatalog(context.Background()); got.SnapshotID != live.catalog.SnapshotID || len(got.Models) != 1 {
		t.Fatalf("forwarded pricing catalog = %#v", got)
	}
	if got := cached.ResolvePricing(context.Background(), "provider", "observed"); got.Kind != source.PricingResolutionUserAlias || got.TargetModelID != "canonical" {
		t.Fatalf("forwarded pricing resolution = %#v", got)
	}

	cached.Invalidate()
	if live.invalidations != 1 {
		t.Fatalf("live invalidations = %d, want 1", live.invalidations)
	}
	cached.gapMu.Lock()
	defer cached.gapMu.Unlock()
	if !cached.gapAt.IsZero() || !cached.gapFrom.IsZero() || cached.gapGeneration != generation+1 {
		t.Fatalf("gap memo was not cleared: from=%v at=%v generation=%d", cached.gapFrom, cached.gapAt, cached.gapGeneration)
	}
}

func TestCachedSourcePricingFallbackIsUnavailable(t *testing.T) {
	cached := WrapSource(nil, &syncFakeSource{id: "no-pricing-interface"})
	catalog := cached.PricingCatalog(context.Background())
	if catalog.SourceID != "no-pricing-interface" || len(catalog.Models) != 0 {
		t.Fatalf("fallback catalog = %#v", catalog)
	}
	resolution := cached.ResolvePricing(context.Background(), "provider", "observed")
	if resolution.SourceID != "no-pricing-interface" || resolution.ProviderID != "provider" || resolution.ModelID != "observed" || resolution.Kind != source.PricingResolutionUnavailable {
		t.Fatalf("fallback pricing resolution = %#v", resolution)
	}
}
