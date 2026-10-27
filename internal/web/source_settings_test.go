package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"opencode-dashboard/internal/pricingalias"
	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/source/antigravity"
	"opencode-dashboard/internal/source/codex"
)

func TestSourceToggleAPIAndDisabledQueries(t *testing.T) {
	ctx := context.Background()
	settings, err := pricingalias.Open(ctx, filepath.Join(t.TempDir(), "settings.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer settings.Close()
	reg := source.NewRegistry(source.SourceOpenCode)
	if err := reg.ConfigurePreferences(settings, nil); err != nil {
		t.Fatal(err)
	}
	for _, id := range source.KnownIDs() {
		reg.RegisterUnavailable(source.SourceInfo{ID: id, Label: string(id)})
	}
	server := NewServer(ServerOptions{Registry: reg, SourceSettings: settings})
	for _, body := range []string{`{}`, `{"enabled":null}`, `{"enabled":"false"}`, `{"enabled":false,"extra":1}`, `{"enabled":false} {}`} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPut, testOrigin+"/api/v1/sources/antigravity", strings.NewReader(body))
		server.Handler.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("accepted %s: %d", body, w.Code)
		}
	}
	for _, id := range source.KnownIDs() {
		w := httptest.NewRecorder()
		server.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodPut, testOrigin+"/api/v1/sources/"+string(id), strings.NewReader(`{"enabled":false}`)))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	server.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, testOrigin+"/api/v1/sources", nil))
	var list source.SourceListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Sources) != 6 || list.SourcesRevision != 6 {
		t.Fatalf("source list: %+v", list)
	}
	for _, info := range list.Sources {
		if info.Enabled {
			t.Fatal("source remained enabled")
		}
	}
	w = httptest.NewRecorder()
	server.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, testOrigin+"/api/v1/overview?source=antigravity", nil))
	if w.Code != 409 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("disabled query: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	server.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, testOrigin+"/api/v1/overview/all?period=all", nil))
	if w.Code != 200 {
		t.Fatalf("all disabled overview failed: %d", w.Code)
	}
}

func TestDisabledUnavailableReferenceCatalogRemainsTargetable(t *testing.T) {
	ctx := context.Background()
	settings, err := pricingalias.Open(ctx, filepath.Join(t.TempDir(), "settings.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer settings.Close()
	if err := settings.SetSourceEnabled(ctx, source.SourceAntigravity, false); err != nil {
		t.Fatal(err)
	}
	reg := source.NewRegistry(source.SourceOpenCode)
	if err := reg.ConfigurePreferences(settings, nil); err != nil {
		t.Fatal(err)
	}
	ag := antigravity.New(antigravity.Options{Home: filepath.Join(t.TempDir(), "missing")})
	reg.RegisterWithInfo(ag, source.SourceInfo{ID: source.SourceAntigravity, Label: "Antigravity"})
	cx := codex.New(codex.Options{CodexHome: filepath.Join("..", "source", "codex", "testdata", "valid_home")})
	reg.Register(cx)
	h := NewHandlers(ServerOptions{Registry: reg, PricingAliases: settings})
	catalogs := h.pricingAliasCatalogs(ctx)
	found := false
	for _, c := range catalogs {
		if c.SourceID == source.SourceAntigravity {
			found = len(c.Models) == 2
		}
	}
	if !found {
		t.Fatal("disabled reference catalog hidden")
	}
	target, apiErr := h.pricingAliasTargetCatalog(ctx, source.SourceAntigravity)
	if apiErr != nil || !catalogHasTarget(target, "gemini-4-argon") {
		t.Fatalf("target=%+v error=%v", target, apiErr)
	}
	if _, err := reg.Resolve("antigravity"); err == nil {
		t.Fatal("catalog access enabled analytics")
	}
}
