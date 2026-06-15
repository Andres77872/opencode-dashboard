package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLatestReleaseSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got == "" {
			t.Errorf("expected a User-Agent header, got empty")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v0.1.20","name":"Release 0.1.20"}`))
	}))
	t.Cleanup(srv.Close)

	tag, err := latestReleaseFrom(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tag != "v0.1.20" {
		t.Fatalf("tag = %q, want v0.1.20", tag)
	}
}

func TestLatestReleaseRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	}))
	t.Cleanup(srv.Close)

	if _, err := latestReleaseFrom(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Fatal("expected an error for 403 rate limit, got nil")
	}
}

func TestLatestReleaseMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name": `)) // truncated
	}))
	t.Cleanup(srv.Close)

	if _, err := latestReleaseFrom(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Fatal("expected a parse error for malformed JSON, got nil")
	}
}

func TestLatestReleaseEmptyTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":""}`))
	}))
	t.Cleanup(srv.Close)

	if _, err := latestReleaseFrom(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Fatal("expected an error for empty tag_name, got nil")
	}
}

func TestFetchScriptRejectsNonScript(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>404 not found</html>"))
	}))
	t.Cleanup(srv.Close)

	if _, err := fetchScript(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Fatal("expected an error for a non-script body, got nil")
	}
}

func TestFetchScriptSuccess(t *testing.T) {
	const script = "#!/usr/bin/env bash\necho hello\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(script))
	}))
	t.Cleanup(srv.Close)

	body, err := fetchScript(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != script {
		t.Fatalf("body = %q, want %q", string(body), script)
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name     string
		a, b     string
		wantSign int
		wantOK   bool
	}{
		{"equal with v", "v0.1.19", "v0.1.19", 0, true},
		{"equal mixed prefix", "0.1.19", "v0.1.19", 0, true},
		{"a less than b", "v0.1.19", "v0.1.20", -1, true},
		{"a greater than b", "v0.2.0", "v0.1.20", 1, true},
		{"missing trailing field equals zero", "v0.1", "v0.1.0", 0, true},
		{"longer beats shorter", "v0.1.0", "v0.1.0.1", -1, true},
		{"shorter loses to longer", "v0.1.0.1", "v0.1.0", 1, true},
		{"dev left incomparable", "dev", "v0.1.20", 0, false},
		{"dev right incomparable", "v0.1.20", "dev", 0, false},
		{"unknown incomparable", "unknown", "v0.1.0", 0, false},
		{"empty incomparable", "", "v0.1.0", 0, false},
		{"non-numeric field incomparable", "v0.1.x", "v0.1.0", 0, false},
		{"prerelease suffix incomparable", "v0.1.0-rc1", "v0.1.0", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotSign, gotOK := CompareVersions(tc.a, tc.b)
			if gotOK != tc.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, tc.wantOK)
			}
			if gotOK && gotSign != tc.wantSign {
				t.Fatalf("sign = %d, want %d", gotSign, tc.wantSign)
			}
		})
	}
}

func TestNormalizeTag(t *testing.T) {
	tests := []struct{ in, want string }{
		{"0.1.20", "v0.1.20"},
		{"v0.1.20", "v0.1.20"},
		{"V0.1.20", "v0.1.20"},
		{"  v0.1.20  ", "v0.1.20"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := NormalizeTag(tc.in); got != tc.want {
			t.Errorf("NormalizeTag(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
