package capture

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/store"
)

const previewMaxLen = 320

// Batcher accumulates events and flushes them to ClickHouse in table batches.
// Its input channel is sized to twice the configured batch size and applies
// backpressure to watcher sends when full; capture does not drop parsed events
// under bursty ingest.
type Batcher struct {
	eventCh chan BatchEvent
	store   *store.Store
	notify  func([]string) // called after each flush to notify SSE broker
	logger  *slog.Logger

	batchSize     int
	flushInterval time.Duration
	defaultInput  float64
	defaultOutput float64
	identity      FleetIdentity
	rawEventCache map[string]string
}

type FleetIdentity struct {
	NodeID            string
	CollectorID       string
	ControlPlaneEpoch string
	Sources           map[string]FleetSourceIdentity
}

type FleetSourceIdentity struct {
	SourceID string
}

type BatcherOption func(*Batcher)

func WithFleetIdentity(identity FleetIdentity) BatcherOption {
	return func(b *Batcher) {
		b.identity = normalizeFleetIdentity(identity)
	}
}

// NewBatcher creates a new batcher.
func NewBatcher(ch *store.Store, batchSize int, flushInterval time.Duration, defaultInput, defaultOutput float64, notify func([]string), logger *slog.Logger, options ...BatcherOption) *Batcher {
	b := &Batcher{
		eventCh:       make(chan BatchEvent, batchSize*2),
		store:         ch,
		notify:        notify,
		logger:        logger,
		batchSize:     batchSize,
		flushInterval: flushInterval,
		defaultInput:  defaultInput,
		defaultOutput: defaultOutput,
		rawEventCache: make(map[string]string),
	}
	b.identity = normalizeFleetIdentity(FleetIdentity{})
	for _, option := range options {
		option(b)
	}
	return b
}

// EventCh returns the channel to send events to. Senders should select on their
// context when writing so cancellation can break out of batcher backpressure.
func (b *Batcher) EventCh() chan<- BatchEvent {
	return b.eventCh
}

// Run starts the batcher loop. It blocks until ctx is cancelled.
func (b *Batcher) Run(ctx context.Context) {
	ticker := time.NewTicker(b.flushInterval)
	defer ticker.Stop()

	var inserts []NormalizedEvent

	flush := func() {
		if len(inserts) == 0 {
			return
		}
		sessionIDs := b.flushInserts(ctx, inserts)
		inserts = inserts[:0]

		if b.notify != nil && len(sessionIDs) > 0 {
			b.notify(sessionIDs)
		}
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			return
		case evt := <-b.eventCh:
			if evt.Insert != nil {
				inserts = append(inserts, evt.Insert.Normalized)
			}
			if len(inserts) >= b.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func changedSessionIDs(events []NormalizedEvent) []string {
	seen := make(map[string]struct{}, len(events))
	ids := make([]string, 0, len(events))
	for _, evt := range events {
		if evt.SessionID == "" {
			continue
		}
		if _, ok := seen[evt.SessionID]; ok {
			continue
		}
		seen[evt.SessionID] = struct{}{}
		ids = append(ids, evt.SessionID)
	}
	return ids
}

func sessionIDsFromEvents(events []models.Event) []string {
	seen := make(map[string]struct{}, len(events))
	ids := make([]string, 0, len(events))
	for _, evt := range events {
		if evt.SessionID == "" {
			continue
		}
		if _, ok := seen[evt.SessionID]; ok {
			continue
		}
		seen[evt.SessionID] = struct{}{}
		ids = append(ids, evt.SessionID)
	}
	return ids
}

func (b *Batcher) flushInserts(ctx context.Context, events []NormalizedEvent) []string {
	start := time.Now()

	batch, rawEvents := buildInsertRowBatchWithKnown(events, b.defaultInput, b.defaultOutput, b.identity, b.rawEventCache)

	if err := b.store.Flush(ctx, batch); err != nil {
		b.logger.Error("clickhouse flush failed", "error", err, "rows", len(events))
		return nil
	}
	for key, uid := range rawEvents {
		if _, ok := b.rawEventCache[key]; !ok {
			b.rawEventCache[key] = uid
		}
	}
	b.logger.Debug("flushInserts complete", "rows", len(events), "duration", time.Since(start))
	return sessionIDsFromEvents(batch.ActivityEvents)
}

func buildInsertRowBatch(events []NormalizedEvent, defaultInput, defaultOutput float64, identity FleetIdentity) store.RowBatch {
	batch, _ := buildInsertRowBatchWithKnown(events, defaultInput, defaultOutput, identity, nil)
	return batch
}

func buildInsertRowBatchWithKnown(events []NormalizedEvent, defaultInput, defaultOutput float64, identity FleetIdentity, knownRawEvents map[string]string) (store.RowBatch, map[string]string) {
	var batch store.RowBatch
	identity = normalizeFleetIdentity(identity)
	batchID := batchID(events, identity)
	prepared := make([]preparedEvent, 0, len(events))
	resolvedRawEvents := make(map[string]string, len(events))
	for key, uid := range knownRawEvents {
		resolvedRawEvents[key] = uid
	}
	batchRawEvents := make(map[string]string, len(events))

	for _, evt := range events {
		source := identity.source(evt.SourceName)
		rawSessionID := firstNonEmptyString(evt.RawSessionID, evt.SessionID)
		rawParentSessionID := firstNonEmptyString(evt.RawParentSessionID, evt.ParentSessionID)
		sourceEventIndex := normalizedSourceEventIndex(evt)
		rawEventID := normalizedRawEventID(evt, sourceEventIndex)
		payloadDigest := digestString(evt.RawPayload)
		sessionID := globalID("session", identity.CollectorID, source.SourceID, rawSessionID)
		parentSessionID := ""
		if rawParentSessionID != "" {
			parentSessionID = globalID("session", identity.CollectorID, source.SourceID, rawParentSessionID)
		}
		uid := globalID("event", identity.CollectorID, source.SourceID, rawSessionID, rawEventID, fmt.Sprint(sourceEventIndex))
		key := rawEventKey(source.SourceID, rawSessionID, rawEventID)
		if _, ok := resolvedRawEvents[key]; !ok {
			resolvedRawEvents[key] = uid
		}
		if _, ok := batchRawEvents[key]; !ok {
			batchRawEvents[key] = uid
		}
		prepared = append(prepared, preparedEvent{
			normalized:         evt,
			source:             source,
			rawSessionID:       rawSessionID,
			rawParentSessionID: rawParentSessionID,
			rawEventID:         rawEventID,
			sourceEventIndex:   sourceEventIndex,
			payloadDigest:      payloadDigest,
			sessionID:          sessionID,
			parentSessionID:    parentSessionID,
			eventUID:           uid,
		})
	}

	for _, item := range prepared {
		evt := item.normalized
		cost := evt.CostUSD
		if cost == 0 && (evt.InputTokens > 0 || evt.OutputTokens > 0) {
			cost = CalcCost(evt.Model, evt.InputTokens, evt.OutputTokens, defaultInput, defaultOutput)
		}

		preview := truncate(evt.TextContent, previewMaxLen)
		event := &models.Event{
			EventUID:           item.eventUID,
			SessionID:          evt.SessionID,
			RawSessionID:       item.rawSessionID,
			ParentSessionID:    item.parentSessionID,
			RawParentSessionID: item.rawParentSessionID,
			NodeID:             identity.NodeID,
			CollectorID:        identity.CollectorID,
			SourceID:           item.source.SourceID,
			SourceName:         evt.SourceName,
			Runtime:            evt.Runtime,
			Provider:           evt.Provider,
			Format:             evt.Format,
			EventKind:          evt.EventKind,
			PayloadType:        evt.PayloadType,
			ActorRole:          evt.ActorRole,
			Timestamp:          evt.Timestamp,
			TextContent:        evt.TextContent,
			TextPreview:        preview,
			ToolName:           evt.ToolName,
			ToolUseID:          evt.ToolUseID,
			Model:              evt.Model,
			InputTokens:        evt.InputTokens,
			OutputTokens:       evt.OutputTokens,
			CacheReadTokens:    evt.CacheReadTokens,
			CacheCreateTokens:  evt.CacheCreateTokens,
			DurationMs:         evt.DurationMs,
			CostUSD:            cost,
			ErrorCode:          evt.ErrorCode,
			ErrorMessage:       evt.ErrorMessage,
			EventVersion:       1,
			PayloadJSON:        evt.RawPayload,
			CWD:                evt.CWD,
			SourceFile:         evt.SourceFile,
			SourceLineNo:       evt.SourceLineNo,
			SourceOffset:       evt.SourceOffset,
			SourceGeneration:   evt.SourceGeneration,
			RawEventID:         item.rawEventID,
			SourceEventIndex:   item.sourceEventIndex,
			BatchID:            batchID,
			ControlPlaneEpoch:  identity.ControlPlaneEpoch,
			PayloadDigest:      item.payloadDigest,
			RedactionStatus:    "unredacted",
		}
		if item.rawSessionID != "" {
			event.SessionID = item.sessionID
		}

		batch.RawRecords = append(batch.RawRecords, store.NewRawRecord(*event))
		batch.ActivityEvents = append(batch.ActivityEvents, *event)

		rawLinkedEventID := firstNonEmptyString(evt.RawLinkedEventID, evt.ParentUUID)
		if rawLinkedEventID != "" {
			rawLinkedSessionID := firstNonEmptyString(evt.RawLinkedSessionID, item.rawSessionID)
			linkedSessionID := globalID("session", identity.CollectorID, item.source.SourceID, rawLinkedSessionID)
			linkedEventUID := resolvedRawEvents[rawEventKey(item.source.SourceID, rawLinkedSessionID, rawLinkedEventID)]
			resolutionStatus := "resolved"
			if linkedEventUID == "" {
				resolutionStatus = "unresolved"
			}
			linkScope := "same_session"
			if rawLinkedSessionID != "" && item.rawSessionID != "" && rawLinkedSessionID != item.rawSessionID {
				linkScope = "cross_session"
			}
			batch.EventLinks = append(batch.EventLinks, models.EventLink{
				EventUID:           item.eventUID,
				LinkedEventUID:     linkedEventUID,
				LinkType:           "parent",
				LinkScope:          linkScope,
				ResolutionStatus:   resolutionStatus,
				SessionID:          event.SessionID,
				RawSessionID:       item.rawSessionID,
				LinkedSessionID:    linkedSessionID,
				RawLinkedSessionID: rawLinkedSessionID,
				RawLinkedEventID:   rawLinkedEventID,
				CollectorID:        identity.CollectorID,
				SourceID:           item.source.SourceID,
				BatchID:            batchID,
				ControlPlaneEpoch:  identity.ControlPlaneEpoch,
			})
		}

		if evt.ToolPhase != "" && (evt.ToolInput != "" || evt.ToolOutput != "") {
			payload := models.ToolPayload{
				EventUID:          item.eventUID,
				CollectorID:       identity.CollectorID,
				SourceID:          item.source.SourceID,
				ToolName:          evt.ToolName,
				ToolPhase:         evt.ToolPhase,
				InputJSON:         evt.ToolInput,
				OutputJSON:        evt.ToolOutput,
				InputPreview:      truncate(evt.ToolInput, previewMaxLen),
				OutputPreview:     truncate(evt.ToolOutput, previewMaxLen),
				BatchID:           batchID,
				ControlPlaneEpoch: identity.ControlPlaneEpoch,
				PayloadDigest:     digestString(evt.ToolInput + "\x00" + evt.ToolOutput),
				RedactionStatus:   "unredacted",
			}
			batch.ToolPayloads = append(batch.ToolPayloads, payload)
		}
	}
	return batch, batchRawEvents
}

type preparedEvent struct {
	normalized         NormalizedEvent
	source             FleetSourceIdentity
	rawSessionID       string
	rawParentSessionID string
	rawEventID         string
	sourceEventIndex   uint64
	payloadDigest      string
	sessionID          string
	parentSessionID    string
	eventUID           string
}

// eventUID generates a deterministic UID for idempotent replay.
func eventUIDOrdinalKey(evt NormalizedEvent) string {
	return fmt.Sprintf("%s|%d|%d|%d|%s", evt.SourceFile, evt.SourceLineNo, evt.SourceOffset, evt.SourceGeneration, evt.RawPayload)
}

func eventUID(sourceFile string, lineNo int, offset int64, generation int, contentHash string, ordinal int) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%d|%d|%d|%s", sourceFile, lineNo, offset, generation, contentHash)
	if ordinal > 0 {
		fmt.Fprintf(h, "|%d", ordinal)
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func normalizeFleetIdentity(identity FleetIdentity) FleetIdentity {
	if identity.CollectorID == "" {
		identity.CollectorID = "collector_local"
	}
	if identity.NodeID == "" {
		identity.NodeID = "node_local"
	}
	if identity.ControlPlaneEpoch == "" {
		identity.ControlPlaneEpoch = "1"
	}
	if identity.Sources == nil {
		identity.Sources = map[string]FleetSourceIdentity{}
	}
	return identity
}

func (identity FleetIdentity) source(name string) FleetSourceIdentity {
	if source, ok := identity.Sources[name]; ok && source.SourceID != "" {
		return source
	}
	return FleetSourceIdentity{SourceID: globalID("source", identity.CollectorID, name)}
}

func batchID(events []NormalizedEvent, identity FleetIdentity) string {
	parts := make([]string, 0, len(events)+3)
	parts = append(parts, identity.CollectorID, identity.ControlPlaneEpoch)
	for _, evt := range events {
		parts = append(parts, eventUIDOrdinalKey(evt))
	}
	sort.Strings(parts[2:])
	return globalID("batch", parts...)
}

func normalizedRawEventID(evt NormalizedEvent, sourceEventIndex uint64) string {
	if evt.RawEventID != "" {
		return evt.RawEventID
	}
	if evt.MessageUUID != "" {
		return evt.MessageUUID
	}
	return fmt.Sprintf("%s:%d:%d:%d:%d", evt.SourceFile, evt.SourceGeneration, evt.SourceLineNo, evt.SourceOffset, sourceEventIndex)
}

func normalizedSourceEventIndex(evt NormalizedEvent) uint64 {
	if evt.SourceEventIndex > 0 {
		return evt.SourceEventIndex
	}
	hashed := hashUint64(
		evt.SourceFile,
		fmt.Sprint(evt.SourceGeneration),
		fmt.Sprint(evt.SourceLineNo),
		fmt.Sprint(evt.SourceOffset),
		evt.RawSessionID,
		evt.SessionID,
		evt.RawEventID,
		evt.MessageUUID,
		evt.ToolUseID,
		evt.ToolPhase,
		evt.RawPayload,
	)
	if hashed == 0 {
		hashed = 1
	}
	if evt.SourceLineNo > 0 {
		return uint64(evt.SourceLineNo)*1000000000 + hashed%1000000000
	}
	return hashed
}

func rawEventKey(sourceID, rawSessionID, rawEventID string) string {
	return sourceID + "\x00" + rawSessionID + "\x00" + rawEventID
}

func globalID(prefix string, parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return prefix + "_" + hex.EncodeToString(h.Sum(nil))[:32]
}

func digestString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func hashUint64(parts ...string) uint64 {
	sum := sha256.Sum256([]byte(globalID("idx", parts...)))
	return binary.BigEndian.Uint64(sum[:8])
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func genID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
