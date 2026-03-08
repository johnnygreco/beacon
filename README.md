# Beacon

Real-time monitoring dashboard for AI coding agents. Beacon watches conversation logs from [Claude Code](https://docs.anthropic.com/en/docs/claude-code) and [Codex](https://github.com/openai/codex), ingests them into [DuckDB](https://duckdb.org/), and serves a live web UI with full-text search and session analytics.

It also ships an [MCP](https://modelcontextprotocol.io/) server so your agents can query their own history.

## Features

- **Live dashboard** — active sessions, token usage charts, and activity feed updated via SSE
- **Session replay** — full conversation view with turn timeline and tool call details
- **Full-text search** — BM25-ranked search across all monitored conversations
- **MCP server** — expose `search`, `open`, and `list_sessions` tools over JSON-RPC
- **Multi-source** — watches Claude Code and OpenAI Codex JSONL logs out of the box
- **Token tracking** — input, output, and cache-read token counts per session
- **Checkpoint recovery** — tracks file offsets so restarts don't reprocess data

## Quick start

### Prerequisites

- Go 1.24+

### Build and run

```bash
git clone https://github.com/johnnygreco/beacon.git
cd beacon
make build
./bin/beacon serve
```

The dashboard is available at [http://localhost:4600](http://localhost:4600).

On first run, Beacon creates its database at `~/.beacon/beacon.duckdb` and begins watching for JSONL files.

## Commands

| Command | Description |
|---------|-------------|
| `beacon serve` | Start the web server, file watcher, and FTS indexer |
| `beacon watch` | Headless mode — ingest only, no web server |
| `beacon mcp` | Start the MCP server on stdin/stdout |
| `beacon status` | Show database stats and FTS index status |
| `beacon db migrate` | Run schema migrations |
| `beacon db reset --force` | Drop and recreate all tables |

## Configuration

Beacon looks for a config file in this order:

1. `--config` flag
2. `./beacon.toml`
3. `$HOME/.config/beacon/beacon.toml`
4. `$HOME/.beacon/beacon.toml`

Default `beacon.toml`:

```toml
[server]
host = "0.0.0.0"
port = 4600

[database]
path = "~/.beacon/beacon.duckdb"
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

[sse]
subscriber_buffer = 64

[mcp]
max_results = 25
context_window = 3
```

## MCP integration

Add Beacon's MCP server to your Claude Code config:

```json
{
  "mcpServers": {
    "beacon": {
      "command": "beacon",
      "args": ["mcp"]
    }
  }
}
```

This gives your agent access to three tools:

- **search** — full-text search across conversations
- **open** — retrieve context around a specific event
- **list_sessions** — list recent sessions with summary stats

## Tech stack

[DuckDB](https://duckdb.org/) &middot; [Templ](https://templ.guide/) &middot; [HTMX](https://htmx.org/) &middot; [Chart.js](https://www.chartjs.org/) &middot; [Chi](https://github.com/go-chi/chi) &middot; [Cobra](https://github.com/spf13/cobra)

## License

[Apache License 2.0](LICENSE)
