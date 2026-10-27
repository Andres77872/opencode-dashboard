package codex

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/klauspost/compress/zstd"
)

const (
	rolloutPlainExt = ".jsonl"
	// rolloutCompressedExt is the suffix Codex gives a rollout when it
	// compresses it in place, roughly a week after the session goes idle. The
	// compressed file replaces the plain one, so it is the only copy of that
	// session's history.
	rolloutCompressedExt = ".jsonl.zst"
)

// isRolloutFileName reports whether name is a plain or zstd-compressed Codex
// rollout transcript.
func isRolloutFileName(name string) bool {
	if !strings.HasPrefix(name, "rollout-") {
		return false
	}
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, rolloutPlainExt) || strings.HasSuffix(lower, rolloutCompressedExt)
}

func isCompressedRollout(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), rolloutCompressedExt)
}

// rolloutStem strips the transcript extension, so a rollout and its
// compressed successor share one stem.
func rolloutStem(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, rolloutCompressedExt):
		return name[:len(name)-len(rolloutCompressedExt)]
	case strings.HasSuffix(lower, rolloutPlainExt):
		return name[:len(name)-len(rolloutPlainExt)]
	}
	return name
}

// compressedRolloutError marks a compressed rollout whose zstd stream could
// not be decoded (truncated mid-compression, or corrupt). Callers skip the
// file with a diagnostic rather than failing the whole source.
type compressedRolloutError struct {
	path string
	err  error
}

func (e *compressedRolloutError) Error() string {
	return fmt.Sprintf("decode compressed Codex rollout %s: %v", e.path, e.err)
}

func (e *compressedRolloutError) Unwrap() error { return e.err }

// openRollout opens a rollout transcript for sequential reading, decoding it
// when Codex has compressed it. Open errors are returned unwrapped so
// os.IsNotExist/os.IsPermission checks keep working.
func openRollout(path string) (io.ReadCloser, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if !isCompressedRollout(path) {
		return fh, nil
	}
	// Files are read one at a time, so a single decoder goroutine is enough
	// and keeps memory bounded across thousands of archived rollouts.
	dec, err := zstd.NewReader(fh, zstd.WithDecoderConcurrency(1))
	if err != nil {
		fh.Close()
		return nil, &compressedRolloutError{path: path, err: err}
	}
	return &zstdRollout{path: path, file: fh, dec: dec}, nil
}

type zstdRollout struct {
	path string
	file *os.File
	dec  *zstd.Decoder
}

func (r *zstdRollout) Read(p []byte) (int, error) {
	n, err := r.dec.Read(p)
	if err != nil && err != io.EOF {
		err = &compressedRolloutError{path: r.path, err: err}
	}
	return n, err
}

func (r *zstdRollout) Close() error {
	r.dec.Close()
	return r.file.Close()
}
