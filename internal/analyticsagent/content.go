package analyticsagent

import (
	"errors"
	"strings"
	"unicode"
)

// stripLeadingThinkBlocks is defense in depth for providers that unexpectedly
// place native reasoning in content despite reasoning_split=true. Complete
// leading blocks are removed; an unclosed leading block fails closed.
func stripLeadingThinkBlocks(content string) (string, error) {
	content = strings.TrimSpace(content)
	for strings.HasPrefix(content, "<think>") {
		end := strings.Index(content, "</think>")
		if end < 0 {
			return "", errors.New("unclosed think block")
		}
		content = strings.TrimSpace(content[end+len("</think>"):])
	}
	if strings.Contains(content, "<think>") || strings.Contains(content, "</think>") {
		return "", errors.New("unexpected think tag")
	}
	return content, nil
}

// visibleContentStream withholds reasoning envelopes, partial tag prefixes,
// and trailing whitespace until it is known to be user-visible answer text.
// Finish reconciles the streamed text with the provider's authoritative final
// content before the signed browser message is returned.
type visibleContentStream struct {
	raw     string
	emitted string
}

func newVisibleContentStream() *visibleContentStream {
	return &visibleContentStream{}
}

func (s *visibleContentStream) Push(delta string) (string, error) {
	if delta == "" {
		return "", nil
	}
	if len(s.raw)+len(delta) > maxProviderAssistantBytes {
		return "", errors.New("provider content is too large")
	}
	s.raw += delta
	visible, err := streamableVisibleContent(s.raw)
	if err != nil {
		return "", err
	}
	if len(visible) > maxFinalResponseBytes {
		return "", errors.New("provider visible content is too large")
	}
	if !strings.HasPrefix(visible, s.emitted) {
		return "", errors.New("provider content changed after it was streamed")
	}
	delta = visible[len(s.emitted):]
	s.emitted = visible
	return delta, nil
}

func (s *visibleContentStream) Finish(authoritative string) (string, error) {
	visible, err := stripLeadingThinkBlocks(authoritative)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(visible, s.emitted) {
		return "", errors.New("provider final content did not match streamed content")
	}
	delta := visible[len(s.emitted):]
	s.raw = authoritative
	s.emitted = visible
	return delta, nil
}

func streamableVisibleContent(content string) (string, error) {
	content = strings.TrimLeftFunc(content, unicode.IsSpace)
	for {
		if content == "" || strings.HasPrefix("<think>", content) {
			return "", nil
		}
		if !strings.HasPrefix(content, "<think>") {
			break
		}
		end := strings.Index(content, "</think>")
		if end < 0 {
			return "", nil
		}
		content = strings.TrimLeftFunc(content[end+len("</think>"):], unicode.IsSpace)
	}

	if strings.Contains(content, "<think>") || strings.Contains(content, "</think>") {
		return "", errors.New("unexpected think tag")
	}

	// Do not emit a suffix that could become a reasoning tag in the next
	// provider chunk. This also prevents the literal partial tag from flashing.
	safeEnd := len(content)
	for _, tag := range []string{"<think>", "</think>"} {
		for prefixBytes := 1; prefixBytes < len(tag); prefixBytes++ {
			if strings.HasSuffix(content[:safeEnd], tag[:prefixBytes]) {
				safeEnd -= prefixBytes
				break
			}
		}
	}
	return strings.TrimRightFunc(content[:safeEnd], unicode.IsSpace), nil
}
