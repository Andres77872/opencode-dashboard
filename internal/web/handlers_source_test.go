package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

func TestSourcesEndpointListsRegisteredSourceMetadata(t *testing.T) {
	opencodeSource := newHandlerFakeSource(source.SourceOpenCode, true, 11)
	claudeSource := newHandlerFakeSource(source.SourceClaudeCode, false, 22)
	claudeSource.info.Path = "/home/test/.claude"
	claudeSource.info.PathSource = "$HOME/.claude"
	claudeSource.info.Warnings = []string{"plaintext transcripts may contain sensitive content"}

	handler := newSourceTestHandler(t, opencodeSource, claudeSource)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sources", nil)

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/sources status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body source.SourceListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode sources response: %v", err)
	}
	if body.DefaultSourceID != source.SourceOpenCode {
		t.Errorf("default_source_id = %q, want %q", body.DefaultSourceID, source.SourceOpenCode)
	}
	if body.StartupSourceID != source.SourceOpenCode {
		t.Errorf("startup_source_id = %q, want %q", body.StartupSourceID, source.SourceOpenCode)
	}

	byID := make(map[source.SourceID]source.SourceInfo, len(body.Sources))
	for _, info := range body.Sources {
		byID[info.ID] = info
	}

	openInfo, ok := byID[source.SourceOpenCode]
	if !ok {
		t.Fatalf("sources response missing opencode: %#v", body.Sources)
	}
	if !openInfo.Available {
		t.Errorf("opencode Available = false, want true")
	}
	if !openInfo.Default {
		t.Errorf("opencode Default = false, want true")
	}
	if !openInfo.Selected {
		t.Errorf("opencode Selected = false, want true because startup source defaults to opencode")
	}

	claudeInfo, ok := byID[source.SourceClaudeCode]
	if !ok {
		t.Fatalf("sources response missing claude_code: %#v", body.Sources)
	}
	if claudeInfo.Available {
		t.Errorf("claude_code Available = true, want false")
	}
	if claudeInfo.Default {
		t.Errorf("claude_code Default = true, want false")
	}
	if claudeInfo.Selected {
		t.Errorf("claude_code Selected = true, want false when startup source is opencode")
	}
	if claudeInfo.PathSource != "$HOME/.claude" {
		t.Errorf("claude_code path_source = %q, want $HOME/.claude", claudeInfo.PathSource)
	}
}

func TestSourceAwareOverviewRouting(t *testing.T) {
	tests := []struct {
		name            string
		path            string
		wantStatus      int
		wantSessions    float64
		wantOpenCalls   int
		wantClaudeCalls int
		wantErrContains string
	}{
		{
			name:          "omitted source defaults to OpenCode",
			path:          "/api/v1/overview?period=all",
			wantStatus:    http.StatusOK,
			wantSessions:  11,
			wantOpenCalls: 1,
		},
		{
			name:          "empty source query defaults to OpenCode",
			path:          "/api/v1/overview?source=&period=all",
			wantStatus:    http.StatusOK,
			wantSessions:  11,
			wantOpenCalls: 1,
		},
		{
			name:          "whitespace source query defaults to OpenCode",
			path:          "/api/v1/overview?source=%20%20&period=all",
			wantStatus:    http.StatusOK,
			wantSessions:  11,
			wantOpenCalls: 1,
		},
		{
			name:          "explicit opencode routes to OpenCode",
			path:          "/api/v1/overview?source=opencode&period=all",
			wantStatus:    http.StatusOK,
			wantSessions:  11,
			wantOpenCalls: 1,
		},
		{
			name:            "invalid source returns 400 without fallback",
			path:            "/api/v1/overview?source=does_not_exist&period=all",
			wantStatus:      http.StatusBadRequest,
			wantErrContains: "does_not_exist",
		},
		{
			name:            "unavailable selected source returns 503 without fallback",
			path:            "/api/v1/overview?source=claude_code&period=all",
			wantStatus:      http.StatusServiceUnavailable,
			wantErrContains: "claude_code",
		},
		{
			name:            "unsupported both source returns 400 without fallback",
			path:            "/api/v1/overview?source=both&period=all",
			wantStatus:      http.StatusBadRequest,
			wantErrContains: "both",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opencodeSource := newHandlerFakeSource(source.SourceOpenCode, true, 11)
			claudeSource := newHandlerFakeSource(source.SourceClaudeCode, false, 22)
			handler := newSourceTestHandler(t, opencodeSource, claudeSource)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("GET %s status = %d, want %d; body: %s", tt.path, rec.Code, tt.wantStatus, rec.Body.String())
			}
			if opencodeSource.overviewCalls != tt.wantOpenCalls {
				t.Errorf("opencode overview calls = %d, want %d", opencodeSource.overviewCalls, tt.wantOpenCalls)
			}
			if claudeSource.overviewCalls != tt.wantClaudeCalls {
				t.Errorf("claude overview calls = %d, want %d", claudeSource.overviewCalls, tt.wantClaudeCalls)
			}

			if tt.wantStatus == http.StatusOK {
				var body map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode overview response: %v", err)
				}
				if got := body["sessions"]; got != tt.wantSessions {
					t.Errorf("overview sessions = %v, want %.0f", got, tt.wantSessions)
				}
				return
			}

			var apiErr APIError
			if err := json.Unmarshal(rec.Body.Bytes(), &apiErr); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if apiErr.Code != tt.wantStatus {
				t.Errorf("error code = %d, want %d", apiErr.Code, tt.wantStatus)
			}
			if !strings.Contains(apiErr.Message, tt.wantErrContains) {
				t.Errorf("error message = %q, want containing %q", apiErr.Message, tt.wantErrContains)
			}
		})
	}
}

func TestSourceAwareDetailRouting(t *testing.T) {
	tests := []struct {
		name                   string
		path                   string
		wantSourceID           source.SourceID
		wantID                 string
		wantOpenSessionCalls   int
		wantClaudeSessionCalls int
	}{
		{
			name:                 "omitted source defaults to OpenCode session detail",
			path:                 "/api/v1/sessions/session-123",
			wantSourceID:         source.SourceOpenCode,
			wantID:               "opencode:session-123",
			wantOpenSessionCalls: 1,
		},
		{
			name:                 "explicit opencode routes to OpenCode session detail",
			path:                 "/api/v1/sessions/session-123?source=opencode",
			wantSourceID:         source.SourceOpenCode,
			wantID:               "opencode:session-123",
			wantOpenSessionCalls: 1,
		},
		{
			name:                   "explicit claude routes to Claude session detail without OpenCode fallback",
			path:                   "/api/v1/sessions/session-123?source=claude_code",
			wantSourceID:           source.SourceClaudeCode,
			wantID:                 "claude_code:session-123",
			wantClaudeSessionCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opencodeSource := newHandlerFakeSource(source.SourceOpenCode, true, 11)
			claudeSource := newHandlerFakeSource(source.SourceClaudeCode, true, 22)
			handler := newSourceTestHandler(t, opencodeSource, claudeSource)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s status = %d, want %d; body: %s", tt.path, rec.Code, http.StatusOK, rec.Body.String())
			}
			if opencodeSource.sessionByIDCalls != tt.wantOpenSessionCalls {
				t.Errorf("opencode session detail calls = %d, want %d", opencodeSource.sessionByIDCalls, tt.wantOpenSessionCalls)
			}
			if claudeSource.sessionByIDCalls != tt.wantClaudeSessionCalls {
				t.Errorf("claude session detail calls = %d, want %d", claudeSource.sessionByIDCalls, tt.wantClaudeSessionCalls)
			}

			var body stats.SessionDetail
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode session detail response: %v", err)
			}
			if body.SourceID != string(tt.wantSourceID) {
				t.Errorf("session detail source_id = %q, want %q", body.SourceID, tt.wantSourceID)
			}
			if body.ID != tt.wantID {
				t.Errorf("session detail id = %q, want %q", body.ID, tt.wantID)
			}
		})
	}
}

func newSourceTestHandler(t *testing.T, sources ...source.Source) http.Handler {
	t.Helper()

	registry := source.NewRegistry(source.SourceOpenCode)
	for _, src := range sources {
		if err := registry.Register(src); err != nil {
			t.Fatalf("Register(%s) failed: %v", src.Info(context.Background()).ID, err)
		}
	}

	server := NewServer("", registry, nil)
	return server.Handler
}

type handlerFakeSource struct {
	info             source.SourceInfo
	sessions         int64
	overviewCalls    int
	sessionByIDCalls int
}

func newHandlerFakeSource(id source.SourceID, available bool, sessions int64) *handlerFakeSource {
	return &handlerFakeSource{
		info: source.SourceInfo{
			ID:           id,
			Label:        string(id),
			Kind:         "test",
			Available:    available,
			ReadOnly:     true,
			LocalOnly:    true,
			Capabilities: []string{"overview"},
		},
		sessions: sessions,
	}
}

func (s *handlerFakeSource) Info(context.Context) source.SourceInfo {
	return s.info
}

func (s *handlerFakeSource) Overview(context.Context, stats.PeriodQuery) (stats.OverviewStats, error) {
	s.overviewCalls++
	return stats.OverviewStats{Sessions: s.sessions}, nil
}

func (s *handlerFakeSource) Daily(context.Context, stats.PeriodQuery, ...stats.Granularity) (stats.DailyStats, error) {
	return stats.DailyStats{}, nil
}

func (s *handlerFakeSource) DailyDimension(context.Context, string, stats.PeriodQuery) (stats.DailyDimensionStats, error) {
	return stats.DailyDimensionStats{}, nil
}

func (s *handlerFakeSource) Models(context.Context, stats.PeriodQuery) (stats.ModelStats, error) {
	return stats.ModelStats{}, nil
}

func (s *handlerFakeSource) Tools(context.Context, stats.PeriodQuery) (stats.ToolStats, error) {
	return stats.ToolStats{}, nil
}

func (s *handlerFakeSource) Projects(context.Context, stats.PeriodQuery) (stats.ProjectStats, error) {
	return stats.ProjectStats{}, nil
}

func (s *handlerFakeSource) ProjectByID(context.Context, string, stats.PeriodQuery, int, int) (*stats.ProjectDetail, error) {
	return nil, nil
}

func (s *handlerFakeSource) Sessions(context.Context, stats.SessionQuery) (stats.SessionList, error) {
	return stats.SessionList{}, nil
}

func (s *handlerFakeSource) SessionByID(_ context.Context, sessionID string) (*stats.SessionDetail, error) {
	s.sessionByIDCalls++
	id := s.info.ID
	return &stats.SessionDetail{
		SourceID:    string(id),
		ID:          string(id) + ":" + sessionID,
		Title:       string(id) + " session",
		ProjectID:   string(id) + "-project",
		ProjectName: string(id) + " project",
	}, nil
}

func (s *handlerFakeSource) Messages(context.Context, stats.PeriodQuery, int, int, stats.MessageSort) (stats.MessageList, error) {
	return stats.MessageList{}, nil
}

func (s *handlerFakeSource) MessageByID(context.Context, string) (*stats.MessageDetail, error) {
	return nil, nil
}

func (s *handlerFakeSource) Config(context.Context) (stats.ConfigView, error) {
	return stats.ConfigView{}, nil
}
