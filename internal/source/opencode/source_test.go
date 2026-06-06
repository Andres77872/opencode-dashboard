package opencode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
	"opencode-dashboard/internal/store"
	"opencode-dashboard/internal/store/fixture"
)

func TestOpenCodeSourceMatchesDirectStatsExceptMetadata(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPCODE_TOOLS_LEGACY", "")

	dbPath, err := fixture.SampleFixture(ctx)
	if err != nil {
		t.Fatalf("SampleFixture() failed: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(dbPath))

	st, err := store.Connect(ctx, dbPath)
	if err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}
	defer st.Close()

	wrapper := New(st)
	period := stats.PeriodQuery{Period: "all"}

	tests := []struct {
		name         string
		direct       func() (any, error)
		wrapped      func() (any, error)
		minSourceIDs int
	}{
		{
			name: "overview matches direct stats",
			direct: func() (any, error) {
				return stats.Overview(ctx, st, period)
			},
			wrapped: func() (any, error) {
				return wrapper.Overview(ctx, period)
			},
			minSourceIDs: 1,
		},
		{
			name: "daily matches direct stats",
			direct: func() (any, error) {
				return stats.Daily(ctx, st, period, stats.GranularityDay)
			},
			wrapped: func() (any, error) {
				return wrapper.Daily(ctx, period, stats.GranularityDay)
			},
			minSourceIDs: 2,
		},
		{
			name: "models matches direct stats",
			direct: func() (any, error) {
				return stats.Models(ctx, st, period)
			},
			wrapped: func() (any, error) {
				return wrapper.Models(ctx, period)
			},
			minSourceIDs: 2,
		},
		{
			name: "tools matches direct stats",
			direct: func() (any, error) {
				return stats.Tools(ctx, st, period)
			},
			wrapped: func() (any, error) {
				return wrapper.Tools(ctx, period)
			},
			minSourceIDs: 1,
		},
		{
			name: "projects matches direct stats",
			direct: func() (any, error) {
				return stats.Projects(ctx, st, period)
			},
			wrapped: func() (any, error) {
				return wrapper.Projects(ctx, period)
			},
			minSourceIDs: 2,
		},
		{
			name: "sessions matches direct stats",
			direct: func() (any, error) {
				return stats.SessionsWithQuery(ctx, st, stats.SessionQuery{Page: 1, PageSize: 10, Sort: stats.SessionSortNewest, Period: "all"})
			},
			wrapped: func() (any, error) {
				return wrapper.Sessions(ctx, stats.SessionQuery{Page: 1, PageSize: 10, Sort: stats.SessionSortNewest, Period: "all"})
			},
			minSourceIDs: 2,
		},
		{
			name: "project by id matches direct stats",
			direct: func() (any, error) {
				return stats.ProjectByID(ctx, st, "proj-001", period, 1, 10)
			},
			wrapped: func() (any, error) {
				return wrapper.ProjectByID(ctx, "proj-001", period, 1, 10)
			},
			minSourceIDs: 2,
		},
		{
			name: "session by id matches direct stats",
			direct: func() (any, error) {
				return stats.SessionByID(ctx, st, "ses-001")
			},
			wrapped: func() (any, error) {
				return wrapper.SessionByID(ctx, "ses-001")
			},
			minSourceIDs: 2,
		},
		{
			name: "messages matches direct stats",
			direct: func() (any, error) {
				return stats.MessagesByPeriod(ctx, st, period, 1, 10, stats.DefaultMessageSort())
			},
			wrapped: func() (any, error) {
				return wrapper.Messages(ctx, period, 1, 10, stats.DefaultMessageSort())
			},
			minSourceIDs: 2,
		},
		{
			name: "message by id matches direct stats",
			direct: func() (any, error) {
				return stats.MessageByID(ctx, st, "msg-002-04")
			},
			wrapped: func() (any, error) {
				return wrapper.MessageByID(ctx, "msg-002-04")
			},
			minSourceIDs: 1,
		},
		{
			name: "config matches direct stats",
			direct: func() (any, error) {
				return stats.Config(ctx, st)
			},
			wrapped: func() (any, error) {
				return wrapper.Config(ctx)
			},
			minSourceIDs: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			direct, err := tt.direct()
			if err != nil {
				t.Fatalf("direct stats failed: %v", err)
			}

			wrapped, err := tt.wrapped()
			if err != nil {
				t.Fatalf("wrapped stats failed: %v", err)
			}

			assertJSONEqualExcept(t, direct, wrapped, "source_id", "cost_status", "cost_provenance", "truncation")
			assertJSONField(t, wrapped, "source_id", string(source.SourceOpenCode))
			assertSourceIDs(t, wrapped, string(source.SourceOpenCode), tt.minSourceIDs)
		})
	}
}

func assertJSONEqualExcept(t *testing.T, want any, got any, ignoredFields ...string) {
	t.Helper()

	wantJSON := marshalJSONValue(t, want)
	gotJSON := marshalJSONValue(t, got)
	stripJSONFields(wantJSON, ignoredFields...)
	stripJSONFields(gotJSON, ignoredFields...)

	if !reflect.DeepEqual(gotJSON, wantJSON) {
		t.Errorf("wrapped output mismatch after removing additive metadata\ngot:  %#v\nwant: %#v", gotJSON, wantJSON)
	}
}

func assertSourceIDs(t *testing.T, value any, want string, minCount int) {
	t.Helper()

	ids := collectJSONFieldValues(marshalJSONValue(t, value), "source_id")
	if len(ids) < minCount {
		t.Errorf("source_id annotations = %d, want at least %d", len(ids), minCount)
	}
	for _, got := range ids {
		if got != want {
			t.Errorf("source_id = %v, want %v", got, want)
		}
	}
}

func assertJSONField(t *testing.T, value any, field string, want any) {
	t.Helper()

	gotMap := marshalObjectMap(t, value)
	got, ok := gotMap[field]
	if !ok {
		t.Fatalf("field %q missing from %#v", field, gotMap)
	}
	if got != want {
		t.Errorf("field %q = %v, want %v", field, got, want)
	}
}

func marshalObjectMap(t *testing.T, value any) map[string]any {
	t.Helper()

	out := marshalJSONValue(t, value)
	obj, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("unmarshal object map: got %T, want object", out)
	}
	return obj
}

func marshalJSONValue(t *testing.T, value any) any {
	t.Helper()

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}

	var out any
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatalf("unmarshal JSON value: %v", err)
	}
	return out
}

func stripJSONFields(value any, ignoredFields ...string) {
	ignored := make(map[string]struct{}, len(ignoredFields))
	for _, field := range ignoredFields {
		ignored[field] = struct{}{}
	}
	stripJSONFieldsSet(value, ignored)
}

func stripJSONFieldsSet(value any, ignored map[string]struct{}) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if _, ok := ignored[key]; ok {
				delete(v, key)
				continue
			}
			stripJSONFieldsSet(child, ignored)
		}
	case []any:
		for _, child := range v {
			stripJSONFieldsSet(child, ignored)
		}
	}
}

func collectJSONFieldValues(value any, field string) []any {
	switch v := value.(type) {
	case map[string]any:
		var values []any
		for key, child := range v {
			if key == field {
				values = append(values, child)
			}
			values = append(values, collectJSONFieldValues(child, field)...)
		}
		return values
	case []any:
		var values []any
		for _, child := range v {
			values = append(values, collectJSONFieldValues(child, field)...)
		}
		return values
	default:
		return nil
	}
}
