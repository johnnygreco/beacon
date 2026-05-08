package store

import (
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
	} {
		if !strings.Contains(schema, expected) {
			t.Fatalf("schema missing %s", expected)
		}
	}
}

func TestSchemaIncludesAnalyticsProjection(t *testing.T) {
	schema := strings.Join(Schema("beacon"), "\n")
	for _, expected := range []string{
		"beacon.analytics_projection",
		"minute DateTime64(3, 'UTC')",
		"call_count UInt64",
		"duration_ms_sum UInt64",
		"ORDER BY (session_id, minute, provider, model, tool_name, event_kind)",
	} {
		if !strings.Contains(schema, expected) {
			t.Fatalf("schema missing %s", expected)
		}
	}
}

func TestNewRawRecordPreservesSourceMetadata(t *testing.T) {
	event := models.Event{
		EventUID:         "evt",
		SourceName:       "custom-source",
		Runtime:          "custom-runtime",
		Provider:         "custom-provider",
		Format:           "jsonl",
		SourceFile:       "session.jsonl",
		SourceLineNo:     12,
		SourceOffset:     34,
		SourceGeneration: 2,
		SessionID:        "sess",
		PayloadJSON:      `{"ok":true}`,
	}

	raw := NewRawRecord(event)
	if raw.Runtime != event.Runtime || raw.Format != event.Format || raw.SourceGeneration != event.SourceGeneration {
		t.Fatalf("raw source metadata = runtime %q format %q generation %d", raw.Runtime, raw.Format, raw.SourceGeneration)
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
