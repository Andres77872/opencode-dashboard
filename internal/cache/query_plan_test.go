package cache

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// planCorpusSessions/planCorpusPerSession describe a corpus big enough that
// ANALYZE gives the planner the row-count ratios it sees in a real cache. With
// an empty database SQLite guesses, and the guesses do not match production.
const (
	planCorpusSessions   = 2000
	planCorpusPerSession = 40

	// A window covering roughly one percent of the seeded span, so a time-range
	// filter is selective enough for the planner to prefer the time index when
	// no better one exists.
	planWindowStartMs = int64(100_000_000)
	planWindowEndMs   = planWindowStartMs + int64(2_000_000)
)

// TestHotReadPathsUseCoveringIndexes pins the query plan of the reads that grow
// with cache size. These queries all filter on an indexed prefix and then want
// their rows ordered or bounded by time; if an index stops carrying
// time_created_ms, SQLite silently falls back to sorting the match set into a
// temporary B-tree (or to scanning the whole time window and filtering after),
// which is invisible in results and only shows up as a slow dashboard on a
// large cache.
func TestHotReadPathsUseCoveringIndexes(t *testing.T) {
	store := newTestStore(t)
	seedQueryPlanCorpus(t, store)

	cases := []struct {
		name string
		sql  string
		args []any
		// wantIndex must appear in the plan; wantNoSort forbids a sort of the
		// match set (SQLite reports a residual sort of a trailing tiebreak
		// column as "LAST TERM OF ORDER BY", which is bounded and allowed).
		wantIndex  string
		wantNoSort bool
	}{
		{
			name: "session detail reads one session's messages in time order",
			sql: `SELECT message_id FROM message_index
				WHERE source_id = ? AND session_id = ?
				ORDER BY time_created_ms ASC, message_id ASC`,
			args:       []any{"opencode", "session-00001"},
			wantIndex:  "idx_message_index_source_session",
			wantNoSort: true,
		},
		{
			name: "project detail bounds one project to a period window",
			sql: `SELECT COUNT(*) FROM message_index
				WHERE source_id = ? AND project_id = ? AND time_created_ms >= ? AND time_created_ms < ?`,
			// A narrow window, so the time index is a genuine alternative the
			// planner must reject; a full-range window would let it pick the
			// project index either way and prove nothing.
			args:       []any{"opencode", "project-01", planWindowStartMs, planWindowEndMs},
			wantIndex:  "idx_message_index_source_project",
			wantNoSort: true,
		},
		{
			name: "per-project session list pages newest first",
			sql: `SELECT session_id FROM sessions
				WHERE source_id = ? AND project_id = ?
				ORDER BY time_created_ms DESC LIMIT ? OFFSET ?`,
			args:       []any{"opencode", "project-01", 10, 0},
			wantIndex:  "idx_sessions_source_project",
			wantNoSort: true,
		},
		{
			name: "message window pages newest first",
			sql: `SELECT message_id FROM message_index
				WHERE source_id = ? AND time_created_ms >= ? AND time_created_ms < ?
				ORDER BY time_created_ms DESC LIMIT ?`,
			args:       []any{"opencode", planWindowStartMs, planWindowEndMs, 10},
			wantIndex:  "idx_message_index_source_time",
			wantNoSort: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := queryPlan(t, store, tc.sql, tc.args...)
			if !strings.Contains(plan, tc.wantIndex) {
				t.Errorf("plan does not use %s:\n%s", tc.wantIndex, plan)
			}
			// "LAST TERM OF ORDER BY" is a bounded tiebreak sort, not a sort of
			// the whole match set, so it is not a regression.
			if sorted := strings.Contains(plan, "USE TEMP B-TREE FOR ORDER BY"); tc.wantNoSort && sorted {
				t.Errorf("plan sorts the match set instead of reading it in index order:\n%s", plan)
			}
			if strings.Contains(plan, "SCAN message_index") || strings.Contains(plan, "SCAN sessions") {
				t.Errorf("plan falls back to a full table scan:\n%s", plan)
			}
		})
	}
}

func queryPlan(t *testing.T, s *Store, query string, args ...any) string {
	t.Helper()
	rows, err := s.db.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		lines = append(lines, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate plan: %v", err)
	}
	return strings.Join(lines, "\n")
}

// seedQueryPlanCorpus writes messages spread over many sessions and a smaller
// number of projects, then runs ANALYZE so the planner chooses from real
// statistics rather than defaults.
func seedQueryPlanCorpus(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin seed: %v", err)
	}
	defer rollback(tx)

	messages, err := tx.PrepareContext(ctx, `INSERT INTO message_index(source_id, message_id, session_id, role, time_created_ms, project_id) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		t.Fatalf("prepare messages: %v", err)
	}
	defer messages.Close()
	sessions, err := tx.PrepareContext(ctx, `INSERT INTO sessions(source_id, session_id, title, project_id, time_created_ms, time_updated_ms) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		t.Fatalf("prepare sessions: %v", err)
	}
	defer sessions.Close()

	for i := range planCorpusSessions {
		sessionID := fmt.Sprintf("session-%05d", i)
		projectID := fmt.Sprintf("project-%02d", i%50)
		base := int64(i) * 100_000
		if _, err := sessions.ExecContext(ctx, "opencode", sessionID, "Session "+sessionID, projectID, base, base+1000); err != nil {
			t.Fatalf("seed session: %v", err)
		}
		for j := range planCorpusPerSession {
			id := fmt.Sprintf("%s-msg-%03d", sessionID, j)
			if _, err := messages.ExecContext(ctx, "opencode", id, sessionID, "assistant", base+int64(j)*100, projectID); err != nil {
				t.Fatalf("seed message: %v", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit seed: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `ANALYZE`); err != nil {
		t.Fatalf("analyze: %v", err)
	}
}
