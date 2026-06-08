package collector

import (
	"github.com/johnnygreco/beacon/internal/capture"
	"github.com/johnnygreco/beacon/internal/ingest"
	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/redaction"
)

const RedactionVersion = ingest.RedactionVersionV1

func RedactEvents(events []capture.NormalizedEvent) []capture.NormalizedEvent {
	return capture.RedactNormalizedEvents(events, redaction.DefaultPolicy())
}

func RedactCaptureErrors(errorsIn []models.CaptureError) []models.CaptureError {
	return capture.RedactCaptureErrors(errorsIn, redaction.DefaultPolicy())
}
