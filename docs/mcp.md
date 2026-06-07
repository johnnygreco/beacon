# MCP Integration

Beacon includes a read-only stdio MCP server for Claude Code, Codex, and other
MCP clients. The MCP server lets agents search prior Beacon sessions while they
stay inside their normal coding workflow.

## How it works

`beacon mcp` is launched by the MCP client over stdin/stdout JSON-RPC. It can
initialize, answer `ping`, and list its tools even when ClickHouse is
temporarily unavailable. Data-backed tool calls open Beacon's ClickHouse
database lazily and return a normal MCP tool error when the database cannot be
reached.

Beacon exposes four tools:

- `search_sessions` searches the precomputed activity index and returns session
  and event IDs.
- `open` retrieves one event plus nearby context from the same session. Pass the
  `event_id` returned by `search_sessions`.
- `list_sessions` lists recent sessions with summary stats.
- `usage_summary` aggregates event-level token usage for exact windows and
  optional top-contributor groupings.

The server does not run capture, migrations, or writes. MCP searches also skip
Beacon's query log, so the tool surface remains read-only. Beacon returns MCP
server instructions during initialization so clients prefer the search-then-open
workflow and treat captured transcripts as historical context that should be
verified against the current workspace before acting.

## Start Beacon

Start Beacon before using MCP tools so local ClickHouse is available and
migrated:

```bash
beacon up
```

The MCP client launches `beacon mcp`, so `beacon` must be installed on the
machine where Claude Code, Codex, or the other MCP client runs.

If an MCP client starts first, the connection should still initialize. Tool
calls that need captured data will report that Beacon's database is unavailable
and suggest starting Beacon with `beacon up`.

## Claude Code

Add Beacon as a local stdio MCP server:

```bash
claude mcp add --transport stdio beacon -- beacon mcp
```

Make Beacon available across all Claude Code projects:

```bash
claude mcp add --transport stdio --scope user beacon -- beacon mcp
```

Share Beacon with one project through `.mcp.json`:

```bash
claude mcp add --transport stdio --scope project beacon -- beacon mcp
```

Verify the Claude Code configuration:

```bash
claude mcp get beacon
```

You can also inspect the connection with `/mcp` inside Claude Code.

## Codex

Add Beacon with the Codex MCP CLI:

```bash
codex mcp add beacon -- beacon mcp
```

Or configure it directly in `~/.codex/config.toml`:

```toml
[mcp_servers.beacon]
command = "beacon"
args = ["mcp"]
startup_timeout_sec = 20
tool_timeout_sec = 60
```

## Remote ClickHouse

If Claude Code or Codex runs on a different machine, install `beacon` on that
machine too. The stdio MCP server runs where the agent runs, not where the
Beacon dashboard is open.

Use one of these layouts:

- Run Beacon's local ClickHouse on the agent machine.
- Connect to ClickHouse through an SSH tunnel and use `127.0.0.1:9000`.
- Point Beacon at a remote ClickHouse TCP address reachable from the agent
  machine.

`beacon mcp` opens ClickHouse read-only and does not run migrations. For a
remote database, run `beacon db migrate` from a trusted machine before pointing
MCP clients at it.

`--clickhouse` overrides only the ClickHouse address. Configure the database
name, credentials, and TLS in Beacon's config on the agent machine:

```toml
[database]
addrs = ["clickhouse.workstation.example:9440"]
database = "beacon"
username = "beacon_readonly"
password = "..."
secure = true
```

Then add the MCP server normally:

```bash
claude mcp add --transport stdio beacon -- beacon mcp
```

```toml
[mcp_servers.beacon]
command = "beacon"
args = ["mcp"]
startup_timeout_sec = 20
tool_timeout_sec = 60
```

Use port `9440` for ClickHouse native TCP over TLS. Use port `9000` only for
plaintext native TCP on a private network or through an SSH tunnel. Require
ClickHouse authentication when exposing the database beyond the local machine.

If you want the MCP entry to select a specific address while still using the
default config file for the database name, credentials, and TLS, pass the address
directly:

```bash
claude mcp add --transport stdio beacon -- beacon mcp --clickhouse clickhouse.workstation.example:9440
```

```toml
[mcp_servers.beacon]
command = "beacon"
args = ["mcp", "--clickhouse", "clickhouse.workstation.example:9440"]
startup_timeout_sec = 20
tool_timeout_sec = 60
```

## Generic MCP clients

For clients that use JSON configuration:

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

With an address override:

```json
{
  "mcpServers": {
    "beacon": {
      "command": "beacon",
      "args": ["mcp", "--clickhouse", "127.0.0.1:9000"]
    }
  }
}
```

## Troubleshooting

If a client reports an MCP startup or handshake warning, first verify that the
installed `beacon mcp` can answer `initialize` without printing diagnostics to
stdout:

```bash
printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"probe","version":"0"}}}' | beacon mcp
```

The output should be one JSON-RPC response. If data-backed tool calls return an
error such as `Beacon database is not available at 127.0.0.1:9000`, start the
local Beacon services:

```bash
beacon up
```

For remote ClickHouse, check the configured address or pass an address override:

```bash
beacon mcp --clickhouse clickhouse.workstation.example:9440
```

## Tool arguments

Beacon keeps MCP tool schemas compatible with OpenAI's MCP/function import path:
the top-level schema is always an object and does not use top-level `anyOf`,
`oneOf`, `allOf`, `enum`, or `not`.

Some clients require every advertised property to be present. Beacon marks
defaulted arguments nullable; send `null` when you want Beacon's default.

`search_sessions`:

```json
{
  "query": "pricing cache bug",
  "limit": null,
  "session_id": null,
  "event_kinds": null
}
```

`open`:

```json
{
  "event_id": "event:abc123",
  "before": null,
  "after": null
}
```

`list_sessions`:

```json
{
  "limit": null,
  "since": null,
  "until": null,
  "source_name": null,
  "model": null,
  "provider": null,
  "working_dir": null,
  "active_during_since": null,
  "active_during_until": null,
  "cursor": null
}
```

`list_sessions` responses include `metadata.result_count`,
`metadata.total_matching_count`, `metadata.result_complete`, and
`metadata.next_cursor`; pass `next_cursor` back as `cursor` to continue paging.

`usage_summary`:

```json
{
  "since": "now-24h",
  "until": "now",
  "window_mode": "event_timestamp",
  "token_mode": "io_only",
  "source_name": "codex",
  "model": null,
  "provider": null,
  "working_dir": null,
  "group_by": ["session_id"],
  "limit": 10
}
```

`open` accepts only `event_id`. Use the ID returned by `search_sessions`; do not
pass legacy `id` or `event_uid` arguments.
