package codex

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
	Path      string
	SessionID string
	ModTime   time.Time
	Size      int64
}

type discoveryResult struct {
	files       []transcriptFile
	diagnostics source.SourceDiagnostics
	available   bool
}

func discoverTranscripts(ctx context.Context, codexHome string) discoveryResult {
	sessionsRoot := filepath.Join(codexHome, "sessions")
	diag := source.SourceDiagnostics{Status: "ok"}

	if err := ctx.Err(); err != nil {
		diag.Status = "unavailable"
		diag.Reason = err.Error()
		return discoveryResult{diagnostics: diag}
	}

	info, err := os.Stat(sessionsRoot)
	if err != nil {
		diag.Status = "unavailable"
		if os.IsNotExist(err) {
			diag.Reason = "Codex sessions directory not found: " + sessionsRoot
		} else if os.IsPermission(err) {
			diag.Reason = "Codex sessions directory is not readable: " + sessionsRoot
		} else {
			diag.Reason = "Codex sessions directory cannot be accessed: " + err.Error()
		}
		return discoveryResult{diagnostics: diag}
	}
	if !info.IsDir() {
		diag.Status = "unavailable"
		diag.Reason = "Codex sessions path is not a directory: " + sessionsRoot
		return discoveryResult{diagnostics: diag}
	}

	files := make([]transcriptFile, 0)
	walkErr := filepath.WalkDir(sessionsRoot, func(path string, d os.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			if os.IsNotExist(err) {
				diag.Reason = appendReason(diag.Reason, "a Codex transcript path disappeared during discovery")
				return nil
			}
			if os.IsPermission(err) {
				diag.Reason = appendReason(diag.Reason, "a Codex transcript path could not be read due to permissions")
				return nil
			}
			return err
		}
		if d.IsDir() {
			switch strings.ToLower(d.Name()) {
			case ".tmp", "tmp", "cache", "skills", "plugins", "plugin", "logs", "pets":
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasPrefix(name, "rollout-") || !strings.EqualFold(filepath.Ext(name), ".jsonl") {
			return nil
		}
		fileInfo, err := d.Info()
		if err != nil {
			if os.IsNotExist(err) {
				diag.Reason = appendReason(diag.Reason, "a Codex transcript file disappeared during discovery")
				return nil
			}
			if os.IsPermission(err) {
				diag.Reason = appendReason(diag.Reason, "a Codex transcript file could not be read due to permissions")
				return nil
			}
			return err
		}
		files = append(files, transcriptFile{
			Path:      path,
			SessionID: rolloutSessionID(name),
			ModTime:   fileInfo.ModTime().UTC(),
			Size:      fileInfo.Size(),
		})
		return nil
	})
	if walkErr != nil {
		diag.Status = "unavailable"
		diag.Reason = "Codex sessions directory cannot be scanned: " + walkErr.Error()
		return discoveryResult{diagnostics: diag}
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	diag.ScannedFiles = int64(len(files))
	if len(files) == 0 {
		diag.Status = "empty"
		diag.Reason = "no Codex rollout JSONL transcripts found"
		return discoveryResult{files: files, diagnostics: diag}
	}
	return discoveryResult{files: files, diagnostics: diag, available: true}
}

func rolloutSessionID(name string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	if idx := strings.LastIndex(base, "Z-"); idx >= 0 && idx+2 < len(base) {
		return base[idx+2:]
	}
	return strings.TrimPrefix(base, "rollout-")
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
