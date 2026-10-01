package stats

import (
	"bytes"
	"encoding/json"
	"errors"
)

var errUnterminatedBlockComment = errors.New("unterminated block comment")

// parseJSONCObject decodes a JSON or JSONC document into a map, keeping numbers
// as json.Number so the redacted view re-encodes them unchanged.
func parseJSONCObject(content []byte) (map[string]any, error) {
	plain, err := stripJSONC(content)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(plain))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// stripJSONC turns JSONC into plain JSON, accepting what OpenCode's
// jsonc-parser accepts with allowTrailingComma: line comments, block comments
// and a trailing comma before a closing brace or bracket. Removed bytes are
// overwritten with spaces (line breaks inside block comments are kept), so the
// output has the same length as the input and decoder error offsets still
// point into the original file. String literals are copied verbatim, so
// comment markers inside them survive.
func stripJSONC(src []byte) ([]byte, error) {
	out := bytes.Clone(src)
	// pendingComma is the index of a comma that follows a value and has not
	// yet been followed by another value; it is blanked if a closer comes next.
	pendingComma := -1
	// last is the last significant byte seen outside strings and comments.
	var last byte
	for i := 0; i < len(out); {
		c := out[i]
		switch {
		case c == '"':
			i++
			for i < len(out) {
				if out[i] == '\\' {
					i += 2
					continue
				}
				i++
				if out[i-1] == '"' {
					break
				}
			}
			last, pendingComma = '"', -1
		case c == '/' && i+1 < len(out) && out[i+1] == '/':
			for i < len(out) && out[i] != '\n' && out[i] != '\r' {
				out[i] = ' '
				i++
			}
		case c == '/' && i+1 < len(out) && out[i+1] == '*':
			out[i], out[i+1] = ' ', ' '
			i += 2
			closed := false
			for i < len(out) {
				if out[i] == '*' && i+1 < len(out) && out[i+1] == '/' {
					out[i], out[i+1] = ' ', ' '
					i += 2
					closed = true
					break
				}
				if out[i] != '\n' && out[i] != '\r' {
					out[i] = ' '
				}
				i++
			}
			if !closed {
				return nil, errUnterminatedBlockComment
			}
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == ',':
			// A comma straight after an opener or another comma is not a
			// trailing comma; leave it for the decoder to reject.
			if last == '{' || last == '[' || last == ',' {
				pendingComma = -1
			} else {
				pendingComma = i
			}
			last = c
			i++
		case c == '}' || c == ']':
			if pendingComma >= 0 {
				out[pendingComma] = ' '
			}
			last, pendingComma = c, -1
			i++
		default:
			last, pendingComma = c, -1
			i++
		}
	}
	return out, nil
}
