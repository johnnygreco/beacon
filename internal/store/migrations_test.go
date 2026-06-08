package store

import (
	"errors"
	"strings"
	"testing"

	"github.com/johnnygreco/beacon/internal/models"
)

func TestSchemaContainsBeaconTables(t *testing.T) {
	schema := strings.Join(Schema("beacon"), "\n")
	for _, table := range tableNames {
		if !strings.Contains(schema, "beacon."+table) {
			t.Fatalf("schema missing table %s", table)
		}
	}
}

func TestTableNamesIncludesDataTables(t *testing.T) {
	if got, want := len(tableNames), len(dataTableNames)+1; got != want {
		t.Fatalf("tableNames length = %d, want %d", got, want)
	}
	present := make(map[string]bool, len(tableNames))
	for _, table := range tableNames {
		present[table] = true
	}
	if !present[schemaVersionTable] {
		t.Fatalf("tableNames missing %s", schemaVersionTable)
	}
	for _, table := range dataTableNames {
		if !present[table] {
			t.Fatalf("tableNames missing data table %s", table)
		}
	}
}

func TestSchemaUsesClickHouseEngines(t *testing.T) {
	schema := strings.Join(Schema("beacon"), "\n")
	for _, expected := range []string{
		"ReplacingMergeTree",
		"MergeTree",
		"LowCardinality(String)",
		"CODEC(ZSTD(3))",
		"tokenbf_v1",
	} {
		if !strings.Contains(schema, expected) {
			t.Fatalf("schema missing %s", expected)
		}
	}
}

func TestSchemaIncludesSourceMetadataColumns(t *testing.T) {
	schema := strings.Join(Schema("beacon"), "\n")
	for _, expected := range []string{
		"runtime LowCardinality(String)",
		"format LowCardinality(String)",
		"source_generation UInt32",
		"collector_id String",
		"source_id String",
		"raw_event_id String",
		"source_event_index UInt64",
		"batch_id String",
		"control_plane_epoch String",
		"payload_digest String",
		"redaction_status LowCardinality(String)",
		"state_json String DEFAULT ''",
		"beacon.ingest_batches",
		"beacon.capture_heartbeats",
		"spool_bytes UInt64",
		"ORDER BY (collector_id, source_id, created_at)",
		"state_version UInt64",
		"status LowCardinality(String)",
		"committed_at Nullable(DateTime64(3, 'UTC'))",
		"ORDER BY (collector_id, source_id, source_name, source_file)",
		"ORDER BY (collector_id, batch_id, id)",
	} {
		if !strings.Contains(schema, expected) {
			t.Fatalf("schema missing %s", expected)
		}
	}
}

func TestSchemaIncludesVersionTableWithoutCompatibilityAlters(t *testing.T) {
	schema := strings.Join(Schema("beacon"), "\n")
	for _, expected := range []string{
		"beacon.schema_version",
		"version UInt32",
		"ReplacingMergeTree(updated_at)",
		"ORDER BY id",
	} {
		if !strings.Contains(schema, expected) {
			t.Fatalf("schema missing %s", expected)
		}
	}
	if strings.Contains(schema, "ALTER TABLE") {
		t.Fatalf("schema should not include compatibility ALTER statements")
	}
}

func TestSchemaIncludesAnalyticsProjection(t *testing.T) {
	schema := strings.Join(Schema("beacon"), "\n")
	for _, expected := range []string{
		"beacon.analytics_projection",
		"minute DateTime64(3, 'UTC')",
		"collector_id String",
		"project_key String",
		"call_count UInt64",
		"duration_ms_sum UInt64",
		"cost_usd_sum Float64",
		"ORDER BY (collector_id, source_id, project_key, session_id, minute, provider, model, tool_name, event_kind)",
	} {
		if !strings.Contains(schema, expected) {
			t.Fatalf("schema missing %s", expected)
		}
	}
}

func TestSchemaIncludesFleetAwareLinkColumns(t *testing.T) {
	schema := strings.Join(Schema("beacon"), "\n")
	for _, expected := range []string{
		"linked_session_id String",
		"raw_linked_session_id String",
		"raw_linked_event_id String",
		"link_scope LowCardinality(String)",
		"resolution_status LowCardinality(String)",
		"ORDER BY (collector_id, source_id, event_uid, link_type, raw_linked_session_id, raw_linked_event_id)",
		"ORDER BY (session_id, collector_id, source_id, timestamp, event_uid)",
		"ORDER BY (event_uid, collector_id, source_id)",
	} {
		if !strings.Contains(schema, expected) {
			t.Fatalf("schema missing %s", expected)
		}
	}
}

func TestValidateSchemaStateRejectsUnsupportedSchemas(t *testing.T) {
	for _, tt := range []struct {
		name  string
		state schemaState
		want  string
	}{
		{
			name:  "legacy tables",
			state: schemaState{hasOwnedTables: true},
			want:  "legacy Beacon tables are missing schema_version",
		},
		{
			name:  "wrong version",
			state: schemaState{hasVersionTable: true, hasVersionRow: true, version: CurrentSchemaVersion + 1},
			want:  "found version",
		},
		{
			name:  "empty version table",
			state: schemaState{hasVersionTable: true},
			want:  "schema_version table has no version row",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSchemaState(tt.state, "beacon", true)
			if err == nil {
				t.Fatalf("validateSchemaState returned nil")
			}
			var unsupported *UnsupportedSchemaError
			if !errors.As(err, &unsupported) {
				t.Fatalf("error = %T, want UnsupportedSchemaError", err)
			}
			if !strings.Contains(err.Error(), tt.want) || !strings.Contains(err.Error(), "beacon db reset --force") {
				t.Fatalf("error = %q, want %q and reset instruction", err.Error(), tt.want)
			}
		})
	}
}

func TestNewRawRecordPreservesSourceMetadata(t *testing.T) {
	event := models.Event{
		EventUID:          "evt",
		NodeID:            "node-a",
		CollectorID:       "collector-a",
		SourceID:          "source-a",
		SourceName:        "custom-source",
		Runtime:           "custom-runtime",
		Provider:          "custom-provider",
		Format:            "jsonl",
		SourceFile:        "session.jsonl",
		SourceLineNo:      12,
		SourceOffset:      34,
		SourceGeneration:  2,
		SessionID:         "sess",
		RawSessionID:      "raw-sess",
		RawEventID:        "raw-event",
		SourceEventIndex:  42,
		BatchID:           "batch-a",
		ControlPlaneEpoch: "1",
		PayloadDigest:     "digest-a",
		RedactionStatus:   "unredacted",
		PayloadJSON:       `{"ok":true}`,
	}

	raw := NewRawRecord(event)
	if raw.Runtime != event.Runtime || raw.Format != event.Format || raw.SourceGeneration != event.SourceGeneration {
		t.Fatalf("raw source metadata = runtime %q format %q generation %d", raw.Runtime, raw.Format, raw.SourceGeneration)
	}
	if raw.EventUID != event.EventUID || raw.CollectorID != event.CollectorID || raw.SourceID != event.SourceID ||
		raw.RawSessionID != event.RawSessionID || raw.RawEventID != event.RawEventID ||
		raw.SourceEventIndex != event.SourceEventIndex || raw.BatchID != event.BatchID ||
		raw.PayloadDigest != event.PayloadDigest || raw.RedactionStatus != event.RedactionStatus {
		t.Fatalf("raw fleet identity not preserved: %#v", raw)
	}
}

func TestCleanIdentRejectsUnsafeDatabaseName(t *testing.T) {
	if got := cleanIdent("beacon; DROP DATABASE default"); got != DefaultDatabase {
		t.Fatalf("cleanIdent unsafe = %q, want %q", got, DefaultDatabase)
	}
	if got := cleanIdent("beacon_test"); got != "beacon_test" {
		t.Fatalf("cleanIdent valid = %q", got)
	}
}
