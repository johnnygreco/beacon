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
	rawEventID := firstNonEmpty(event.RawEventID, event.EventUID)
	payloadDigest := event.PayloadDigest
	if payloadDigest == "" {
		payloadDigest = recordPayloadDigest(event.PayloadJSON)
	}
	return models.RawRecord{
		RecordUID:        recordUID(event.SourceName, event.RawSessionID, rawEventID, event.SourceEventIndex, payloadDigest),
		EventUID:         event.EventUID,
		SourceName:       event.SourceName,
		Runtime:          firstNonEmpty(event.Runtime, runtimeForSource(event.SourceName)),
		Provider:         event.Provider,
		Format:           firstNonEmpty(event.Format, models.FormatJSONL),
		SourceFile:       event.SourceFile,
		SourceLineNo:     event.SourceLineNo,
		SourceOffset:     event.SourceOffset,
		SourceGeneration: event.SourceGeneration,
		SessionID:        event.SessionID,
		RawSessionID:     event.RawSessionID,
		RawEventID:       rawEventID,
		SourceEventIndex: event.SourceEventIndex,
		PayloadDigest:    payloadDigest,
		RedactionStatus:  event.RedactionStatus,
		RedactionVersion: event.RedactionVersion,
		PayloadJSON:      event.PayloadJSON,
		CapturedAt:       capturedAt,
	}
}

func recordPayloadDigest(payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func recordUID(sourceName, rawSessionID, rawEventID string, sourceEventIndex uint64, payloadDigest string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s|%d|%s", sourceName, rawSessionID, rawEventID, sourceEventIndex, payloadDigest)
	return hex.EncodeToString(h.Sum(nil))[:32]
}
