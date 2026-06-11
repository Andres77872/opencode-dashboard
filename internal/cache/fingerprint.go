package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"opencode-dashboard/internal/source"
)

func sourceFingerprint(ctx context.Context, info source.SourceInfo) (string, error) {
	h := sha256.New()
	fmt.Fprintf(h, "v=%d\nid=%s\nkind=%s\npath=%s\n", schemaVersion, info.ID, info.Kind, info.Path)

	switch info.ID {
	case source.SourceOpenCode:
		if err := hashFileStat(h, info.Path, info.Path); err != nil {
			return "", err
		}
		if err := hashFileIfExists(h, info.Path, info.Path+"-wal"); err != nil {
			return "", err
		}
		if err := hashFileIfExists(h, info.Path, info.Path+"-shm"); err != nil {
			return "", err
		}
	case source.SourceClaudeCode:
		if err := hashClaudeFiles(ctx, h, info.Path); err != nil {
			return "", err
		}
	case source.SourceCodex:
		if err := hashCodexFiles(ctx, h, info.Path); err != nil {
			return "", err
		}
	default:
		fmt.Fprintf(h, "diagnostics=%d:%d\n", info.Diagnostics.ScannedFiles, info.Diagnostics.MalformedLines)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fallbackFingerprint(info source.SourceInfo) string {
	h := sha256.New()
	fmt.Fprintf(h, "v=%d\nid=%s\nkind=%s\npath=%s\navailable=%v\nfiles=%d\nmalformed=%d\nunsupported=%d\n",
		schemaVersion, info.ID, info.Kind, info.Path, info.Available, info.Diagnostics.ScannedFiles, info.Diagnostics.MalformedLines, info.Diagnostics.UnsupportedEvents)
	return hex.EncodeToString(h.Sum(nil))
}

type hashWriter interface {
	Write([]byte) (int, error)
}

func hashFileStat(h hashWriter, root, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		rel = path
	}
	fmt.Fprintf(h, "file=%s size=%d mtime=%d\n", filepath.ToSlash(rel), info.Size(), info.ModTime().UTC().UnixNano())
	return nil
}

func hashFileIfExists(h hashWriter, root, path string) error {
	if err := hashFileStat(h, root, path); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		fmt.Fprintf(h, "missing=%s\n", filepath.ToSlash(path))
	}
	return nil
}

func hashClaudeFiles(ctx context.Context, h hashWriter, home string) error {
	root := filepath.Join(home, "projects")
	return hashWalk(ctx, h, root, func(path string, d os.DirEntry) (bool, error) {
		if d.IsDir() {
			switch d.Name() {
			case "tool-results", "debug":
				return false, filepath.SkipDir
			}
			return false, nil
		}
		return strings.EqualFold(filepath.Ext(d.Name()), ".jsonl"), nil
	})
}

func hashCodexFiles(ctx context.Context, h hashWriter, home string) error {
	root := filepath.Join(home, "sessions")
	return hashWalk(ctx, h, root, func(path string, d os.DirEntry) (bool, error) {
		if d.IsDir() {
			switch strings.ToLower(d.Name()) {
			case ".tmp", "tmp", "cache", "skills", "plugins", "plugin", "logs", "pets":
				return false, filepath.SkipDir
			}
			return false, nil
		}
		name := d.Name()
		return strings.HasPrefix(name, "rollout-") && strings.EqualFold(filepath.Ext(name), ".jsonl"), nil
	})
}

func hashWalk(ctx context.Context, h hashWriter, root string, include func(string, os.DirEntry) (bool, error)) error {
	files := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			if os.IsNotExist(err) || os.IsPermission(err) {
				return nil
			}
			return err
		}
		ok, err := include(path, d)
		if err != nil {
			return err
		}
		if ok {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, path := range files {
		if err := hashFileStat(h, root, path); err != nil {
			if os.IsNotExist(err) || os.IsPermission(err) {
				continue
			}
			return err
		}
	}
	fmt.Fprintf(h, "files=%d\n", len(files))
	return nil
}
