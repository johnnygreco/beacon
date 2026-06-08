package collector

import (
	"regexp"

	"github.com/johnnygreco/beacon/internal/capture"
	"github.com/johnnygreco/beacon/internal/ingest"
	"github.com/johnnygreco/beacon/internal/models"
)

const RedactionVersion = ingest.RedactionVersionV1

var tokenPattern = regexp.MustCompile(`bcn_(owner|enroll|ingest|read|admin)_[A-Za-z0-9_:-]+_[A-Fa-f0-9]{16,}`)

type secretValuePattern struct {
	re          *regexp.Regexp
	replacement string
}

var secretValuePatterns = []secretValuePattern{
	{regexp.MustCompile(`(?i)(\\["'])(api[_-]?key|token|secret|password)(\\["'])(\s*:\s*)(\\["'])(?:\\\\.|[^\\])*?(\\["'])`), `$1$2$3$4$5[REDACTED_SECRET]$6`},
	{regexp.MustCompile(`(?i)(["']?)(api[_-]?key|token|secret|password)(["']?)(\s*[:=]\s*)(")(?:\\.|[^"\\])*(")`), `$1$2$3$4$5[REDACTED_SECRET]$6`},
	{regexp.MustCompile(`(?i)(["']?)(api[_-]?key|token|secret|password)(["']?)(\s*[:=]\s*)(')(?:\\.|[^'\\])*(')`), `$1$2$3$4$5[REDACTED_SECRET]$6`},
	{regexp.MustCompile(`(?i)(["']?)(api[_-]?key|token|secret|password)(["']?)(\s*[:=]\s*)[^\s"',}]+`), `$1$2$3$4[REDACTED_SECRET]`},
}

func RedactEvents(events []capture.NormalizedEvent) []capture.NormalizedEvent {
	out := make([]capture.NormalizedEvent, len(events))
	for i, event := range events {
		out[i] = redactEvent(event)
	}
	return out
}

func RedactCaptureErrors(errorsIn []models.CaptureError) []models.CaptureError {
	out := make([]models.CaptureError, len(errorsIn))
	for i, errRow := range errorsIn {
		errRow.ErrorMessage = redactString(errRow.ErrorMessage)
		errRow.ContextFragment = redactString(errRow.ContextFragment)
		out[i] = errRow
	}
	return out
}

func redactEvent(event capture.NormalizedEvent) capture.NormalizedEvent {
	event.TextContent = redactString(event.TextContent)
	event.ToolInput = redactString(event.ToolInput)
	event.ToolOutput = redactString(event.ToolOutput)
	event.ErrorMessage = redactString(event.ErrorMessage)
	event.RawPayload = redactString(event.RawPayload)
	event.CWD = redactPath(event.CWD)
	return event
}

func redactString(value string) string {
	if value == "" {
		return value
	}
	value = tokenPattern.ReplaceAllString(value, "[REDACTED_TOKEN]")
	for _, pattern := range secretValuePatterns {
		value = pattern.re.ReplaceAllString(value, pattern.replacement)
	}
	return value
}

func redactPath(value string) string {
	if value == "" {
		return value
	}
	return redactString(value)
}
