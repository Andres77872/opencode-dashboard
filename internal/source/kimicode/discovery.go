package kimicode

import (
	"context"
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
}

type sessionFiles struct {
	ID        string
	Dir       string
	StatePath string
	ModTime   time.Time
	Agents    []agentFile
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
			sessionDir := filepath.Dir(path)
			if !strings.HasPrefix(filepath.Base(sessionDir), "session_") {
				return nil
			}
			item := ensureSessionFiles(byDir, sessionDir)
			item.StatePath = path
			if fileInfo, err := d.Info(); err == nil {
				item.ModTime = laterTime(item.ModTime, fileInfo.ModTime().UTC())
			}
		case "wire.jsonl":
			agentDir := filepath.Dir(path)
			agentsDir := filepath.Dir(agentDir)
			if filepath.Base(agentsDir) != "agents" {
				return nil
			}
			sessionDir := filepath.Dir(agentsDir)
			if !strings.HasPrefix(filepath.Base(sessionDir), "session_") {
				return nil
			}
			fileInfo, err := d.Info()
			if err != nil {
				if os.IsNotExist(err) || os.IsPermission(err) {
					return nil
				}
				return err
			}
			item := ensureSessionFiles(byDir, sessionDir)
			item.Agents = append(item.Agents, agentFile{
				Path:    path,
				AgentID: filepath.Base(agentDir),
				ModTime: fileInfo.ModTime().UTC(),
				Size:    fileInfo.Size(),
			})
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
		ID:        id,
		Dir:       dir,
		StatePath: filepath.Join(dir, "state.json"),
	}
	byDir[dir] = item
	return item
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
