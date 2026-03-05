# Technodrome

Real-time monitoring dashboard for AI coding agents. Passively watches JSONL log files from Claude Code and Codex, ingests events into DuckDB, and serves a live-updating web dashboard. Includes an MCP server so agents can query their own history.

```
JSONL logs → Watcher → Batcher → DuckDB → Web Dashboard (SSE)
                                        → MCP Server (stdin/stdout)
```

## Quick Start

**Prerequisites:** Go 1.24+

```bash
# Clone and build
git clone https://github.com/technodrome-ai/technodrome.git
cd technodrome
make build

# Run (watches Claude Code and Codex logs by default)
./bin/technodrome serve
```

Open [http://localhost:4600](http://localhost:4600).

No configuration is required for the default setup — Technodrome watches `~/.claude/projects/**/*.jsonl` and `~/.codex/sessions/**/*.jsonl` out of the box.

## Installation

```bash
go install github.com/technodrome-ai/technodrome/cmd/technodrome@latest
```

Or build from source:

```bash
make build        # builds to bin/technodrome
```

## Usage

```
technodrome              # default: starts the web dashboard
technodrome serve        # web dashboard + JSONL watcher
technodrome watch        # headless watcher (no web server)
technodrome mcp          # MCP server (stdin/stdout JSON-RPC)
technodrome status       # print database statistics
technodrome db migrate   # run database migrations
technodrome db reset     # drop and recreate all tables
```

### Generate Test Data

```bash
make simulator    # or: go run ./cmd/simulator
```

## Configuration

Technodrome looks for `technodrome.toml` in the working directory, `~/.config/technodrome/`, or `~/.technodrome/`. All settings have sensible defaults.

```toml
[server]
host = "0.0.0.0"
port = 4600

[database]
path = "~/.technodrome/technodrome.duckdb"
read_pool_size = 4

[watch]
enabled = true
debounce_ms = 50
reconcile_interval = "30s"
backfill_on_start = true

[[watch.sources]]
name = "claude"
provider = "anthropic"
glob = "~/.claude/projects/**/*.jsonl"

[[watch.sources]]
name = "codex"
provider = "openai"
glob = "~/.codex/sessions/**/*.jsonl"

[search]
max_results = 25
rebuild_interval = "5m"

[pricing]
default_input_cost = 3.00    # per 1M input tokens
default_output_cost = 15.00  # per 1M output tokens

[mcp]
max_results = 25
context_window = 3
```

## MCP Server

The MCP server lets AI agents query Technodrome's data over stdin/stdout JSON-RPC.

```bash
technodrome mcp [--db path/to/technodrome.duckdb]
```

**Available tools:**

| Tool | Description |
|------|-------------|
| `search` | BM25 full-text search across all conversations |
| `open` | Retrieve an event with surrounding context |
| `list_sessions` | List recent sessions with summary stats |

### Claude Code Integration

Add to your Claude Code MCP config:

```json
{
  "mcpServers": {
    "technodrome": {
      "command": "technodrome",
      "args": ["mcp"]
    }
  }
}
```

## Architecture

- **Single events table** — all event types (messages, tool calls, errors) in one table. Views derive sessions, turns, and aggregates.
- **Deterministic UIDs** — event IDs are SHA256 of `file|lineNo|offset|content`, enabling idempotent replay.
- **Single writer** — all DuckDB writes go through a serialized batcher channel. Read pool handles concurrent queries.
- **BM25 search** — DuckDB FTS extension with ILIKE recency fallback. Index rebuilds every 5 minutes.
- **SSE updates** — the batcher notifies the web updater after each flush, which renders templ partials and broadcasts via SSE.

## Project Layout

```
cmd/
  technodrome/       CLI entry point and subcommands
  simulator/         test data generator
internal/
  config/            TOML configuration (viper)
  database/          DuckDB connections, migrations, appender
  ingestion/         file watcher, JSONL parsers, batcher, normalizer
  mcp/               MCP JSON-RPC server
  search/            BM25 full-text search
  sse/               SSE broker
  views/             templ templates
  web/               HTTP handlers, queries, API, SSE updater
static/              CSS and JavaScript assets
```

## Development

```bash
# Edit .templ files, then regenerate
make generate

# Build and run
make run

# Generate test data in another terminal
make simulator
```
