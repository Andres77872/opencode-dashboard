package cache

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"opencode-dashboard/internal/source/codex"
	"opencode-dashboard/internal/stats"
)

func TestCacheBackedCodexOverviewAndMessagesMatchLiveSource(t *testing.T) {
	ctx := context.Background()
	live := codex.New(codex.Options{
		CodexHome:  filepath.Join("..", "source", "codex", "testdata", "valid_home"),
		PathSource: "test fixture",
	})
	store := newTestStore(t)
	if err := store.SyncSource(ctx, live); err != nil {
		t.Fatalf("SyncSource() failed: %v", err)
	}
	cached := WrapSource(store, live)
	period := stats.PeriodQuery{Period: "all"}

	liveOverview, err := live.Overview(ctx, period)
	if err != nil {
		t.Fatalf("live Overview() failed: %v", err)
	}
	cacheOverview, err := cached.Overview(ctx, period)
	if err != nil {
		t.Fatalf("cached Overview() failed: %v", err)
	}
	if cacheOverview.SourceID != liveOverview.SourceID {
		t.Errorf("cached SourceID = %q, want %q", cacheOverview.SourceID, liveOverview.SourceID)
	}
	if cacheOverview.Sessions != liveOverview.Sessions || cacheOverview.Messages != liveOverview.Messages {
		t.Errorf("cached overview sessions/messages = %d/%d, want %d/%d", cacheOverview.Sessions, cacheOverview.Messages, liveOverview.Sessions, liveOverview.Messages)
	}
	if cacheOverview.Tokens != liveOverview.Tokens {
		t.Errorf("cached tokens = %#v, want %#v", cacheOverview.Tokens, liveOverview.Tokens)
	}

	liveMessages, err := live.Messages(ctx, period, 1, 100, stats.MessageSort{Field: stats.MessageSortTime, Direction: stats.MessageSortAsc})
	if err != nil {
		t.Fatalf("live Messages() failed: %v", err)
	}
	cacheMessages, err := cached.Messages(ctx, period, 1, 100, stats.MessageSort{Field: stats.MessageSortTime, Direction: stats.MessageSortAsc})
	if err != nil {
		t.Fatalf("cached Messages() failed: %v", err)
	}
	if cacheMessages.Total != liveMessages.Total || len(cacheMessages.Messages) != len(liveMessages.Messages) {
		t.Fatalf("cached messages total/len = %d/%d, want %d/%d", cacheMessages.Total, len(cacheMessages.Messages), liveMessages.Total, len(liveMessages.Messages))
	}
	for i := range liveMessages.Messages {
		if cacheMessages.Messages[i].ID != liveMessages.Messages[i].ID {
			t.Fatalf("cached message[%d].ID = %q, want %q", i, cacheMessages.Messages[i].ID, liveMessages.Messages[i].ID)
		}
		if strings.Contains(cacheMessages.Messages[i].SessionTitle, "Summarize") {
			t.Errorf("cached session title appears transcript-derived: %q", cacheMessages.Messages[i].SessionTitle)
		}
	}
}

func TestCacheDoesNotPersistCodexConversationContent(t *testing.T) {
	ctx := context.Background()
	live := codex.New(codex.Options{
		CodexHome:  filepath.Join("..", "source", "codex", "testdata", "privacy_home"),
		PathSource: "test fixture",
	})
	store := newTestStore(t)
	if err := store.SyncSource(ctx, live); err != nil {
		t.Fatalf("SyncSource() failed: %v", err)
	}

	for _, forbidden := range []string{
		"SYNTHETIC_PROMPT_SECRET_MUST_NOT_LEAK",
		"SYNTHETIC_ASSISTANT_SECRET_MUST_NOT_LEAK",
		"SYNTHETIC_TOOL_ARG_SECRET_MUST_NOT_LEAK",
		"SYNTHETIC_TOOL_OUTPUT_SECRET_MUST_NOT_LEAK",
		"SYNTHETIC_PATCH_SECRET_MUST_NOT_LEAK",
		"SYNTHETIC_NON_ROLLOUT_SENTINEL_MUST_NOT_LEAK",
	} {
		if location := findTextInCache(t, store.db, forbidden); location != "" {
			t.Fatalf("cache persisted forbidden text %q at %s", forbidden, location)
		}
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "usage-cache.sqlite"))
	if err != nil {
		t.Fatalf("Open() failed: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func findTextInCache(t *testing.T, db *sql.DB, text string) string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatalf("scan table: %v", err)
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate tables: %v", err)
	}
	rows.Close()

	for _, table := range tables {
		cols, err := db.Query(`PRAGMA table_info(` + table + `)`)
		if err != nil {
			t.Fatalf("pragma %s: %v", table, err)
		}
		var textColumns []string
		for cols.Next() {
			var cid int
			var name, typ string
			var notNull int
			var defaultValue any
			var pk int
			if err := cols.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
				cols.Close()
				t.Fatalf("scan column: %v", err)
			}
			if !strings.Contains(strings.ToUpper(typ), "TEXT") {
				continue
			}
			textColumns = append(textColumns, name)
		}
		if err := cols.Err(); err != nil {
			cols.Close()
			t.Fatalf("iterate columns: %v", err)
		}
		cols.Close()

		for _, name := range textColumns {
			var count int
			query := `SELECT COUNT(*) FROM ` + table + ` WHERE ` + name + ` LIKE ?`
			if err := db.QueryRow(query, "%"+text+"%").Scan(&count); err != nil {
				t.Fatalf("search %s.%s: %v", table, name, err)
			}
			if count > 0 {
				return table + "." + name
			}
		}
	}
	return ""
}
