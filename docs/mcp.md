# MCP Integration

Beacon includes a stdio MCP server for coding agents and other MCP clients. The
MCP server lets agents search prior local Beacon sessions, inspect context, read
usage summaries, and annotate traces while they stay inside their normal
workflow.

Run Beacon locally with `beacon up` and configure the MCP client to launch
`beacon mcp`.

## How It Works

`beacon mcp` is launched by the MCP client over stdin/stdout JSON-RPC. It opens
Beacon's configured ClickHouse database.

Beacon exposes these read tools:

- `search_sessions` searches the precomputed activity index and returns session
  and event IDs plus `open_ref` values.
- `open` retrieves one event plus nearby context from the same session. Pass the
  `event_id` or `open_ref` returned by Beacon MCP tools.
- `list_sessions` lists recent sessions with summary stats and `open_ref`
  values.
- `usage_summary` aggregates event-level token usage for exact windows and
  optional top-contributor groupings.
- `list_annotations` lists Beacon annotations for a session, message, event, or
  returned `open_ref`.
- `get_annotation` reads one annotation by ID after verifying its target is in
  scope.

Beacon exposes these write tools:

- `create_annotation` creates an agent annotation on a session, message, or
  event.
- `update_annotation` updates an existing annotation after verifying the target
  remains in scope.
- `delete_annotation` soft-deletes one annotation after verifying the target
  remains in scope.

The server does not run capture. It opens the writable Beacon store so annotation
tools can persist notes, which means startup may run schema migrations on the
configured database. MCP searches skip Beacon's query log, and annotation write
tools are explicitly advertised as non-read-only in their tool annotations.
Beacon returns MCP server instructions during initialization so clients prefer
the search-then-open workflow and treat captured transcripts as historical
context that should be verified against the current workspace before acting.

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
explicit scope filters and the token's auth scope. Do not pass `id` or
`event_uid` arguments.

## Annotation Tools

Annotation tools share the same scope filters as search tools. Message targets
require a message event and are returned with both `event_id` and `message_id`;
event targets use `event_id`; session targets use `session_id`. Use
`message_id` or the message `open_ref` returned by message search/open results
when you want a message-level annotation; event `open_ref` values create event
annotations unless the target type is explicit.

`create_annotation`:

```json
{
  "target_type": "message",
  "session_id": null,
  "message_id": "message:abc123",
  "event_id": null,
  "open_ref": null,
  "author_id": "agent-run-42",
  "author_name": "Reviewer Agent",
  "category": "quality",
  "outcome": "needs_fix",
  "quality_score": 2,
  "confidence": 85,
  "needs_followup": true,
  "labels": ["dataset:eval", "rubric:correctness"],
  "note": "The assistant missed the user's explicit constraint.",
  "metadata_json": "{\"rubric_version\":\"2026-06\"}",
  "source_name": null,
  "source_names": null,
  "runtime": null,
  "runtimes": null,
  "project_key": null,
  "project_keys": null
}
```

`target_type` may be `session`, `message`, or `event`. If omitted or `null`,
Beacon infers it from `message_id`, `event_id`, `session_id`, or `open_ref`.
For message annotations, prefer `message_id` or use the message `open_ref`
returned for message search/open results. Event `open_ref` values infer event
targets unless `target_type: "message"` is explicit.
`metadata_json` must be a JSON object encoded as a string when provided.
Successful creates return schema `beacon.mcp.create_annotation.v1`, the
annotation record, and an `open_ref` for the target.

`update_annotation`:

```json
{
  "annotation_id": "ann_abc123",
  "category": "quality",
  "outcome": "fixed",
  "quality_score": 4,
  "confidence": 90,
  "needs_followup": false,
  "labels": ["dataset:train"],
  "note": "Updated after verifying the corrected trace.",
  "metadata_json": "{\"rubric_version\":\"2026-06\"}",
  "source_name": null,
  "source_names": null,
  "runtime": null,
  "runtimes": null,
  "project_key": null,
  "project_keys": null
}
```

Updates return schema `beacon.mcp.update_annotation.v1`. Updates change content
fields and preserve the annotation's existing author and source attribution.

`list_annotations`:

```json
{
  "target_type": null,
  "session_id": "session:abc123",
  "message_id": null,
  "event_id": null,
  "open_ref": null,
  "include_deleted": false,
  "limit": 50,
  "offset": 0,
  "source_name": null,
  "source_names": null,
  "runtime": null,
  "runtimes": null,
  "project_key": null,
  "project_keys": null
}
```

For a session ID with no explicit `target_type`, Beacon lists all visible
annotations in that session, including session, message, and event annotations.
Use `"target_type": "session"` only when you want session-level annotations and
not message or event annotations.
Responses use schema `beacon.mcp.list_annotations.v1` with `metadata.limit`,
`metadata.offset`, `metadata.result_count`, and `metadata.result_complete`.

`get_annotation`:

```json
{
  "annotation_id": "ann_abc123",
  "include_deleted": false,
  "source_name": null,
  "source_names": null,
  "runtime": null,
  "runtimes": null,
  "project_key": null,
  "project_keys": null
}
```

`delete_annotation`:

```json
{
  "annotation_id": "ann_abc123",
  "source_name": null,
  "source_names": null,
  "runtime": null,
  "runtimes": null,
  "project_key": null,
  "project_keys": null
}
```

Deletes are soft deletes and return schema `beacon.mcp.delete_annotation.v1`
with `status: "deleted"`.
