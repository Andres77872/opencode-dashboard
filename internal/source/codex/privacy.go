package codex

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"opencode-dashboard/internal/stats"
)

const (
	messageTextMaxBytes = 2000
	toolTextMaxBytes    = 2000
	redactedValue       = "[REDACTED]"
)

var absolutePathPattern = regexp.MustCompile(`(?i)(/[A-Za-z0-9._@%+\-=]+){2,}`)

func redactText(text string) (string, bool) {
	if text == "" {
		return "", false
	}
	redacted := text
	changed := false
	if strings.Contains(redacted, "MUST_NOT_LEAK") || strings.Contains(redacted, "SYNTHETIC_") && !strings.Contains(redacted, "[REDACTED_") {
		redacted = redactedValue
		changed = true
	}
	pathRedacted := absolutePathPattern.ReplaceAllStringFunc(redacted, func(path string) string {
		base := filepath.Base(path)
		if base == "." || base == string(filepath.Separator) || base == "" {
			return "[REDACTED_PATH]"
		}
		return "[REDACTED_PATH]/" + base
	})
	if pathRedacted != redacted {
		redacted = pathRedacted
		changed = true
	}
	return redacted, changed
}

func redactDisplayPath(path string) string {
	redacted, _ := redactText(path)
	return redacted
}

func redactAndTruncateMessagePart(kind, text string) stats.MessagePart {
	redactedText, redacted := redactText(text)
	truncated, truncation := truncateText(redactedText, messageTextMaxBytes)
	return stats.MessagePart{Type: kind, Text: truncated, Truncation: truncation, Redacted: redacted || truncation != nil}
}

func redactToolText(text string) (string, *stats.TruncationInfo, bool) {
	redactedText, redacted := redactText(text)
	truncated, truncation := truncateText(redactedText, toolTextMaxBytes)
	return truncated, truncation, redacted || truncation != nil
}

func redactToolInput(text string) (map[string]any, *stats.TruncationInfo, bool) {
	if text == "" {
		return nil, nil, false
	}
	redactedText, truncation, redacted := redactToolText(text)
	return map[string]any{"redacted": redactedText}, truncation, redacted
}

func truncateText(content string, maxBytes int) (string, *stats.TruncationInfo) {
	if maxBytes <= 0 || len(content) <= maxBytes {
		return content, nil
	}
	truncated := content[:maxBytes] + "..."
	return truncated, &stats.TruncationInfo{Truncated: true, OriginalBytes: int64(len(content)), DisplayBytes: int64(len(truncated))}
}

func mergeTruncation(current, next *stats.TruncationInfo) *stats.TruncationInfo {
	if next == nil {
		return current
	}
	if current == nil {
		copy := *next
		return &copy
	}
	current.Truncated = current.Truncated || next.Truncated
	current.OriginalBytes += next.OriginalBytes
	current.DisplayBytes += next.DisplayBytes
	return current
}

/* ---------- Structural config redaction (parsed map) ---------- */

// redactConfigMap structurally redacts a parsed config document. Values under
// keys matching shouldRedactKey or under env*/header* parents are replaced
// wholesale with redactedValue; remaining strings get redactText path
// scrubbing.
func redactConfigMap(m map[string]any) (map[string]any, bool) {
	return redactConfigMapWithParent(m, "")
}

func redactConfigMapWithParent(input map[string]any, parentKey string) (map[string]any, bool) {
	result := make(map[string]any, len(input))
	changed := false
	for key, value := range input {
		if shouldRedactKey(key) || hasSensitiveConfigPrefix(parentKey) {
			result[key] = redactWholeConfigValue(value)
			changed = true
			continue
		}
		redacted, redactedChanged := redactConfigValue(key, value)
		result[key] = redacted
		changed = changed || redactedChanged
	}
	return result, changed
}

func redactConfigValue(key string, value any) (any, bool) {
	if shouldRedactKey(key) || hasSensitiveConfigPrefix(key) {
		return redactWholeConfigValue(value), true
	}
	switch v := value.(type) {
	case map[string]any:
		return redactConfigMapWithParent(v, key)
	case []any:
		out := make([]any, len(v))
		changed := false
		for i, item := range v {
			redacted, itemChanged := redactConfigValue(key, item)
			out[i] = redacted
			changed = changed || itemChanged
		}
		return out, changed
	case string:
		return redactText(v)
	default:
		return value, false
	}
}

func redactWholeConfigValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key := range v {
			out[key] = redactedValue
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = redactedValue
		}
		return out
	case nil:
		return nil
	default:
		return redactedValue
	}
}

func hasSensitiveConfigPrefix(key string) bool {
	lower := strings.ToLower(key)
	return strings.HasPrefix(lower, "env") || strings.HasPrefix(lower, "header")
}

/* ---------- Raw line redaction (original file text) ---------- */

// lineSkipState swallows the continuation lines of a redacted multi-line
// value so secret containers (JSON objects/arrays, TOML multiline strings and
// arrays, YAML blocks) never reach the redacted raw text.
type lineSkipState struct {
	active  bool
	mode    string // "bracket" | "delimiter" | "indent"
	balance int    // bracket mode: unclosed brackets remaining
	delim   string // delimiter mode: closing TOML multiline string delimiter
	indent  int    // indent mode: swallow lines indented deeper than this
}

// consume reports whether line belongs to the swallowed block.
func (s *lineSkipState) consume(line string) bool {
	switch s.mode {
	case "bracket":
		s.balance += bracketDelta(line)
		if s.balance <= 0 {
			s.active = false
		}
		return true
	case "delimiter":
		if strings.Contains(line, s.delim) {
			s.active = false
		}
		return true
	case "indent":
		if strings.TrimSpace(line) == "" {
			return true
		}
		if leadingIndentWidth(line) > s.indent {
			return true
		}
		s.active = false
		return false
	}
	s.active = false
	return false
}

// bracketDelta counts net unclosed {[ brackets outside quoted strings.
func bracketDelta(line string) int {
	delta := 0
	inString := byte(0)
	escaped := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if inString != 0 {
			if escaped {
				escaped = false
				continue
			}
			switch c {
			case '\\':
				escaped = true
			case inString:
				inString = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			inString = c
		case '{', '[':
			delta++
		case '}', ']':
			delta--
		}
	}
	return delta
}

func leadingIndentWidth(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

// redactConfigLines line-redacts the original config file text, preserving
// formatting and comments. Format selects the key-extraction syntax.
func redactConfigLines(format string, content []byte) (string, bool) {
	normalized := strings.ReplaceAll(string(content), "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	out := make([]string, 0, len(lines))
	changed := false
	skip := &lineSkipState{}
	for _, line := range lines {
		if skip.active && skip.consume(line) {
			changed = true
			continue
		}
		var redactedLine string
		var lineChanged bool
		switch format {
		case stats.ConfigFormatJSON:
			redactedLine, lineChanged = redactJSONLine(line, skip)
		case stats.ConfigFormatYAML:
			redactedLine, lineChanged = redactYAMLLine(line, skip)
		default:
			redactedLine, lineChanged = redactTOMLLine(line, skip)
		}
		out = append(out, redactedLine)
		changed = changed || lineChanged
	}
	return strings.Join(out, "\n"), changed
}

func redactTOMLLine(line string, skip *lineSkipState) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "[") {
		return redactText(line)
	}
	eq := strings.Index(trimmed, "=")
	if eq < 0 {
		return redactText(line)
	}
	key := strings.TrimSpace(trimmed[:eq])
	value := strings.TrimSpace(trimmed[eq+1:])
	indent := line[:leadingIndentWidth(line)]
	if shouldRedactKey(key) || hasSensitiveConfigPrefix(key) ||
		strings.Contains(trimmed, "MUST_NOT_LEAK") || sensitiveInlineTOMLKey(value) {
		armTOMLValueSkip(value, skip)
		return indent + key + " = \"" + redactedValue + "\"", true
	}
	return redactText(line)
}

// sensitiveInlineTOMLKey reports whether an inline table value contains a
// sensitive inner key, e.g. provider = { api_key = "..." }.
func sensitiveInlineTOMLKey(value string) bool {
	if !strings.HasPrefix(value, "{") {
		return false
	}
	for _, match := range inlineTOMLKeyPattern.FindAllStringSubmatch(value, -1) {
		key := strings.Trim(match[1], `"'`)
		if shouldRedactKey(key) || hasSensitiveConfigPrefix(key) {
			return true
		}
	}
	return false
}

var inlineTOMLKeyPattern = regexp.MustCompile(`([A-Za-z0-9_\-"']+)\s*=`)

// armTOMLValueSkip arms continuation skipping when a redacted TOML value
// spans multiple lines (multiline strings, arrays, inline tables).
func armTOMLValueSkip(value string, skip *lineSkipState) {
	for _, delim := range []string{`"""`, "'''"} {
		if strings.HasPrefix(value, delim) && strings.Count(value, delim) == 1 {
			skip.active = true
			skip.mode = "delimiter"
			skip.delim = delim
			return
		}
	}
	if delta := bracketDelta(value); delta > 0 {
		skip.active = true
		skip.mode = "bracket"
		skip.balance = delta
	}
}

var jsonKeyLinePattern = regexp.MustCompile(`^(\s*)"((?:[^"\\]|\\.)*)"\s*:\s*(.*)$`)

func redactJSONLine(line string, skip *lineSkipState) (string, bool) {
	match := jsonKeyLinePattern.FindStringSubmatch(line)
	if match == nil {
		return redactText(line)
	}
	indent, key, rest := match[1], match[2], match[3]
	if shouldRedactKey(key) || hasSensitiveConfigPrefix(key) || strings.Contains(line, "MUST_NOT_LEAK") {
		trimmedRest := strings.TrimSpace(rest)
		comma := ""
		if strings.HasSuffix(trimmedRest, ",") {
			comma = ","
		}
		if delta := bracketDelta(rest); delta > 0 {
			skip.active = true
			skip.mode = "bracket"
			skip.balance = delta
		}
		return indent + `"` + key + `": "` + redactedValue + `"` + comma, true
	}
	return redactText(line)
}

var yamlKeyLinePattern = regexp.MustCompile(`^(\s*)(- )?("(?:[^"\\]|\\.)*"|'[^']*'|[^\s:#][^:#]*?)\s*:(.*)$`)

func redactYAMLLine(line string, skip *lineSkipState) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return redactText(line)
	}
	match := yamlKeyLinePattern.FindStringSubmatch(line)
	if match == nil {
		return redactText(line)
	}
	indent, dash, key, rest := match[1], match[2], strings.Trim(match[3], `"'`), match[4]
	if shouldRedactKey(key) || hasSensitiveConfigPrefix(key) || strings.Contains(line, "MUST_NOT_LEAK") {
		trimmedRest := strings.TrimSpace(rest)
		// Empty values, block scalars (| / >) and unterminated flow
		// containers continue on following, deeper-indented lines.
		if trimmedRest == "" || strings.HasPrefix(trimmedRest, "|") || strings.HasPrefix(trimmedRest, ">") || bracketDelta(trimmedRest) > 0 {
			skip.active = true
			skip.mode = "indent"
			skip.indent = leadingIndentWidth(line)
		}
		return indent + dash + key + `: "` + redactedValue + `"`, true
	}
	return redactText(line)
}

/* ---------- Parse error sanitization ---------- */

var quotedSpanPattern = regexp.MustCompile(`"[^"]{12,}"|'[^']{12,}'`)

const parseErrorMaxBytes = 300

// sanitizeParseError caps and scrubs a parser error so it never echoes file
// content (parsers quote offending lines, which may contain secrets).
func sanitizeParseError(err error) string {
	if err == nil {
		return ""
	}
	// Strip quoted spans before redactText: parsers quote the offending
	// content, and dropping it first preserves the position info that a
	// whole-message sentinel wipe would destroy.
	msg := quotedSpanPattern.ReplaceAllString(err.Error(), `"…"`)
	msg, _ = redactText(msg)
	msg = strings.ReplaceAll(msg, "\n", " ")
	if len(msg) > parseErrorMaxBytes {
		msg = msg[:parseErrorMaxBytes] + "…"
	}
	return msg
}

func shouldRedactKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
	if normalized == "key" || normalized == "apikey" || normalized == "password" || normalized == "secret" || normalized == "token" || normalized == "credential" || normalized == "auth" || normalized == "authorization" {
		return true
	}
	for _, needle := range []string{"apikey", "password", "secret", "token", "credential", "header", "projectroot"} {
		if strings.Contains(normalized, needle) {
			return true
		}
	}
	return false
}

func stringifySafe(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any, map[string]any:
		encoded, err := json.Marshal(v)
		if err == nil {
			return string(encoded)
		}
	}
	return fmt.Sprint(value)
}
