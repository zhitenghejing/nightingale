// Package modeloutput separates provider reasoning envelopes from visible text.
package modeloutput

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Visible removes complete and unterminated think blocks. An incomplete block
// never becomes an answer; reasoning is not a user-facing operational record.
func Visible(raw string) string {
	var b strings.Builder
	for {
		lower := asciiLower(raw)
		i := strings.Index(lower, "<think>")
		if i < 0 {
			b.WriteString(raw)
			break
		}
		b.WriteString(raw[:i])
		end, ok := thinkingEnd(raw, i)
		if !ok {
			break
		}
		raw = raw[end:]
	}
	return strings.TrimSpace(b.String())
}

// JSON accepts only an object after leading reasoning blocks and a code fence.
// It does not search for braces inside reasoning or repair guessed content.
func JSON(raw string, out interface{}) error {
	if len(raw) > 1<<20 {
		return fmt.Errorf("model output exceeds limit")
	}
	raw = strings.TrimSpace(raw)
	for strings.HasPrefix(asciiLower(raw), "<think>") {
		end, ok := thinkingEnd(raw, 0)
		if !ok {
			return fmt.Errorf("model output is incomplete")
		}
		raw = strings.TrimSpace(raw[end:])
	}
	if strings.HasPrefix(raw, "```") {
		line := strings.IndexByte(raw, '\n')
		if line < 0 {
			return fmt.Errorf("model output fence is incomplete")
		}
		raw = strings.TrimSpace(raw[line+1:])
		if !strings.HasSuffix(raw, "```") {
			return fmt.Errorf("model output fence is incomplete")
		}
		raw = strings.TrimSpace(strings.TrimSuffix(raw, "```"))
	}
	if !strings.HasPrefix(raw, "{") || json.Unmarshal([]byte(raw), out) != nil {
		return fmt.Errorf("model did not return one complete JSON object")
	}
	return nil
}

// Nested or unterminated envelopes must not leak a tail of reasoning.
func thinkingEnd(raw string, start int) (int, bool) {
	lower := asciiLower(raw)
	position := start + len("<think>")
	depth := 1
	for position < len(lower) {
		close := strings.Index(lower[position:], "</think>")
		if close < 0 {
			return 0, false
		}
		close += position
		open := strings.Index(lower[position:], "<think>")
		if open >= 0 && position+open < close {
			depth++
			position += open + len("<think>")
			continue
		}
		depth--
		position = close + len("</think>")
		if depth == 0 {
			return position, true
		}
	}
	return 0, false
}

// ASCII protocol tags must not shift byte offsets in non-English text.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
