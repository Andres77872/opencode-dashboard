package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"opencode-dashboard/internal/source"
)

type fakeCacheManager struct {
	syncedSource string
	syncedMode   string
}

func (m *fakeCacheManager) Status(context.Context) (CacheStatusResponse, error) {
	return CacheStatusResponse{
		Enabled: true,
		Path:    "/tmp/cache.sqlite",
		Source:  "test",
		Active:  false,
		Sources: []CacheSourceStatus{{
			SourceID:       string(source.SourceCodex),
			Label:          "Codex",
			Available:      true,
			Cached:         false,
			NeedsSync:      true,
			Reason:         "cache has no consolidated data for this source",
			FreshThroughMS: 1700000000000,
			FillAttemptMS:  1700000001000,
			FillError:      "synthetic fill error",
		}},
		Sync: &CacheSyncStatus{
			Running:      true,
			Status:       "running",
			Total:        1,
			CurrentPhase: "messages",
			ItemsDone:    1200,
			ItemsTotal:   4800,
		},
	}, nil
}

func (m *fakeCacheManager) Sync(_ context.Context, sourceID string, mode string) (CacheStatusResponse, error) {
	m.syncedSource = sourceID
	m.syncedMode = mode
	return CacheStatusResponse{
		Enabled: true,
		Path:    "/tmp/cache.sqlite",
		Source:  "test",
		Active:  true,
		Sources: []CacheSourceStatus{{
			SourceID:  sourceID,
			Label:     "Codex",
			Available: true,
			Cached:    true,
		}},
	}, nil
}

func TestCacheEndpoints(t *testing.T) {
	registry := source.NewRegistry(source.SourceOpenCode)
	manager := &fakeCacheManager{}
	server := NewServerWithCache("", registry, nil, manager)

	statusReq := httptest.NewRequest(http.MethodGet, "/api/v1/cache", nil)
	statusRec := httptest.NewRecorder()
	server.Handler.ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/cache status = %d, want %d; body: %s", statusRec.Code, http.StatusOK, statusRec.Body.String())
	}
	if cc := statusRec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("GET /api/v1/cache Cache-Control = %q, want no-store (live sync status must not be served from the browser HTTP cache)", cc)
	}
	var status CacheStatusResponse
	if err := json.NewDecoder(statusRec.Body).Decode(&status); err != nil {
		t.Fatalf("decode cache status: %v", err)
	}
	if !status.Enabled || len(status.Sources) != 1 || !status.Sources[0].NeedsSync {
		t.Fatalf("cache status = %#v, want enabled source needing sync", status)
	}
	if status.Sources[0].FreshThroughMS != 1700000000000 {
		t.Fatalf("fresh_through_ms = %d, want passthrough of 1700000000000", status.Sources[0].FreshThroughMS)
	}
	if status.Sources[0].FillAttemptMS != 1700000001000 || status.Sources[0].FillError != "synthetic fill error" {
		t.Fatalf("fill state = %d/%q, want passthrough", status.Sources[0].FillAttemptMS, status.Sources[0].FillError)
	}
	if status.Sync == nil || status.Sync.CurrentPhase != "messages" || status.Sync.ItemsDone != 1200 || status.Sync.ItemsTotal != 4800 {
		t.Fatalf("sync progress = %#v, want phase/items passthrough", status.Sync)
	}

	syncReq := httptest.NewRequest(http.MethodPost, "/api/v1/cache/sync?source=codex&mode=rebuild", nil)
	syncRec := httptest.NewRecorder()
	server.Handler.ServeHTTP(syncRec, syncReq)
	if syncRec.Code != http.StatusOK {
		t.Fatalf("POST /api/v1/cache/sync status = %d, want %d; body: %s", syncRec.Code, http.StatusOK, syncRec.Body.String())
	}
	if cc := syncRec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("POST /api/v1/cache/sync Cache-Control = %q, want no-store", cc)
	}
	if manager.syncedSource != "codex" {
		t.Fatalf("synced source = %q, want codex", manager.syncedSource)
	}
	if manager.syncedMode != "rebuild" {
		t.Fatalf("synced mode = %q, want rebuild", manager.syncedMode)
	}
}
