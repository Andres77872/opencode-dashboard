package kimicode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"opencode-dashboard/internal/source"
)

type agentFile struct {
	Path    string
	AgentID string
	ModTime time.Time
	Size    int64
	Root    bool
}

type sessionFiles struct {
	ID              string
	Dir             string
	WorkspaceKey    string
	StatePath       string
	LegacyStatePath string
	IndexPath       string
	IndexWorkDir    string
	ModTime         time.Time
	Agents          []agentFile
	RootWire        *agentFile
}

type discoveryResult struct {
	sessions    []sessionFiles
	diagnostics source.SourceDiagnostics
	available   bool
}

func discoverSessions(ctx context.Context, kimiHome string) discoveryResult {
	root := filepath.Join(kimiHome, "sessions")
	diag := source.SourceDiagnostics{Status: "ok"}
	if err := ctx.Err(); err != nil {
		diag.Status = "unavailable"
		diag.Reason = err.Error()
		return discoveryResult{diagnostics: diag}
	}

	info, err := os.Stat(root)
	if err != nil {
		diag.Status = "unavailable"
		switch {
		case os.IsNotExist(err):
			diag.Reason = "Kimi Code sessions directory not found: " + root
		case os.IsPermission(err):
			diag.Reason = "Kimi Code sessions directory is not readable: " + root
		default:
			diag.Reason = "Kimi Code sessions directory cannot be accessed: " + err.Error()
		}
		return discoveryResult{diagnostics: diag}
	}
	if !info.IsDir() {
		diag.Status = "unavailable"
		diag.Reason = "Kimi Code sessions path is not a directory: " + root
		return discoveryResult{diagnostics: diag}
	}

	indexByID, indexByDir, indexDiag := readSessionIndexes(ctx, kimiHome)
	diag.MalformedLines += indexDiag.MalformedLines
	if indexDiag.Reason != "" {
		diag.Reason = appendReason(diag.Reason, indexDiag.Reason)
	}

	byDir := make(map[string]*sessionFiles)
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				diag.Reason = appendReason(diag.Reason, "a Kimi Code session path disappeared during discovery")
				return nil
			}
			if os.IsPermission(walkErr) {
				diag.Reason = appendReason(diag.Reason, "a Kimi Code session path could not be read due to permissions")
				return nil
			}
			return walkErr
		}
		if d.IsDir() {
			switch strings.ToLower(d.Name()) {
			case "logs", "plans", "media", "images":
				return filepath.SkipDir
			}
			return nil
		}

		switch d.Name() {
		case "state.json":
			sessionDir, nested, ok := stateSessionDir(root, path)
			if !ok {
				return nil
			}
			item := ensureSessionFiles(byDir, sessionDir)
			if nested {
				item.LegacyStatePath = path
			} else {
				item.StatePath = path
			}
			if fileInfo, err := d.Info(); err == nil {
				item.ModTime = laterTime(item.ModTime, fileInfo.ModTime().UTC())
			}
		case "wire.jsonl":
			fileInfo, err := d.Info()
			if err != nil {
				if os.IsNotExist(err) || os.IsPermission(err) {
					return nil
				}
				return err
			}
			agentDir := filepath.Dir(path)
			agentsDir := filepath.Dir(agentDir)
			if filepath.Base(agentsDir) == "agents" {
				sessionDir := filepath.Dir(agentsDir)
				if !validSessionDir(root, sessionDir) {
					return nil
				}
				item := ensureSessionFiles(byDir, sessionDir)
				item.Agents = append(item.Agents, agentFile{
					Path: path, AgentID: filepath.Base(agentDir),
					ModTime: fileInfo.ModTime().UTC(), Size: fileInfo.Size(),
				})
				item.ModTime = laterTime(item.ModTime, fileInfo.ModTime().UTC())
				diag.ScannedFiles++
				return nil
			}

			// Old releases also wrote a UI-oriented wire at the session root.
			// It is only considered later, after we know whether agents/main is
			// present and whether the root contains canonical agent records.
			sessionDir := agentDir
			if !validSessionDir(root, sessionDir) {
				return nil
			}
			item := ensureSessionFiles(byDir, sessionDir)
			rootFile := agentFile{
				Path: path, AgentID: "main", ModTime: fileInfo.ModTime().UTC(),
				Size: fileInfo.Size(), Root: true,
			}
			item.RootWire = &rootFile
			item.ModTime = laterTime(item.ModTime, fileInfo.ModTime().UTC())
			diag.ScannedFiles++
		}
		return nil
	})
	if walkErr != nil {
		diag.Status = "unavailable"
		diag.Reason = "Kimi Code sessions directory cannot be scanned: " + walkErr.Error()
		return discoveryResult{diagnostics: diag}
	}

	sessions := make([]sessionFiles, 0, len(byDir))
	for _, item := range byDir {
		if indexed, ok := indexByID[item.ID]; ok {
			applySessionIndex(item, indexed)
		} else if indexed, ok := indexByDir[filepath.Clean(item.Dir)]; ok {
			applySessionIndex(item, indexed)
		}
		sort.Slice(item.Agents, func(i, j int) bool {
			if item.Agents[i].AgentID != item.Agents[j].AgentID {
				return item.Agents[i].AgentID < item.Agents[j].AgentID
			}
			return item.Agents[i].Path < item.Agents[j].Path
		})
		sessions = append(sessions, *item)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].Dir < sessions[j].Dir })

	if diag.ScannedFiles == 0 {
		diag.Status = "empty"
		diag.Reason = "no Kimi Code agent wire JSONL transcripts found"
		return discoveryResult{sessions: sessions, diagnostics: diag}
	}
	return discoveryResult{sessions: sessions, diagnostics: diag, available: true}
}

func ensureSessionFiles(byDir map[string]*sessionFiles, dir string) *sessionFiles {
	if item := byDir[dir]; item != nil {
		return item
	}
	id := strings.TrimPrefix(filepath.Base(dir), "session_")
	if id == "" {
		id = filepath.Base(dir)
	}
	item := &sessionFiles{
		ID:           id,
		Dir:          dir,
		WorkspaceKey: filepath.Base(filepath.Dir(dir)),
	}
	byDir[dir] = item
	return item
}

func validSessionDir(root, dir string) bool {
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." || rel == "" || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return false
	}
	parts := strings.Split(filepath.Clean(rel), string(filepath.Separator))
	return len(parts) >= 2
}

func stateSessionDir(root, path string) (dir string, nested bool, ok bool) {
	dir = filepath.Dir(path)
	if filepath.Base(dir) == "session-meta" {
		dir = filepath.Dir(dir)
		nested = true
	}
	return dir, nested, validSessionDir(root, dir)
}

type sessionIndexEntry struct {
	SessionID  string    `json:"sessionId"`
	ID         string    `json:"id"`
	SessionDir string    `json:"sessionDir"`
	WorkDir    string    `json:"workDir"`
	Cwd        string    `json:"cwd"`
	Path       string    `json:"-"`
	ModTime    time.Time `json:"-"`
}

func readSessionIndexes(ctx context.Context, kimiHome string) (map[string]sessionIndexEntry, map[string]sessionIndexEntry, parseDiagnostics) {
	byID := make(map[string]sessionIndexEntry)
	byDir := make(map[string]sessionIndexEntry)
	var diag parseDiagnostics
	paths := []string{
		filepath.Join(kimiHome, "session_index.jsonl"),
		filepath.Join(kimiHome, "sessions", "session_index.jsonl"),
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			diag.Reason = appendReason(diag.Reason, "a Kimi Code session index could not be read")
			continue
		}
		lineDiag, err := readBoundedLines(ctx, path, maxWireLineBytes, func(_ int, line []byte, oversized bool) {
			if oversized {
				return
			}
			if strings.TrimSpace(string(line)) == "" {
				return
			}
			var entry sessionIndexEntry
			if json.Unmarshal(line, &entry) != nil {
				diag.MalformedLines++
				return
			}
			entry.Path = path
			entry.ModTime = info.ModTime().UTC()
			id := strings.TrimSpace(entry.SessionID)
			if id == "" {
				id = strings.TrimSpace(entry.ID)
			}
			if id != "" {
				byID[id] = entry
				byID[strings.TrimPrefix(id, "session_")] = entry
			}
			if dir := strings.TrimSpace(entry.SessionDir); dir != "" {
				if !filepath.IsAbs(dir) {
					dir = filepath.Join(kimiHome, dir)
				}
				byDir[filepath.Clean(dir)] = entry
			}
		})
		diag.MalformedLines += lineDiag.MalformedLines
		if err != nil {
			diag.Reason = appendReason(diag.Reason, "a Kimi Code session index could not be read")
		}
	}
	return byID, byDir, diag
}

func applySessionIndex(item *sessionFiles, entry sessionIndexEntry) {
	if item == nil {
		return
	}
	item.IndexWorkDir = strings.TrimSpace(entry.WorkDir)
	if item.IndexWorkDir == "" {
		item.IndexWorkDir = strings.TrimSpace(entry.Cwd)
	}
	item.IndexPath = entry.Path
	item.ModTime = laterTime(item.ModTime, entry.ModTime)
}

func laterTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func appendReason(current, next string) string {
	if next == "" {
		return current
	}
	if current == "" {
		return next
	}
	return current + "; " + next
}
