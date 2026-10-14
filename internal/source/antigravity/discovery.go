package antigravity

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

var safeConversationID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type conversation struct {
	id, title, parent, agent, project, workspace string
	updated                                      time.Time
}
type stepRef struct {
	conversation string
	index        int64
}

type readDatabase struct {
	*sql.DB
	temporary string
}

func (db *readDatabase) Close() error {
	err := db.DB.Close()
	if db.temporary != "" {
		_ = os.RemoveAll(db.temporary)
	}
	return err
}

// SQLite may create SHM files even for mode=ro WAL readers. Use a private
// temporary snapshot, validating DB/WAL generations across the copy, so the
// original source directory is never opened for writing.
func openReadDB(ctx context.Context, path string) (*readDatabase, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "opencode-antigravity-read-")
	if err != nil {
		return nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	copyPath := filepath.Join(dir, "snapshot.db")
	stable := false
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			cleanup()
			return nil, err
		}
		before, err := os.Stat(path)
		if err != nil {
			cleanup()
			return nil, err
		}
		walBefore, walErr := os.Stat(path + "-wal")
		if walErr != nil && !os.IsNotExist(walErr) {
			cleanup()
			return nil, walErr
		}
		if err := copyReadFile(ctx, path, copyPath); err != nil {
			cleanup()
			return nil, err
		}
		_ = os.Remove(copyPath + "-wal")
		if walBefore != nil {
			if err := copyReadFile(ctx, path+"-wal", copyPath+"-wal"); err != nil {
				if os.IsNotExist(err) {
					continue
				}
				cleanup()
				return nil, err
			}
		}
		after, err := os.Stat(path)
		if err != nil {
			continue
		}
		walAfter, _ := os.Stat(path + "-wal")
		if sameFileState(before, after) && sameFileState(walBefore, walAfter) {
			stable = true
			break
		}
	}
	if !stable {
		cleanup()
		return nil, fmt.Errorf("conversation changed while taking a read snapshot")
	}
	u := url.URL{Scheme: "file", Path: copyPath}
	db, err := sql.Open("sqlite", u.String()+"?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		cleanup()
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		cleanup()
		return nil, err
	}
	return &readDatabase{DB: db, temporary: dir}, nil
}

func sameFileState(a, b os.FileInfo) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime() == b.ModTime()
}
func copyReadFile(ctx context.Context, from, to string) error {
	input, err := os.Open(from)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(to, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	buf := make([]byte, 128<<10)
	for {
		if err := ctx.Err(); err != nil {
			output.Close()
			return err
		}
		n, err := input.Read(buf)
		if n > 0 {
			if _, e := output.Write(buf[:n]); e != nil {
				output.Close()
				return e
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			output.Close()
			return err
		}
	}
	return output.Close()
}

func (s *Source) loadSnapshot(ctx context.Context, pricing pricingSnapshot) (*snapshot, map[string]stepRef, error) {
	snap := &snapshot{home: s.opts.Home, projectMap: map[string]*projectRecord{}, sessionMap: map[string]*sessionRecord{}, messageMap: map[string]*messageRecord{}, seenSteps: map[string]bool{}, ordered: []*messageRecord{}, diagnostics: source.SourceDiagnostics{Status: "ok"}}
	refs := map[string]stepRef{}
	conversations := map[string]conversation{}
	if db, err := openReadDB(ctx, filepath.Join(s.opts.Home, "conversation_summaries.db")); err == nil {
		rows, err := db.QueryContext(ctx, `SELECT conversation_id,title,parent_conversation_id,agent_name,project_id,workspace_uris,last_modified_time FROM conversation_summaries`)
		if err == nil {
			for rows.Next() {
				var c conversation
				var workspace, updated string
				if err := rows.Scan(&c.id, &c.title, &c.parent, &c.agent, &c.project, &workspace, &updated); err != nil {
					rows.Close()
					db.Close()
					return nil, nil, err
				}
				if !safeConversationID.MatchString(c.id) {
					snap.diagnostics.UnsupportedEvents++
					continue
				}
				c.workspace = workspacePath(workspace)
				c.updated = parseTime(updated)
				conversations[c.id] = c
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				db.Close()
				return nil, nil, err
			}
			rows.Close()
		} else {
			snap.diagnostics.UnsupportedEvents++
			snap.diagnostics.Reason = "conversation summary schema is unsupported"
		}
		db.Close()
	} else if !os.IsNotExist(err) {
		snap.diagnostics.UnsupportedEvents++
		snap.diagnostics.Reason = "conversation summaries could not be read; grouping metadata may be incomplete"
	}
	files, err := filepath.Glob(filepath.Join(s.opts.Home, "conversations", "*.db"))
	if err != nil {
		return nil, nil, err
	}
	fileIDs := make(map[string]bool)
	for _, path := range files {
		fileIDs[strings.TrimSuffix(filepath.Base(path), ".db")] = true
	}
	for _, path := range files {
		id := strings.TrimSuffix(filepath.Base(path), ".db")
		if safeConversationID.MatchString(id) {
			if _, exists := conversations[id]; !exists {
				conversations[id] = conversation{id: id}
			}
		}
	}
	if len(files) == 0 {
		return snap, refs, source.UnavailableSourceError{ID: source.SourceAntigravity, Reason: "no Antigravity conversation databases found"}
	}
	// Originals precede replayed copies so attribution remains deterministic.
	sort.Strings(files)
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		id := strings.TrimSuffix(filepath.Base(path), ".db")
		if !safeConversationID.MatchString(id) {
			continue
		}
		c := conversations[id]
		root := c
		visited := map[string]bool{id: true}
		for root.parent != "" {
			parent, exists := conversations[root.parent]
			if !exists || !fileIDs[root.parent] || visited[root.parent] {
				snap.diagnostics.UnsupportedEvents++
				snap.diagnostics.Reason = "a conversation parent is missing or cyclic"
				break
			}
			visited[parent.id] = true
			root = parent
		}
		projectID := root.project
		if projectID == "" {
			projectID = root.workspace
		}
		if projectID == "" {
			projectID = "unknown"
		}
		name := filepath.Base(root.workspace)
		if name == "." || name == "" {
			name = "Unknown project"
		}
		project := snap.ensureProject(projectID, name, redactDisplayPath(root.workspace))
		session := snap.ensureSession(root.id, project, project.Worktree)
		session.Title, _ = redactText(root.title)
		if session.Title == "" {
			session.Title = "Antigravity session"
		}
		if err := s.readConversation(ctx, path, c, session, project, snap, refs, pricing); err != nil {
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			snap.diagnostics.UnsupportedEvents++
			snap.diagnostics.Reason = "a conversation database or its schema could not be read"
			continue
		}
		snap.diagnostics.ScannedFiles++
	}
	if snap.diagnostics.ScannedFiles == 0 {
		return snap, refs, source.UnavailableSourceError{ID: source.SourceAntigravity, Reason: snap.diagnostics.Reason}
	}
	if snap.diagnostics.UnsupportedEvents > 0 || snap.diagnostics.MalformedLines > 0 {
		snap.diagnostics.Status = "partial"
		if snap.diagnostics.Reason == "" {
			snap.diagnostics.Reason = "some persisted records have unsupported or malformed metadata"
		}
	}
	sort.Slice(snap.ordered, func(i, j int) bool {
		a, b := snap.ordered[i].Entry, snap.ordered[j].Entry
		if a.TimeCreated.Equal(b.TimeCreated) {
			return a.ID < b.ID
		}
		return a.TimeCreated.Before(b.TimeCreated)
	})
	for _, session := range snap.sessionMap {
		sort.Slice(session.Messages, func(i, j int) bool {
			return session.Messages[i].Entry.TimeCreated.Before(session.Messages[j].Entry.TimeCreated)
		})
		for _, msg := range session.Messages {
			msg.Entry.SessionTitle = session.Title
		}
	}
	return snap, refs, nil
}

func (s *Source) readConversation(ctx context.Context, path string, c conversation, session *sessionRecord, project *projectRecord, snap *snapshot, refs map[string]stepRef, pricing pricingSnapshot) error {
	db, err := openReadDB(ctx, path)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"steps", "gen_metadata"} {
		var oversized int64
		column := "metadata"
		if table == "gen_metadata" {
			column = "data"
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM `+table+` WHERE length(`+column+`)>?`, maxBlobBytes).Scan(&oversized); err != nil {
			return err
		}
		snap.diagnostics.UnsupportedEvents += oversized
	}
	// Probe only the schema we use. Reject unknown layouts instead of treating
	// an unreadable history as a measured zero.
	rows, err := tx.QueryContext(ctx, `SELECT idx,data FROM gen_metadata WHERE length(data)<=?`, maxBlobBytes)
	if err != nil {
		return err
	}
	generations := map[int64]protoMessage{}
	for rows.Next() {
		var idx int64
		var data []byte
		if err := rows.Scan(&idx, &data); err != nil {
			rows.Close()
			return err
		}
		m, err := decodeProto(data)
		if err != nil {
			snap.diagnostics.MalformedLines++
			continue
		}
		generations[idx] = m
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	rows, err = tx.QueryContext(ctx, `SELECT idx,step_type,status,step_format,coalesce(metadata,x'') FROM steps WHERE length(coalesce(metadata,x''))<=? ORDER BY idx`, maxBlobBytes)
	if err != nil {
		return err
	}
	defer rows.Close()
	var lastPlanner *messageRecord
	var inspected, supported int64
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var index, typ, status, format int64
		var blob []byte
		if err := rows.Scan(&index, &typ, &status, &format, &blob); err != nil {
			return err
		}
		inspected++
		if format != 0 {
			snap.diagnostics.UnsupportedEvents++
			continue
		}
		supported++
		meta, err := decodeProto(blob)
		if err != nil {
			snap.diagnostics.MalformedLines++
			continue
		}
		origin, err := meta.child(20)
		if err != nil {
			snap.diagnostics.MalformedLines++
			continue
		}
		originID, originIndex := origin.text(1), origin.integer(2)
		if originID == "" {
			originID, originIndex = c.id, index
		}
		id := fmt.Sprintf("antigravity:%s:%d", originID, originIndex)
		if snap.seenSteps[id] {
			continue
		}
		// Imported rows retain their original identity. Their native database
		// will supply accounting; copies must not be charged to a fork again.
		if origin.text(4) != "" && origin.text(4) != c.id {
			continue
		}
		snap.seenSteps[id] = true
		created := protoTime(meta.bytes(1))
		if created.IsZero() {
			created = c.updated
		}
		if created.IsZero() {
			snap.diagnostics.UnsupportedEvents++
			continue
		}
		role := "tool"
		if typ == 14 {
			role = "user"
		} else if typ == 15 {
			role = "assistant"
		}
		if typ != 14 && typ != 15 && len(meta.bytes(4)) == 0 {
			continue
		}
		msg := newMessage(session, project, id, role, created)
		msg.Entry.Agent = c.agent
		msg.Entry.IsSubagent = c.id != session.ID
		if typ == 15 {
			usage := meta.bytes(9)
			msg.Entry.UsageStatus = stats.UsageStatusRecorded
			var gen protoMessage
			if origin.text(1) != "" {
				gen = generations[origin.integer(3)]
			}
			response, err := gen.child(1)
			if err != nil {
				snap.diagnostics.MalformedLines++
			}
			msg.Entry.ModelID = response.text(19)
			msg.Entry.ProviderID = providerForModel(msg.Entry.ModelID)
			if len(usage) == 0 {
				usage = response.bytes(4)
				if len(usage) > 0 {
					msg.Entry.UsageStatus = stats.UsageStatusRecovered
				}
			}
			msg.Entry.Tokens, err = usageTokens(usage)
			if err != nil {
				snap.diagnostics.MalformedLines++
			}
			if msg.Entry.Tokens == nil {
				msg.Entry.UsageStatus = stats.UsageStatusUnavailable
				msg.Entry.UsageUnavailableReason = stats.UsageUnavailableUnknown
			}
			msg.Entry.RequestTrace = stats.RequestTraceInferred
			if msg.Entry.ModelID == "" { // Pinned enum fallback only for verified values.
				model, _ := decodeProto(usage)
				switch model.integer(1) {
				case 1417:
					msg.Entry.ModelID = "gemini-4-argon"
				case 1322:
					msg.Entry.ModelID = "gemini-3.8-flash-tiered"
				}
				msg.Entry.ProviderID = providerForModel(msg.Entry.ModelID)
			}
			pricing.price(msg)
		}
		if callData := meta.bytes(4); len(callData) > 0 {
			call, err := decodeProto(callData)
			if err != nil {
				snap.diagnostics.MalformedLines++
			} else {
				name := call.text(2)
				if name != "" {
					callID := call.text(1)
					if callID == "" {
						callID = id + ":tool"
					}
					state := "partial"
					if status == 3 {
						state = "completed"
					} else if status == 7 || status == 4 {
						state = "error"
					}
					msg.ToolParts = append(msg.ToolParts, stats.ToolPart{SourceID: antigravitySourceID, Type: "tool", CallID: callID, Tool: name, State: stats.ToolState{Status: state, Title: name}})
				}
			}
		}
		if typ != 14 && typ != 15 && lastPlanner != nil {
			for _, tool := range msg.ToolParts {
				lastPlanner.ToolParts = append(lastPlanner.ToolParts, tool)
				refs[lastPlanner.Entry.ID+":"+tool.CallID] = stepRef{c.id, index}
			}
			updateSessionTimes(session, created)
			continue
		}
		register(snap, session, msg)
		refs[id] = stepRef{c.id, index}
		updateSessionTimes(session, created)
		if typ == 15 {
			lastPlanner = msg
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if inspected > 0 && supported == 0 {
		return fmt.Errorf("unsupported Antigravity step layout")
	}
	return tx.Commit()
}

func workspacePath(raw string) string {
	// Workspace arrays have existed both as strings and protobuf-JSON objects.
	var items []any
	if json.Unmarshal([]byte(raw), &items) != nil {
		return ""
	}
	for _, item := range items {
		var value string
		switch v := item.(type) {
		case string:
			value = v
		case map[string]any:
			for _, key := range []string{"workspace_uri", "workspaceUri", "uri"} {
				if s, ok := v[key].(string); ok {
					value = s
					break
				}
			}
		}
		if u, err := url.Parse(value); err == nil && u.Scheme == "file" {
			return u.Path
		}
	}
	return ""
}

func parseTime(value string) time.Time {
	value = strings.Replace(value, " ", "T", 1)
	t, _ := time.Parse(time.RFC3339Nano, value)
	return t.UTC()
}
func providerForModel(model string) string {
	if strings.HasPrefix(model, "claude") {
		return "anthropic"
	}
	if strings.HasPrefix(model, "gemini") {
		return "google"
	}
	return ""
}
