package collector

import (
	"regexp"
	"strings"

	"github.com/johnnygreco/beacon/internal/capture"
	"github.com/johnnygreco/beacon/internal/models"
)

const RedactionVersion = "redact-v1"

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`bcn_(owner|enroll|ingest|read|admin)_[A-Za-z0-9_:-]+_[A-Fa-f0-9]{16,}`),
	regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password)\s*[:=]\s*["']?[^"'\s,}]+`),
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
	for _, pattern := range secretPatterns {
		value = pattern.ReplaceAllStringFunc(value, func(match string) string {
			if strings.HasPrefix(match, "bcn_") {
				return "[REDACTED_TOKEN]"
			}
			if idx := strings.IndexAny(match, ":="); idx >= 0 {
				return match[:idx+1] + "[REDACTED_SECRET]"
			}
			return "[REDACTED_SECRET]"
		})
	}
	return value
}

func redactPath(value string) string {
	if value == "" {
		return value
	}
	return redactString(value)
}
