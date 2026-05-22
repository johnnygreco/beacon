package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
)

func NewRawRecord(event models.Event) models.RawRecord {
	capturedAt := time.Now().UTC()
	return models.RawRecord{
		RecordUID:        recordUID(event.SourceFile, event.SourceLineNo, event.SourceOffset, event.SourceGeneration, event.PayloadJSON),
		SourceName:       event.SourceName,
		Runtime:          firstNonEmpty(event.Runtime, runtimeForSource(event.SourceName)),
		Provider:         event.Provider,
		Format:           firstNonEmpty(event.Format, models.FormatJSONL),
		SourceFile:       event.SourceFile,
		SourceLineNo:     event.SourceLineNo,
		SourceOffset:     event.SourceOffset,
		SourceGeneration: event.SourceGeneration,
		SessionID:        event.SessionID,
		PayloadJSON:      event.PayloadJSON,
		CapturedAt:       capturedAt,
	}
}

func recordUID(sourceFile string, lineNo int, offset int64, generation int, payload string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%d|%d|%d|%s", sourceFile, lineNo, offset, generation, payload)
	return hex.EncodeToString(h.Sum(nil))[:32]
}
