# Privacy, retention, and local data boundaries

Beacon is a local observability tool. It reads agent session files that already
exist on the machine, normalizes them, and stores derived rows in ClickHouse so
the dashboard, search UI, and MCP tools can query them quickly.

Beacon does not currently implement automatic redaction or retention expiry.
Treat the Beacon database as sensitive local data.

## Data Beacon stores

Beacon can store the following content from configured capture sources:

- raw source records from agent session files;
- normalized prompts, responses, reasoning summaries, tool calls, tool results,
  errors, paths, working directories, models, token counts, durations, and
  source metadata;
- full tool input/output payloads when a parser extracts them;
- text previews and tokenized search documents/postings derived from event text,
  tool names, paths, models, tool payload previews, and bounded payload content;
- session and analytics projections derived from captured events;
- search query log rows containing search text, normalized terms, result counts,
  and timing data;
- capture checkpoints and capture errors used to replay files safely.

The ClickHouse schema and table ownership are documented in
[clickhouse.md](clickhouse.md).

## Storage locations

Beacon's default config file is `~/.beacon/beacon.toml`.

When Beacon manages native ClickHouse, local database files are under
`~/.beacon/clickhouse`, including:

- `~/.beacon/clickhouse/data`;
- `~/.beacon/clickhouse/logs`;
- `~/.beacon/clickhouse/access`;
- `~/.beacon/clickhouse/clickhouse.pid`.

When Beacon starts Docker ClickHouse, database files live in the Docker volume
named `beacon-clickhouse-data`.

If `[database].addrs` points to a remote ClickHouse host, Beacon writes captured
data to that remote database. In that mode, Beacon does not control host-level
storage, backup, access control, or deletion outside its own tables.

## Surfaces that expose captured data

The web dashboard exposes captured session summaries, transcripts, search
results, tool payloads, metrics, and recent activity through local HTTP routes.
Anyone who can reach the Beacon web server can inspect that data.

The MCP server exposes the same local database through `search_sessions`, `open`,
and `list_sessions`. MCP clients should be configured only for trusted local
agent environments.

Search results use a derived index, but the source content may still be present
in raw records, activity events, tool payloads, previews, and transcript views.
Deleting only search rows is not a privacy cleanup.

## Retention policy

Beacon keeps captured data until the user removes it. There is no automatic TTL,
per-project expiration, or redaction pass.

Current cleanup options:

- `beacon db reset --force` drops and recreates Beacon-owned ClickHouse tables in
  the configured database. This deletes Beacon data from those tables but does
  not remove original agent session files.
- `curl -sSfL https://johnnygreco.dev/beacon/install.sh | UNINSTALL=1 sh`
  removes the installed `beacon` binary and deletes `~/.beacon`, including
  Beacon-managed native ClickHouse data.
- For Docker ClickHouse, `beacon db reset --force` deletes Beacon table data.
  Removing the Docker volume `beacon-clickhouse-data` is a separate Docker
  operation and is destructive to that volume.
- For remote ClickHouse, delete or reset the configured database according to
  that deployment's operational policy.

Original agent session files remain in their source locations, such as
`~/.claude`, `~/.codex`, `~/.hermes`, `~/.local/share/opencode`, or `~/.pi`,
unless those tools or the user delete them.

## Redaction policy

Beacon currently preserves captured content rather than redacting it. This keeps
transcripts, search, diagnostics, and MCP retrieval faithful to the source
session data, but it also means secrets copied into prompts, responses, tool
arguments, file paths, or tool outputs may be stored and indexed.

Any future redaction feature should define:

- which fields are redacted before insertion into raw records, activity events,
  tool payloads, and search documents;
- whether redaction is reversible or destructive;
- how existing ClickHouse data is backfilled or invalidated;
- tests that prove redacted content does not appear in raw payloads, previews,
  search indexes, web API responses, or MCP tool output.

Until such a feature exists, use Beacon only on machines and ClickHouse
instances whose local data access is trusted.
