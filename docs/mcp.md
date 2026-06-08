# MCP Integration

Beacon includes a read-only stdio MCP server for coding agents and other MCP
clients. The MCP server lets agents search prior Beacon sessions while they stay
inside their normal workflow.

For the full personal-production control-plane and collector setup that remote
MCP usually reads from, see [Personal production guide](production.md).

## How it works

`beacon mcp` is launched by the MCP client over stdin/stdout JSON-RPC. It can run
locally against ClickHouse or proxy to a central Beacon control plane over
HTTP(S). In remote mode the agent machine does not need ClickHouse credentials.

Beacon exposes five tools:

- `search_sessions` searches the precomputed activity index and returns session
  and event IDs, provenance, and `open_ref` values.
- `open` retrieves one event plus nearby context from the same session. Pass the
  `event_id` or `open_ref` returned by Beacon MCP tools.
- `list_agents` lists enrolled collector/source/runtime/project rollups.
- `list_sessions` lists recent sessions with summary stats and `open_ref`
  values.
- `usage_summary` aggregates event-level token usage for exact windows and
  optional top-contributor groupings.

The server does not run capture, migrations, or writes. MCP searches also skip
Beacon's query log, so the tool surface remains read-only. Beacon returns MCP
server instructions during initialization so clients prefer the search-then-open
workflow and treat captured transcripts as historical context that should be
verified against the current workspace before acting.

## Start Beacon

Start Beacon on the machine that serves the central dashboard/control plane:

```bash
beacon up
```

The MCP client launches `beacon mcp`, so `beacon` must also be installed on the
machine where the MCP client runs.

If an MCP client starts first, the connection should still initialize. In local
ClickHouse mode, data-backed tool calls report that Beacon's database is
unavailable and suggest starting Beacon with `beacon up`.

## Remote Control-Plane MCP

Use remote mode when the agent process runs on a laptop, home server, VM, cloud
host, or any other machine separate from the central Beacon server. Remote mode
forwards each MCP JSON-RPC request to the dashboard API and never opens
ClickHouse from the agent machine.

```bash
BEACON_MCP_URL=https://beacon.example.com/api/mcp \
BEACON_READ_TOKEN="$BEACON_READ_TOKEN" \
beacon mcp
```

Equivalent flags:

```bash
beacon mcp \
  --remote-url https://beacon.example.com/api/mcp \
  --read-token-file ~/.beacon/read-token
```

The token file can contain the same owner, admin, or read-scoped token you would
otherwise put in `BEACON_READ_TOKEN`. If the URL has no path, Beacon appends
`/api/mcp`. Use any Beacon token that carries read scope. The owner token shown
by `beacon init` works for a full personal dataset; admin tokens do too. Bound
read tokens, when minted by token tooling and enforced by Beacon MCP auth,
silently scope results to their configured node, collector, or source bindings.
Runtime and project are explicit tool filters and can also be carried forward by
returned `open_ref` values. Returned payloads include
`scope.auth_scope_applied` and the effective filters.

In the loopback reverse-proxy production layout, Beacon trusts the proxy
boundary for `/api/mcp`; the proxy must authenticate that route and Beacon does
not apply read-token scoping there. Beacon-enforced MCP bearer tokens and
read-token scopes are active in owner-token mode, or in reverse-proxy mode bound
to a non-loopback private interface that only the trusted proxy can reach.

Remote MCP URLs must use HTTPS for non-loopback hosts; plain HTTP is accepted
only for loopback development.

Remote `open` calls that target data outside the effective scope return
`forbidden` without revealing whether the event or session exists.

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

## Direct ClickHouse Mode

The default `beacon mcp` mode opens ClickHouse read-only from the same machine
where the MCP client runs. Use this for local development or trusted
administrative debugging. For normal cross-machine agents, prefer remote
control-plane MCP.

Local/direct layouts:

- Run Beacon's local ClickHouse on the agent machine.
- Connect to a trusted ClickHouse through an SSH tunnel and use `127.0.0.1:9000`.
- Point Beacon at a ClickHouse TCP address reachable from the agent machine.

`beacon mcp` opens ClickHouse read-only and does not run migrations. For a direct
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
Do not use direct ClickHouse credentials for normal remote agent MCP workflows.

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

Remote control-plane mode:

```json
{
  "mcpServers": {
    "beacon": {
      "command": "beacon",
      "args": ["mcp", "--remote-url", "https://beacon.example.com/api/mcp"],
      "env": {
        "BEACON_READ_TOKEN": "..."
      }
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

The output should be one JSON-RPC response. In local ClickHouse mode, if
data-backed tool calls return an error such as `Beacon database is not available
at 127.0.0.1:9000`, the MCP server has initialized but ClickHouse is not
reachable for tool execution. Start the local Beacon services:

```bash
beacon up
```

For remote control-plane mode, verify the URL and token:

```bash
printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"ping"}' | \
  env BEACON_MCP_URL=https://beacon.example.com/api/mcp \
      BEACON_READ_TOKEN="$BEACON_READ_TOKEN" \
      beacon mcp
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
  "event_kinds": null,
  "node_id": null,
  "node_ids": null,
  "collector_id": null,
  "collector_ids": null,
  "source_id": null,
  "source_ids": null,
  "source_name": null,
  "source_names": null,
  "runtime": null,
  "runtimes": null,
  "project_key": null,
  "project_keys": null
}
```

`limit` defaults to `25` and is capped at `100`.

`open`:

```json
{
  "event_id": "event:abc123",
  "session_id": null,
  "anchor": null,
  "open_ref": null,
  "before": null,
  "after": null,
  "node_id": null,
  "node_ids": null,
  "collector_id": null,
  "collector_ids": null,
  "source_id": null,
  "source_ids": null,
  "source_name": null,
  "source_names": null,
  "runtime": null,
  "runtimes": null,
  "project_key": null,
  "project_keys": null
}
```

`before` and `after` default to `mcp.context_window` and are capped at `25`
events per side.

`list_agents`:

```json
{
  "limit": null,
  "node_id": null,
  "node_ids": null,
  "collector_id": null,
  "collector_ids": null,
  "source_id": null,
  "source_ids": null,
  "source_name": null,
  "source_names": null,
  "runtime": null,
  "runtimes": null,
  "project_key": null,
  "project_keys": null
}
```

`limit` defaults to `50` and is capped at `200`.

`list_sessions`:

```json
{
  "limit": null,
  "since": null,
  "until": null,
  "model": null,
  "provider": null,
  "working_dir": null,
  "active_during_since": null,
  "active_during_until": null,
  "cursor": null,
  "node_id": null,
  "node_ids": null,
  "collector_id": null,
  "collector_ids": null,
  "source_id": null,
  "source_ids": null,
  "source_name": null,
  "source_names": null,
  "runtime": null,
  "runtimes": null,
  "project_key": null,
  "project_keys": null
}
```

`list_sessions` responses include `metadata.result_count`,
`metadata.total_matching_count`, `metadata.result_complete`, and
`metadata.next_cursor`; pass `next_cursor` back as `cursor` to continue paging.
`since` and `until` filter session `started_at`; `active_during_since` and
`active_during_until` use overlap semantics across `started_at` and `ended_at`.
`model`, `provider`, and `working_dir` are exact filters. Scope filters accept
node, collector, source, runtime, and project values. `limit` defaults to `20`
and is capped at `100`. Deep cursor offsets are capped at `10000`.

`usage_summary`:

```json
{
  "since": "now-24h",
  "until": "now",
  "window_mode": "event_timestamp",
  "token_mode": "io_only",
  "model": null,
  "provider": null,
  "working_dir": null,
  "node_id": null,
  "node_ids": null,
  "collector_id": null,
  "collector_ids": null,
  "source_id": null,
  "source_ids": null,
  "source_name": "codex",
  "source_names": null,
  "runtime": null,
  "runtimes": null,
  "project_key": null,
  "project_keys": null,
  "group_by": ["session_id"],
  "limit": 10
}
```

`usage_summary` arguments:

- `since` and `until` accept RFC3339 timestamps, `now`, or `now-<duration>` such
  as `now-24h`. Defaults to the last 24 hours ending at `now`. Usage windows are
  half-open: `since` is inclusive and `until` is exclusive.
- `window_mode` currently supports `event_timestamp`.
- `token_mode` defaults to `io_only`, where selected totals are
  `input_tokens + output_tokens`; use `include_cache` to include
  `cache_read_tokens + cache_create_tokens` in the selected total.
- `source_name`, `model`, `provider`, and `working_dir` are exact filters.
  Scope filters accept node, collector, source, runtime, and project values.
- `group_by` accepts `source_name`, `provider`, `model`, `session_id`, and
  `working_dir`, plus `node_id`, `collector_id`, `source_id`, `runtime`, and
  `project_key`; grouped rows are ordered by selected total and then event count.
- `limit` defaults to `10` and is capped server-side.

The response includes `total_definition` and `selected_total_definition` so
agents can state token semantics precisely. Use `beacon usage` for the same
summary from a shell; see [usage summaries](usage.md).

`open` accepts `event_id`, returned `open_ref` objects, or `session_id` with
`anchor: "latest"`. Returned `open_ref` values carry the effective scope from
the tool result that produced them; `open` intersects that scope with any
explicit scope filters and the token's auth scope. Direct `open` calls can also
pass node, collector, source, runtime, and project filters. Do not pass legacy
`id` or `event_uid` arguments.

## Safe ClickHouse Escape Hatches

Prefer MCP tools for agent workflows. If you need to debug the database directly,
keep queries read-only and dedupe `activity_events` by `event_uid` before
summing token fields.

Reachability check:

```bash
clickhouse-client --database beacon --query "SELECT count() FROM activity_events"
```

Exact last-24-hour I/O totals by source:

```bash
clickhouse-client --database beacon --query "
WITH latest_events AS (
  SELECT
    event_uid,
    argMax(session_id, captured_at) AS session_id,
    argMax(source_name, captured_at) AS source_name,
    argMax(timestamp, captured_at) AS timestamp,
    argMax(input_tokens, captured_at) AS input_tokens,
    argMax(output_tokens, captured_at) AS output_tokens
  FROM activity_events
  GROUP BY event_uid
)
SELECT
  source_name,
  uniqExact(session_id) AS sessions,
  count() AS events,
  sum(input_tokens + output_tokens) AS io_tokens
FROM latest_events
WHERE timestamp >= now() - INTERVAL 24 HOUR
  AND timestamp < now()
  AND session_id != ''
GROUP BY source_name
ORDER BY io_tokens DESC
LIMIT 20
"
```

Recent sessions:

```bash
clickhouse-client --database beacon --query "
SELECT
  session_id,
  source_name,
  started_at,
  ended_at,
  total_tokens AS io_tokens
FROM session_projection FINAL
ORDER BY ended_at DESC
LIMIT 20
"
```
