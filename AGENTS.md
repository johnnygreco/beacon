# Technodrome v2 — Agent Guide

## Overview

Technodrome is a real-time monitoring dashboard for AI coding agents (Claude Code, Codex). It passively watches JSONL log files, ingests events into DuckDB, and serves a web dashboard with SSE live updates. An MCP server enables agents to query their own history.

## Architecture

```
JSONL Files → Watcher → Batcher → DuckDB ← Web Queries → SSE → Browser
                                        ← MCP Server → Agent (stdin/stdout)
```

### Core Pipeline

1. **Watcher** (`internal/ingestion/watcher.go`) — uses fsnotify + glob to monitor JSONL files. Debounces writes, handles log rotation via inode tracking, saves checkpoints for crash recovery.

2. **Parsers** (`internal/ingestion/parser_claude_jsonl.go`, `parser_codex_jsonl.go`) — parse JSONL lines into `NormalizedEvent` structs. Each line may produce multiple events (e.g., a message with tool_use blocks).

3. **Batcher** (`internal/ingestion/batcher.go`) — accumulates events, generates deterministic UIDs, calculates costs, and flushes to DuckDB. Calls `notify()` after each flush to trigger SSE updates.

4. **DuckDB** (`internal/database/`) — single-writer model. One pinned write connection, pool for reads. FTS extension for BM25 search.

5. **SSE** (`internal/sse/`, `internal/web/updater.go`) — the Updater renders templ partials and broadcasts as SSE events. HTMX sse-swap attributes consume these on the frontend.

### Data Model

Single `events` table with all event types. No separate tables for sessions, turns, tool calls, etc. Views derive aggregates:

- `v_session_summary` — per-session stats
- `v_conversation_trace` — turn sequencing via window functions
- `v_tokens_per_minute` — time-series token data
- `v_tool_stats` — tool usage counts
- `v_hourly_costs` — cost breakdown

Supporting tables: `event_links` (parent-child), `tool_io` (tool input/output), `ingest_checkpoints` (watcher state), `ingest_errors` (parse failures).

## Build & Run

```bash
# Build
make build           # or: go build -o bin/technodrome ./cmd/technodrome

# Run
make run             # or: go run ./cmd/technodrome serve

# Generate templ files (required before build if .templ files changed)
make generate        # or: templ generate

# Simulator (generates fake JSONL)
make simulator       # or: go run ./cmd/simulator
```

## CLI Commands

```
technodrome              # default: serve
technodrome serve        # web dashboard + JSONL watcher
technodrome watch        # headless JSONL watcher only
technodrome mcp          # MCP server (stdin/stdout)
technodrome status       # DB statistics
technodrome db migrate   # run migrations
technodrome db reset     # drop + recreate (--force to skip prompt)
```

## Package Organization

```
cmd/
  technodrome/     — CLI entry point, subcommands
  simulator/       — fake JSONL generator
internal/
  config/          — viper-based TOML config
  database/        — DuckDB connection, migrations, appender functions
  ingestion/       — watcher, parsers, batcher, normalizer, pricing
  mcp/             — MCP JSON-RPC server
  search/          — BM25 FTS + ILIKE fallback
  sse/             — SSE broker and handlers
  views/           — templ templates and view types
  web/             — HTTP handlers, queries, API, updater
static/            — CSS and JS assets
```

## Key Conventions

- **Single writer:** All DuckDB writes go through the batcher's serialized channel. Never write directly.
- **Deterministic UIDs:** Event UIDs are SHA256 of `file|lineNo|offset|content`. This enables idempotent replay.
- **Event kinds:** `message`, `tool_call`, `tool_result`, `reasoning`, `session_meta`, `turn_context`, `event_msg`, `error`, `context_snapshot`
- **Actor roles:** `user`, `assistant`, `system`, `tool`
- **Cost calculation:** `internal/ingestion/pricing.go` has per-model pricing. Cost is calculated at ingest time if not provided.
- **Templ workflow:** Edit `.templ` files, run `templ generate`, then `go build`. Generated `_templ.go` files should be committed.

## Adding a New Ingestion Source

1. Create `internal/ingestion/parser_<name>_jsonl.go` with signature:
   ```go
   func Parse<Name>JSONL(line []byte, file string, lineNo int, offset int64) ([]NormalizedEvent, error)
   ```

2. Add a `SourceConfig` entry in `technodrome.toml` under `[[watch.sources]]`

3. Wire the parser in `cmd/technodrome/cmd_serve.go` `buildSources()`

## Adding a New MCP Tool

1. Add tool definition to `toolDefinitions()` in `internal/mcp/tools.go`
2. Add handler method to `Server` struct
3. Add case to `callTool()` switch
4. Add formatter if needed in `internal/mcp/format.go`

## DuckDB Notes

- `INSERT OR IGNORE INTO` for idempotent event inserts
- `INSERT OR REPLACE INTO` for checkpoint upserts
- FTS via `PRAGMA create_fts_index('events', 'event_uid', 'text_content', overwrite=1)`
- `LOAD fts` must be called per-connection
- Read-only connections can query FTS but not rebuild it
- `time_bucket()` for time-series aggregation
