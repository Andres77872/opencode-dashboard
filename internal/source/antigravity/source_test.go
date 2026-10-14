package antigravity

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opencode-dashboard/internal/pricingalias"
	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

func wireInt(field int, value uint64) []byte {
	b := binary.AppendUvarint(nil, uint64(field<<3))
	return binary.AppendUvarint(b, value)
}
func wireBytes(field int, value []byte) []byte {
	b := binary.AppendUvarint(nil, uint64(field<<3|2))
	b = binary.AppendUvarint(b, uint64(len(value)))
	return append(b, value...)
}
func wire(parts ...[]byte) []byte {
	var result []byte
	for _, p := range parts {
		result = append(result, p...)
	}
	return result
}

func fixture(t *testing.T) (string, *sql.DB) {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "conversations"), 0700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(home, "conversation_summaries.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE conversation_summaries(conversation_id TEXT,title TEXT,parent_conversation_id TEXT,agent_name TEXT,project_id TEXT,workspace_uris TEXT,last_modified_time TEXT)`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return home, db
}

func addConversation(t *testing.T, home string, summary *sql.DB, id, parent string, recoverUsage bool) {
	t.Helper()
	_, err := summary.Exec(`INSERT INTO conversation_summaries VALUES(?,?,?,?,?,?,?)`, id, "A session", parent, "research", "project", `["file:///home/user/project"]`, "2026-10-14T07:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(home, "conversations", id+".db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE gen_metadata(idx INTEGER PRIMARY KEY,data BLOB);CREATE TABLE steps(idx INTEGER PRIMARY KEY,step_type INTEGER,status INTEGER,step_format INTEGER,metadata BLOB);`)
	if err != nil {
		t.Fatal(err)
	}
	usage := wire(wireInt(1, 1417), wireInt(2, 2292), wireInt(3, 337), wireInt(5, 3732), wireInt(9, 115), wireInt(10, 222))
	response := wire(wireBytes(4, usage), wireBytes(19, []byte("gemini-4-argon")))
	if _, err = db.Exec(`INSERT INTO gen_metadata VALUES(0,?)`, wireBytes(1, response)); err != nil {
		t.Fatal(err)
	}
	stamp := wireInt(1, uint64(time.Date(2026, 10, 14, 7, 0, 0, 0, time.UTC).Unix()))
	origin := wire(wireBytes(1, []byte(id)), wireInt(2, 1))
	meta := wire(wireBytes(1, stamp), wireBytes(20, origin))
	if !recoverUsage {
		meta = append(meta, wireBytes(9, usage)...)
	}
	if _, err = db.Exec(`INSERT INTO steps VALUES(1,15,3,0,?)`, meta); err != nil {
		t.Fatal(err)
	}
	call := wire(wireBytes(1, []byte("call-1")), wireBytes(2, []byte("view_file")), wireBytes(3, []byte(`{"api_key":"SYNTHETIC_SECRET","path":"/home/user/project/file.go"}`)))
	toolMeta := wire(wireBytes(1, stamp), wireBytes(4, call), wireBytes(20, wire(wireBytes(1, []byte(id)), wireInt(2, 2))))
	if _, err = db.Exec(`INSERT INTO steps VALUES(2,132,3,0,?)`, toolMeta); err != nil {
		t.Fatal(err)
	}
	logs := filepath.Join(home, "brain", id, ".system_generated", "logs")
	os.MkdirAll(logs, 0700)
	content := `{"step_index":1,"content":"Hello","thinking":"Reasoning"}` + "\n" + `{"step_index":2,"content":"api_key=SYNTHETIC_SECRET"}` + "\n"
	for _, name := range []string{"transcript.jsonl", "transcript_full.jsonl"} {
		if err := os.WriteFile(filepath.Join(logs, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSQLiteAccountingGroupingAndLazyPrivacy(t *testing.T) {
	home, db := fixture(t)
	addConversation(t, home, db, "parent", "", false)
	addConversation(t, home, db, "child", "parent", true)
	s := New(Options{Home: home})
	ctx := context.Background()
	overview, err := s.Overview(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if overview.Sessions != 1 || overview.Requests != 2 || overview.Tokens.Input != 4584 || overview.Tokens.Cache.Read != 7464 || overview.Tokens.Output != 444 || overview.Tokens.Reasoning != 230 {
		t.Fatalf("wrong accounting: %+v", overview)
	}
	if overview.RequestAccounting.UsageRecovered != 1 || overview.RequestAccounting.TraceCoverage != stats.TraceCoverageSuccessfulOnly {
		t.Fatalf("wrong provenance: %+v", overview.RequestAccounting)
	}
	detail, err := s.MessageByID(ctx, "antigravity:child:1")
	if err != nil {
		t.Fatal(err)
	}
	if !detail.IsSubagent || detail.SessionID != "parent" || len(detail.Content.ToolParts) != 1 || len(detail.Content.TextParts) != 1 {
		t.Fatalf("wrong detail: %+v", detail)
	}
	encoded, _ := json.Marshal(detail)
	if strings.Contains(string(encoded), "SYNTHETIC_SECRET") {
		t.Fatal("detail leaked secret")
	}
	data, err := s.ConsolidationData(ctx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ = json.Marshal(data)
	for _, secret := range []string{"Hello", "Reasoning", "SYNTHETIC_SECRET", "A session"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("cache contains %s", secret)
		}
	}
	if len(data.Messages) != 2 || len(data.Messages[0].Tools) != 1 {
		t.Fatalf("wrong cache accounting: %+v", data)
	}
}

func TestProtoBoundsAndIndependentTokenCounters(t *testing.T) {
	for _, blob := range [][]byte{{0}, {10, 255}, {8, 128}, wire(wireInt(2, 1), wireInt(3, 4), wireInt(9, 3), wireInt(10, 4))} {
		if _, err := usageTokens(blob); err == nil {
			t.Fatalf("accepted malformed usage %x", blob)
		}
	}
	u, err := usageTokens(wire(wireInt(2, 2), wireInt(3, 3), wireInt(5, 9), wireInt(9, 1)))
	if err != nil || u.Input != 2 || u.Cache.Read != 9 || u.Output != 2 || u.Reasoning != 1 {
		t.Fatalf("wrong independent counters: %+v %v", u, err)
	}
}

func TestDatedPricingAndUnknownModel(t *testing.T) {
	p := readPricing()
	for _, test := range []struct {
		model, at string
		input     float64
	}{{"gemini-4-argon", "2026-11-30", 2}, {"gemini-4-argon", "2026-12-01", 4}, {"gemini-3.8-flash", "2026-12-31", .75}, {"gemini-3.8-flash", "2027-01-01", 1.5}} {
		at, _ := time.Parse("2006-01-02", test.at)
		r, ok := p.resolve("google", test.model, at)
		if !ok || r.Rate.InputPerMillion != test.input {
			t.Fatalf("wrong dated price: %+v", r)
		}
	}
	if _, ok := p.resolve("google", "gemini-3.8-flash-tiered", time.Now()); ok {
		t.Fatal("internal model received guessed price")
	}
}

func TestTranscriptChunksCrossRecordBoundary(t *testing.T) {
	home := t.TempDir()
	chunks := filepath.Join(home, "brain", "parent", ".system_generated", "logs", "chunks", "transcript_full")
	os.MkdirAll(chunks, 0700)
	for i, body := range []string{`{"step_index":1,"con`, `tent":"Across chunks"}` + "\n" + `{"step_index":2`} {
		name := []string{"00000000.jsonl", "00000001.jsonl"}[i]
		os.WriteFile(filepath.Join(chunks, name), []byte(body), 0600)
	}
	record, err := New(Options{Home: home}).transcript(context.Background(), stepRef{"parent", 1})
	if err != nil || record.Content != "Across chunks" {
		t.Fatalf("chunks: %+v %v", record, err)
	}
}

func TestLiveAntigravityReadOnly(t *testing.T) {
	home := os.Getenv("OPENCODE_DASHBOARD_TEST_ANTIGRAVITY_HOME")
	if home == "" {
		t.Skip("explicit live review only")
	}
	s := New(Options{Home: home})
	v, err := s.Overview(context.Background(), stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Requests == 0 || v.Tokens.Input == 0 {
		t.Fatalf("no live usage: %+v", v)
	}
	models, err := s.Models(context.Background(), stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("diagnostics=%+v", s.Info(context.Background()).Diagnostics)
	t.Logf("sessions=%d generations=%d models=%d input=%d cache=%d", v.Sessions, v.Requests, len(models.Models), v.Tokens.Input, v.Tokens.Cache.Read)
	if s.Info(context.Background()).Diagnostics.MalformedLines > 0 {
		t.Fatal("live schema had malformed accounting")
	}
	_ = source.SourceAntigravity
}

func TestLiveDatabaseSnapshots(t *testing.T) {
	home := os.Getenv("OPENCODE_DASHBOARD_TEST_ANTIGRAVITY_HOME")
	if home == "" {
		t.Skip("explicit live review only")
	}
	files, _ := filepath.Glob(filepath.Join(home, "conversations", "*.db"))
	for _, path := range files {
		db, err := openReadDB(context.Background(), path)
		if err != nil {
			t.Logf("open: %v", err)
			continue
		}
		var n int
		err = db.QueryRow(`SELECT count(*) FROM steps`).Scan(&n)
		db.Close()
		t.Logf("%s rows=%d error=%v", filepath.Base(path), n, err)
	}
}

func TestReadSnapshotIncludesWALWithoutChangingOriginalFiles(t *testing.T) {
	home, summary := fixture(t)
	addConversation(t, home, summary, "parent", "", false)
	path := filepath.Join(home, "conversations", "parent.db")
	writer, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	writer.SetMaxOpenConns(1)
	if _, err := writer.Exec(`PRAGMA journal_mode=WAL;PRAGMA wal_autocheckpoint=0;INSERT INTO steps VALUES(3,101,3,0,x'')`); err != nil {
		t.Fatal(err)
	}
	beforeDB, _ := os.ReadFile(path)
	beforeWAL, _ := os.ReadFile(path + "-wal")
	reader, err := openReadDB(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	temp := reader.temporary
	var n int
	err = reader.QueryRow(`SELECT count(*) FROM steps`).Scan(&n)
	reader.Close()
	if err != nil || n != 3 {
		t.Fatalf("WAL snapshot: %d %v", n, err)
	}
	afterDB, _ := os.ReadFile(path)
	afterWAL, _ := os.ReadFile(path + "-wal")
	if string(beforeDB) != string(afterDB) || string(beforeWAL) != string(afterWAL) {
		t.Fatal("read modified source database or WAL")
	}
	if _, err := os.Stat(temp); !os.IsNotExist(err) {
		t.Fatal("private snapshot was retained")
	}
}

func mutateConversation(t *testing.T, home, id string, mutate func(*sql.DB)) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(home, "conversations", id+".db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mutate(db)
}

func TestReplayIdentityMissingParentsAndToolErrors(t *testing.T) {
	home, summary := fixture(t)
	addConversation(t, home, summary, "parent", "", false)
	addConversation(t, home, summary, "child", "parent", true)
	addConversation(t, home, summary, "orphan", "missing", false)
	mutateConversation(t, home, "child", func(db *sql.DB) {
		stamp := wireInt(1, uint64(time.Date(2026, 10, 14, 7, 0, 0, 0, time.UTC).Unix()))
		original := wire(wireBytes(1, []byte("parent-trajectory")), wireInt(2, 1), wireBytes(4, []byte("parent")))
		usage := wire(wireInt(2, 999999), wireInt(3, 99999))
		replay := wire(wireBytes(1, stamp), wireBytes(20, original), wireBytes(9, usage))
		if _, err := db.Exec(`INSERT INTO steps VALUES(3,15,3,0,?)`, replay); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE steps SET status=7 WHERE idx=2`); err != nil {
			t.Fatal(err)
		}
	})
	s := New(Options{Home: home})
	v, err := s.Overview(context.Background(), stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Requests != 3 || v.Sessions != 2 || v.Tokens.Input != 2292*3 {
		t.Fatalf("replay counted: %+v", v)
	}
	tools, err := s.Tools(context.Background(), stats.PeriodQuery{Period: "all"})
	if err != nil || len(tools.Tools) != 1 || tools.Tools[0].Invocations != 3 {
		t.Fatalf("tools=%+v error=%v", tools, err)
	}
	detail, err := s.MessageByID(context.Background(), "antigravity:child:1")
	if err != nil || detail.Content.ToolParts[0].State.Status != "error" {
		t.Fatalf("tool failure=%+v %v", detail, err)
	}
	if s.Info(context.Background()).Diagnostics.Status != "partial" {
		t.Fatal("missing parent diagnostic absent")
	}
}

func TestUnsupportedLayoutAndUnavailableUsage(t *testing.T) {
	home, summary := fixture(t)
	addConversation(t, home, summary, "unsupported", "", false)
	mutateConversation(t, home, "unsupported", func(db *sql.DB) {
		if _, err := db.Exec(`UPDATE steps SET step_format=99`); err != nil {
			t.Fatal(err)
		}
	})
	s := New(Options{Home: home})
	if _, err := s.Overview(context.Background(), stats.PeriodQuery{Period: "all"}); err == nil {
		t.Fatal("unknown schema became measured zero")
	}
	if s.Info(context.Background()).Available {
		t.Fatal("unknown schema remained available")
	}
	mutateConversation(t, home, "unsupported", func(db *sql.DB) {
		if _, err := db.Exec(`UPDATE steps SET step_format=0;DELETE FROM gen_metadata;UPDATE steps SET metadata=x'0a060880ecb7d606' WHERE idx=1`); err != nil {
			t.Fatal(err)
		}
	})
	s.Invalidate()
	v, err := s.Overview(context.Background(), stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if v.RequestAccounting.UsageUnavailable != 1 || v.CostStatus != stats.CostMissing {
		t.Fatalf("missing usage=%+v", v)
	}
}

func TestTranscriptPreferenceAndUpdates(t *testing.T) {
	home, summary := fixture(t)
	addConversation(t, home, summary, "parent", "", false)
	root := filepath.Join(home, "brain", "parent", ".system_generated", "logs")
	if err := os.WriteFile(filepath.Join(root, "transcript.jsonl"), []byte(`{"step_index":1,"content":"Wrong representation"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(filepath.Join(root, "transcript_full.jsonl"), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.WriteString(`{"step_index":1,"content":"Updated content"}` + "\n" + `{"step_index":1`)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	s := New(Options{Home: home})
	detail, err := s.MessageByID(context.Background(), "antigravity:parent:1")
	if err != nil || detail.Content.TextParts[0].Text != "Updated content" {
		t.Fatalf("updated content=%+v %v", detail, err)
	}
	v, err := s.Overview(context.Background(), stats.PeriodQuery{Period: "all"})
	if err != nil || v.Requests != 1 {
		t.Fatalf("updates counted as requests: %+v %v", v, err)
	}
}

func TestPricingAliasesAndReasoningChargedOnce(t *testing.T) {
	home, summary := fixture(t)
	addConversation(t, home, summary, "parent", "", false)
	aliases, err := pricingalias.Open(context.Background(), filepath.Join(t.TempDir(), "settings.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer aliases.Close()
	if _, err := aliases.Upsert(context.Background(), pricingalias.Alias{SourceID: source.SourceAntigravity, ProviderID: "google", ModelID: "gemini-4-argon", TargetModelID: "gemini-3.8-flash"}); err != nil {
		t.Fatal(err)
	}
	s := New(Options{Home: home, PricingAliases: aliases})
	v, err := s.Overview(context.Background(), stats.PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatal(err)
	}
	want := (2292*.75 + 3732*.075 + 337*3.75) / 1e6
	if math.Abs(v.Cost-want) > 1e-12 || v.CostStatus != stats.CostEstimatedAPIEquivalent {
		t.Fatalf("cost=%g want=%g", v.Cost, want)
	}
	if s.ResolvePricing(context.Background(), "google", "gemini-4-argon").Kind != source.PricingResolutionUserAlias {
		t.Fatal("alias was ignored")
	}
}
