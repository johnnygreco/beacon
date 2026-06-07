# Usage Summaries

`beacon usage` reports event-level token usage from Beacon's ClickHouse data
without requiring direct SQL. It uses the same aggregation contract as the MCP
`usage_summary` tool.

Start Beacon first so ClickHouse is available and the schema is migrated:

```bash
beacon up
```

## Examples

Last 24 hours for Codex:

```bash
beacon usage --source codex --since now-24h
```

The current UTC calendar day:

```bash
beacon usage --today --timezone UTC
```

Top models by selected total:

```bash
beacon usage --group-by model --limit 10
```

Machine-readable output:

```bash
beacon usage --source claude --since now-168h --group-by session_id --json
```

Remote ClickHouse address override:

```bash
beacon usage --clickhouse clickhouse.workstation.example:9440 --since now-24h
```

`--clickhouse` overrides only the ClickHouse address. Configure the database
name, credentials, and TLS in Beacon's `[database]` config, and run
`beacon db migrate` from a trusted machine before reading from a remote Beacon
database. Use `secure = true` for ClickHouse native TCP over TLS on port `9440`;
use port `9000` only for plaintext native TCP on a private network or through an
SSH tunnel.

## Windows

`--since` and `--until` accept RFC3339 timestamps, `now`, or `now-<duration>`
values such as `now-24h` and `now-168h`. When omitted, Beacon reports the last
24 hours ending at `now`. Usage windows are half-open: `since` is inclusive and
`until` is exclusive.

`--today` resolves a calendar-day window in the timezone you provide with
`--timezone`, then prints the absolute UTC window it queried. `--today` cannot
be combined with `--since` or `--until`.

```bash
beacon usage --today --timezone America/New_York
```

## Filters And Grouping

Filters:

- `--source <name>`
- `--model <model>`
- `--provider <provider>`
- `--working-dir <path>`

Group fields:

- `source_name`
- `provider`
- `model`
- `session_id`
- `working_dir`

Use `--group-by` more than once, or pass a comma-separated list:

```bash
beacon usage --source codex --group-by model,session_id --limit 20
```

Grouped results are ordered by selected total tokens, then event count. `--limit`
defaults to `10` and is capped by the shared usage-summary maximum.

## Token Semantics

Beacon always reports these separate counters:

- `input_tokens`
- `output_tokens`
- `total_tokens`, defined as `input_tokens + output_tokens`
- `cache_read_tokens`
- `cache_create_tokens`

The selected total defaults to input plus output tokens:

```text
input_tokens + output_tokens
```

Pass `--include-cache` to include cache categories in the selected total:

```text
input_tokens + output_tokens + cache_read_tokens + cache_create_tokens
```

JSON output includes `token_mode`, `total_definition`, and
`selected_total_definition` so downstream tools can preserve the same meaning.

## MCP Equivalent

Agents should use the MCP `usage_summary` tool for the same data:

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

Use `token_mode: "include_cache"` when cache tokens should be included in the
selected total.

## Safe ClickHouse Escape Hatches

Prefer `beacon usage` or MCP `usage_summary` for normal accounting. If you need
to debug the database directly, keep queries read-only and dedupe
`activity_events` by `event_uid` before summing token fields.

Check that the database is reachable:

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

Do not use `session_projection` as the source for exact event-window usage
accounting; it is useful for session summaries, while `beacon usage` and
`usage_summary` aggregate deduped event rows over the requested window.
