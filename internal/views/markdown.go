package views

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(
		html.WithHardWraps(),
	),
)

// RenderMarkdown converts markdown text to HTML for transcript messages.
// Goldmark's unsafe HTML mode is intentionally disabled, so raw HTML payloads
// are omitted before callers render the result with templ.Raw.
func RenderMarkdown(text string) string {
	var buf bytes.Buffer
	if err := md.Convert([]byte(text), &buf); err != nil {
		return text
	}
	return buf.String()
}

// systemTagPatterns matches common Claude Code XML wrapper tags that should be
// stripped or simplified when displaying user messages.
var systemTagPatterns = []*regexp.Regexp{
	// Full tag pairs with content — extract inner text
	regexp.MustCompile(`<command-name>(.*?)</command-name>`),
	regexp.MustCompile(`<command-message>(.*?)</command-message>`),
	regexp.MustCompile(`<command-args>(.*?)</command-args>`),
	// Multi-line tag pairs — use (?s) to match newlines
	regexp.MustCompile(`(?s)<local-command-caveat>(.*?)</local-command-caveat>`),
	regexp.MustCompile(`(?s)<persisted-output>(.*?)</persisted-output>`),
	// Opening tag without closing (truncated content)
	regexp.MustCompile(`<persisted-output>\s*`),
	// Catch-all for other system reminder / context tags
	regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`),
	regexp.MustCompile(`(?s)<available-deferred-tools>.*?</available-deferred-tools>`),
}

// CleanSystemTags strips or simplifies Claude Code system XML tags from text.
func CleanSystemTags(text string) string {
	result := text
	for _, re := range systemTagPatterns {
		result = re.ReplaceAllString(result, "$1")
	}
	// Collapse excessive whitespace left after stripping
	result = strings.TrimSpace(result)
	return result
}
