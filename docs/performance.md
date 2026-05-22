# Performance baselines and query-plan review

This page records the initial Beacon ClickHouse performance baseline for issue
`#68` and documents the local regression process for query-heavy changes.

Perf runs use the disposable `beacon_perf` database. Do not point
`BEACON_TEST_CLICKHOUSE` at a database that contains user data.

## Repeatable process

Start local ClickHouse once:

```bash
go run ./cmd/beacon db up
```

Capture benchmark timings:

```bash
BEACON_TEST_CLICKHOUSE=127.0.0.1:9000 PERF_SIZE=medium make perf-bench
```

Capture ClickHouse plans for representative dashboard, search, transcript, and
MCP queries:

```bash
BEACON_TEST_CLICKHOUSE=127.0.0.1:9000 PERF_SIZE=medium make perf-explain
```

Use `PERF_SIZE=small|medium|large` for fixture scale. For PRs that touch
`internal/web/queries.go`, `internal/search/search.go`, `internal/mcp/tools.go`,
or ClickHouse schema/projection code, run at least `medium`; run `large` when
the change alters query shape, table order keys, projections, or search index
paths. For noisy comparisons, prefer:

```bash
BEACON_TEST_CLICKHOUSE=127.0.0.1:9000 PERF_SIZE=medium PERF_BENCHTIME=3s PERF_COUNT=3 make perf-bench
```

Compare the same benchmark names against `main` on the same machine and
ClickHouse version. Re-run outliers before treating them as regressions.

## Baseline environment

Captured on 2026-05-22 with:

- CPU: Apple M4
- Memory: 16 GiB
- OS/runtime: darwin/arm64
- Go: 1.26.0
- ClickHouse: 24.12.6.70
- Benchmark settings: `PERF_BENCHTIME=1s`, `PERF_COUNT=1`

Fixture seed counts from `internal/perf`:

| Size | Sessions | Events | Tool payloads | Seed time |
| --- | ---: | ---: | ---: | ---: |
| small | 250 | 22,954 | 14,784 | 490 ms |
| medium | 2,500 | 259,679 | 166,844 | 5.587 s |
| large | 10,000 | 904,408 | 578,966 | 19.466 s |

## Timing baseline

Values are `ms/op` from `make perf-bench`.

| Benchmark | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `QueryDashboardData` | 42.11 | 60.50 | 72.17 |
| `QueryDashboardSessions` | 6.42 | 6.75 | 7.69 |
| `QueryActiveSessions` | 2.46 | 2.52 | 2.97 |
| `QuerySessionConversation_Small` | 7.73 | 14.99 | 19.49 |
| `QuerySessionConversation_Large` | 8.59 | 19.44 | 23.47 |
| `SearchBM25` | 12.48 | 22.89 | 40.92 |
| `SearchKeyword` | 10.74 | 16.58 | 33.32 |
| `SearchBrowse` | 2.32 | 11.05 | 25.10 |
| `QueryRecentActivity` | 4.62 | 11.46 | 20.12 |
| `QueryTokensTimeSeries` | 2.04 | 2.26 | 1.97 |
| `QuerySessionDetail` | 7.91 | 9.77 | 10.13 |
| `APIDashboardJSON/Metrics` | 0.90 | 1.65 | 1.66 |
| `APIDashboardJSON/ActiveSessions` | 1.30 | 2.53 | 2.96 |
| `APIDashboardJSON/CompletedSessions` | 2.27 | 4.02 | 4.65 |
| `APIDashboardJSON/Activity` | 4.64 | 11.54 | 20.63 |
| `APIDashboardJSON/Charts` | 41.15 | 59.06 | 67.42 |
| `APIDashboardJSON/TokensPerMinute` | 2.49 | 3.76 | 3.91 |
| `APIDashboardJSON/ToolStats` | 2.48 | 4.40 | 3.88 |
| `APIDashboardJSON/TokensByModel` | 2.54 | 4.25 | 3.70 |

## Query-plan review

`make perf-explain` was run for small, medium, and large fixtures. Plan shapes
were stable across sizes; larger fixtures increased granule counts and set
sizes. The checked-in test prints full plans with `go test -v`, while this table
captures the baseline plan findings that matter for reviews.

| Workload | Baseline plan shape | Review finding |
| --- | --- | --- |
| Active dashboard sessions | `session_projection FINAL`, filter, sort, limit. Large fixture reads 6/6 granules. | `ended_at` and `has_session_end` are not leading key fields, so active-session refresh scans the projection and pays `FINAL` before sorting. |
| Completed sessions with filtering/sorting | `session_projection FINAL`, string filters, sort, limit. Large fixture reads 6/6 granules. | Text filters and alternate sort keys cannot use the `session_id` order key; keep page limits tight and watch this path when adding filters. |
| Dashboard model analytics | Multiple `analytics_projection FINAL` CTE reads, `CreatingSets`, joins, grouping, and `last_value ... OVER (PARTITION BY session_id ...)` window sorts. Large fixture reads 50/51 analytics granules in the filtered branches. | This is the hottest dashboard chart path. The expensive operations are repeated `FINAL` projection scans and per-session window sorting. |
| Transcript open for a large session | `activity_events` prunes by `session_id` to 1 granule, then groups by `event_uid`, computes per-session windows, and joins payload previews by event set. | The primary key makes this scale with target session length rather than database size. Very large sessions still pay window and payload grouping costs. |
| Search BM25 | `search_postings FINAL` prunes by token, then computes `count() OVER (PARTITION BY token)`, groups by `event_uid`, and sorts by score. Large fixture reads 42/1148 posting granules for `binary search`. | Token key pruning works, but posting-frequency windows and final score grouping scale with matched posting volume. |
| Search browse | `search_documents FINAL` uses timestamp min-max and partition pruning, then filters and sorts by timestamp. Large fixture reads 114/114 current-month granules. | The table is ordered by `event_uid`, so timestamp browse cannot use the primary key after partition pruning. |
| MCP `open` context | First lookup by `event_uid` scans `activity_events`; the follow-up session scan prunes to 1 granule, then computes row-number windows around the target. | Context retrieval avoids full-session materialization after the target session is known, but `event_uid` is not a leading key in `activity_events`. |
| MCP `list_sessions` | `session_projection` scan with `argMax(..., updated_at)` grouped by session and sorted by `started_at`. | The review caught and fixed a ClickHouse alias bug in the `since` filter by qualifying `sp.started_at`; without that, ClickHouse rewrote the filter to the aggregate alias. |

## Follow-up candidates

- Evaluate whether `session_projection FINAL` can be replaced with explicit
  `argMax(..., updated_at)` for dashboard pages without regressing correctness.
- Split or precompute dashboard model attribution so the 24-hour chart path does
  not repeatedly scan `analytics_projection FINAL` and window-sort by session.
- Consider a lightweight lookup table or secondary projection for MCP/event
  opens keyed by `event_uid`.
- Consider a timestamp-oriented search browse projection if browse latency
  becomes user-visible on multi-million-event local stores.
