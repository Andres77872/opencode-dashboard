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

const (
	fingerprintFormatVersion = "fp1"
	pricingTagPrefix         = fingerprintFormatVersion + ":pricing="
	fingerprintDataSeparator = ":data="
)

func sourceFingerprint(ctx context.Context, info source.SourceInfo) (string, error) {
	h := sha256.New()
	fmt.Fprintf(h, "v=%d\nid=%s\nkind=%s\npath=%s\npricing_snapshot=%s\n", dataVersion, info.ID, info.Kind, info.Path, info.CostPolicy.PricingSnapshotID)

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
	case source.SourceKimiCode:
		if err := hashKimiFiles(ctx, h, info.Path); err != nil {
			return "", err
		}
	default:
		fmt.Fprintf(h, "diagnostics=%d:%d\n", info.Diagnostics.ScannedFiles, info.Diagnostics.MalformedLines)
	}
	return tagFingerprint(hex.EncodeToString(h.Sum(nil)), info.CostPolicy.PricingSnapshotID), nil
}

func fallbackFingerprint(info source.SourceInfo) string {
	h := sha256.New()
	fmt.Fprintf(h, "v=%d\nid=%s\nkind=%s\npath=%s\npricing_snapshot=%s\navailable=%v\nfiles=%d\nmalformed=%d\nunsupported=%d\n",
		dataVersion, info.ID, info.Kind, info.Path, info.CostPolicy.PricingSnapshotID, info.Available, info.Diagnostics.ScannedFiles, info.Diagnostics.MalformedLines, info.Diagnostics.UnsupportedEvents)
	return tagFingerprint(hex.EncodeToString(h.Sum(nil)), info.CostPolicy.PricingSnapshotID)
}

// tagFingerprint keeps the content digest opaque while making the pricing
// catalog identity independently comparable. A plain fingerprint mismatch can
// be handled incrementally, but a pricing mismatch requires all historical
// rows to be re-collected so their persisted costs are recomputed.
func tagFingerprint(dataDigest, pricingSnapshotID string) string {
	pricingDigest := sha256.Sum256([]byte(pricingSnapshotID))
	return pricingTagPrefix + hex.EncodeToString(pricingDigest[:]) + fingerprintDataSeparator + dataDigest
}

// fingerprintPricingIdentity returns the fixed-width digest of the pricing
// snapshot encoded in a tagged fingerprint. False identifies legacy/invalid
// fingerprints, which callers handle conservatively for priced sources.
func fingerprintPricingIdentity(fingerprint string) (string, bool) {
	if !strings.HasPrefix(fingerprint, pricingTagPrefix) {
		return "", false
	}
	rest := strings.TrimPrefix(fingerprint, pricingTagPrefix)
	separator := strings.Index(rest, fingerprintDataSeparator)
	if separator < 0 {
		return "", false
	}
	pricingDigest := rest[:separator]
	dataDigest := rest[separator+len(fingerprintDataSeparator):]
	if len(pricingDigest) != sha256.Size*2 || len(dataDigest) != sha256.Size*2 {
		return "", false
	}
	if _, err := hex.DecodeString(pricingDigest); err != nil {
		return "", false
	}
	if _, err := hex.DecodeString(dataDigest); err != nil {
		return "", false
	}
	return pricingDigest, true
}

func pricingIdentityChanged(cachedFingerprint, currentFingerprint, currentSnapshotID string) bool {
	currentIdentity, currentTagged := fingerprintPricingIdentity(currentFingerprint)
	if !currentTagged {
		return false
	}
	cachedIdentity, cachedTagged := fingerprintPricingIdentity(cachedFingerprint)
	if cachedTagged {
		return cachedIdentity != currentIdentity
	}
	// Existing databases may contain the pre-tag opaque hash. If this source
	// has a pricing catalog, its historical rows cannot safely be assumed to
	// use the current catalog, so upgrade it with one full rebuild.
	return currentSnapshotID != ""
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

func hashKimiFiles(ctx context.Context, h hashWriter, home string) error {
	root := filepath.Join(home, "sessions")
	return hashWalk(ctx, h, root, func(path string, d os.DirEntry) (bool, error) {
		if d.IsDir() {
			switch strings.ToLower(d.Name()) {
			case "logs", "plans", "media", "images":
				return false, filepath.SkipDir
			}
			return false, nil
		}
		switch d.Name() {
		case "state.json":
			return strings.HasPrefix(filepath.Base(filepath.Dir(path)), "session_"), nil
		case "wire.jsonl":
			return filepath.Base(filepath.Dir(filepath.Dir(path))) == "agents", nil
		default:
			return false, nil
		}
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
