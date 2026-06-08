# Architecture and data flow

Beacon is a local capture, storage, dashboard, search, and MCP service for AI
coding-agent sessions. The runtime shape is intentionally simple:

1. `cmd/beacon` loads configuration, opens ClickHouse, migrates the schema, and
   wires the capture, search, SSE, web, and MCP services.
2. `internal/capture` watches configured session sources and normalizes each
   source format into `NormalizedEvent` values.
3. The capture batcher converts normalized events into storage rows and flushes
   them through `internal/store`.
4. `internal/store` inserts raw records, canonical activity events, tool
   payloads, search rows, and refreshed projections into ClickHouse.
5. `internal/web`, `internal/search`, and `internal/mcp` read from those tables
   and projections to serve the dashboard, JSON APIs, search, and MCP tools.
6. `internal/web.Updater` and `internal/sse` notify connected browsers after
   flushes so dashboards and session transcripts can refresh without polling.

## Event lifecycle

```text
Configured source files
  -> capture.Watcher
  -> source parser or whole-file parser
  -> capture.NormalizedEvent
  -> capture.Batcher
  -> store.RowBatch
  -> ClickHouse raw_records, activity_events, event_links, tool_payloads
  -> ingest-time search_documents and search_postings
  -> refreshed session_projection and analytics_projection
  -> web query handlers, search.Searcher, and MCP tools
  -> SSE invalidation for open dashboard/session pages
```

### 1. Source discovery and watching

`cmd/beacon` builds capture sources from `internal/config.Config`. Default
sources cover Claude Code JSONL, Codex JSONL, Hermes SQLite, OpenCode SQLite,
and Pi coding-agent JSONL. Each source provides:

- `name`, `runtime`, `provider`, and `format` metadata
- one or more glob patterns for existing files
- a watch root for new files
- either a line parser for append-only JSONL or a whole-file parser for formats
  such as SQLite session state

`internal/capture.Watcher` owns filesystem discovery and replay safety. On
startup it loads `capture_checkpoints`, resolves source globs, optionally
backfills existing files, and starts `fsnotify` watchers for configured roots
and resolved directories. During normal operation it debounces write/create
events and periodically reconciles globs so newly created files are processed
even if an individual filesystem event is missed.

For line-oriented files, checkpoints track source file, inode, generation,
offset, line number, and parser state. The watcher detects rotation by inode and
size, advances `source_generation` to keep event IDs unique, replays a small
prefix window when needed to preserve parser context, and then emits only new
rows. Parse failures are stored in `capture_errors` with source coordinates and
a context fragment.

### 2. Normalization

Every parser in `internal/capture` converts source-specific payloads into the
common `NormalizedEvent` shape. The normalized event is the ownership boundary
between capture adapters and the rest of Beacon. It carries session identity,
source metadata, event kind, actor role, timestamp, text, tool call/result data,
token counts, model, cost, error fields, parent links, working directory,
parent-session metadata, and source coordinates.

After parsing a file window, the watcher applies shared normalization helpers:

- model propagation fills missing model values from surrounding context events
- token deduplication suppresses repeated cumulative token snapshots from the
  same provider API call
- source metadata defaults are filled from the configured source

The watcher sends each completed `NormalizedEvent` into the batcher's channel.

### 3. Batching and storage writes

`internal/capture.Batcher` owns the write boundary between capture and storage.
It buffers normalized events until either the configured batch size is reached
or the flush interval fires. A final flush also runs on context cancellation.

On flush, the batcher:

- computes deterministic `event_uid` values from source coordinates,
  source-native IDs, source event indexes, collector/source identity, and an
  ordinal for duplicate source coordinates
- maps raw source session IDs into global session IDs using durable
  collector/source assignments from the control-plane metadata store
- attaches `batch_id`, `collector_id`, `source_id`, `control_plane_epoch`,
  `payload_digest`, and redaction placeholders to primary ingest rows
- calculates token cost when the parser did not provide one
- builds short text previews
- creates `models.Event` rows for `activity_events`
- creates `raw_records` rows that retain the original payload JSON
- creates `event_links` rows for parent event references, preserving raw linked
  IDs and unresolved-link status for later reconciliation
- creates `tool_payloads` rows for tool call/result input and output JSON

The batcher passes a `store.RowBatch` to `store.Flush`. After a successful flush
it reports changed session IDs to the dashboard updater.

### 4. ClickHouse tables and projections

`internal/store` owns schema migration, ClickHouse connections, native batch
inserts, ingest-time derived rows, and projection refreshes.

Core write tables:

- `raw_records` stores the original payload, source coordinates, raw/global
  identity, fleet identity, batch metadata, and payload digest for audit and
  replay debugging.
- `activity_events` is the canonical normalized event stream. Most read paths
  use this table directly or through projections.
- `event_links` records event-to-event relationships such as parser parent
  links, raw linked IDs, cross-session scope, and unresolved-link status.
- `tool_payloads` stores structured tool call/result input and output payloads
  apart from the event row.
- `capture_errors` stores parser and capture failures.
- `capture_checkpoints` stores per-source, per-file replay state.
- `capture_heartbeats` is reserved for capture health telemetry.

Search tables are built during ingest, not by a separate external indexer:

- `search_documents` stores one searchable document per event with preview and
  metadata.
- `search_postings` stores token frequencies for BM25-style ranked lookup.
- `search_query_log` records executed search queries asynchronously.

Projection tables summarize the event stream for fast UI/API reads:

- `session_projection` stores per-session start/end times, event and turn
  counts, token totals, tool/MCP/error counts, last model, working directory,
  parent session, and completion status.
- `analytics_projection` stores per-session, per-minute aggregates by provider,
  model, tool, and event kind.

`store.Flush` inserts the batch tables first. When activity events were written,
it builds search documents/postings from event text and tool previews, inserts
those rows, and refreshes `session_projection` and `analytics_projection` for
only the affected session IDs. The projection tables use `ReplacingMergeTree`
plus `argMax` query patterns so repeated deterministic event inserts and
projection refreshes remain idempotent.

### 5. Query and rendering paths

`internal/web` owns HTTP routing, page handlers, JSON API handlers, query
helpers, and live update orchestration.

- `router.go` maps page routes, JSON API routes, static assets, and SSE
  endpoints.
- `handlers.go` renders templ pages for dashboard, session, and conversation
  views. Legacy `/search` links redirect back to the dashboard search table.
- `api.go` serves JSON endpoints used by dashboard JavaScript.
- `queries.go` contains ClickHouse read models for dashboard metrics, active
  and completed sessions, recent activity, session detail, conversations,
  subagents, token charts, and tool stats.
- `viewmodels.go` maps query results into stable response and view structures.
- `updater.go` coalesces ingest notifications into browser invalidations.

Dashboard and API handlers prefer projection tables for summary data. Session
detail and transcript views query the latest version of `activity_events`, using
ClickHouse `argMax` patterns to collapse repeated rows by `event_uid`.

`internal/search.Searcher` owns text search. It tokenizes queries with
`internal/textindex`, reads `search_postings` for ranked text searches, reads
`search_documents` for filter-only browsing, caches index statistics briefly,
and logs query metadata into `search_query_log` without blocking the caller.

`internal/views` owns templ components, page layouts, partials, markdown
rendering, and view-only types. It should not own database access or capture
logic.

### 6. SSE refresh path

The ingest-to-browser refresh path is deliberately an invalidation path:

1. `capture.Batcher` flushes a batch and computes changed session IDs.
2. The batcher calls `web.Updater.MarkDirty`.
3. `Updater.Run` coalesces dirty signals with a short debounce and a max-stale
   cap.
4. `Updater.NotifyDashboard` broadcasts lightweight dashboard invalidation
   events: `active-sessions-update`, `completed-sessions-update`,
   `activity-update`, and `dashboard-charts-update`.
5. `Updater.NotifySessions` broadcasts `conversation-update` for each changed
   session ID.
6. `internal/sse.Broker` fans events out to subscribers for `dashboard`,
   `session:<id>`, or wildcard topics.
7. Browser code reacts to the events by refetching the relevant JSON endpoint or
   HTMX partial.

The SSE payload is only `{"dirty":true}`. Fresh data is loaded by the normal
query handlers, so capture does not block on server-side template rendering.

### Backpressure and drop policies

Live update paths use explicit buffering rules:

- Capture watcher -> batcher: the batcher input channel is `batch_size * 2`.
  Watcher sends block when it is full and unblock on context cancellation; parsed
  capture events are not dropped. Delayed sends are counted and logged at debug
  level.
- Watcher backfill: `capture.backfill_workers` bounds concurrent file workers.
  File jobs are unbuffered so backfills cannot queue unbounded file work in
  memory.
- Batcher -> updater: `Updater.MarkDirty` has a single pending dirty signal.
  Additional dirty wakeups are coalesced, counted, and logged at debug level,
  while changed session IDs stay buffered until notification.
- Updater -> SSE broker: each subscriber has a `sse.subscriber_buffer` channel.
  Broadcasts are best effort per subscriber; full subscriber buffers drop only
  that subscriber's event and increment/log the broker drop count.
- Search query logging: result queries never wait on `search_query_log` writes.
  A four-slot semaphore bounds async inserts; saturated query-log attempts are
  counted and logged at debug level without affecting search results.

### 7. MCP read path

`beacon mcp` opens ClickHouse in read-only mode and starts `internal/mcp.Server`
over stdin/stdout JSON-RPC. The server returns MCP initialization instructions
that tell clients to search first, open returned event IDs for transcript
context, and treat captured data as historical context rather than current
workspace truth. It exposes:

- `search_sessions`, backed by `internal/search.Searcher` and the precomputed
  search tables
- `open`, backed by a window query over `activity_events` for one returned
  `event_id` plus surrounding session context
- `list_sessions`, backed by `session_projection`

Tool input schemas are kept compatible with OpenAI's MCP/function import path:
the top-level schema is always an object and does not use top-level union or
composition keywords such as `anyOf`, `oneOf`, `allOf`, `enum`, or `not`.
The advertised schema should stay simple and match the IDs returned by Beacon
tools.

The MCP server uses the same database tables as the web UI. It does not run
capture, migrations, or writes. MCP searches skip query logging so the tool
surface remains read-only.

## Ownership boundaries

### `cmd/`

- `cmd/beacon` is the product CLI and composition root. It owns command-line
  UX, config loading, ClickHouse startup/migration orchestration, service
  wiring, signal handling, and process lifecycle. It should not own event
  parsing, storage SQL, rendering logic, or search ranking.
- `cmd/perfseed` is a developer/performance utility for seeding ClickHouse test
  data through `internal/perf` and `internal/store`.
- `cmd/simulator` is a local development utility that writes fake Claude-style
  JSONL files so the watcher pipeline can be exercised end to end.

### `internal/`

- `internal/capture` owns source adapters, filesystem watching, checkpointing,
  replay/rotation handling, parser normalization, token/model cleanup, batching,
  and conversion from `NormalizedEvent` into storage row batches.
- `internal/config` owns the typed Beacon configuration, defaults, and config
  file loading. Runtime-specific wiring happens in `cmd/beacon`.
- `internal/mcp` owns the MCP JSON-RPC protocol surface, tool definitions, tool
  argument parsing, and read queries needed by MCP responses.
- `internal/models` owns shared data structs used across capture, storage,
  search, web, and MCP. It should remain behavior-light.
- `internal/perf` owns synthetic data generation and performance benchmark
  support. It is not part of the production request path.
- `internal/search` owns query normalization, ranked search, filter-only browse,
  index statistics, and search query logging. Ingest-time index row creation
  lives in `internal/store` because it is part of the write transaction path.
- `internal/sse` owns SSE wire formatting, subscriber management, topic matching,
  and streaming HTTP handlers. It does not decide what data changed.
- `internal/store` owns ClickHouse schema, migrations, connection setup, native
  batch inserts, search row derivation during flush, projection refresh, and
  checkpoint/error persistence.
- `internal/textindex` owns tokenization and term-frequency helpers shared by
  ingest-time indexing and query-time search.
- `internal/views` owns templ-rendered UI components, pages, partials, markdown
  rendering, and view-level types. It should remain a presentation layer.
- `internal/web` owns HTTP routing, page/API handlers, ClickHouse read queries,
  response/view-model mapping, and SSE invalidation decisions.

## Change guidance

- Add new source formats in `internal/capture` by producing
  `NormalizedEvent`; avoid leaking source-specific shapes into `store` or `web`.
- Add new persistent fields by updating `internal/models`, `internal/store`
  schema/inserts, and the relevant query/view paths together.
- Add dashboard summary data through projection refreshes when it is derived
  from many events; avoid expensive per-request scans over `activity_events`.
- Add searchable fields by updating `store.searchableText` and keeping
  query-time tokenization in `internal/textindex`.
- Add new browser live behavior by broadcasting an invalidation event through
  `web.Updater` and reading fresh data through existing HTTP handlers.
