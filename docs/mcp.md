# MCP Integration

Beacon includes a read-only stdio MCP server for coding agents and other MCP
clients. The MCP server lets agents search prior local Beacon sessions while
they stay inside their normal workflow.

Run Beacon locally with `beacon up` and configure the MCP client to launch
`beacon mcp`.

## How It Works

`beacon mcp` is launched by the MCP client over stdin/stdout JSON-RPC. It opens
Beacon's configured ClickHouse database read-only.

Beacon exposes four tools:

- `search_sessions` searches the precomputed activity index and returns session
  and event IDs plus `open_ref` values.
- `open` retrieves one event plus nearby context from the same session. Pass the
  `event_id` or `open_ref` returned by Beacon MCP tools.
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

For local usage, start Beacon on the same machine as your agent tools:

```bash
beacon up
```

The MCP client launches `beacon mcp`, so `beacon` must also be installed on the
machine where the MCP client runs.

If an MCP client starts first, the connection should still initialize. In local
ClickHouse mode, data-backed tool calls report that Beacon's database is
unavailable and suggest starting Beacon with `beacon up`.

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

## Tool Arguments

Beacon keeps MCP tool schemas compatible with OpenAI's MCP/function import path:
the top-level schema is always an object and does not use top-level `anyOf`,
`oneOf`, `allOf`, `enum`, or `not`.

Some clients require every advertised property to be present. Beacon marks
defaulted arguments nullable; send `null` when you want Beacon's default.

All scope-aware tools accept the same local scope filters:

- `source_name` or `source_names`
- `runtime` or `runtimes`
- `project_key` or `project_keys`

`search_sessions`:

```json
{
  "query": "pricing cache bug",
  "limit": null,
  "session_id": null,
  "event_kinds": null,
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
`model`, `provider`, and `working_dir` are exact filters. `limit` defaults to
`20` and is capped at `100`.

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
  as `now-24h`. Usage windows are half-open: `since` is inclusive and `until` is
  exclusive.
- `window_mode` currently supports `event_timestamp`.
- `token_mode` defaults to `io_only`, where selected totals are
  `input_tokens + output_tokens`; use `include_cache` to include
  `cache_read_tokens + cache_create_tokens`.
- `source_name`, `runtime`, `project_key`, `model`, `provider`, and
  `working_dir` are exact filters.
- `group_by` accepts `source_name`, `provider`, `model`, `session_id`,
  `working_dir`, `runtime`, and `project_key`.
- `limit` defaults to `10` and is capped server-side.

The response includes `total_definition` and `selected_total_definition` so
agents can state token semantics precisely. Use `beacon usage` for the same
summary from a shell; see [usage summaries](usage.md).

`open` accepts `event_id`, returned `open_ref` objects, or `session_id` with
`anchor: "latest"`. Returned `open_ref` values carry the effective scope from
the tool result that produced them; `open` intersects that scope with any
explicit scope filters and the token's auth scope. Do not pass legacy `id` or
`event_uid` arguments.
