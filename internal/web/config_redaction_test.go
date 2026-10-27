package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/source/opencode"
	"opencode-dashboard/internal/stats"
	"opencode-dashboard/internal/store"
	"opencode-dashboard/internal/store/fixture"
)

func TestConfigEndpointRedactsMergedSecretContainers(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	dir := filepath.Join(configHome, "opencode")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"opencode.json": `{"model":"custom/model","provider":{"custom":{"options":{"headers":{"Authorization":"base-secret","X-Api-Key":"header-secret"}}}},"mcp":{"local":{"environment":{"DATABASE_URL":"environment-secret"}}},"items":[[{"refresh_token":"refresh-secret","name":"visible"}]],"maxTokens":4096}`,
		"opencode.jsonc": `{// highest precedence
			"provider":{"custom":{"options":{"headers":{"Authorization":"override-secret"}}}}
		}`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	dbPath, err := fixture.NewBuilder().Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(dbPath)) })
	st, err := store.Connect(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	registry := source.NewRegistry(source.SourceOpenCode)
	t.Cleanup(func() { _ = registry.Close() })
	if err := registry.Register(opencode.New(st)); err != nil {
		_ = st.Close()
		t.Fatal(err)
	}
	server := NewServer(ServerOptions{Registry: registry})
	rec := httptest.NewRecorder()
	server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, testOrigin+"/api/v1/config?source=opencode", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, secret := range []string{"base-secret", "override-secret", "header-secret", "environment-secret", "refresh-secret"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Errorf("config endpoint exposed %q", secret)
		}
	}
	var view stats.ConfigView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(view.Raw), &raw); err != nil {
		t.Fatal(err)
	}
	for _, preview := range []map[string]any{view.Content, raw} {
		if preview["model"] != "custom/model" || preview["maxTokens"] != float64(4096) {
			t.Errorf("non-sensitive settings changed: %#v", preview)
		}
	}
	if !strings.Contains(view.Raw, `"name": "visible"`) || !strings.Contains(view.Raw, "[REDACTED]") || len(view.MergedPaths) != 2 {
		t.Errorf("config preview lost safe content or merged provenance: %#v", view)
	}
	for name, content := range files {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(got) != content {
			t.Errorf("source config %s changed: %v", name, err)
		}
	}
}
