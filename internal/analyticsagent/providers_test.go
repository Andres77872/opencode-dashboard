package analyticsagent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"opencode-dashboard/internal/pricingalias"
)

func TestProviderListEncodesEmptyModelCatalogsAsArrays(t *testing.T) {
	ctx := context.Background()
	settings, err := pricingalias.Open(ctx, filepath.Join(t.TempDir(), "dashboard-settings.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = settings.Close() })

	registry := NewAssistantProviderRegistry(settings, nil, nil, nil)
	response, err := registry.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Providers) != 2 {
		t.Fatalf("providers = %d, want 2 built-ins", len(response.Providers))
	}
	for _, provider := range response.Providers {
		if provider.Models == nil {
			t.Errorf("provider %q models is nil, want an empty slice", provider.ID)
		}
	}

	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"models":null`) {
		t.Fatalf("provider response contains a null model catalog: %s", encoded)
	}
}
