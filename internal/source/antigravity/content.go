package antigravity

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"

	"opencode-dashboard/internal/stats"
)

type transcriptRecord struct {
	StepIndex int64  `json:"step_index"`
	Content   string `json:"content"`
	Thinking  string `json:"thinking"`
	Error     string `json:"error"`
}

// Chunks are byte fragments, not independent JSONL documents: concatenate
// first so a JSON record crossing a chunk boundary remains intact.
func (s *Source) transcript(ctx context.Context, ref stepRef) (transcriptRecord, error) {
	records, err := s.transcripts(ctx, ref.conversation, map[int64]bool{ref.index: true})
	return records[ref.index], err
}

func (s *Source) transcripts(ctx context.Context, conversation string, wanted map[int64]bool) (map[int64]transcriptRecord, error) {
	root := filepath.Join(s.opts.Home, "brain", conversation, ".system_generated", "logs")
	var paths []string
	for _, name := range []string{"transcript_full.jsonl", "transcript.jsonl"} {
		path := filepath.Join(root, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			paths = []string{path}
			break
		}
	}
	if len(paths) == 0 {
		for _, name := range []string{"transcript_full", "transcript"} {
			paths, _ = filepath.Glob(filepath.Join(root, "chunks", name, "*.jsonl"))
			if len(paths) > 0 {
				sort.Strings(paths)
				break
			}
		}
	}
	files := []*os.File{}
	readers := []io.Reader{}
	defer func() {
		for _, f := range files {
			f.Close()
		}
	}()
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
		readers = append(readers, f)
	}
	scanner := bufio.NewScanner(io.LimitReader(io.MultiReader(readers...), 256<<20))
	scanner.Buffer(make([]byte, 64<<10), maxBlobBytes)
	records := make(map[int64]transcriptRecord)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var record transcriptRecord
		if json.Unmarshal(scanner.Bytes(), &record) != nil {
			continue
		}
		if wanted[record.StepIndex] {
			records[record.StepIndex] = record
		}
	}
	return records, scanner.Err()
}

func (s *Source) loadDetail(ctx context.Context, detail *stats.MessageDetail, refs map[string]stepRef) (*stats.MessageDetail, error) {
	ref, ok := refs[detail.ID]
	if !ok {
		return detail, nil
	}
	wanted := map[int64]bool{ref.index: true}
	for _, part := range detail.Content.ToolParts {
		if toolRef, ok := refs[detail.ID+":"+part.CallID]; ok {
			wanted[toolRef.index] = true
		}
	}
	records, err := s.transcripts(ctx, ref.conversation, wanted)
	if err != nil {
		return nil, err
	}
	record := records[ref.index]
	if record.Content != "" {
		detail.Content.TextParts = append(detail.Content.TextParts, redactAndTruncateMessagePart("text", record.Content))
	}
	if record.Thinking != "" {
		detail.Content.ReasoningParts = append(detail.Content.ReasoningParts, redactAndTruncateMessagePart("reasoning", record.Thinking))
	}
	var db *readDatabase
	defer func() {
		if db != nil {
			db.Close()
		}
	}()
	// Clone tool parts before adding payloads: the cached accounting snapshot
	// only contains tool names/statuses and never acquires transcript content.
	for i, part := range detail.Content.ToolParts {
		toolRef, ok := refs[detail.ID+":"+part.CallID]
		if !ok {
			continue
		}
		toolRecord := records[toolRef.index]
		part.State.Output, part.State.Truncation, part.State.Redacted = redactToolText(toolRecord.Content)
		if toolRecord.Error != "" {
			var redacted bool
			part.State.Error, _, redacted = redactToolText(toolRecord.Error)
			part.State.Redacted = part.State.Redacted || redacted
		}
		if db == nil {
			db, err = openReadDB(ctx, filepath.Join(s.opts.Home, "conversations", ref.conversation+".db"))
			if err != nil {
				return nil, err
			}
		}
		var metadata []byte
		err = db.QueryRowContext(ctx, `SELECT metadata FROM steps WHERE idx=? AND length(metadata)<=?`, toolRef.index, maxBlobBytes).Scan(&metadata)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		if err == nil {
			meta, e := decodeProto(metadata)
			if e == nil {
				call, e := meta.child(4)
				if e == nil {
					var args any
					if json.Unmarshal(call.bytes(3), &args) == nil {
						var redacted bool
						part.State.Input, _, redacted = redactToolInput(args)
						part.State.Redacted = part.State.Redacted || redacted
					}
				}
			}
		}
		detail.Content.ToolParts[i] = part
	}
	return detail, nil
}
