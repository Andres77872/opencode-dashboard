package stats

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opencode-dashboard/internal/store"
	"opencode-dashboard/internal/store/fixture"
)

var v2Day = time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

// v2DayQuery covers the whole fixture day.
var v2DayQuery = PeriodQuery{From: "2025-01-15", To: "2025-01-15"}

func at(offset time.Duration) time.Time { return v2Day.Add(offset) }

// buildV2Fixture models what OpenCode 2 writes:
//
//   - ses_alpha (project alpha): a prompt, two assistant steps, a settled and a
//     running compaction, and bookkeeping rows that are not messages.
//   - ses_beta (project beta): one priced step, one step that failed before
//     usage was recorded, and one the user aborted.
//   - ses_fork: a fork of ses_beta carrying copies of its parent's rows (with
//     the parent's timestamps) plus one new turn.
//   - ses_child: a subagent session of ses_alpha with a failed compaction that
//     was still billed and has no model.
func buildV2Fixture(t *testing.T) *store.Store {
	t.Helper()
	b := fixture.NewV2Builder().
		AddProject(fixture.NewProject("prj_alpha", "/work/alpha").Name("alpha")).
		AddProject(fixture.NewProject("prj_beta", "/work/beta")).
		AddSession(fixture.V2Session{ID: "ses_alpha", ProjectID: "prj_alpha", Title: "Alpha work", Created: at(0)}).
		AddSession(fixture.V2Session{ID: "ses_beta", ProjectID: "prj_beta", Title: "Beta work", Created: at(time.Hour)}).
		AddSession(fixture.V2Session{ID: "ses_fork", ProjectID: "prj_beta", Title: "Beta work (fork #1)", ForkOf: "ses_beta", Created: at(2 * time.Hour)}).
		AddSession(fixture.V2Session{ID: "ses_child", ProjectID: "prj_alpha", Title: "Explore", ParentID: "ses_alpha", Created: at(3 * time.Hour)})

	shell := fixture.V2Tool("call_1", "shell", map[string]any{
		"status":  "completed",
		"input":   map[string]any{"command": "ls"},
		"content": []any{map[string]any{"type": "text", "text": "file1"}},
	})
	readFailed := fixture.V2Tool("call_2", "read", map[string]any{
		"status": "error",
		"input":  map[string]any{"path": "/missing"},
		"error":  map[string]any{"type": "tool.execution", "message": "boom"},
	})
	screenshot := fixture.V2Tool("call_3", "shell", map[string]any{
		"status": "completed",
		"input":  map[string]any{"command": "shot"},
		"content": []any{
			map[string]any{"type": "text", "text": "captured"},
			map[string]any{"type": "file", "uri": "data:image/png;base64,AAAA", "mime": "image/png"},
		},
	})

	rows := []fixture.V2Row{
		// ses_alpha
		{ID: "msg_a_u1", SessionID: "ses_alpha", Type: "agent-switched", Seq: 0, Created: at(time.Second), Data: map[string]any{"agent": "build"}},
		{ID: "msg_a_u2", SessionID: "ses_alpha", Type: "user", Seq: 1, Created: at(2 * time.Second), Data: fixture.V2User("hello")},
		{ID: "msg_a_a1", SessionID: "ses_alpha", Type: "assistant", Seq: 2, Created: at(3 * time.Second), Data: fixture.V2Assistant("openai", "gpt-x",
			&fixture.V2Usage{Cost: 0.10, Input: 100, Output: 20, Reasoning: 5, CacheRead: 50, CacheWrite: 10},
			fixture.V2Reasoning("think"), shell, fixture.V2Text("done"))},
		{ID: "msg_a_a2", SessionID: "ses_alpha", Type: "assistant", Seq: 3, Created: at(4 * time.Second), Data: fixture.V2Assistant("openai", "gpt-x",
			&fixture.V2Usage{Cost: 0.05, Input: 50, Output: 10}, readFailed, fixture.V2Text("ok"))},
		{ID: "msg_a_c1", SessionID: "ses_alpha", Type: "compaction", Seq: 4, Created: at(5 * time.Second), Data: fixture.V2Compaction("completed",
			&fixture.V2Usage{Cost: 0.02, Input: 30, Output: 5}, map[string]any{"id": "claude-y", "providerID": "anthropic"})},
		{ID: "msg_a_c2", SessionID: "ses_alpha", Type: "compaction", Seq: 5, Created: at(6 * time.Second), Data: fixture.V2Compaction("running", nil, nil)},
		{ID: "msg_a_sy", SessionID: "ses_alpha", Type: "synthetic", Seq: 6, Created: at(7 * time.Second), Data: map[string]any{"text": "reminder"}},
		{ID: "msg_a_sh", SessionID: "ses_alpha", Type: "shell", Seq: 7, Created: at(8 * time.Second), Data: map[string]any{"shellID": "sh_1", "command": "make", "status": "completed"}},
		{ID: "msg_a_id", SessionID: "ses_alpha", Type: "idle", Seq: 8, Created: at(9 * time.Second), Data: map[string]any{"outcome": "succeeded"}},
		// ses_beta
		{ID: "msg_b_u1", SessionID: "ses_beta", Type: "user", Seq: 0, Created: at(time.Hour + time.Second), Data: fixture.V2User("fix it")},
		{ID: "msg_b_a1", SessionID: "ses_beta", Type: "assistant", Seq: 1, Created: at(time.Hour + 2*time.Second), Data: fixture.V2Assistant("anthropic", "claude-y",
			&fixture.V2Usage{Cost: 0.30, Input: 300, Output: 60}, screenshot)},
		{ID: "msg_b_a2", SessionID: "ses_beta", Type: "assistant", Seq: 2, Created: at(time.Hour + 3*time.Second), Data: withError(fixture.V2Assistant("anthropic", "claude-y", nil), "provider.auth", "bad key")},
		{ID: "msg_b_a3", SessionID: "ses_beta", Type: "assistant", Seq: 3, Created: at(time.Hour + 4*time.Second), Data: withError(fixture.V2Assistant("anthropic", "claude-y", nil), "aborted", "aborted")},
		// ses_fork: copies keep the parent's timestamps; only the new turn counts.
		{ID: "msg_fork_0", SessionID: "ses_fork", Type: "user", Seq: 0, Created: at(time.Hour + time.Second), Data: fixture.V2User("fix it")},
		{ID: "msg_fork_1", SessionID: "ses_fork", Type: "assistant", Seq: 1, Created: at(time.Hour + 2*time.Second), Data: fixture.V2Assistant("anthropic", "claude-y",
			&fixture.V2Usage{Cost: 0.30, Input: 300, Output: 60}, screenshot)},
		{ID: "msg_f_u1", SessionID: "ses_fork", Type: "user", Seq: 4, Created: at(2*time.Hour + time.Second), Data: fixture.V2User("continue")},
		{ID: "msg_f_a1", SessionID: "ses_fork", Type: "assistant", Seq: 5, Created: at(2*time.Hour + 2*time.Second), Data: fixture.V2Assistant("anthropic", "claude-y",
			&fixture.V2Usage{Cost: 0.01, Input: 10, Output: 2})},
		// ses_child
		{ID: "msg_c_a1", SessionID: "ses_child", Type: "assistant", Seq: 0, Created: at(3*time.Hour + time.Second), Data: fixture.V2Assistant("openai", "gpt-x",
			&fixture.V2Usage{Cost: 0.04, Input: 40, Output: 8})},
		{ID: "msg_c_c1", SessionID: "ses_child", Type: "compaction", Seq: 1, Created: at(3*time.Hour + 2*time.Second), Data: withError(fixture.V2Compaction("failed",
			&fixture.V2Usage{Cost: 0.005, Input: 5, Output: 1}, nil), "provider.error", "overloaded")},
	}
	for _, row := range rows {
		b.AddRow(row)
	}

	ctx := context.Background()
	path, err := b.Build(ctx)
	if err != nil {
		t.Fatalf("build v2 fixture: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Dir(path)) })
	st, err := store.Connect(ctx, path)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if st.Version() != store.SchemaV2 {
		t.Fatalf("fixture detected as %v, want v2", st.Version())
	}
	return st
}

func withError(data map[string]any, errorType, message string) map[string]any {
	data["error"] = map[string]any{"type": errorType, "message": message}
	return data
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// Counted rows: alpha u2,a1,a2,c1; beta u1,a1,a2,a3; fork u1,a1; child a1,c1.
const (
	v2WantMessages = 12
	v2WantRequests = 9
	v2WantCost     = 0.10 + 0.05 + 0.02 + 0.30 + 0.01 + 0.04 + 0.005
)

var v2WantTokens = TokenStats{
	Input:     100 + 50 + 30 + 300 + 10 + 40 + 5,
	Output:    20 + 10 + 5 + 60 + 2 + 8 + 1,
	Reasoning: 5,
	Cache:     CacheStats{Read: 50, Write: 10},
}

func TestV2Overview(t *testing.T) {
	st := buildV2Fixture(t)
	got, err := Overview(context.Background(), st, v2DayQuery)
	if err != nil {
		t.Fatal(err)
	}
	if got.Sessions != 4 || got.Messages != v2WantMessages || got.Requests != v2WantRequests || got.Days != 1 {
		t.Errorf("sessions/messages/requests/days = %d/%d/%d/%d, want 4/%d/%d/1", got.Sessions, got.Messages, got.Requests, got.Days, v2WantMessages, v2WantRequests)
	}
	if !approx(got.Cost, v2WantCost) {
		t.Errorf("cost = %v, want %v (fork copies must not be counted twice)", got.Cost, v2WantCost)
	}
	if got.Tokens != v2WantTokens {
		t.Errorf("tokens = %+v, want %+v", got.Tokens, v2WantTokens)
	}
}

func TestV2PeriodAllStartsAtEarliestActivity(t *testing.T) {
	st := buildV2Fixture(t)
	window, err := ComputePeriodWindowFromQuery(context.Background(), st, PeriodQuery{Period: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC); !window.StartDate.Equal(want) {
		t.Errorf("all starts at %v, want %v", window.StartDate, want)
	}
}

func TestV2Daily(t *testing.T) {
	st := buildV2Fixture(t)
	ctx := context.Background()

	daily, err := Daily(ctx, st, v2DayQuery, GranularityDay)
	if err != nil {
		t.Fatal(err)
	}
	if len(daily.Days) != 1 {
		t.Fatalf("days = %d, want 1", len(daily.Days))
	}
	day := daily.Days[0]
	if day.Sessions != 4 || day.Messages != v2WantMessages || day.Requests != v2WantRequests || !approx(day.Cost, v2WantCost) || day.Tokens != v2WantTokens {
		t.Errorf("day = %+v", day)
	}

	hourly, err := Daily(ctx, st, v2DayQuery, GranularityHour)
	if err != nil {
		t.Fatal(err)
	}
	byHour := map[string]DayStats{}
	for _, bucket := range hourly.Days {
		byHour[bucket.Date] = bucket
	}
	// The fork's copies predate it, so its 11:00 history stays with ses_beta.
	wantHours := map[string][3]int64{ // sessions, messages, requests
		"2025-01-15T10:00:00Z": {1, 4, 3},
		"2025-01-15T11:00:00Z": {1, 4, 3},
		"2025-01-15T12:00:00Z": {1, 2, 1},
		"2025-01-15T13:00:00Z": {1, 2, 2},
	}
	for hour, want := range wantHours {
		bucket := byHour[hour]
		if got := [3]int64{bucket.Sessions, bucket.Messages, bucket.Requests}; got != want {
			t.Errorf("%s sessions/messages/requests = %v, want %v", hour, got, want)
		}
	}
}

func TestV2DailyModelDimension(t *testing.T) {
	st := buildV2Fixture(t)
	got, err := DailyDimension(context.Background(), st, "model", v2DayQuery, GranularityDay)
	if err != nil {
		t.Fatal(err)
	}
	byModel := map[string]DimensionDayStats{}
	for _, row := range got.Days {
		byModel[row.Dimension] = row
	}
	if len(byModel) != 2 {
		t.Fatalf("dimensions = %v, want gpt-x and claude-y", got.Days)
	}
	if row := byModel["gpt-x"]; row.Messages != 3 || !approx(row.Cost, 0.19) || row.Sessions != 2 {
		t.Errorf("gpt-x = %+v", row)
	}
	if row := byModel["claude-y"]; row.Messages != 5 || !approx(row.Cost, 0.33) || row.Sessions != 3 {
		t.Errorf("claude-y = %+v", row)
	}
}

func TestV2Models(t *testing.T) {
	st := buildV2Fixture(t)
	got, err := Models(context.Background(), st, v2DayQuery)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Models) != 2 {
		t.Fatalf("models = %+v, want 2 (the model-less compaction is not attributed)", got.Models)
	}
	claude, gpt := got.Models[0], got.Models[1]
	if claude.ModelID != "claude-y" || claude.ProviderID != "anthropic" || claude.Sessions != 3 || claude.Messages != 5 || !approx(claude.Cost, 0.33) {
		t.Errorf("first model = %+v", claude)
	}
	if claude.Tokens.Input != 340 || claude.Tokens.Output != 67 {
		t.Errorf("claude tokens = %+v", claude.Tokens)
	}
	if gpt.ModelID != "gpt-x" || gpt.ProviderID != "openai" || gpt.Sessions != 2 || gpt.Messages != 3 || !approx(gpt.Cost, 0.19) {
		t.Errorf("second model = %+v", gpt)
	}
}

func TestV2Tools(t *testing.T) {
	st := buildV2Fixture(t)
	got, err := Tools(context.Background(), st, v2DayQuery)
	if err != nil {
		t.Fatal(err)
	}
	want := []ToolEntry{
		{Name: "shell", Invocations: 2, Successes: 2, Sessions: 2},
		{Name: "read", Invocations: 1, Failures: 1, Sessions: 1},
	}
	if len(got.Tools) != len(want) {
		t.Fatalf("tools = %+v, want %+v", got.Tools, want)
	}
	for i := range want {
		if got.Tools[i] != want[i] {
			t.Errorf("tool %d = %+v, want %+v", i, got.Tools[i], want[i])
		}
	}

	// The legacy streaming path reads 1.x parts; 2.x always uses SQL.
	t.Setenv("OPCODE_TOOLS_LEGACY", "true")
	legacy, err := Tools(context.Background(), st, v2DayQuery)
	if err != nil || len(legacy.Tools) != len(want) {
		t.Errorf("legacy flag on 2.x: %+v, %v", legacy.Tools, err)
	}
}

func TestV2Projects(t *testing.T) {
	st := buildV2Fixture(t)
	ctx := context.Background()
	got, err := Projects(ctx, st, v2DayQuery)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Projects) != 2 {
		t.Fatalf("projects = %+v", got.Projects)
	}
	beta, alpha := got.Projects[0], got.Projects[1]
	if beta.ProjectID != "prj_beta" || beta.ProjectName != "beta" || beta.Sessions != 2 || beta.Messages != 4 || !approx(beta.Cost, 0.31) {
		t.Errorf("beta = %+v", beta)
	}
	if alpha.ProjectID != "prj_alpha" || alpha.ProjectName != "alpha" || alpha.Sessions != 2 || alpha.Messages != 5 || !approx(alpha.Cost, 0.215) {
		t.Errorf("alpha = %+v", alpha)
	}

	detail, err := ProjectByID(ctx, st, "prj_beta", v2DayQuery, 1, 10)
	if err != nil || detail == nil {
		t.Fatalf("ProjectByID: %+v, %v", detail, err)
	}
	if detail.Sessions != 2 || detail.Messages != 4 || !approx(detail.Cost, 0.31) || detail.TotalSessions != 2 {
		t.Errorf("beta detail = %+v", detail)
	}
	if len(detail.RecentSessions) != 2 || detail.RecentSessions[0].ID != "ses_fork" {
		t.Fatalf("recent sessions = %+v", detail.RecentSessions)
	}
	if fork := detail.RecentSessions[0]; fork.MessageCount != 2 || !approx(fork.Cost, 0.01) {
		t.Errorf("fork recent session = %+v, want only its own turn", fork)
	}
}

func TestV2Sessions(t *testing.T) {
	st := buildV2Fixture(t)
	ctx := context.Background()
	got, err := SessionsWithQuery(ctx, st, SessionQuery{Page: 1, PageSize: 10, Sort: SessionSortCost, Period: "", From: "2025-01-15", To: "2025-01-15"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 4 || len(got.Sessions) != 4 {
		t.Fatalf("sessions = %d/%+v", got.Total, got.Sessions)
	}
	if top := got.Sessions[0]; top.ID != "ses_beta" || top.MessageCount != 3 || !approx(top.Cost, 0.30) {
		t.Errorf("costliest session = %+v", top)
	}

	filtered, err := SessionsWithQuery(ctx, st, SessionQuery{Page: 1, PageSize: 10, Filter: "fork", From: "2025-01-15", To: "2025-01-15"})
	if err != nil || filtered.Total != 1 || filtered.Sessions[0].ID != "ses_fork" {
		t.Errorf("filtered = %+v, %v", filtered, err)
	}

	other, err := SessionsWithQuery(ctx, st, SessionQuery{Page: 1, PageSize: 10, From: "2025-01-16", To: "2025-01-16"})
	if err != nil || other.Total != 0 {
		t.Errorf("sessions active on another day = %+v, %v", other, err)
	}
}

func TestV2SessionByID(t *testing.T) {
	st := buildV2Fixture(t)
	ctx := context.Background()

	alpha, err := SessionByID(ctx, st, "ses_alpha")
	if err != nil || alpha == nil {
		t.Fatalf("SessionByID: %+v, %v", alpha, err)
	}
	if alpha.Title != "Alpha work" || alpha.ProjectName != "alpha" || alpha.Directory != "/work" {
		t.Errorf("metadata = %+v", alpha)
	}
	if alpha.MessageCount != 4 || !approx(alpha.TotalCost, 0.17) {
		t.Fatalf("messages/cost = %d/%v", alpha.MessageCount, alpha.TotalCost)
	}
	wantRoles := []string{"user", "assistant", "assistant", "assistant"}
	for i, msg := range alpha.Messages {
		if msg.Role != wantRoles[i] {
			t.Errorf("message %d role = %q, want %q", i, msg.Role, wantRoles[i])
		}
	}
	if compaction := alpha.Messages[3]; compaction.Agent != "compaction" || compaction.ModelID != "claude-y" || compaction.UsageStatus != UsageStatusRecorded {
		t.Errorf("compaction = %+v", compaction)
	}
	if step := alpha.Messages[1]; step.Agent != "build" || step.ModelID != "gpt-x" || step.ProviderID != "openai" || step.Tokens == nil || step.Tokens.Cache.Write != 10 {
		t.Errorf("step = %+v", step)
	}

	beta, err := SessionByID(ctx, st, "ses_beta")
	if err != nil || beta == nil {
		t.Fatalf("SessionByID(beta): %v", err)
	}
	failed, aborted := beta.Messages[2], beta.Messages[3]
	if failed.Tokens != nil || failed.UsageStatus != UsageStatusUnavailable || failed.UsageUnavailableReason != UsageUnavailableFailed {
		t.Errorf("failed step = %+v", failed)
	}
	if aborted.Tokens != nil || aborted.UsageUnavailableReason != UsageUnavailableCancelled {
		t.Errorf("aborted step = %+v", aborted)
	}

	fork, err := SessionByID(ctx, st, "ses_fork")
	if err != nil || fork == nil || fork.MessageCount != 2 || !approx(fork.TotalCost, 0.01) {
		t.Errorf("fork detail = %+v, %v", fork, err)
	}

	missing, err := SessionByID(ctx, st, "ses_missing")
	if err != nil || missing != nil {
		t.Errorf("missing session = %+v, %v", missing, err)
	}
}

func TestV2Messages(t *testing.T) {
	st := buildV2Fixture(t)
	ctx := context.Background()
	got, err := MessagesByPeriod(ctx, st, v2DayQuery, 1, 50, MessageSort{Field: MessageSortCost, Direction: MessageSortDesc})
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != v2WantMessages || len(got.Messages) != v2WantMessages {
		t.Fatalf("messages = %d/%d", got.Total, len(got.Messages))
	}
	top := got.Messages[0]
	if top.ID != "msg_b_a1" || top.SessionTitle != "Beta work" || top.ModelID != "claude-y" || top.ProviderID != "anthropic" || !approx(top.Cost, 0.30) {
		t.Errorf("costliest message = %+v", top)
	}
	reasons := map[UsageUnavailableReason]int{}
	for _, msg := range got.Messages {
		if msg.UsageStatus == UsageStatusUnavailable {
			reasons[msg.UsageUnavailableReason]++
		}
	}
	if reasons[UsageUnavailableFailed] != 1 || reasons[UsageUnavailableCancelled] != 1 || len(reasons) != 2 {
		t.Errorf("unavailable usage reasons = %v", reasons)
	}

	for _, sort := range []MessageSort{
		{Field: MessageSortTime, Direction: MessageSortAsc},
		{Field: MessageSortTokens, Direction: MessageSortDesc},
		{Field: MessageSortModel, Direction: MessageSortAsc},
		{Field: MessageSortRole, Direction: MessageSortDesc},
	} {
		if list, err := MessagesByPeriod(ctx, st, v2DayQuery, 1, 5, sort); err != nil || len(list.Messages) != 5 {
			t.Errorf("sort %+v: %d messages, %v", sort, len(list.Messages), err)
		}
	}
}

func TestV2MessageByID(t *testing.T) {
	st := buildV2Fixture(t)
	ctx := context.Background()

	step, err := MessageByID(ctx, st, "msg_a_a1")
	if err != nil || step == nil {
		t.Fatalf("MessageByID: %+v, %v", step, err)
	}
	if step.Role != "assistant" || step.ModelID != "gpt-x" || !approx(step.Cost, 0.10) || step.Tokens == nil || step.Tokens.Input != 100 {
		t.Errorf("step entry = %+v", step.MessageEntry)
	}
	content := step.Content
	if len(content.TextParts) != 1 || content.TextParts[0].Text != "done" || len(content.ReasoningParts) != 1 || content.ReasoningParts[0].Text != "think" {
		t.Errorf("text/reasoning = %+v / %+v", content.TextParts, content.ReasoningParts)
	}
	if len(content.ToolParts) != 1 {
		t.Fatalf("tools = %+v", content.ToolParts)
	}
	tool := content.ToolParts[0]
	if tool.Tool != "shell" || tool.CallID != "call_1" || tool.State.Status != "completed" || tool.State.Output != "file1" || tool.State.Input["command"] != "ls" {
		t.Errorf("tool = %+v", tool)
	}
	if tool.State.Time == nil || tool.State.Time.Start != 1000 || tool.State.Time.End != 2000 {
		t.Errorf("tool time = %+v", tool.State.Time)
	}

	failedTool, err := MessageByID(ctx, st, "msg_a_a2")
	if err != nil || len(failedTool.Content.ToolParts) != 1 || failedTool.Content.ToolParts[0].State.Error != "boom" {
		t.Errorf("failed tool = %+v, %v", failedTool, err)
	}
	fileTool, err := MessageByID(ctx, st, "msg_b_a1")
	if err != nil || len(fileTool.Content.ToolParts) != 1 || fileTool.Content.ToolParts[0].State.Output != "captured\n[file: image/png]" {
		t.Errorf("file tool = %+v, %v", fileTool, err)
	}

	user, err := MessageByID(ctx, st, "msg_a_u2")
	if err != nil || user.Role != "user" || user.Tokens != nil || len(user.Content.TextParts) != 1 || user.Content.TextParts[0].Text != "hello" {
		t.Errorf("user = %+v, %v", user, err)
	}
	compaction, err := MessageByID(ctx, st, "msg_a_c1")
	if err != nil || len(compaction.Content.TextParts) != 1 || compaction.Content.TextParts[0].Text != "summary of earlier work" {
		t.Errorf("compaction = %+v, %v", compaction, err)
	}

	for _, id := range []string{"msg_a_sy", "msg_a_c2", "msg_fork_1", "msg_missing"} {
		if got, err := MessageByID(ctx, st, id); err != nil || got != nil {
			t.Errorf("MessageByID(%s) = %+v, %v; want not a message", id, got, err)
		}
	}
}

// TestV2RangeQueriesSeekTimeIndex pins that the message relation stays
// flattenable: session_message rows embed whole transcripts, so a range query
// that materializes the relation (or scans the table) would parse every
// message ever written instead of seeking the requested window.
func TestV2RangeQueriesSeekTimeIndex(t *testing.T) {
	st := buildV2Fixture(t)
	queries := map[string]string{
		"overview": overviewV2Query,
		"daily":    messageStatsByBucketV2Query(TrendBucketSQL("m.time_created", GranularityDay)),
		"models":   modelsV2Query,
		"tools":    toolsV2Query,
		"messages": messagesCountV2Query,
		"trend":    dailyDimensionV2Query("model", validDimensions["model"], TrendBucketSQL("m.time_created", GranularityDay)),
	}
	for name, query := range queries {
		rows, err := st.DB().QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+query, int64(1), int64(2))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, detail)
		}
		rows.Close()
		joined := strings.Join(plan, "\n")
		if !strings.Contains(joined, "SEARCH m USING INDEX session_message_time_created_idx") || strings.Contains(joined, "MATERIALIZE") || strings.Contains(joined, "CO-ROUTINE") {
			t.Errorf("%s plan does not seek the time index on a flattened relation:\n%s", name, joined)
		}
	}
}
