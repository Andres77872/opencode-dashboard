package stats

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSupportedPeriodPresetsCanonicalOrderAndMembership(t *testing.T) {
	want := []string{"1h", "6h", "12h", "24h", "72h", "1d", "7d", "14d", "30d", "1y", "all"}
	got := SupportedPeriodPresets()
	if !slices.Equal(got, want) {
		t.Fatalf("SupportedPeriodPresets() = %v, want %v", got, want)
	}
	if DefaultPeriodPreset != "7d" {
		t.Fatalf("DefaultPeriodPreset = %q, want 7d", DefaultPeriodPreset)
	}
	for _, period := range want {
		if !IsSupportedPeriodPreset(period) {
			t.Errorf("IsSupportedPeriodPreset(%q) = false, want true", period)
		}
	}
	for _, period := range []string{"", "7D", "90d", "week", " 7d "} {
		if IsSupportedPeriodPreset(period) {
			t.Errorf("IsSupportedPeriodPreset(%q) = true, want false", period)
		}
	}
}

func TestFrontendPeriodCatalogMatchesBackend(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "types", "api.ts"))
	if err != nil {
		t.Fatal(err)
	}
	quoted := make([]string, 0, len(SupportedPeriodPresets()))
	for _, preset := range SupportedPeriodPresets() {
		quoted = append(quoted, "'"+preset+"'")
	}
	want := "export const DAILY_PERIOD_VALUES = [" + strings.Join(quoted, ", ") + "] as const"
	if !strings.Contains(string(content), want) {
		t.Fatalf("frontend period catalog must exactly mirror the backend; want declaration %q", want)
	}
}

func TestSupportedPeriodPresetsReturnsDefensiveCopy(t *testing.T) {
	first := SupportedPeriodPresets()
	first[0] = "mutated"
	second := SupportedPeriodPresets()
	if second[0] != "1h" {
		t.Fatalf("mutating one catalog copy changed the shared catalog: %v", second)
	}
}

func TestEveryCatalogPresetHasRuntimeSemantics(t *testing.T) {
	for _, period := range SupportedPeriodPresets() {
		if hours, ok := HourPresetHours(period); ok {
			if hours <= 0 {
				t.Errorf("HourPresetHours(%q) = %d", period, hours)
			}
			continue
		}
		if _, err := parsePeriod(period); err != nil {
			t.Errorf("catalog preset %q has no calendar runtime semantics: %v", period, err)
		}
	}
}

func TestInvalidPeriodErrorUsesCanonicalCatalog(t *testing.T) {
	err := InvalidPeriodError("90d")
	if !errors.Is(err, ErrInvalidPeriod) {
		t.Fatalf("InvalidPeriodError() = %v, want ErrInvalidPeriod", err)
	}
	want := `invalid period: "90d" (supported presets: ` + strings.Join(SupportedPeriodPresets(), ", ") + `)`
	if err.Error() != want {
		t.Fatalf("InvalidPeriodError() = %q, want %q", err, want)
	}
}
