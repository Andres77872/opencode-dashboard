package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
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

func TestOverviewAllSourceDimensionSkipsModelsColdScan(t *testing.T) {
	src := newHandlerFakeSource(source.SourceOpenCode, true, 8)
	src.models = []stats.ModelEntry{{ModelID: "slow-model-scan"}}
	handler := newSourceTestHandler(t, src)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview/all?period=all&dimension=source&trend=true", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if src.modelsCalls != 0 {
		t.Fatalf("source-grouped cold overview called Models %d times, want 0", src.modelsCalls)
	}
	if src.overviewCalls != 1 || src.dailyCalls != 1 || src.toolsCalls != 1 || src.projectsCalls != 1 {
		t.Fatalf(
			"source calls Overview/Daily/Tools/Projects = %d/%d/%d/%d, want 1/1/1/1",
			src.overviewCalls, src.dailyCalls, src.toolsCalls, src.projectsCalls,
		)
	}

	var body source.AllSourcesOverview
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.TopModels == nil || len(body.TopModels) != 0 {
		t.Fatalf("top_models = %#v, want a JSON-safe empty slice on the deferred path", body.TopModels)
	}
	for _, partial := range body.PartialErrors {
		if partial.Dimension == "models" {
			t.Fatalf("a deliberately deferred model dimension was reported as an error: %+v", body.PartialErrors)
		}
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

func TestOverviewAllRejectsUnknownPresetBeforeAggregation(t *testing.T) {
	for _, path := range []string{
		"/api/v1/overview/all?period=bogus",
		"/api/v1/overview/all?period=bogus&dimension=model&trend=true",
	} {
		t.Run(path, func(t *testing.T) {
			src := newHandlerFakeSource(source.SourceOpenCode, true, 8)
			handler := newSourceTestHandler(t, src)

			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			if src.overviewCalls != 0 || src.dailyCalls != 0 || src.dailyDimensionCalls != 0 || src.modelsCalls != 0 || src.toolsCalls != 0 || src.projectsCalls != 0 {
				t.Fatalf(
					"invalid preset reached aggregation: overview=%d daily=%d daily_dimension=%d models=%d tools=%d projects=%d",
					src.overviewCalls, src.dailyCalls, src.dailyDimensionCalls, src.modelsCalls, src.toolsCalls, src.projectsCalls,
				)
			}
		})
	}
}

func TestOverviewAllRejectsUnknownDimension(t *testing.T) {
	handler := newSourceTestHandler(t, newHandlerFakeSource(source.SourceOpenCode, true, 8))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview/all?period=all&dimension=provider", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestOverviewAllModelDimensionReturnsLeanCompletePayload(t *testing.T) {
	src := newHandlerFakeSource(source.SourceOpenCode, true, 8)
	src.models = []stats.ModelEntry{
		{ModelID: "anthropic/claude", ProviderID: "openrouter", Messages: 3, Tokens: stats.TokenStats{Input: 30}},
		{ModelID: "gpt", ProviderID: "openai", Messages: 1, Tokens: stats.TokenStats{Input: 10}},
	}
	src.modelTrend = []stats.DimensionDayStats{
		{Date: "2026-07-16", Dimension: "anthropic/claude", Messages: 3, Tokens: stats.TokenStats{Input: 30}},
	}
	handler := newSourceTestHandler(t, src)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview/all?period=all&dimension=model&trend=true&top=1", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body source.AllSourcesModelUsage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.ModelUsage) != 2 {
		t.Fatalf("model_usage len = %d, want all 2 despite top=1", len(body.ModelUsage))
	}
	if len(body.ModelTrend) != 1 || body.ModelTrend[0].SourceID != string(source.SourceOpenCode) {
		t.Fatalf("model_trend = %+v", body.ModelTrend)
	}
	if src.overviewCalls != 0 {
		t.Fatalf("model dimension repeated base Overview %d times", src.overviewCalls)
	}
	if src.modelsCalls != 1 {
		t.Fatalf("Models calls = %d, want 1", src.modelsCalls)
	}
	if src.dailyDimensionCalls != 1 {
		t.Fatalf("DailyDimension calls = %d, want 1", src.dailyDimensionCalls)
	}
	if src.dailyCalls != 0 || src.toolsCalls != 0 || src.projectsCalls != 0 {
		t.Fatalf(
			"lean model calls Daily/Tools/Projects = %d/%d/%d, want 0/0/0",
			src.dailyCalls, src.toolsCalls, src.projectsCalls,
		)
	}
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, repeated := raw["top_tools"]; repeated {
		t.Fatal("lean model payload unexpectedly recomputed base top signals")
	}
}
