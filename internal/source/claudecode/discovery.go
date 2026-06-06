package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"opencode-dashboard/internal/source"
)

type transcriptFile struct {
	Path        string
	ProjectID   string
	ProjectPath string
	SessionID   string
	ModTime     time.Time
	Size        int64
}

type discoveryResult struct {
	files       []transcriptFile
	diagnostics source.SourceDiagnostics
	available   bool
}

func discoverTranscripts(ctx context.Context, claudeHome string) discoveryResult {
	projectsRoot := filepath.Join(claudeHome, "projects")
	diag := source.SourceDiagnostics{Status: "ok"}

	if err := ctx.Err(); err != nil {
		diag.Status = "unavailable"
		diag.Reason = err.Error()
		return discoveryResult{diagnostics: diag}
	}

	info, err := os.Stat(projectsRoot)
	if err != nil {
		diag.Status = "unavailable"
		if os.IsNotExist(err) {
			diag.Reason = "Claude Code projects directory not found: " + projectsRoot
		} else if os.IsPermission(err) {
			diag.Reason = "Claude Code projects directory is not readable: " + projectsRoot
		} else {
			diag.Reason = "Claude Code projects directory cannot be accessed: " + err.Error()
		}
		return discoveryResult{diagnostics: diag}
	}
	if !info.IsDir() {
		diag.Status = "unavailable"
		diag.Reason = "Claude Code projects path is not a directory: " + projectsRoot
		return discoveryResult{diagnostics: diag}
	}

	files := make([]transcriptFile, 0)
	walkErr := filepath.WalkDir(projectsRoot, func(path string, d os.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			if os.IsNotExist(err) {
				diag.Reason = appendReason(diag.Reason, "a transcript path disappeared during discovery")
				return nil
			}
			if os.IsPermission(err) {
				diag.Reason = appendReason(diag.Reason, "a transcript path could not be read due to permissions")
				return nil
			}
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "subagents", "tool-results", "debug":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(d.Name()), ".jsonl") {
			return nil
		}
		fileInfo, err := d.Info()
		if err != nil {
			if os.IsNotExist(err) {
				diag.Reason = appendReason(diag.Reason, "a transcript file disappeared during discovery")
				return nil
			}
			if os.IsPermission(err) {
				diag.Reason = appendReason(diag.Reason, "a transcript file could not be read due to permissions")
				return nil
			}
			return err
		}
		rel, err := filepath.Rel(projectsRoot, path)
		if err != nil {
			return err
		}
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) < 2 {
			return nil
		}
		projectID := parts[0]
		files = append(files, transcriptFile{
			Path:        path,
			ProjectID:   projectID,
			ProjectPath: decodeProjectPath(projectID),
			SessionID:   strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
			ModTime:     fileInfo.ModTime().UTC(),
			Size:        fileInfo.Size(),
		})
		return nil
	})
	if walkErr != nil {
		diag.Status = "unavailable"
		diag.Reason = "Claude Code projects directory cannot be scanned: " + walkErr.Error()
		return discoveryResult{diagnostics: diag}
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	diag.ScannedFiles = int64(len(files))
	if len(files) == 0 {
		diag.Status = "empty"
		diag.Reason = "no persisted Claude Code JSONL transcripts found"
	}
	return discoveryResult{files: files, diagnostics: diag, available: true}
}

func decodeProjectPath(projectID string) string {
	if projectID == "" {
		return ""
	}
	if strings.HasPrefix(projectID, "-") {
		return strings.ReplaceAll(projectID, "-", string(filepath.Separator))
	}
	return strings.ReplaceAll(projectID, "-", string(filepath.Separator))
}

func projectName(projectID, projectPath string) string {
	if projectPath != "" && projectPath != string(filepath.Separator) {
		base := filepath.Base(projectPath)
		if base != "." && base != string(filepath.Separator) && base != "" {
			return base
		}
	}
	if projectID == "" {
		return "unknown"
	}
	return projectID
}
