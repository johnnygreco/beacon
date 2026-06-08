# Errors and observability

Beacon keeps user-facing errors short and actionable while preserving internal
details in logs or persisted diagnostics.

## User-facing messages

- Validation and not-found errors should say what the caller can fix, such as
  `invalid limit`, `event_id is required`, or `session not found`.
- Backend, database, scan, and serialization failures should not expose raw
  driver errors, SQL fragments, credentials, filesystem paths, or stack details
  in HTTP JSON or MCP tool responses.
- CLI commands may return wrapped errors because they are operator-facing, but
  the top-level message should name the failed operation, for example
  `loading config` or `opening clickhouse store`.

## Internal diagnostics

- Web JSON handlers use generic response text for internal failures and log the
  raw error with operation context.
- MCP tool calls return public tool errors to the client. Internal tool
  failures log the tool name, public message, and wrapped cause.
- Dashboard query helpers that feed best-effort panels may log and return empty
  data when a partial panel failure should not take down the whole page.
- Capture stores parse and ingest failures in `capture_errors` with fleet, batch,
  and source coordinates where possible, and logs watcher or batcher failures
  with source context.

## Remote collector recovery

- `collector spool is full` means local source reads paused before advancing the
  spooled checkpoint. Restore ingest connectivity or raise `fleet.spool_max_bytes`
  before deleting source files that have not been acknowledged.
- Corrupt spool files are moved to the `quarantine` directory and scanning stops
  while quarantine is non-empty. Inspect the quarantined JSON for disk or manual
  edit damage, then move it out of the spool or reset the collector only after
  confirming the affected source data can be reread.
- `401` and `403` ingest responses are terminal for the pending batch. Re-enroll
  the collector or fix token placement, then restart collection; Beacon will not
  keep reposting the same terminal batch automatically.
- `409` epoch or sequence conflicts indicate stale collector metadata or a broken
  batch order. Re-run `beacon enroll` against the active control plane before
  clearing local spool state.

## Tests

Representative tests should cover both sides of this policy: the response body
must stay useful and sanitized, while direct/internal errors retain enough
context for debugging.
