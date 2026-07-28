package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"opencode-dashboard/internal/stats"
)

func TestBadRequest(t *testing.T) {
	err := BadRequest("invalid parameter")

	if err.Error != "Bad Request" {
		t.Errorf("BadRequest().Error = %q, want %q", err.Error, "Bad Request")
	}
	if err.Message != "invalid parameter" {
		t.Errorf("BadRequest().Message = %q, want %q", err.Message, "invalid parameter")
	}
	if err.Code != http.StatusBadRequest {
		t.Errorf("BadRequest().Code = %d, want %d", err.Code, http.StatusBadRequest)
	}
}

func TestNotFound(t *testing.T) {
	err := NotFound("session not found")

	if err.Error != "Not Found" {
		t.Errorf("NotFound().Error = %q, want %q", err.Error, "Not Found")
	}
	if err.Message != "session not found" {
		t.Errorf("NotFound().Message = %q, want %q", err.Message, "session not found")
	}
	if err.Code != http.StatusNotFound {
		t.Errorf("NotFound().Code = %d, want %d", err.Code, http.StatusNotFound)
	}
}

func TestInternalError(t *testing.T) {
	err := InternalError("database connection failed")

	if err.Error != "Internal Server Error" {
		t.Errorf("InternalError().Error = %q, want %q", err.Error, "Internal Server Error")
	}
	if err.Message != "database connection failed" {
		t.Errorf("InternalError().Message = %q, want %q", err.Message, "database connection failed")
	}
	if err.Code != http.StatusInternalServerError {
		t.Errorf("InternalError().Code = %d, want %d", err.Code, http.StatusInternalServerError)
	}
}

func TestServiceUnavailable(t *testing.T) {
	err := ServiceUnavailable("maintenance in progress")

	if err.Error != "Service Unavailable" {
		t.Errorf("ServiceUnavailable().Error = %q, want %q", err.Error, "Service Unavailable")
	}
	if err.Message != "maintenance in progress" {
		t.Errorf("ServiceUnavailable().Message = %q, want %q", err.Message, "maintenance in progress")
	}
	if err.Code != http.StatusServiceUnavailable {
		t.Errorf("ServiceUnavailable().Code = %d, want %d", err.Code, http.StatusServiceUnavailable)
	}
}

func TestAPIErrorWrite(t *testing.T) {
	tests := []struct {
		name       string
		apiError   APIError
		wantStatus int
		wantBody   string
	}{
		{
			name:       "bad request",
			apiError:   BadRequest("test message"),
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"Bad Request","message":"test message","code":400}`,
		},
		{
			name:       "not found",
			apiError:   NotFound("resource missing"),
			wantStatus: http.StatusNotFound,
			wantBody:   `{"error":"Not Found","message":"resource missing","code":404}`,
		},
		{
			name:       "internal error",
			apiError:   InternalError("server crash"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"error":"Internal Server Error","message":"server crash","code":500}`,
		},
		{
			name:       "service unavailable",
			apiError:   ServiceUnavailable("overloaded"),
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `{"error":"Service Unavailable","message":"overloaded","code":503}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tt.apiError.Write(w)

			if w.Code != tt.wantStatus {
				t.Errorf("APIError.Write() status = %d, want %d", w.Code, tt.wantStatus)
			}

			if w.Header().Get("Content-Type") != "application/json" {
				t.Errorf("APIError.Write() Content-Type = %q, want %q", w.Header().Get("Content-Type"), "application/json")
			}

			// Decode and compare JSON (order may vary)
			var gotBody APIError
			var wantBody APIError

			if err := json.Unmarshal(w.Body.Bytes(), &gotBody); err != nil {
				t.Fatalf("Failed to decode response body: %v", err)
			}
			if err := json.Unmarshal([]byte(tt.wantBody), &wantBody); err != nil {
				t.Fatalf("Failed to decode expected body: %v", err)
			}

			if gotBody.Error != wantBody.Error {
				t.Errorf("response error = %q, want %q", gotBody.Error, wantBody.Error)
			}
			if gotBody.Message != wantBody.Message {
				t.Errorf("response message = %q, want %q", gotBody.Message, wantBody.Message)
			}
			if gotBody.Code != wantBody.Code {
				t.Errorf("response code = %d, want %d", gotBody.Code, wantBody.Code)
			}
		})
	}
}

func TestWriteJSON(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		data       any
		wantStatus int
	}{
		{
			name:       "simple object",
			status:     http.StatusOK,
			data:       map[string]string{"key": "value"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "array",
			status:     http.StatusOK,
			data:       []string{"a", "b", "c"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "nil",
			status:     http.StatusOK,
			data:       nil,
			wantStatus: http.StatusOK,
		},
		{
			name:       "created status",
			status:     http.StatusCreated,
			data:       map[string]int{"id": 123},
			wantStatus: http.StatusCreated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeJSON(w, tt.status, tt.data)

			if w.Code != tt.wantStatus {
				t.Errorf("writeJSON() status = %d, want %d", w.Code, tt.wantStatus)
			}

			if w.Header().Get("Content-Type") != "application/json" {
				t.Errorf("writeJSON() Content-Type = %q, want %q", w.Header().Get("Content-Type"), "application/json")
			}

			// Verify body is valid JSON
			if w.Body.Len() > 0 {
				var decodeTarget any
				if err := json.Unmarshal(w.Body.Bytes(), &decodeTarget); err != nil {
					t.Errorf("writeJSON() body is not valid JSON: %v", err)
				}
			}
		})
	}
}

// QueryError must classify by sentinel, including through wrapping. The
// previous substring matching broke whenever a message was reworded, and it
// misclassified unrelated errors that happened to contain a matched word.
func TestQueryErrorClassifiesBySentinelNotMessageText(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantCode int
		wantMsg  string
	}{
		{
			name:     "invalid period is the caller's to fix",
			err:      stats.InvalidPeriodError("2d"),
			wantCode: http.StatusBadRequest,
			wantMsg:  stats.InvalidPeriodError("2d").Error(),
		},
		{
			name:     "invalid dimension is the caller's to fix",
			err:      stats.InvalidDimensionError("bogus", "model, tool, project"),
			wantCode: http.StatusBadRequest,
			wantMsg:  stats.InvalidDimensionError("bogus", "model, tool, project").Error(),
		},
		{
			name:     "sentinel survives wrapping by an intermediate layer",
			err:      fmt.Errorf("aggregate codex: %w", stats.ErrInvalidPeriod),
			wantCode: http.StatusBadRequest,
			wantMsg:  "aggregate codex: invalid period",
		},
		{
			name:     "unfilterable source is reported to the caller",
			err:      fmt.Errorf("%w: needs consolidated cache data", stats.ErrFilterUnavailable),
			wantCode: http.StatusBadRequest,
			wantMsg:  "filter unavailable: needs consolidated cache data",
		},
		{
			// The old substring check turned this into a 400 because the text
			// contains "invalid period", leaking internals as a client error.
			name:     "backend failure mentioning a request term stays a server fault",
			err:      errors.New("scan hourly rollup for invalid period bucket: disk I/O error"),
			wantCode: http.StatusInternalServerError,
			wantMsg:  "failed to compute overview",
		},
		{
			name:     "unrelated failure is a server fault",
			err:      errors.New("database is locked"),
			wantCode: http.StatusInternalServerError,
			wantMsg:  "failed to compute overview",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := QueryError(tc.err, "failed to compute overview")
			if got.Code != tc.wantCode {
				t.Fatalf("code = %d, want %d", got.Code, tc.wantCode)
			}
			if got.Message != tc.wantMsg {
				t.Fatalf("message = %q, want %q", got.Message, tc.wantMsg)
			}
		})
	}
}
