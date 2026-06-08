package capture

import (
	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/redaction"
)

func RedactNormalizedEvents(events []NormalizedEvent, policy *redaction.Policy) []NormalizedEvent {
	if policy == nil {
		policy = redaction.DefaultPolicy()
	}
	out := make([]NormalizedEvent, len(events))
	for i, event := range events {
		out[i] = RedactNormalizedEvent(event, policy)
	}
	return out
}

func RedactNormalizedEvent(event NormalizedEvent, policy *redaction.Policy) NormalizedEvent {
	if policy == nil {
		policy = redaction.DefaultPolicy()
	}
	event.TextContent = policy.Redact(event.TextContent)
	event.ToolInput = policy.Redact(event.ToolInput)
	event.ToolOutput = policy.Redact(event.ToolOutput)
	event.ErrorMessage = policy.Redact(event.ErrorMessage)
	event.RawPayload = policy.Redact(event.RawPayload)
	event.CWD = policy.RedactPath(event.CWD)
	event.SourceFile = policy.RedactPath(event.SourceFile)
	return event
}

func RedactCaptureErrors(errorsIn []models.CaptureError, policy *redaction.Policy) []models.CaptureError {
	if policy == nil {
		policy = redaction.DefaultPolicy()
	}
	out := make([]models.CaptureError, len(errorsIn))
	for i, errRow := range errorsIn {
		out[i] = RedactCaptureError(errRow, policy)
	}
	return out
}

func RedactCaptureError(errRow models.CaptureError, policy *redaction.Policy) models.CaptureError {
	if policy == nil {
		policy = redaction.DefaultPolicy()
	}
	errRow.SourceFile = policy.RedactPath(errRow.SourceFile)
	errRow.ErrorMessage = policy.Redact(errRow.ErrorMessage)
	errRow.ContextFragment = policy.Redact(errRow.ContextFragment)
	return errRow
}

func RedactCheckpoints(checkpoints []models.Checkpoint, policy *redaction.Policy) []models.Checkpoint {
	if policy == nil {
		policy = redaction.DefaultPolicy()
	}
	out := make([]models.Checkpoint, len(checkpoints))
	for i, checkpoint := range checkpoints {
		out[i] = RedactCheckpoint(checkpoint, policy)
	}
	return out
}

func RedactCheckpoint(checkpoint models.Checkpoint, policy *redaction.Policy) models.Checkpoint {
	if policy == nil {
		policy = redaction.DefaultPolicy()
	}
	if checkpoint.SourceFileKey == "" {
		checkpoint.SourceFileKey = models.CheckpointSourceFileKey(checkpoint.SourceName, checkpoint.SourceFile)
	}
	checkpoint.SourceFile = policy.RedactPath(checkpoint.SourceFile)
	checkpoint.StateJSON = policy.Redact(checkpoint.StateJSON)
	return checkpoint
}
