package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
)

func setupLiveClickHouse(t *testing.T) *Store {
	t.Helper()
	addr := os.Getenv("BEACON_TEST_CLICKHOUSE")
	if addr == "" {
		t.Skip("set BEACON_TEST_CLICKHOUSE to run ClickHouse store integration tests")
	}

	opts := DefaultOptions()
	opts.Addrs = []string{addr}
	opts.Database = "beacon_test_store"
	resetter, err := OpenForReset(context.Background(), opts)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	if err := Reset(context.Background(), resetter.DB, resetter.Database()); err != nil {
		resetter.Close()
		t.Fatalf("reset: %v", err)
	}
	resetter.Close()

	ch, err := Open(context.Background(), opts)
	if err != nil {
		t.Fatalf("open reset clickhouse store: %v", err)
	}
	t.Cleanup(func() { _ = ch.Close() })
	return ch
}

func TestClickHouseResetDropsRowsAndRecreatesTables(t *testing.T) {
	ch := setupLiveClickHouse(t)
	ctx := context.Background()
	sessionID := "reset-session"
	event := models.Event{
		EventUID:     "evt-reset",
		SessionID:    sessionID,
		SourceName:   "codex",
		Provider:     "openai",
		Format:       "jsonl",
		EventKind:    "message",
		ActorRole:    "user",
		Timestamp:    time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC),
		TextContent:  "reset keeps migrations usable",
		TextPreview:  "reset keeps migrations usable",
		InputTokens:  1,
		OutputTokens: 2,
		SourceFile:   "reset.jsonl",
		SourceLineNo: 1,
	}
	batch := RowBatch{
		ActivityEvents: []models.Event{event},
		RawRecords:     []models.RawRecord{NewRawRecord(event)},
	}
	countRows := func(table string) uint64 {
		t.Helper()
		var count uint64
		if err := ch.DB.QueryRowContext(ctx,
			"SELECT count() FROM "+table+" WHERE session_id = ?", sessionID).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return count
	}

	if err := ch.Flush(ctx, batch); err != nil {
		t.Fatalf("initial flush: %v", err)
	}
	if got := countRows("activity_events"); got != 1 {
		t.Fatalf("activity_events before reset = %d, want 1", got)
	}
	if got := countRows("raw_records"); got != 1 {
		t.Fatalf("raw_records before reset = %d, want 1", got)
	}

	if err := Reset(ctx, ch.DB, ch.Database()); err != nil {
		t.Fatalf("reset: %v", err)
	}
	for _, table := range []string{"raw_records", "activity_events", "search_documents", "session_projection"} {
		if got := countRows(table); got != 0 {
			t.Fatalf("%s after reset = %d, want 0", table, got)
		}
	}

	if err := ch.Flush(ctx, batch); err != nil {
		t.Fatalf("post-reset flush: %v", err)
	}
	if got := countRows("activity_events"); got != 1 {
		t.Fatalf("activity_events after post-reset flush = %d, want 1", got)
	}
	if got := countRows("session_projection"); got != 1 {
		t.Fatalf("session_projection after post-reset flush = %d, want 1", got)
	}
}

func TestClickHouseRefreshOutdatedProjectionsRepairsMissingAndStaleProjection(t *testing.T) {
	ch := setupLiveClickHouse(t)
	ctx := context.Background()
	sessionID := "missing-projection-session"
	event := models.Event{
		EventUID:     "evt-missing-projection",
		SessionID:    sessionID,
		SourceName:   "claude",
		Provider:     "anthropic",
		Format:       "jsonl",
		EventKind:    "message",
		ActorRole:    "assistant",
		Timestamp:    time.Date(2026, 5, 22, 11, 0, 0, 0, time.UTC),
		TextContent:  "projection repair event",
		TextPreview:  "projection repair event",
		OutputTokens: 42,
		SourceFile:   "repair.jsonl",
		SourceLineNo: 1,
	}

	if err := ch.insertActivityEvents(ctx, []models.Event{event}); err != nil {
		t.Fatalf("insert activity events: %v", err)
	}
	refreshed, didRefresh, err := ch.RefreshOutdatedProjections(ctx)
	if err != nil {
		t.Fatalf("refresh outdated projections: %v", err)
	}
	if !didRefresh || refreshed != 1 {
		t.Fatalf("refresh outdated projections = count %d refreshed %v, want 1 true", refreshed, didRefresh)
	}

	var projectedEvents, projectedTokens uint64
	if err := ch.DB.QueryRowContext(ctx,
		`SELECT argMax(event_count, updated_at), argMax(total_tokens, updated_at)
		 FROM session_projection
		 WHERE session_id = ?`, sessionID).Scan(&projectedEvents, &projectedTokens); err != nil {
		t.Fatalf("projection query: %v", err)
	}
	if projectedEvents != 1 || projectedTokens != 42 {
		t.Fatalf("projection = events %d tokens %d, want 1 and 42", projectedEvents, projectedTokens)
	}

	staleEvent := event
	staleEvent.EventUID = "evt-stale-projection"
	staleEvent.TextContent = "stale projection event"
	staleEvent.TextPreview = "stale projection event"
	staleEvent.OutputTokens = 10
	if err := ch.insertActivityEvents(ctx, []models.Event{staleEvent}); err != nil {
		t.Fatalf("insert stale activity event: %v", err)
	}
	refreshed, didRefresh, err = ch.RefreshOutdatedProjections(ctx)
	if err != nil {
		t.Fatalf("refresh stale projections: %v", err)
	}
	if !didRefresh || refreshed != 1 {
		t.Fatalf("refresh stale projections = count %d refreshed %v, want 1 true", refreshed, didRefresh)
	}
	if err := ch.DB.QueryRowContext(ctx,
		`SELECT argMax(event_count, updated_at), argMax(total_tokens, updated_at)
		 FROM session_projection
		 WHERE session_id = ?`, sessionID).Scan(&projectedEvents, &projectedTokens); err != nil {
		t.Fatalf("stale projection query: %v", err)
	}
	if projectedEvents != 2 || projectedTokens != 52 {
		t.Fatalf("stale projection = events %d tokens %d, want 2 and 52", projectedEvents, projectedTokens)
	}

	refreshed, didRefresh, err = ch.RefreshOutdatedProjections(ctx)
	if err != nil {
		t.Fatalf("second refresh outdated projections: %v", err)
	}
	if didRefresh || refreshed != 0 {
		t.Fatalf("second refresh outdated projections = count %d refreshed %v, want 0 false", refreshed, didRefresh)
	}
}

func TestClickHouseRefreshOutdatedSearchIndexRebuildsLegacyDocuments(t *testing.T) {
	ch := setupLiveClickHouse(t)
	ctx := context.Background()
	sessionID := "legacy-search-index-session"
	event := models.Event{
		EventUID:    "evt-legacy-search-index",
		SessionID:   sessionID,
		SourceName:  "claude",
		Provider:    "anthropic",
		Format:      "jsonl",
		EventKind:   "tool_call",
		ActorRole:   "assistant",
		Timestamp:   time.Date(2026, 5, 22, 11, 30, 0, 0, time.UTC),
		TextPreview: "Read",
		ToolName:    "Read",
		PayloadJSON: `{"request":"eventpayloadrefresh"}`,
		SourceFile:  "search.jsonl",
	}
	payload := models.ToolPayload{
		EventUID:     event.EventUID,
		ToolName:     "Read",
		ToolPhase:    "call",
		InputJSON:    `{"file_path":"inputpayloadrefresh.go"}`,
		OutputJSON:   `{"result":"outputpayloadrefresh"}`,
		InputPreview: `{"file_path":"preview.go"}`,
	}
	legacyDoc := models.SearchDocument{
		EventUID:       event.EventUID,
		SessionID:      sessionID,
		EventKind:      event.EventKind,
		Timestamp:      event.Timestamp,
		TextPreview:    event.TextPreview,
		ToolName:       event.ToolName,
		Provider:       event.Provider,
		SearchableText: "legacy search text without payload marker",
		DocumentLength: 6,
	}

	if err := ch.insertActivityEvents(ctx, []models.Event{event}); err != nil {
		t.Fatalf("insert activity event: %v", err)
	}
	if err := ch.insertToolPayloads(ctx, []models.ToolPayload{payload}); err != nil {
		t.Fatalf("insert tool payload: %v", err)
	}
	if err := ch.insertSearchDocuments(ctx, []models.SearchDocument{legacyDoc}); err != nil {
		t.Fatalf("insert legacy search document: %v", err)
	}

	refreshed, didRefresh, err := ch.RefreshOutdatedSearchIndex(ctx)
	if err != nil {
		t.Fatalf("refresh outdated search index: %v", err)
	}
	if !didRefresh || refreshed != 1 {
		t.Fatalf("refresh outdated search index = count %d refreshed %v, want 1 true", refreshed, didRefresh)
	}

	var indexedText string
	if err := ch.DB.QueryRowContext(ctx,
		`SELECT argMax(searchable_text, updated_at)
		 FROM search_documents
		 WHERE event_uid = ?`, event.EventUID).Scan(&indexedText); err != nil {
		t.Fatalf("query refreshed search document: %v", err)
	}
	for _, want := range []string{searchIndexVersionMarker, "eventpayloadrefresh", "inputpayloadrefresh.go", "outputpayloadrefresh"} {
		if !strings.Contains(indexedText, want) {
			t.Fatalf("refreshed search text missing %q: %s", want, indexedText)
		}
	}

	refreshed, didRefresh, err = ch.RefreshOutdatedSearchIndex(ctx)
	if err != nil {
		t.Fatalf("second refresh outdated search index: %v", err)
	}
	if didRefresh || refreshed != 0 {
		t.Fatalf("second refresh outdated search index = count %d refreshed %v, want 0 false", refreshed, didRefresh)
	}

	postingSessionID := "missing-postings-session"
	postingEvent := models.Event{
		EventUID:    "evt-missing-postings",
		SessionID:   postingSessionID,
		SourceName:  "claude",
		Provider:    "anthropic",
		Format:      "jsonl",
		EventKind:   "message",
		ActorRole:   "assistant",
		Timestamp:   time.Date(2026, 5, 22, 11, 31, 0, 0, time.UTC),
		TextContent: "missingpostingsneedle",
		TextPreview: "missingpostingsneedle",
		SourceFile:  "search.jsonl",
	}
	if err := ch.insertActivityEvents(ctx, []models.Event{postingEvent}); err != nil {
		t.Fatalf("insert posting activity event: %v", err)
	}
	currentDocs, _ := buildSearchRows([]models.Event{postingEvent}, nil)
	if len(currentDocs) != 1 {
		t.Fatalf("current docs = %d, want 1", len(currentDocs))
	}
	if err := ch.insertSearchDocuments(ctx, currentDocs); err != nil {
		t.Fatalf("insert current search document without postings: %v", err)
	}
	refreshed, didRefresh, err = ch.RefreshOutdatedSearchIndex(ctx)
	if err != nil {
		t.Fatalf("refresh missing search postings: %v", err)
	}
	if !didRefresh || refreshed != 2 {
		t.Fatalf("refresh missing search postings = count %d refreshed %v, want 2 true", refreshed, didRefresh)
	}

	var postingCount uint64
	if err := ch.DB.QueryRowContext(ctx,
		`SELECT count()
		 FROM search_postings FINAL
		 WHERE event_uid = ? AND token = 'missingpostingsneedle'`, postingEvent.EventUID).Scan(&postingCount); err != nil {
		t.Fatalf("query refreshed search postings: %v", err)
	}
	if postingCount != 1 {
		t.Fatalf("missingpostingsneedle posting count = %d, want 1", postingCount)
	}
}

func TestClickHouseSchemaVersionRecorded(t *testing.T) {
	ch := setupLiveClickHouse(t)
	version, ok, err := DetectSchemaVersion(context.Background(), ch.DB, ch.Database())
	if err != nil {
		t.Fatalf("detect schema version: %v", err)
	}
	if !ok || version != CurrentSchemaVersion {
		t.Fatalf("schema version = %d ok=%v, want %d true", version, ok, CurrentSchemaVersion)
	}
}

func TestClickHouseCommitIngestBatchIsIdempotent(t *testing.T) {
	ch := setupLiveClickHouse(t)
	ctx := context.Background()
	meta := IngestBatchMeta{
		CollectorID:       "collector-ingest",
		BatchID:           "batch-ingest-1",
		NodeID:            "node-ingest",
		Sequence:          1,
		ControlPlaneEpoch: "1",
		PayloadDigest:     "sha256:batch-one",
		RedactionVersion:  "redact-v1",
		CreatedAt:         time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC),
	}
	event := models.Event{
		EventUID:          "evt-ingest-1",
		SessionID:         "session-ingest",
		NodeID:            meta.NodeID,
		CollectorID:       meta.CollectorID,
		SourceID:          "source-ingest",
		SourceName:        "codex",
		Runtime:           "codex",
		Provider:          "openai",
		Format:            "jsonl",
		EventKind:         "message",
		ActorRole:         "assistant",
		Timestamp:         time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC),
		TextContent:       "ingest idempotency",
		TextPreview:       "ingest idempotency",
		SourceFile:        "session.jsonl",
		SourceLineNo:      1,
		BatchID:           meta.BatchID,
		ControlPlaneEpoch: meta.ControlPlaneEpoch,
		PayloadDigest:     "sha256:event-one",
		RedactionStatus:   "redacted",
		RedactionVersion:  meta.RedactionVersion,
	}
	rows := RowBatch{
		ActivityEvents: []models.Event{event},
		RawRecords:     []models.RawRecord{NewRawRecord(event)},
		Checkpoints: []models.Checkpoint{{
			NodeID:      meta.NodeID,
			CollectorID: meta.CollectorID,
			SourceID:    "source-ingest",
			SourceName:  "codex",
			SourceFile:  "session.jsonl",
			LastOffset:  42,
			LastLineNo:  1,
		}},
	}

	ack, err := ch.CommitIngestBatch(ctx, meta, rows)
	if err != nil {
		t.Fatalf("CommitIngestBatch: %v", err)
	}
	if ack.NextSequence != 2 || ack.EventsWritten != 1 || ack.RawRecordsWritten != 1 {
		t.Fatalf("ack = %#v, want next 2 and one row", ack)
	}
	duplicate, err := ch.CommitIngestBatch(ctx, meta, rows)
	if err != nil {
		t.Fatalf("duplicate CommitIngestBatch: %v", err)
	}
	if duplicate != ack {
		t.Fatalf("duplicate ack = %#v, want %#v", duplicate, ack)
	}
	conflict := meta
	conflict.PayloadDigest = "sha256:different"
	if _, err := ch.CommitIngestBatch(ctx, conflict, rows); !errors.Is(err, ErrIngestBatchDigestMismatch) {
		t.Fatalf("conflict error = %v, want ErrIngestBatchDigestMismatch", err)
	}
	gap := meta
	gap.BatchID = "batch-ingest-gap"
	gap.Sequence = 3
	gap.PayloadDigest = "sha256:gap"
	if _, err := ch.CommitIngestBatch(ctx, gap, rows); !errors.Is(err, ErrIngestBatchSequenceGap) {
		t.Fatalf("sequence gap error = %v, want ErrIngestBatchSequenceGap", err)
	}

	var status string
	if err := ch.DB.QueryRowContext(ctx,
		`SELECT argMax(status, state_version)
			 FROM ingest_batches
			 WHERE collector_id = ? AND batch_id = ?`,
		meta.CollectorID,
		meta.BatchID,
	).Scan(&status); err != nil {
		t.Fatalf("query ingest batch status: %v", err)
	}
	if status != BatchStateCommitted {
		t.Fatalf("status = %q, want committed", status)
	}
	var checkpointOffset uint64
	if err := ch.DB.QueryRowContext(ctx,
		`SELECT argMax(last_offset, updated_at)
		 FROM capture_checkpoints
		 WHERE collector_id = ? AND source_id = ? AND source_file = ?`,
		meta.CollectorID,
		"source-ingest",
		"session.jsonl",
	).Scan(&checkpointOffset); err != nil {
		t.Fatalf("query checkpoint: %v", err)
	}
	if checkpointOffset != 42 {
		t.Fatalf("checkpoint offset = %d, want 42", checkpointOffset)
	}
}

func TestClickHouseIngestBatchStateVersionBreaksTimestampTies(t *testing.T) {
	ch := setupLiveClickHouse(t)
	ctx := context.Background()
	meta := IngestBatchMeta{
		CollectorID:       "collector-state-version",
		BatchID:           "batch-state-version",
		NodeID:            "node-state-version",
		Sequence:          1,
		ControlPlaneEpoch: "1",
		PayloadDigest:     "sha256:state-version",
		RedactionVersion:  "redact-v1",
		CreatedAt:         time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC),
	}
	tiedAt := time.Date(2026, 6, 7, 12, 1, 2, 123000000, time.UTC)
	receiving := batchRecordFromRows(meta, RowBatch{}, 1, BatchStateReceiving, "")
	receiving.ReceivedAt = tiedAt
	receiving.UpdatedAt = tiedAt
	committed := batchRecordFromRows(meta, RowBatch{}, 2, BatchStateCommitted, "")
	committed.ReceivedAt = tiedAt
	committed.UpdatedAt = tiedAt
	committed.CommittedAt = &tiedAt
	if err := ch.insertIngestBatchRecord(ctx, receiving); err != nil {
		t.Fatalf("insert receiving: %v", err)
	}
	if err := ch.insertIngestBatchRecord(ctx, committed); err != nil {
		t.Fatalf("insert committed: %v", err)
	}
	record, ok, err := ch.GetIngestBatch(ctx, meta.CollectorID, meta.BatchID)
	if err != nil {
		t.Fatalf("GetIngestBatch: %v", err)
	}
	if !ok || record.Status != BatchStateCommitted || record.StateVersion != 2 {
		t.Fatalf("record = %#v ok=%v, want committed state_version 2", record, ok)
	}
}

func TestClickHouseCommitIngestBatchConcurrentDuplicateIsIdempotent(t *testing.T) {
	ch := setupLiveClickHouse(t)
	ctx := context.Background()
	meta := IngestBatchMeta{
		CollectorID:       "collector-concurrent",
		BatchID:           "batch-concurrent",
		NodeID:            "node-concurrent",
		Sequence:          1,
		ControlPlaneEpoch: "1",
		PayloadDigest:     "sha256:concurrent",
		RedactionVersion:  "redact-v1",
		CreatedAt:         time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC),
	}
	event := models.Event{
		EventUID:          "evt-concurrent",
		SessionID:         "session-concurrent",
		NodeID:            meta.NodeID,
		CollectorID:       meta.CollectorID,
		SourceID:          "source-concurrent",
		SourceName:        "codex",
		Runtime:           "codex",
		Provider:          "openai",
		Format:            "jsonl",
		EventKind:         "message",
		ActorRole:         "assistant",
		Timestamp:         time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC),
		TextContent:       "concurrent duplicate",
		TextPreview:       "concurrent duplicate",
		SourceFile:        "session.jsonl",
		SourceLineNo:      1,
		BatchID:           meta.BatchID,
		ControlPlaneEpoch: meta.ControlPlaneEpoch,
		PayloadDigest:     meta.PayloadDigest,
		RedactionStatus:   "redacted",
		RedactionVersion:  meta.RedactionVersion,
	}
	rows := RowBatch{
		ActivityEvents: []models.Event{event},
		RawRecords:     []models.RawRecord{NewRawRecord(event)},
	}

	const workers = 4
	acks := make(chan IngestBatchAck, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ack, err := ch.CommitIngestBatch(ctx, meta, rows)
			if err != nil {
				errs <- err
				return
			}
			acks <- ack
		}()
	}
	wg.Wait()
	close(acks)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent CommitIngestBatch: %v", err)
	}
	var first IngestBatchAck
	for ack := range acks {
		if first.BatchID == "" {
			first = ack
			continue
		}
		if ack != first {
			t.Fatalf("ack = %#v, want %#v", ack, first)
		}
	}
	record, ok, err := ch.GetIngestBatch(ctx, meta.CollectorID, meta.BatchID)
	if err != nil {
		t.Fatalf("GetIngestBatch: %v", err)
	}
	if !ok || record.Status != BatchStateCommitted {
		t.Fatalf("record = %#v ok=%v, want committed", record, ok)
	}
}

func TestClickHouseLocalCheckpointsDoNotCollapseBySharedFile(t *testing.T) {
	ch := setupLiveClickHouse(t)
	ctx := context.Background()
	if err := ch.Flush(ctx, RowBatch{Checkpoints: []models.Checkpoint{
		{SourceName: "codex", SourceFile: "shared-session.jsonl", LastOffset: 10, LastLineNo: 1},
		{SourceName: "claude", SourceFile: "shared-session.jsonl", LastOffset: 20, LastLineNo: 2},
	}}); err != nil {
		t.Fatalf("Flush checkpoints: %v", err)
	}
	var count uint64
	if err := ch.DB.QueryRowContext(ctx,
		`SELECT count()
		 FROM capture_checkpoints FINAL
		 WHERE source_file = ?`,
		"shared-session.jsonl",
	).Scan(&count); err != nil {
		t.Fatalf("query checkpoints: %v", err)
	}
	if count != 2 {
		t.Fatalf("checkpoint count = %d, want 2", count)
	}
}

func TestClickHouseCaptureErrorsAreReplayDeduped(t *testing.T) {
	ch := setupLiveClickHouse(t)
	ctx := context.Background()
	errRow := models.CaptureError{
		ID:                "capture-error-dedupe",
		NodeID:            "node-errors",
		CollectorID:       "collector-errors",
		SourceID:          "source-errors",
		SourceName:        "codex",
		SourceFile:        "session.jsonl",
		BatchID:           "batch-errors",
		ControlPlaneEpoch: "1",
		ErrorClass:        "parse_error",
		ErrorMessage:      "bad json",
		ContextFragment:   "{bad",
		CreatedAt:         time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC),
	}
	rows := RowBatch{CaptureErrors: []models.CaptureError{errRow}}
	if err := ch.Flush(ctx, rows); err != nil {
		t.Fatalf("first Flush: %v", err)
	}
	if err := ch.Flush(ctx, rows); err != nil {
		t.Fatalf("second Flush: %v", err)
	}
	var count uint64
	if err := ch.DB.QueryRowContext(ctx,
		`SELECT count()
		 FROM capture_errors FINAL
		 WHERE collector_id = ? AND batch_id = ? AND id = ?`,
		errRow.CollectorID,
		errRow.BatchID,
		errRow.ID,
	).Scan(&count); err != nil {
		t.Fatalf("query capture errors: %v", err)
	}
	if count != 1 {
		t.Fatalf("capture error count = %d, want 1", count)
	}
}

func TestClickHouseMigrateRejectsLegacySchemaWithoutVersion(t *testing.T) {
	ch := setupLiveClickHouse(t)
	ctx := context.Background()

	if _, err := ch.DB.ExecContext(ctx, "DROP TABLE IF EXISTS "+schemaVersionTable); err != nil {
		t.Fatalf("drop schema version table: %v", err)
	}
	err := Migrate(ctx, ch.DB, ch.Database())
	if err == nil {
		t.Fatalf("Migrate returned nil for legacy schema")
	}
	if !strings.Contains(err.Error(), "legacy Beacon tables are missing schema_version") ||
		!strings.Contains(err.Error(), "beacon db reset --force") {
		t.Fatalf("Migrate error = %q, want legacy reset instruction", err.Error())
	}

	if err := Reset(ctx, ch.DB, ch.Database()); err != nil {
		t.Fatalf("reset after legacy rejection: %v", err)
	}
}

func TestClickHouseFreshMigrateAndResetCreateSameTables(t *testing.T) {
	ch := setupLiveClickHouse(t)
	ctx := context.Background()
	database := "beacon_test_schema_exact"
	if _, err := ch.DB.ExecContext(ctx, "DROP DATABASE IF EXISTS "+database); err != nil {
		t.Fatalf("drop test database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ch.DB.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+database)
	})

	if err := Migrate(ctx, ch.DB, database); err != nil {
		t.Fatalf("fresh migrate: %v", err)
	}
	freshTables := clickHouseTableSet(t, ch.DB, database)
	assertBeaconTableSet(t, freshTables)
	freshSchema := clickHouseCreateStatements(t, ch.DB, database)

	if err := Reset(ctx, ch.DB, database); err != nil {
		t.Fatalf("reset: %v", err)
	}
	resetTables := clickHouseTableSet(t, ch.DB, database)
	assertBeaconTableSet(t, resetTables)
	resetSchema := clickHouseCreateStatements(t, ch.DB, database)

	if len(freshTables) != len(resetTables) {
		t.Fatalf("fresh tables = %v, reset tables = %v", freshTables, resetTables)
	}
	for table := range freshTables {
		if !resetTables[table] {
			t.Fatalf("table %s exists after fresh migrate but not reset; fresh=%v reset=%v", table, freshTables, resetTables)
		}
		if resetSchema[table] != freshSchema[table] {
			t.Fatalf("schema for %s differs after reset\nfresh:\n%s\nreset:\n%s", table, freshSchema[table], resetSchema[table])
		}
	}
}

func clickHouseTableSet(t *testing.T, db *sql.DB, database string) map[string]bool {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `SELECT name FROM system.tables WHERE database = ?`, database)
	if err != nil {
		t.Fatalf("query tables: %v", err)
	}
	defer rows.Close()

	tables := make(map[string]bool)
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatalf("scan table: %v", err)
		}
		tables[table] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read tables: %v", err)
	}
	return tables
}

func clickHouseCreateStatements(t *testing.T, db *sql.DB, database string) map[string]string {
	t.Helper()
	schema := make(map[string]string, len(tableNames))
	for _, table := range tableNames {
		var stmt string
		if err := db.QueryRowContext(context.Background(), fmt.Sprintf("SHOW CREATE TABLE %s.%s", database, table)).Scan(&stmt); err != nil {
			t.Fatalf("show create table %s: %v", table, err)
		}
		schema[table] = stmt
	}
	return schema
}

func assertBeaconTableSet(t *testing.T, tables map[string]bool) {
	t.Helper()
	if len(tables) != len(tableNames) {
		t.Fatalf("tables = %v, want %d Beacon tables", tables, len(tableNames))
	}
	for _, table := range tableNames {
		if !tables[table] {
			t.Fatalf("missing Beacon table %s in %v", table, tables)
		}
	}
}

func TestClickHouseFlushRefreshesDeduplicatedProjection(t *testing.T) {
	ch := setupLiveClickHouse(t)
	now := time.Now().UTC()
	sessionID := "live-session"
	fullPayload := strings.Repeat("SECRET_FULL_ONLY ", 200)

	batch := RowBatch{
		ActivityEvents: []models.Event{
			{
				EventUID:         "evt-message",
				SessionID:        sessionID,
				SourceName:       "test-source",
				Runtime:          "test-runtime",
				Provider:         "test-provider",
				Format:           "jsonl",
				EventKind:        "message",
				ActorRole:        "user",
				Timestamp:        now,
				TextContent:      "alpha beta beta",
				TextPreview:      "alpha beta beta",
				EventVersion:     1,
				SourceFile:       "live.jsonl",
				SourceLineNo:     1,
				SourceOffset:     0,
				SourceGeneration: 3,
			},
			{
				EventUID:         "evt-tool",
				SessionID:        sessionID,
				SourceName:       "test-source",
				Runtime:          "test-runtime",
				Provider:         "test-provider",
				Format:           "jsonl",
				EventKind:        "tool_call",
				ToolName:         "Bash",
				Model:            "gpt-4",
				Timestamp:        now.Add(time.Second),
				InputTokens:      3,
				OutputTokens:     4,
				DurationMs:       42,
				EventVersion:     1,
				SourceFile:       "live.jsonl",
				SourceLineNo:     2,
				SourceOffset:     10,
				SourceGeneration: 3,
			},
		},
		ToolPayloads: []models.ToolPayload{
			{
				EventUID:     "evt-tool",
				ToolName:     "Bash",
				ToolPhase:    "call",
				InputJSON:    fullPayload,
				InputPreview: `{"command":"echo preview"}`,
			},
		},
	}
	for _, event := range batch.ActivityEvents {
		batch.RawRecords = append(batch.RawRecords, NewRawRecord(event))
	}

	if err := ch.Flush(context.Background(), batch); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if err := ch.Flush(context.Background(), batch); err != nil {
		t.Fatalf("replay flush: %v", err)
	}

	var eventCount, totalTokens, toolCalls uint64
	if err := ch.DB.QueryRowContext(context.Background(),
		`SELECT argMax(event_count, updated_at),
		        argMax(total_tokens, updated_at),
		        argMax(tool_call_count, updated_at)
		 FROM session_projection
		 WHERE session_id = ?`, sessionID).Scan(&eventCount, &totalTokens, &toolCalls); err != nil {
		t.Fatalf("projection query: %v", err)
	}
	if eventCount != 2 || totalTokens != 7 || toolCalls != 1 {
		t.Fatalf("projection = events %d tokens %d tools %d", eventCount, totalTokens, toolCalls)
	}

	var analyticsEvents, analyticsTokens, analyticsCalls, analyticsToolCalls, analyticsDuration uint64
	if err := ch.DB.QueryRowContext(context.Background(),
		`SELECT sum(event_count),
		        sum(total_tokens),
		        sum(call_count),
		        sum(tool_call_count),
		        sum(duration_ms_sum)
		 FROM (
			SELECT session_id, minute, provider, model, tool_name, event_kind,
			       argMax(event_count, updated_at) AS event_count,
			       argMax(total_tokens, updated_at) AS total_tokens,
			       argMax(call_count, updated_at) AS call_count,
			       argMax(tool_call_count, updated_at) AS tool_call_count,
			       argMax(duration_ms_sum, updated_at) AS duration_ms_sum
			FROM analytics_projection
			WHERE session_id = ?
			GROUP BY session_id, minute, provider, model, tool_name, event_kind
		 )`, sessionID).Scan(&analyticsEvents, &analyticsTokens, &analyticsCalls, &analyticsToolCalls, &analyticsDuration); err != nil {
		t.Fatalf("analytics projection query: %v", err)
	}
	if analyticsEvents != 2 || analyticsTokens != 7 || analyticsCalls != 1 || analyticsToolCalls != 1 || analyticsDuration != 42 {
		t.Fatalf("analytics projection = events %d tokens %d calls %d tools %d duration %d",
			analyticsEvents, analyticsTokens, analyticsCalls, analyticsToolCalls, analyticsDuration)
	}

	var runtime, format string
	var generation uint32
	if err := ch.DB.QueryRowContext(context.Background(),
		`SELECT argMax(runtime, captured_at),
		        argMax(format, captured_at),
		        argMax(source_generation, captured_at)
		 FROM activity_events
		 WHERE event_uid = ?`, "evt-tool").Scan(&runtime, &format, &generation); err != nil {
		t.Fatalf("source metadata query: %v", err)
	}
	if runtime != "test-runtime" || format != "jsonl" || generation != 3 {
		t.Fatalf("source metadata = runtime %q format %q generation %d", runtime, format, generation)
	}

	var secretDocs uint64
	if err := ch.DB.QueryRowContext(context.Background(),
		`SELECT countIf(position(searchable_text, 'SECRET_FULL_ONLY') > 0)
		 FROM search_documents FINAL
		 WHERE session_id = ?`, sessionID).Scan(&secretDocs); err != nil {
		t.Fatalf("search document query: %v", err)
	}
	if secretDocs != 1 {
		t.Fatalf("full payload indexed docs = %d, want 1", secretDocs)
	}
}

func TestClickHouseProjectionIgnoresZeroTimestampEventsForDuration(t *testing.T) {
	ch := setupLiveClickHouse(t)
	sessionID := "missing-timestamp-session"
	start := time.Date(2026, 5, 22, 12, 0, 0, 123000000, time.UTC)
	end := start.Add(2 * time.Minute)

	batch := RowBatch{
		ActivityEvents: []models.Event{
			{
				EventUID:     "evt-file-history",
				SessionID:    sessionID,
				SourceName:   "claude",
				Runtime:      "claude-code",
				Provider:     "anthropic",
				Format:       "jsonl",
				EventKind:    "event_msg",
				PayloadType:  "file-history-snapshot",
				ActorRole:    "system",
				SourceFile:   "claude.jsonl",
				SourceLineNo: 1,
			},
			{
				EventUID:     "evt-start",
				SessionID:    sessionID,
				SourceName:   "claude",
				Runtime:      "claude-code",
				Provider:     "anthropic",
				Format:       "jsonl",
				EventKind:    "message",
				ActorRole:    "user",
				Timestamp:    start,
				TextPreview:  "start",
				SourceFile:   "claude.jsonl",
				SourceLineNo: 2,
			},
			{
				EventUID:     "evt-end",
				SessionID:    sessionID,
				SourceName:   "claude",
				Runtime:      "claude-code",
				Provider:     "anthropic",
				Format:       "jsonl",
				EventKind:    "message",
				ActorRole:    "assistant",
				Timestamp:    end,
				TextPreview:  "end",
				SourceFile:   "claude.jsonl",
				SourceLineNo: 3,
			},
			{
				EventUID:     "evt-last-prompt",
				SessionID:    sessionID,
				SourceName:   "claude",
				Runtime:      "claude-code",
				Provider:     "anthropic",
				Format:       "jsonl",
				EventKind:    "session_end",
				PayloadType:  "last-prompt",
				ActorRole:    "system",
				SourceFile:   "claude.jsonl",
				SourceLineNo: 4,
			},
		},
	}
	for _, event := range batch.ActivityEvents {
		batch.RawRecords = append(batch.RawRecords, NewRawRecord(event))
	}

	if err := ch.Flush(context.Background(), batch); err != nil {
		t.Fatalf("flush: %v", err)
	}

	var projectedStart, projectedEnd time.Time
	var hasSessionEnd, eventCount uint64
	if err := ch.DB.QueryRowContext(context.Background(),
		`SELECT argMax(started_at, updated_at),
		        argMax(ended_at, updated_at),
		        argMax(has_session_end, updated_at),
		        argMax(event_count, updated_at)
		 FROM session_projection
		 WHERE session_id = ?`, sessionID).Scan(&projectedStart, &projectedEnd, &hasSessionEnd, &eventCount); err != nil {
		t.Fatalf("projection query: %v", err)
	}
	if !projectedStart.Equal(start) || !projectedEnd.Equal(end) {
		t.Fatalf("projection range = %s..%s, want %s..%s", projectedStart, projectedEnd, start, end)
	}
	if hasSessionEnd != 1 {
		t.Fatalf("has_session_end = %d, want 1", hasSessionEnd)
	}
	if eventCount != 4 {
		t.Fatalf("event_count = %d, want 4", eventCount)
	}
	if duration := projectedEnd.Sub(projectedStart); duration != 2*time.Minute {
		t.Fatalf("duration = %s, want 2m", duration)
	}

	refreshed, err := ch.RefreshAllProjections(context.Background(), 0)
	if err != nil {
		t.Fatalf("refresh all projections: %v", err)
	}
	if refreshed != 1 {
		t.Fatalf("refreshed sessions = %d, want 1", refreshed)
	}

	var rebuiltHasSessionEnd uint64
	if err := ch.DB.QueryRowContext(context.Background(),
		`SELECT argMax(has_session_end, updated_at)
		 FROM session_projection
		 WHERE session_id = ?`, sessionID).Scan(&rebuiltHasSessionEnd); err != nil {
		t.Fatalf("rebuilt projection query: %v", err)
	}
	if rebuiltHasSessionEnd != 1 {
		t.Fatalf("rebuilt has_session_end = %d, want 1", rebuiltHasSessionEnd)
	}
}

func TestClickHouseProjectionRejectsLegacyLastPromptEventMsgAsSessionEnd(t *testing.T) {
	ch := setupLiveClickHouse(t)
	sessionID := "legacy-last-prompt-session"
	start := time.Date(2026, 5, 22, 13, 0, 0, 0, time.UTC)

	batch := RowBatch{
		ActivityEvents: []models.Event{
			{
				EventUID:   "evt-message",
				SessionID:  sessionID,
				SourceName: "claude",
				Runtime:    "claude-code",
				Provider:   "anthropic",
				Format:     "jsonl",
				EventKind:  "message",
				ActorRole:  "user",
				Timestamp:  start,
				SourceFile: "claude.jsonl",
			},
			{
				EventUID:    "evt-legacy-last-prompt",
				SessionID:   sessionID,
				SourceName:  "claude",
				Runtime:     "claude-code",
				Provider:    "anthropic",
				Format:      "jsonl",
				EventKind:   "event_msg",
				PayloadType: "last-prompt",
				ActorRole:   "system",
				Timestamp:   start.Add(time.Minute),
				SourceFile:  "claude.jsonl",
			},
		},
	}
	for _, event := range batch.ActivityEvents {
		batch.RawRecords = append(batch.RawRecords, NewRawRecord(event))
	}

	if err := ch.Flush(context.Background(), batch); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if _, err := ch.RefreshAllProjections(context.Background(), 0); err != nil {
		t.Fatalf("refresh all projections: %v", err)
	}

	var hasSessionEnd uint64
	if err := ch.DB.QueryRowContext(context.Background(),
		`SELECT argMax(has_session_end, updated_at)
		 FROM session_projection
		 WHERE session_id = ?`, sessionID).Scan(&hasSessionEnd); err != nil {
		t.Fatalf("projection query: %v", err)
	}
	if hasSessionEnd != 0 {
		t.Fatalf("legacy event_msg last-prompt has_session_end = %d, want 0", hasSessionEnd)
	}
}

func TestClickHouseFlushProjectsSessionStatesSubagentsErrorsAndSearchRows(t *testing.T) {
	ch := setupLiveClickHouse(t)
	base := time.Date(2026, 5, 22, 14, 0, 0, 0, time.UTC)
	parentID := "projection-parent"
	childID := "projection-child"
	activeID := "projection-active"
	batch := RowBatch{
		ActivityEvents: []models.Event{
			{
				EventUID:          "evt-parent-user",
				SessionID:         parentID,
				SourceName:        "claude",
				Provider:          "anthropic",
				EventKind:         "message",
				ActorRole:         "user",
				Timestamp:         base,
				TextContent:       "parent asks for integrationneedle search coverage",
				TextPreview:       "parent asks for integrationneedle search coverage",
				InputTokens:       10,
				OutputTokens:      2,
				CacheReadTokens:   1,
				CacheCreateTokens: 2,
				Model:             "claude-sonnet-4",
				CWD:               "/tmp/beacon-parent",
				SourceFile:        "parent.jsonl",
				SourceLineNo:      1,
				SourceOffset:      10,
				EventVersion:      1,
				CreatedAt:         base,
			},
			{
				EventUID:          "evt-parent-bash-call",
				SessionID:         parentID,
				SourceName:        "claude",
				Provider:          "anthropic",
				EventKind:         "tool_call",
				ToolName:          "Bash",
				Timestamp:         base.Add(time.Minute),
				InputTokens:       3,
				OutputTokens:      4,
				CacheReadTokens:   5,
				CacheCreateTokens: 6,
				DurationMs:        250,
				Model:             "claude-sonnet-4",
				SourceFile:        "parent.jsonl",
				SourceLineNo:      2,
				SourceOffset:      20,
				EventVersion:      1,
				CreatedAt:         base.Add(time.Minute),
			},
			{
				EventUID:          "evt-parent-mcp-call",
				SessionID:         parentID,
				SourceName:        "claude",
				Provider:          "anthropic",
				EventKind:         "tool_call",
				ToolName:          "mcp__repo__search",
				Timestamp:         base.Add(2 * time.Minute),
				InputTokens:       7,
				OutputTokens:      8,
				CacheReadTokens:   9,
				CacheCreateTokens: 10,
				DurationMs:        300,
				Model:             "claude-sonnet-4",
				SourceFile:        "parent.jsonl",
				SourceLineNo:      3,
				SourceOffset:      30,
				EventVersion:      1,
				CreatedAt:         base.Add(2 * time.Minute),
			},
			{
				EventUID:     "evt-parent-result",
				SessionID:    parentID,
				SourceName:   "claude",
				Provider:     "anthropic",
				EventKind:    "tool_result",
				ToolName:     "Bash",
				Timestamp:    base.Add(3 * time.Minute),
				DurationMs:   50,
				Model:        "claude-sonnet-4",
				SourceFile:   "parent.jsonl",
				SourceLineNo: 4,
				SourceOffset: 40,
				EventVersion: 1,
				CreatedAt:    base.Add(3 * time.Minute),
			},
			{
				EventUID:     "evt-parent-error",
				SessionID:    parentID,
				SourceName:   "claude",
				Provider:     "anthropic",
				EventKind:    "error",
				Timestamp:    base.Add(4 * time.Minute),
				ErrorCode:    "exit_1",
				ErrorMessage: "integrationneedle parser failed",
				SourceFile:   "parent.jsonl",
				SourceLineNo: 5,
				SourceOffset: 50,
				CreatedAt:    base.Add(4 * time.Minute),
			},
			{
				EventUID:     "evt-parent-tool-error",
				SessionID:    parentID,
				SourceName:   "claude",
				Provider:     "anthropic",
				EventKind:    "tool_error",
				ToolName:     "Bash",
				Timestamp:    base.Add(5 * time.Minute),
				ErrorCode:    "exit_2",
				ErrorMessage: "integrationneedle command failed",
				SourceFile:   "parent.jsonl",
				SourceLineNo: 6,
				SourceOffset: 60,
				CreatedAt:    base.Add(5 * time.Minute),
			},
			{
				EventUID:     "evt-parent-end",
				SessionID:    parentID,
				SourceName:   "claude",
				Provider:     "anthropic",
				EventKind:    "session_end",
				ActorRole:    "system",
				Timestamp:    base.Add(6 * time.Minute),
				SourceFile:   "parent.jsonl",
				SourceLineNo: 7,
				SourceOffset: 70,
				CreatedAt:    base.Add(6 * time.Minute),
			},
			{
				EventUID:        "evt-child-user",
				SessionID:       childID,
				ParentSessionID: parentID,
				SourceName:      "codex",
				Provider:        "openai",
				EventKind:       "message",
				ActorRole:       "user",
				Timestamp:       base.Add(7 * time.Minute),
				TextPreview:     "child subagent work",
				InputTokens:     5,
				OutputTokens:    7,
				Model:           "gpt-5.4-codex",
				CWD:             "/tmp/beacon-child",
				SourceFile:      "child.jsonl",
				SourceLineNo:    1,
				SourceOffset:    80,
				CreatedAt:       base.Add(7 * time.Minute),
			},
			{
				EventUID:        "evt-child-end",
				SessionID:       childID,
				ParentSessionID: parentID,
				SourceName:      "codex",
				Provider:        "openai",
				EventKind:       "session_end",
				ActorRole:       "system",
				Timestamp:       base.Add(8 * time.Minute),
				SourceFile:      "child.jsonl",
				SourceLineNo:    2,
				SourceOffset:    90,
				CreatedAt:       base.Add(8 * time.Minute),
			},
			{
				EventUID:     "evt-active-user",
				SessionID:    activeID,
				SourceName:   "codex",
				Provider:     "openai",
				EventKind:    "message",
				ActorRole:    "user",
				Timestamp:    base.Add(9 * time.Minute),
				TextPreview:  "active session still running",
				InputTokens:  11,
				OutputTokens: 13,
				Model:        "gpt-5.4-codex",
				CWD:          "/tmp/beacon-active",
				SourceFile:   "active.jsonl",
				SourceLineNo: 1,
				SourceOffset: 100,
				CreatedAt:    base.Add(9 * time.Minute),
			},
		},
		ToolPayloads: []models.ToolPayload{
			{
				EventUID:     "evt-parent-bash-call",
				ToolName:     "Bash",
				ToolPhase:    "call",
				InputPreview: `{"command":"echo ordinary tool"}`,
			},
			{
				EventUID:     "evt-parent-mcp-call",
				ToolName:     "mcp__repo__search",
				ToolPhase:    "call",
				InputJSON:    `{"query":"full payload should not be needed"}`,
				InputPreview: `{"query":"integrationneedle integrationneedle repo search"}`,
			},
			{
				EventUID:      "evt-parent-result",
				ToolName:      "Bash",
				ToolPhase:     "result",
				OutputPreview: "ordinary tool completed",
			},
			{
				EventUID:      "evt-parent-tool-error",
				ToolName:      "Bash",
				ToolPhase:     "result",
				OutputPreview: "integrationneedle command failed",
			},
		},
	}
	for _, event := range batch.ActivityEvents {
		batch.RawRecords = append(batch.RawRecords, NewRawRecord(event))
	}

	if err := ch.Flush(context.Background(), batch); err != nil {
		t.Fatalf("flush: %v", err)
	}

	var parentEvents, parentTurns, parentInput, parentOutput, parentCacheRead, parentCacheCreate, parentTokens uint64
	var parentTools, parentMCP, parentErrors, parentEnded uint64
	var parentModel, parentDir string
	if err := ch.DB.QueryRowContext(context.Background(),
		`SELECT argMax(event_count, updated_at),
		        argMax(turn_count, updated_at),
		        argMax(total_input_tokens, updated_at),
		        argMax(total_output_tokens, updated_at),
		        argMax(total_cache_read_tokens, updated_at),
		        argMax(total_cache_create_tokens, updated_at),
		        argMax(total_tokens, updated_at),
		        argMax(tool_call_count, updated_at),
		        argMax(mcp_call_count, updated_at),
		        argMax(error_count, updated_at),
		        argMax(has_session_end, updated_at),
		        argMax(last_model, updated_at),
		        argMax(working_dir, updated_at)
		 FROM session_projection
		 WHERE session_id = ?`, parentID).Scan(
		&parentEvents,
		&parentTurns,
		&parentInput,
		&parentOutput,
		&parentCacheRead,
		&parentCacheCreate,
		&parentTokens,
		&parentTools,
		&parentMCP,
		&parentErrors,
		&parentEnded,
		&parentModel,
		&parentDir,
	); err != nil {
		t.Fatalf("parent projection query: %v", err)
	}
	if parentEvents != 7 || parentTurns != 1 || parentInput != 20 || parentOutput != 14 ||
		parentCacheRead != 15 || parentCacheCreate != 18 || parentTokens != 34 ||
		parentTools != 2 || parentMCP != 1 || parentErrors != 2 || parentEnded != 1 {
		t.Fatalf("parent projection = events %d turns %d input %d output %d cache %d/%d tokens %d tools %d mcp %d errors %d ended %d",
			parentEvents, parentTurns, parentInput, parentOutput, parentCacheRead, parentCacheCreate,
			parentTokens, parentTools, parentMCP, parentErrors, parentEnded)
	}
	if parentModel != "claude-sonnet-4" || parentDir != "/tmp/beacon-parent" {
		t.Fatalf("parent model/dir = %q/%q", parentModel, parentDir)
	}

	var childParent string
	var childEnded uint64
	if err := ch.DB.QueryRowContext(context.Background(),
		`SELECT argMax(parent_session_id, updated_at), argMax(has_session_end, updated_at)
		 FROM session_projection
		 WHERE session_id = ?`, childID).Scan(&childParent, &childEnded); err != nil {
		t.Fatalf("child projection query: %v", err)
	}
	if childParent != parentID || childEnded != 1 {
		t.Fatalf("child parent/ended = %q/%d, want %q/1", childParent, childEnded, parentID)
	}

	var activeEnded, activeTokens uint64
	if err := ch.DB.QueryRowContext(context.Background(),
		`SELECT argMax(has_session_end, updated_at), argMax(total_tokens, updated_at)
		 FROM session_projection
		 WHERE session_id = ?`, activeID).Scan(&activeEnded, &activeTokens); err != nil {
		t.Fatalf("active projection query: %v", err)
	}
	if activeEnded != 0 || activeTokens != 24 {
		t.Fatalf("active ended/tokens = %d/%d, want 0/24", activeEnded, activeTokens)
	}

	var parentRawRecords uint64
	if err := ch.DB.QueryRowContext(context.Background(),
		`SELECT count() FROM raw_records WHERE session_id = ?`, parentID).Scan(&parentRawRecords); err != nil {
		t.Fatalf("raw records query: %v", err)
	}
	if parentRawRecords != 7 {
		t.Fatalf("parent raw records = %d, want 7", parentRawRecords)
	}

	var analyticsEvents, analyticsCalls, analyticsToolCalls, analyticsToolResults uint64
	var analyticsInput, analyticsOutput, analyticsCacheRead, analyticsCacheCreate, analyticsTokens, analyticsDuration uint64
	if err := ch.DB.QueryRowContext(context.Background(),
		`SELECT sum(event_count),
		        sum(call_count),
		        sum(tool_call_count),
		        sum(tool_result_count),
		        sum(input_tokens),
		        sum(output_tokens),
		        sum(cache_read_tokens),
		        sum(cache_create_tokens),
		        sum(total_tokens),
		        sum(duration_ms_sum)
		 FROM (
			SELECT session_id, minute, provider, model, tool_name, event_kind,
			       argMax(event_count, updated_at) AS event_count,
			       argMax(call_count, updated_at) AS call_count,
			       argMax(tool_call_count, updated_at) AS tool_call_count,
			       argMax(tool_result_count, updated_at) AS tool_result_count,
			       argMax(input_tokens, updated_at) AS input_tokens,
			       argMax(output_tokens, updated_at) AS output_tokens,
			       argMax(cache_read_tokens, updated_at) AS cache_read_tokens,
			       argMax(cache_create_tokens, updated_at) AS cache_create_tokens,
			       argMax(total_tokens, updated_at) AS total_tokens,
			       argMax(duration_ms_sum, updated_at) AS duration_ms_sum
			FROM analytics_projection
			WHERE session_id = ?
			GROUP BY session_id, minute, provider, model, tool_name, event_kind
		 )`, parentID).Scan(
		&analyticsEvents,
		&analyticsCalls,
		&analyticsToolCalls,
		&analyticsToolResults,
		&analyticsInput,
		&analyticsOutput,
		&analyticsCacheRead,
		&analyticsCacheCreate,
		&analyticsTokens,
		&analyticsDuration,
	); err != nil {
		t.Fatalf("analytics projection query: %v", err)
	}
	if analyticsEvents != 7 || analyticsCalls != 3 || analyticsToolCalls != 2 || analyticsToolResults != 1 ||
		analyticsInput != 20 || analyticsOutput != 14 || analyticsCacheRead != 15 || analyticsCacheCreate != 18 ||
		analyticsTokens != 34 || analyticsDuration != 600 {
		t.Fatalf("analytics projection = events %d calls %d tool calls/results %d/%d input %d output %d cache %d/%d tokens %d duration %d",
			analyticsEvents, analyticsCalls, analyticsToolCalls, analyticsToolResults, analyticsInput, analyticsOutput,
			analyticsCacheRead, analyticsCacheCreate, analyticsTokens, analyticsDuration)
	}

	var searchDocs uint64
	var documentLen uint32
	var documentTool, documentModel, documentProvider string
	if err := ch.DB.QueryRowContext(context.Background(),
		`SELECT count(),
		        argMax(document_len, updated_at),
		        argMax(tool_name, updated_at),
		        argMax(model, updated_at),
		        argMax(provider, updated_at)
		 FROM search_documents
		 WHERE event_uid = ? AND position(searchable_text, 'integrationneedle') > 0`, "evt-parent-mcp-call").Scan(
		&searchDocs,
		&documentLen,
		&documentTool,
		&documentModel,
		&documentProvider,
	); err != nil {
		t.Fatalf("search docs query: %v", err)
	}
	if searchDocs != 1 || documentLen == 0 || documentTool != "mcp__repo__search" ||
		documentModel != "claude-sonnet-4" || documentProvider != "anthropic" {
		t.Fatalf("search document = count %d len %d tool/model/provider %q/%q/%q",
			searchDocs, documentLen, documentTool, documentModel, documentProvider)
	}

	var searchPostings uint64
	var termFrequency uint32
	var postingEventUID, postingSessionID, postingEventKind, postingTool, postingModel, postingProvider string
	if err := ch.DB.QueryRowContext(context.Background(),
		`SELECT count(),
		        argMax(term_frequency, updated_at),
		        argMax(event_uid, updated_at),
		        argMax(session_id, updated_at),
		        argMax(event_kind, updated_at),
		        argMax(tool_name, updated_at),
		        argMax(model, updated_at),
		        argMax(provider, updated_at)
		 FROM search_postings
		 WHERE token = ? AND event_uid = ?`, "integrationneedle", "evt-parent-mcp-call").Scan(
		&searchPostings,
		&termFrequency,
		&postingEventUID,
		&postingSessionID,
		&postingEventKind,
		&postingTool,
		&postingModel,
		&postingProvider,
	); err != nil {
		t.Fatalf("search postings query: %v", err)
	}
	if searchPostings != 1 || termFrequency != 2 || postingEventUID != "evt-parent-mcp-call" ||
		postingSessionID != parentID || postingEventKind != "tool_call" || postingTool != "mcp__repo__search" ||
		postingModel != "claude-sonnet-4" || postingProvider != "anthropic" {
		t.Fatalf("search posting = count %d frequency %d event/session/kind/tool/model/provider %q/%q/%q/%q/%q/%q",
			searchPostings, termFrequency, postingEventUID, postingSessionID, postingEventKind,
			postingTool, postingModel, postingProvider)
	}
}
