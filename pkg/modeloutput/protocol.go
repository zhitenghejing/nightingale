package modeloutput

import (
	"regexp"
	"strings"
	"unicode"
)

var protocolTag = regexp.MustCompile(`(?i)</?(?:tool_call|tool_calls|function|function_call|tool_use|parameter|arguments|invoke)(?:[ =][^>]*)?>`)

// ProtocolOnly is deliberately structural. Ordinary prose that quotes a tool
// tag is still an answer and must never trigger answer-quality resampling.
func ProtocolOnly(text string) bool {
	text = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, text)
	return strings.TrimSpace(protocolTag.ReplaceAllString(text, "")) == ""
}

// Answer removes orphan transport prefixes at answer boundaries. Quoted
// tags within ordinary prose/code remain untouched.
var answerBoundaryTags = regexp.MustCompile(`(?i)^(?:\s*</(?:tool_call|tool_calls|function|function_call|tool_use|parameter|arguments|invoke)>)+\s*`)
var answerParameterPrefix = regexp.MustCompile(`(?is)^(?:\s*<parameter(?:[ =][^>]*)?>.*?</parameter>)+\s*(?:</(?:function|function_call|tool_call|tool_calls|tool_use|invoke)>\s*)+`)

func Answer(text string) string {
	text = Visible(text)
	for {
		text = strings.TrimLeftFunc(text, func(r rune) bool { return unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) })
		if prefix := answerParameterPrefix.FindStringIndex(text); prefix != nil {
			text = text[prefix[1]:]
			continue
		}
		if !strings.HasPrefix(text, "<") {
			return strings.TrimSpace(text)
		}
		end := strings.IndexByte(text, '>')
		if end < 0 || end > 256 {
			return strings.TrimSpace(text)
		}
		tag := strings.Map(func(r rune) rune {
			if unicode.Is(unicode.Cf, r) {
				return -1
			}
			return r
		}, text[:end+1])
		if answerBoundaryTags.ReplaceAllString(tag, "") != "" {
			return strings.TrimSpace(text)
		}
		text = text[end+1:]
	}
}

// ToolCommentary projects only ordinary visible commentary alongside native
// tool_calls. Transport tails are diagnostics, not another tool-call syntax to
// replay to the model. The canonical transcript still retains the original.
func ToolCommentary(text string) string {
	text = Answer(text)
	if strings.HasPrefix(text, "<") && protocolTag.MatchString(text) {
		return ""
	}
	return text
}
