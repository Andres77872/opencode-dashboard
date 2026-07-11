package quota

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

const minimaxRemainsFixture = `{
  "model_remains": [
    {
      "start_time": 1783800000000, "end_time": 1783814400000,
      "model_name": "video",
      "current_interval_remaining_percent": 90, "current_interval_status": 1,
      "weekly_start_time": 1783296000000, "weekly_end_time": 1783900800000,
      "current_weekly_remaining_percent": 95, "current_weekly_status": 1
    },
    {
      "start_time": 1783800000000, "end_time": 1783814400000,
      "model_name": "general",
      "current_interval_remaining_percent": 63, "current_interval_status": 1,
      "weekly_start_time": 1783296000000, "weekly_end_time": 1783900800000,
      "current_weekly_remaining_percent": 69, "current_weekly_status": 1
    }
  ],
  "base_resp": {"status_code": 0, "status_msg": "success"}
}`

func newMiniMaxProvider(t *testing.T, url string) *minimaxProvider {
	t.Helper()
	t.Setenv("OPENCODE_DASHBOARD_MINIMAX_API_KEY", "test-key")
	return &minimaxProvider{
		authPath: filepath.Join(t.TempDir(), "auth.json"),
		client:   &http.Client{Timeout: time.Second},
		now:      time.Now,
		url:      url,
	}
}

func TestMiniMaxQuotaMapsRemainingToUsed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", got)
		}
		w.Write([]byte(minimaxRemainsFixture))
	}))
	defer server.Close()

	got := newMiniMaxProvider(t, server.URL).quota(context.Background())
	if got.Status != StatusOK {
		t.Fatalf("status = %q (reason %q), want ok", got.Status, got.Reason)
	}
	if got.Plan != "coding plan" {
		t.Errorf("plan = %q, want coding plan", got.Plan)
	}
	if len(got.Windows) != 2 {
		t.Fatalf("windows = %d, want 2", len(got.Windows))
	}
	interval := got.Windows[0]
	if interval.ID != "5h" || !approxEqual(interval.UsedPercent, 37) {
		t.Errorf("interval window = %+v, want used 37%% of general entry (not video)", interval)
	}
	if interval.ResetsAt != 1783814400 {
		t.Errorf("interval resets_at = %d, want 1783814400 (ms converted to s)", interval.ResetsAt)
	}
	if interval.WindowMinutes != 240 {
		t.Errorf("interval window_minutes = %d, want 240", interval.WindowMinutes)
	}
	weekly := got.Windows[1]
	if weekly.ID != "weekly" || !approxEqual(weekly.UsedPercent, 31) {
		t.Errorf("weekly window = %+v, want used 31%%", weekly)
	}
	if weekly.WindowMinutes != 10080 {
		t.Errorf("weekly window_minutes = %d, want 10080", weekly.WindowMinutes)
	}
}

func TestMiniMaxQuotaErrorStatuses(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantReason string
	}{
		{
			name:       "rejected key",
			handler:    func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) },
			wantReason: "MiniMax API key was rejected (status 401)",
		},
		{
			name:       "server error",
			handler:    func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) },
			wantReason: "MiniMax API returned status 502",
		},
		{
			name: "api-level error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(`{"model_remains":[],"base_resp":{"status_code":1004,"status_msg":"invalid api key"}}`))
			},
			wantReason: "MiniMax API error: invalid api key (code 1004)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()
			got := newMiniMaxProvider(t, server.URL).quota(context.Background())
			if got.Status != StatusUnavailable {
				t.Errorf("status = %q, want unavailable", got.Status)
			}
			if got.Reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", got.Reason, tt.wantReason)
			}
		})
	}
}

func TestMiniMaxQuotaKeepsLastGoodOnNetworkError(t *testing.T) {
	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(minimaxRemainsFixture))
	}))
	defer server.Close()

	p := newMiniMaxProvider(t, server.URL)
	clock := time.Now()
	p.now = func() time.Time { return clock }

	if got := p.quota(context.Background()); got.Status != StatusOK {
		t.Fatalf("first fetch status = %q, want ok", got.Status)
	}

	fail.Store(true)
	clock = clock.Add(2 * minimaxTTL) // expire the TTL cache
	got := p.quota(context.Background())
	if got.Status != StatusStale {
		t.Fatalf("status after failure = %q, want stale (serving last good)", got.Status)
	}
	if len(got.Windows) != 2 {
		t.Errorf("windows = %d, want last-good data retained", len(got.Windows))
	}
	if got.Reason == "" {
		t.Error("reason = empty, want failure explanation")
	}
}

func TestMiniMaxQuotaTTLAvoidsRepeatFetches(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write([]byte(minimaxRemainsFixture))
	}))
	defer server.Close()

	p := newMiniMaxProvider(t, server.URL)
	p.quota(context.Background())
	p.quota(context.Background())
	p.quota(context.Background())
	if got := calls.Load(); got != 1 {
		t.Errorf("server calls = %d, want 1 (TTL cache)", got)
	}
}

func TestResolveMiniMaxKey(t *testing.T) {
	writeAuth := func(t *testing.T, content string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "auth.json")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write auth fixture: %v", err)
		}
		return path
	}

	t.Run("env var wins over auth store", func(t *testing.T) {
		t.Setenv("OPENCODE_DASHBOARD_MINIMAX_API_KEY", "env-key")
		path := writeAuth(t, `{"minimax-coding-plan":{"type":"api","key":"store-key"}}`)
		key, err := resolveMiniMaxKey(path)
		if err != nil || key != "env-key" {
			t.Errorf("key = %q, err = %v; want env-key", key, err)
		}
	})

	t.Run("falls back to auth store", func(t *testing.T) {
		t.Setenv("OPENCODE_DASHBOARD_MINIMAX_API_KEY", "")
		path := writeAuth(t, `{"openai":{"key":"other"},"minimax-coding-plan":{"type":"api","key":"store-key"}}`)
		key, err := resolveMiniMaxKey(path)
		if err != nil || key != "store-key" {
			t.Errorf("key = %q, err = %v; want store-key", key, err)
		}
	})

	t.Run("missing everywhere", func(t *testing.T) {
		t.Setenv("OPENCODE_DASHBOARD_MINIMAX_API_KEY", "")
		key, err := resolveMiniMaxKey(filepath.Join(t.TempDir(), "absent.json"))
		if err != nil || key != "" {
			t.Errorf("key = %q, err = %v; want empty, nil", key, err)
		}
	})

	t.Run("unavailable without key", func(t *testing.T) {
		t.Setenv("OPENCODE_DASHBOARD_MINIMAX_API_KEY", "")
		p := &minimaxProvider{
			authPath: filepath.Join(t.TempDir(), "absent.json"),
			client:   &http.Client{Timeout: time.Second},
			now:      time.Now,
		}
		got := p.quota(context.Background())
		if got.Status != StatusUnavailable {
			t.Errorf("status = %q, want unavailable", got.Status)
		}
		if got.Help == "" {
			t.Error("help = empty, want key setup guidance")
		}
	})
}

func TestMiniMaxResponseFixtureParses(t *testing.T) {
	var remains minimaxRemainsResponse
	if err := json.Unmarshal([]byte(minimaxRemainsFixture), &remains); err != nil {
		t.Fatalf("fixture does not parse: %v", err)
	}
	entry := pickMiniMaxModel(remains.ModelRemains)
	if entry == nil || entry.ModelName != "general" {
		t.Fatalf("pickMiniMaxModel = %#v, want general entry", entry)
	}
}
