# ClickHouse schema and reset policy

Beacon stores normalized agent activity in ClickHouse. The schema is defined in
[`internal/store/migrations.go`](../internal/store/migrations.go), and all
application writes go through `internal/store`.

The default database is `beacon`. A different database name can be configured in
`[database].database`; invalid names are sanitized back to `beacon`.

## Migration behavior

`store.Open` automatically migrates the configured ClickHouse database every time
Beacon opens the write store. It first connects to the `default` ClickHouse
database, runs `store.Migrate`, then opens the configured Beacon database for
normal reads and writes.

Commands that use the write store, including `beacon up`, `beacon watch`,
`beacon db up`, and `beacon db migrate`, therefore create the database and latest
tables before ingest starts. `store.OpenReadOnly` does not run migrations.

Migrations are intentionally simple: `CREATE DATABASE IF NOT EXISTS`, `CREATE
TABLE IF NOT EXISTS`, and targeted `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`
statements when a small additive change is useful.

## Schema ownership

Beacon owns every table in the configured database listed below. External tools
may read them, but should not write or mutate them directly.

### `raw_records`

Purpose: deduplicated raw source records, including source file, offset,
generation, session id, and compressed original payload JSON. This is the audit
trail for parser input.

Owner: capture parsers and `Store.Flush`.

### `activity_events`

Purpose: canonical normalized event stream used by the dashboard, transcript
views, MCP tools, analytics, and search indexing. It includes event metadata,
text, tool fields, token counts, cost, errors, source position, and payload JSON.

Owner: capture normalizer and `Store.Flush`.

### `event_links`

Purpose: relationships between normalized events, such as tool-call to
tool-result links or other parser-derived references.

Owner: capture normalizer and `Store.Flush`.

### `tool_payloads`

Purpose: full tool input/output JSON and previews keyed by `event_uid`. This
keeps large tool payloads out of the primary event row while preserving
transcript detail.

Owner: capture normalizer and `Store.Flush`.

### `capture_errors`

Purpose: parser and capture failures with source position and context fragment.
Used by status and diagnostics.

Owner: capture pipeline through `Store.InsertCaptureError` or `Store.Flush`.

### `capture_checkpoints`

Purpose: per-source file checkpoint state: inode, generation, last processed
offset and line, plus source-specific `state_json`. Used to resume capture
without replaying already-ingested data.

Owner: capture watcher/checkpoint code through `Store.UpsertCheckpoint` and
`Store.Flush`.

### `capture_heartbeats`

Purpose: runtime capture health samples: queue depth, active file count, and
append-to-visible latency.

Owner: reserved for the capture service.

### `session_projection`

Purpose: application-maintained session summary: start/end time, event and turn
counts, token totals, tool and error counts, latest model, working directory,
parent session, and completion marker.

Owner: `Store.RefreshSessionProjections`, called after event ingest.

### `analytics_projection`

Purpose: application-maintained minute-level rollup by session, provider, model,
tool, and event kind. Used by dashboard token and activity charts.

Owner: `Store.RefreshAnalyticsProjections`, called after event ingest.

### `search_documents`

Purpose: one searchable document per indexed event, including searchable text,
preview, event metadata, and document length.

Owner: ingest-time search row builder in `Store.Flush`.

### `search_postings`

Purpose: token postings for Beacon's built-in BM25-style search: token, event
id, term frequency, document length, preview, and metadata.

Owner: ingest-time search row builder in `Store.Flush`.

### `search_query_log`

Purpose: best-effort log of search queries, normalized terms, result counts, and
duration.

Owner: `internal/search.Searcher`.

Most primary tables use `ReplacingMergeTree` with a timestamp column so Beacon can
re-ingest or refresh rows for the same logical key and have `FINAL`/`argMax`
queries select the latest version. Tables that are append-only diagnostics or
logs use plain `MergeTree`.

## Projection tables

`session_projection` and `analytics_projection` are not ClickHouse materialized
views. They are Beacon-owned tables that the application refreshes after ingest.

On each flush with activity events, Beacon:

1. inserts the raw normalized rows;
2. builds search documents and postings for those events;
3. refreshes `session_projection` for the affected session ids;
4. refreshes `analytics_projection` for the affected session ids.

Because the projection tables are derived from `activity_events`, they can be
dropped and rebuilt from captured source files by resetting the database and
letting Beacon backfill. They should not be edited by hand.

## Reset and destructive behavior

`beacon db reset --force` is destructive. It drops all Beacon-owned tables in the
configured database and then recreates the latest schema. Without `--force`, the
command prompts before dropping data.

Reset removes:

- captured raw records and normalized events;
- tool payloads and event links;
- capture errors, checkpoints, and heartbeats;
- session and analytics projection rows;
- search documents, postings, and query logs.

Reset does not delete the ClickHouse server's files, Docker volume, binaries, or
configuration. It only drops and recreates Beacon tables through ClickHouse.

For a full local ClickHouse data wipe, stop the managed server first and remove
the backing storage:

```bash
beacon db down
rm -rf ~/.beacon/clickhouse/{data,logs,access,clickhouse.pid}
```

For Docker mode, remove the Beacon Docker volume only when you intend to delete
all data stored by that managed container:

```bash
beacon db down
docker volume rm beacon-clickhouse-data
```

If ClickHouse is shared with other data, use `beacon db reset --force` rather
than deleting server storage.

## Local data locations

Beacon can use a native ClickHouse binary or a Docker container for local
development.

Native mode uses:

- binary lookup: `BEACON_CLICKHOUSE_BIN`, then `PATH`, then
  `~/.beacon/bin/clickhouse`;
- base directory: `~/.beacon/clickhouse`;
- data: `~/.beacon/clickhouse/data`;
- logs: `~/.beacon/clickhouse/logs`;
- access metadata: `~/.beacon/clickhouse/access`;
- pid file: `~/.beacon/clickhouse/clickhouse.pid`.

Docker mode uses:

- container name: `beacon-clickhouse`;
- image default: `clickhouse/clickhouse-server:24.12`;
- TCP port: `9000`;
- HTTP port: `8123`;
- data volume: `beacon-clickhouse-data` mounted at `/var/lib/clickhouse`.

`beacon db down` stops Beacon-managed native ClickHouse processes and the
`beacon-clickhouse` Docker container, but it leaves the data directories and
Docker volume in place.

## Schema-change policy

Beacon is a local-first application and currently does not promise backwards
compatibility for old local ClickHouse schemas.

The supported schema is the latest schema in `internal/store/migrations.go`.
During active development, schema changes may drop, rename, or recreate tables
instead of preserving every historical local schema. Additive migrations are fine
when they are simple, but compatibility migrations for older development schemas
are not required.

When a local database cannot be migrated cleanly, the intended recovery path is:

1. stop Beacon;
2. run `beacon db reset --force`, or wipe the managed local ClickHouse storage if
   the server metadata itself is inconsistent;
3. start Beacon again and let backfill rebuild the derived Beacon data from the
   configured capture sources.

If a future release needs durable upgrade guarantees, this policy should be
revisited before shipping that release.
