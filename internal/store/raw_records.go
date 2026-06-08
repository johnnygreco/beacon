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
		RecordUID:         recordUID(event.CollectorID, event.SourceID, event.RawSessionID, rawEventID, payloadDigest),
		EventUID:          event.EventUID,
		NodeID:            event.NodeID,
		CollectorID:       event.CollectorID,
		SourceID:          event.SourceID,
		SourceName:        event.SourceName,
		Runtime:           firstNonEmpty(event.Runtime, runtimeForSource(event.SourceName)),
		Provider:          event.Provider,
		Format:            firstNonEmpty(event.Format, models.FormatJSONL),
		SourceFile:        event.SourceFile,
		SourceLineNo:      event.SourceLineNo,
		SourceOffset:      event.SourceOffset,
		SourceGeneration:  event.SourceGeneration,
		SessionID:         event.SessionID,
		RawSessionID:      event.RawSessionID,
		RawEventID:        rawEventID,
		SourceEventIndex:  event.SourceEventIndex,
		BatchID:           event.BatchID,
		ControlPlaneEpoch: event.ControlPlaneEpoch,
		PayloadDigest:     payloadDigest,
		RedactionStatus:   event.RedactionStatus,
		RedactionVersion:  event.RedactionVersion,
		PayloadJSON:       event.PayloadJSON,
		CapturedAt:        capturedAt,
	}
}

func recordPayloadDigest(payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func recordUID(collectorID, sourceID, rawSessionID, rawEventID, payloadDigest string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s|%s|%s", collectorID, sourceID, rawSessionID, rawEventID, payloadDigest)
	return hex.EncodeToString(h.Sum(nil))[:32]
}
