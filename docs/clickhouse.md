# ClickHouse Schema And Reset Policy

Beacon stores normalized local agent activity in ClickHouse. The schema is
defined in [`internal/store/migrations.go`](../internal/store/migrations.go),
and application writes go through `internal/store`.

The default database is `beacon`. A different database name can be configured in
`[database].database`; invalid names are rejected during config validation before
Beacon connects to ClickHouse.

## Migration Behavior

`store.Open` checks the configured ClickHouse database every time Beacon opens
the write store. It first connects to the `default` database, runs
`store.Migrate`, then opens the configured Beacon database for normal reads and
writes.

The current schema version is recorded in `schema_version`. Fresh empty
databases are initialized to the current version. Databases with Beacon tables
but no version marker, an empty marker, or an unsupported version fail with reset
guidance before ingest starts.

Beacon does not carry compatibility shims for old identity layouts. Reset and
reimport when moving between incompatible schema versions.

## Table Ownership

Beacon owns every table in the configured database listed below. External tools
may read them, but should not write or mutate them directly.

### `schema_version`

One-row marker for the ClickHouse schema version supported by this Beacon build.
Owned by `store.Migrate` and `store.Reset`.

### `raw_records`

Deduplicated raw source records, including source name, source file, offset,
generation, raw/global session and event IDs, source event index, payload digest,
redaction placeholders, and compressed original payload JSON.

### `activity_events`

Canonical normalized event stream used by the dashboard, transcript views, MCP
tools, analytics, and search indexing. It includes global event/session identity,
raw source-native IDs, source name, runtime, format, event metadata, text, tool
fields, token counts, cost, errors, working directory, source position,
redaction placeholders, and payload JSON.

### `event_links`

Relationships between normalized events, such as parent-event, tool-call to
tool-result, cross-session, or other parser-derived references. Rows preserve
raw linked event/session IDs, global linked IDs when resolvable, link scope, and
resolution status.

### `tool_payloads`

Full tool input/output JSON and previews keyed by `event_uid`. This keeps large
tool payloads out of the primary event row while preserving transcript detail.

### `capture_errors`

Parser and capture failures with source name, source file/position, error
message, and context fragment.

### `capture_checkpoints`

Per-source file checkpoint state keyed by source name and file key: inode,
generation, last processed offset and line, plus source-specific `state_json`.

### `session_projection`

Application-maintained session summary: source name, runtime, provider, format,
project key/path, start/end time, event and turn counts, token totals, cost,
attention/archive metadata, tool and error counts, latest model, working
directory, parent session, and completion marker.

### `analytics_projection`

Application-maintained minute-level rollup by source name, runtime, format,
project key/path, session, provider, model, tool, and event kind. Used by
dashboard token, model, and activity charts.

### `search_documents`

One searchable document per indexed event, including source name, runtime,
project key/path, searchable text, preview, event metadata, and document length.

### `search_postings`

Token postings for Beacon's built-in BM25-style search: token, event id, source
name, runtime, project key/path, term frequency, document length, preview, and
metadata.

### `search_query_log`

Best-effort log of search queries, normalized terms, result counts, and
duration.

Most primary tables use `ReplacingMergeTree` with a timestamp column so Beacon
can re-ingest or refresh rows for the same logical key and have `FINAL`/`argMax`
queries select the latest version. Append-only logs use plain `MergeTree`.

## Projection Tables

`session_projection` and `analytics_projection` are not ClickHouse materialized
views. They are Beacon-owned tables that the application refreshes after ingest.

On each flush with activity events, Beacon:

1. inserts the raw normalized rows;
2. refreshes `session_projection` for affected session IDs;
3. refreshes `analytics_projection` for affected session IDs with a new refresh
   ID, so reads can ignore older rollup generations without synchronous
   ClickHouse delete mutations;
4. refreshes search documents and postings for changed events from the latest
   deduplicated event stream.

Because projection tables are derived from `activity_events`, they can be
dropped and rebuilt from captured source files by resetting the database and
letting Beacon backfill. To rebuild derived rows without dropping raw capture
tables, run `beacon db refresh-projections` or restart `beacon up` and let the
startup repair pass refresh stale rows.

## Store Integration Tests

Most `internal/store` coverage runs without ClickHouse. The live ClickHouse
regression tests are opt-in and reset the `beacon_test_store` database, so point
them only at a disposable local server:

```bash
beacon db up
BEACON_TEST_CLICKHOUSE=127.0.0.1:9000 go test ./internal/store
```

## Reset And Destructive Behavior

`beacon db reset --force` is destructive. It drops all Beacon-owned tables in the
configured database and then recreates the latest schema. Without `--force`, the
command prompts before dropping data.
