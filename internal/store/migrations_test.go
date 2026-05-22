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
		"state_json String DEFAULT ''",
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
		"call_count UInt64",
		"duration_ms_sum UInt64",
		"ORDER BY (session_id, minute, provider, model, tool_name, event_kind)",
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
