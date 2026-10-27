package analyticsagent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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

// TestProviderUpdateNeverSendsStoredKeyToNewHost guards the PATCH that used to
// keep the saved key while only base_url changed, so the automatic model
// refresh delivered "Bearer <key>" to whatever host the request named.
func TestProviderUpdateNeverSendsStoredKeyToNewHost(t *testing.T) {
	ctx := context.Background()
	settings, err := pricingalias.Open(ctx, filepath.Join(t.TempDir(), "dashboard-settings.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = settings.Close() })

	modelsHandler := func(seen *[]string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			*seen = append(*seen, r.Header.Get("Authorization"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
		}
	}
	var original, other []string
	originalServer := httptest.NewServer(modelsHandler(&original))
	t.Cleanup(originalServer.Close)
	otherServer := httptest.NewServer(modelsHandler(&other))
	t.Cleanup(otherServer.Close)

	registry := NewAssistantProviderRegistry(settings, nil, nil, nil)
	created, err := registry.Create(ctx, pricingalias.CreateAssistantProviderInput{Name: "Local", BaseURL: originalServer.URL + "/v1", APIKey: "SECRET_SENTINEL", InsecureTransportAck: true})
	if err != nil {
		t.Fatal(err)
	}

	moved := otherServer.URL + "/v1"
	if _, err := registry.Update(ctx, created.ID, pricingalias.UpdateAssistantProviderInput{BaseURL: &moved}); !errors.Is(err, pricingalias.ErrProviderKeyRequired) {
		t.Fatalf("Update(base_url only) err = %v, want ErrProviderKeyRequired", err)
	}
	for _, auth := range other {
		if strings.Contains(auth, "SECRET_SENTINEL") {
			t.Fatalf("stored key reached the new host: %q", auth)
		}
	}

	newKey := "NEW_KEY"
	if _, err := registry.Update(ctx, created.ID, pricingalias.UpdateAssistantProviderInput{BaseURL: &moved, APIKey: &newKey}); err != nil {
		t.Fatalf("Update(base_url + key) err = %v", err)
	}
	if len(other) == 0 || other[len(other)-1] != "Bearer NEW_KEY" {
		t.Fatalf("new host Authorization headers = %q, want the re-entered key only", other)
	}
}
