# Privacy, retention, and local data boundaries

Beacon is a local observability tool. It reads agent session files that already
exist on the machine, normalizes them, and stores derived rows in ClickHouse so
the dashboard, search UI, and MCP tools can query them quickly.

For the advanced multi-machine personal-production setup and operator runbooks,
see [Advanced personal production guide](production.md). The default `beacon up`
workflow keeps capture, storage, and the dashboard local.

Beacon does not implement retention expiry. Beacon applies a best-effort
destructive redaction policy before local capture writes, collector spool files,
and HTTP ingest commits. Treat the Beacon database and collector spools as
sensitive local data even after redaction.

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

Beacon also stores a small control-plane metadata database containing the local
owner instance ID, schema epoch, node and collector IDs, source names, runtime
metadata, platform, hostname, configured source roots, and token metadata for
owner, enrollment, read/admin, and ingest access. Token metadata includes token
hashes, non-secret prefixes, scopes, status, expiry, revocation timestamps, and
node/collector/source bindings. Plain control-plane token values are shown once
by CLI setup commands and are not stored by the control plane. Remote collectors
store their bound ingest token in the configured `[fleet].ingest_token_file`
with owner-only permissions. This metadata is separate from captured ClickHouse
data so reset/replay coordination can keep stable local identity.

The ClickHouse schema and table ownership are documented in
[clickhouse.md](clickhouse.md).

## Storage locations

Beacon's default config file is `~/.beacon/beacon.toml`.

Beacon's default control-plane metadata database is
`~/.beacon/control-plane.db`, with SQLite sidecar files such as
`~/.beacon/control-plane.db-wal` and `~/.beacon/control-plane.db-shm` when SQLite
uses WAL mode. Beacon creates these files owner-readable only. `beacon status`
reports the metadata path, schema epoch, and counts for nodes, collectors, and
sources when the metadata store has been initialized.

`beacon init` prints owner and enrollment tokens once. `beacon join` prompts
securely on an interactive terminal and also accepts enrollment tokens through
stdin, an environment variable name, or an invite file. The advanced
`beacon enroll` primitive accepts enrollment tokens through stdin or an
environment variable name. These paths keep tokens out of process listings.

Remote-safe `beacon collect` writes pending HTTP ingest batches under
`~/.beacon/spool` by default. The spool directories are owner-only and batch
files are checksummed owner-only JSON files. Collector redaction runs before a
batch is written to spool or sent to the control plane.

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

The MCP server exposes the same local database through read-only tools:
`search_sessions`, `open`, `list_agents`, `list_sessions`, and
`usage_summary`. Those tools can return transcript context, session summaries,
machine, collector, source, runtime, and project rollups, token-usage aggregates,
working directories, and source/project metadata. MCP clients should be
configured only for trusted agent environments, especially when pointing Beacon
at a ClickHouse host or remote control plane reachable from another machine.

Search results use a derived index, but the source content may still be present
in raw records, activity events, tool payloads, previews, and transcript views.
Deleting only search rows is not a privacy cleanup.

## Dashboard browser hardening

Beacon sets dashboard security headers including `Content-Security-Policy`,
`X-Content-Type-Options`, `Referrer-Policy`, `X-Frame-Options`, and a restrictive
`Permissions-Policy`. The CSP uses `script-src 'self'` and does not allow inline
JavaScript execution. Dashboard controls are wired through external scripts and
server-rendered captured content is escaped by default.

Current machine-to-machine POST routes are HTTP ingest and MCP JSON-RPC routes;
they are protected by token authentication and are not browser form mutation
surfaces. If Beacon adds browser-driven mutation routes such as token
management, reset, enrollment approval, or admin settings, those routes should
require same-origin proof or CSRF protection and must not mutate state via GET.

## Retention policy

Beacon keeps captured data until the user removes it. There is no automatic TTL,
per-project expiration, or redaction pass.

Current cleanup options:

- `beacon db reset --force` drops and recreates Beacon-owned ClickHouse tables in
  the configured database. This deletes Beacon data from those tables but does
  not remove original agent session files or the control-plane metadata database.
- `curl -sSfL https://johnnygreco.dev/beacon/install.sh | UNINSTALL=1 sh`
  removes the installed `beacon` binary and deletes `~/.beacon`, including
  Beacon-managed native ClickHouse data and the control-plane metadata database.
- For Docker ClickHouse, `beacon db reset --force` deletes Beacon table data.
  Removing the Docker volume `beacon-clickhouse-data` is a separate Docker
  operation and is destructive to that volume.
- For remote ClickHouse, delete or reset the configured database according to
  that deployment's operational policy.

Original agent session files remain in their source locations, such as
`~/.claude`, `~/.codex`, `~/.hermes`, `~/.local/share/opencode`, or `~/.pi`,
unless those tools or the user delete them.

## Redaction policy

Beacon runs `redact-v1` before data reaches durable capture storage:

- local capture redacts normalized events before ClickHouse insert rows are
  built;
- `beacon collect` redacts normalized events, capture errors, and checkpoints
  before writing collector spool files;
- HTTP ingest redacts accepted batches before committing rows to ClickHouse.

The policy is destructive and best effort. It covers Beacon token formats,
common credential formats such as bearer/basic auth, GitHub/OpenAI/Anthropic/AWS
style tokens, private-key blocks, URL credentials, common credential assignment
keys, configured `[redaction].path_masks`, configured `[redaction].env_masks`,
configured `[redaction].literal_masks`, and explicit fixture values used by
tests.

This is personal-production hardening, not enterprise DLP. Beacon does not
claim to detect every arbitrary secret pasted into a prompt, response, tool
argument, path, or tool output. Data that does not match the configured policy
can still be stored and indexed. Existing ClickHouse data is not automatically
backfilled when the policy changes; reset/replay or reingest is required if you
want old rows rewritten under a new policy.

Dashboard, API, MCP, raw-table, search, log, and spool leak-prevention tests are
checks that protected surfaces do not re-expose values already matched by the
configured write-boundary policy. They are not a second read-time classifier or
policy engine.
