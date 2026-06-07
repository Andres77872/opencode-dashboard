package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"opencode-dashboard/internal/source"
)

func TestOverviewAllAggregatesSources(t *testing.T) {
	handler := newSourceTestHandler(t,
		newHandlerFakeSource(source.SourceOpenCode, true, 8),
		newHandlerFakeSource(source.SourceCodex, true, 4),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview/all?period=all", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var body source.AllSourcesOverview
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Total.Sessions != 12 {
		t.Errorf("total.sessions = %d, want 12", body.Total.Sessions)
	}
	if len(body.Sources) != 2 {
		t.Errorf("sources len = %d, want 2", len(body.Sources))
	}
}

func TestOverviewAllIgnoresSourceParam(t *testing.T) {
	handler := newSourceTestHandler(t,
		newHandlerFakeSource(source.SourceOpenCode, true, 8),
		newHandlerFakeSource(source.SourceCodex, true, 4),
	)

	// A source param is irrelevant: the aggregate always spans all sources.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview/all?source=codex&period=all", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body source.AllSourcesOverview
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Total.Sessions != 12 {
		t.Errorf("total.sessions = %d, want 12 (source param must be ignored)", body.Total.Sessions)
	}
}

func TestOverviewAllRejectsBadPeriod(t *testing.T) {
	handler := newSourceTestHandler(t, newHandlerFakeSource(source.SourceOpenCode, true, 8))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview/all?from=not-a-date", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
