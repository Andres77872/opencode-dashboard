package qwencode

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"opencode-dashboard/internal/source"
)

type chatFile struct {
	SessionID  string
	Path       string
	ProjectDir string
	ModTime    time.Time
	Size       int64
}

type usageFile struct {
	Path    string
	Month   string // "2006-01" from the file name, "" when unparseable
	ModTime time.Time
}

type discoveryResult struct {
	chats           []chatFile
	usageFiles      []usageFile
	usageRecordPath string
	diagnostics     source.SourceDiagnostics
	available       bool
}

func discoverData(ctx context.Context, qwenHome string) discoveryResult {
	diag := source.SourceDiagnostics{Status: "ok"}
	if err := ctx.Err(); err != nil {
		diag.Status = "unavailable"
		diag.Reason = err.Error()
		return discoveryResult{diagnostics: diag}
	}

	info, err := os.Stat(qwenHome)
	if err != nil {
		diag.Status = "unavailable"
		switch {
		case os.IsNotExist(err):
			diag.Reason = "Qwen Code home directory not found: " + qwenHome
		case os.IsPermission(err):
			diag.Reason = "Qwen Code home directory is not readable: " + qwenHome
		default:
			diag.Reason = "Qwen Code home directory cannot be accessed: " + err.Error()
		}
		return discoveryResult{diagnostics: diag}
	}
	if !info.IsDir() {
		diag.Status = "unavailable"
		diag.Reason = "Qwen Code home path is not a directory: " + qwenHome
		return discoveryResult{diagnostics: diag}
	}

	result := discoveryResult{}
	result.chats = discoverChatFiles(ctx, qwenHome, &diag)
	result.usageFiles = discoverUsageFiles(ctx, qwenHome, &diag)
	if err := ctx.Err(); err != nil {
		diag.Status = "unavailable"
		diag.Reason = err.Error()
		return discoveryResult{diagnostics: diag}
	}

	recordPath := filepath.Join(qwenHome, "usage_record.jsonl")
	if stat, err := os.Stat(recordPath); err == nil && !stat.IsDir() {
		result.usageRecordPath = recordPath
	}

	if len(result.chats) == 0 && len(result.usageFiles) == 0 {
		diag.Status = "empty"
		diag.Reason = "no Qwen Code chat transcripts or usage logs found"
		result.diagnostics = diag
		return result
	}
	result.diagnostics = diag
	result.available = true
	return result
}

func discoverChatFiles(ctx context.Context, qwenHome string, diag *source.SourceDiagnostics) []chatFile {
	projectsDir := filepath.Join(qwenHome, "projects")
	projects, err := os.ReadDir(projectsDir)
	if err != nil {
		if !os.IsNotExist(err) {
			diag.Reason = appendReason(diag.Reason, "Qwen Code projects directory could not be read")
		}
		return nil
	}

	chats := make([]chatFile, 0)
	for _, project := range projects {
		if err := ctx.Err(); err != nil {
			return chats
		}
		if !project.IsDir() {
			continue
		}
		projectDir := filepath.Join(projectsDir, project.Name())
		chatsDir := filepath.Join(projectDir, "chats")
		entries, err := os.ReadDir(chatsDir)
		if err != nil {
			if !os.IsNotExist(err) {
				diag.Reason = appendReason(diag.Reason, "a Qwen Code chats directory could not be read")
			}
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(name, ".jsonl") || strings.HasSuffix(name, ".runtime.json") {
				continue
			}
			fileInfo, err := entry.Info()
			if err != nil {
				if os.IsNotExist(err) || os.IsPermission(err) {
					diag.Reason = appendReason(diag.Reason, "a Qwen Code chat transcript disappeared during discovery")
					continue
				}
				diag.Reason = appendReason(diag.Reason, "a Qwen Code chat transcript could not be inspected")
				continue
			}
			chats = append(chats, chatFile{
				SessionID:  strings.TrimSuffix(name, ".jsonl"),
				Path:       filepath.Join(chatsDir, name),
				ProjectDir: projectDir,
				ModTime:    fileInfo.ModTime().UTC(),
				Size:       fileInfo.Size(),
			})
			diag.ScannedFiles++
		}
	}
	sort.Slice(chats, func(i, j int) bool { return chats[i].Path < chats[j].Path })
	return chats
}

func discoverUsageFiles(ctx context.Context, qwenHome string, diag *source.SourceDiagnostics) []usageFile {
	usageDir := filepath.Join(qwenHome, "usage")
	entries, err := os.ReadDir(usageDir)
	if err != nil {
		if !os.IsNotExist(err) {
			diag.Reason = appendReason(diag.Reason, "Qwen Code usage directory could not be read")
		}
		return nil
	}
	files := make([]usageFile, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return files
		}
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "token-usage-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		fileInfo, err := entry.Info()
		if err != nil {
			if os.IsNotExist(err) || os.IsPermission(err) {
				continue
			}
			diag.Reason = appendReason(diag.Reason, "a Qwen Code usage log could not be inspected")
			continue
		}
		month := strings.TrimSuffix(strings.TrimPrefix(name, "token-usage-"), ".jsonl")
		if _, err := time.Parse("2006-01", month); err != nil {
			month = ""
		}
		files = append(files, usageFile{
			Path:    filepath.Join(usageDir, name),
			Month:   month,
			ModTime: fileInfo.ModTime().UTC(),
		})
		diag.ScannedFiles++
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

func appendReason(current, next string) string {
	if next == "" {
		return current
	}
	if current == "" {
		return next
	}
	if strings.Contains(current, next) {
		return current
	}
	return current + "; " + next
}
