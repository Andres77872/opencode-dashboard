package pricingalias

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssistantProviderCRUDSelectionAndSecretSafety(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "dashboard")
	path := filepath.Join(dir, "dashboard-settings.sqlite")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("settings dir mode=%v err=%v", info.Mode().Perm(), err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("settings db mode=%v err=%v", info.Mode().Perm(), err)
	}

	created, err := store.CreateAssistantProvider(ctx, CreateAssistantProviderInput{Name: "Local", BaseURL: "http://127.0.0.1:8080/v1", APIKey: "SECRET_SENTINEL", InsecureTransportAck: true})
	if err != nil {
		t.Fatal(err)
	}
	if !created.APIKeyConfigured || created.APIKey != "SECRET_SENTINEL" {
		t.Fatalf("created=%#v", created)
	}
	encoded, err := json.Marshal(created)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "SECRET_SENTINEL") || strings.Contains(string(encoded), "api_key\"") {
		t.Fatalf("secret disclosed: %s", encoded)
	}

	model, err := store.PutAssistantModel(ctx, created.ID, AssistantModel{ID: "local-model", ContextLimit: 32768})
	if err != nil || model.ContextLimit != 32768 || model.Verified {
		t.Fatalf("model=%#v err=%v", model, err)
	}
	selection, err := store.SetAssistantSelection(ctx, created.ID, model.ID)
	if err != nil || selection.Revision != 1 {
		t.Fatalf("selection=%#v err=%v", selection, err)
	}

	newName := "Renamed"
	updated, err := store.UpdateAssistantProvider(ctx, created.ID, UpdateAssistantProviderInput{Name: &newName})
	if err != nil || updated.APIKey != "SECRET_SENTINEL" {
		t.Fatalf("omitted key was not preserved: %#v err=%v", updated, err)
	}
	updated, err = store.UpdateAssistantProvider(ctx, created.ID, UpdateAssistantProviderInput{ClearAPIKey: true})
	if err != nil || updated.APIKeyConfigured || updated.APIKey != "" {
		t.Fatalf("key was not cleared: %#v err=%v", updated, err)
	}
	if updatedSelection, err := store.AssistantSelection(ctx); err != nil || updatedSelection.Revision <= selection.Revision {
		t.Fatalf("revision not invalidated: %#v err=%v", updatedSelection, err)
	}

	if err := store.DeleteAssistantProvider(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	cleared, err := store.AssistantSelection(ctx)
	if err != nil || cleared.ProviderID != "" || cleared.ModelID != "" {
		t.Fatalf("selection not cleared: %#v err=%v", cleared, err)
	}
}

func TestAssistantCatalogFailureKeepsLastGoodModelsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "dashboard-settings.sqlite")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := store.CreateAssistantProvider(ctx, CreateAssistantProviderInput{Name: "Remote", BaseURL: "https://example.test/v1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceAssistantCatalog(ctx, provider.ID, []AssistantModel{{ID: "verified-model", ContextLimit: 65536}}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordAssistantCatalogFailure(ctx, provider.ID, "safe discovery failure"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	loaded, err := store.GetAssistantProvider(ctx, provider.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Catalog.Status != "error" || len(loaded.Models) != 1 || loaded.Models[0].ID != "verified-model" {
		t.Fatalf("loaded=%#v", loaded)
	}
}
