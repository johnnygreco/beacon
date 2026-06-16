# Performance baselines and query-plan review

This page records the initial Beacon ClickHouse performance baseline for issue
`#68` and documents the local regression process for backend, browser, and
query-heavy changes.

Perf runs use the disposable `beacon_perf` database. Do not point
`BEACON_TEST_CLICKHOUSE` at a database that contains user data.

## Repeatable process

Run the fast non-ClickHouse backend benchmark suite:

```bash
make perf-fast
```

`make perf-fast` covers capture parsing and event normalization, batch row
construction, search tokenization and index row construction, MCP formatting and
fake dispatch, API model shaping and JSON serialization, and dashboard/chat view
rendering. Filter by component with `PERF_FAST_BENCH`, for example:

```bash
PERF_FAST_BENCH='Benchmark(Capture|MCP)' make perf-fast
```

Run a local end-to-end performance lab smoke report:

```bash
make perf-lab-smoke
```

The smoke lab installs the current workspace binary, resets and seeds the
disposable `beacon_perf_lab` ClickHouse database, starts Beacon on a lab port
with capture disabled, runs short fast Go benchmarks, runs selected live
ClickHouse search/MCP benchmarks, drives browser performance against the served
dashboard, and writes:

- `test-results/perf/lab/latest/perf-lab-report.json`
- `test-results/perf/lab/latest/perf-lab-report.md`
- `test-results/perf/lab/latest/browser-performance.json`
- `test-results/perf/lab/latest/beacon-server.log`

The lab requires ClickHouse to be reachable before seeding. Start local
ClickHouse with `go run ./cmd/beacon db up`, or point the lab at an existing
instance with `PERF_LAB_CLICKHOUSE` / `--clickhouse`. The runner refuses invalid
database identifiers and refuses to reset databases whose names do not start
with `beacon_perf` unless `--allow-unsafe-database-reset` is passed. Keep the
default database for PR smoke runs. Live benchmarks use a separate disposable
database by default (`<served-database>_bench`) so they can reset and seed
without disturbing the dashboard server.

Useful lab controls:

| Variable / argument | Default | Purpose |
| --- | --- | --- |
| `PERF_LAB_SIZE`, `--size` | `small` | Synthetic dataset size for the served lab app and live benchmarks: `small`, `medium`, `large`, or heavy opt-in `stress`. |
| `PERF_LAB_CLICKHOUSE`, `--clickhouse` | `127.0.0.1:9000` | ClickHouse address used for seeding and live benchmarks. |
| `PERF_LAB_DATABASE`, `--database` | `beacon_perf_lab` | Disposable ClickHouse database for the served lab app. |
| `PERF_LAB_LIVE_DATABASE`, `--live-database` | `<database>_bench` | Disposable ClickHouse database reset by live benchmarks. Must use a `beacon_perf*` name. |
| `PERF_LAB_OUTPUT_DIR`, `--output-dir` | `test-results/perf/lab/latest` | Report directory. |
| `PERF_LAB_BASE_URL`, `--base-url` | unset | Use an already-running Beacon server instead of starting one; also pass `--skip-live` because the lab cannot verify or reset the external server's database. |
| `PERF_LAB_ARGS` | unset | Extra arguments passed by `make perf-lab-smoke` or `make perf-lab`. |
| `--skip-fast`, `--skip-live`, `--skip-explain`, `--skip-browser` | false | Disable specific layers for focused local runs. `--skip-live` also skips query-plan assertions because the lab cannot safely reset a verified live benchmark database. |
| `--fast-benchtime`, `--live-benchtime` | smoke target: `100ms` | Go benchmark duration knobs. |
| `--browser-repeats` | smoke target: `1` | Browser repeats per viewport. |

## Stress Validation Profiles

The perf seed uses generic local source metadata rather than a required host or
agent topology. Every profile spreads sessions across five runtime adapters,
mixed projects, active sessions, idle sessions, tool payloads, and a
high-frequency common search token (`commonsearch`) that is exercised with
source-name and project filters. Specific adapter names in the fixtures are
sample source data, not product requirements.

| Size | Intended use | Sessions | Active | Idle | Target events | Notes |
| --- | --- | ---: | ---: | ---: | ---: | --- |
| `small` | CI/local smoke | 250 | 25 | 50 | ~25k | Default for `make perf-lab-smoke`. |
| `medium` | PR query review | 2,500 | 250 | 500 | ~250k | Use for ClickHouse query-shape changes. |
| `large` | Manual preflight | 10,000 | 750 | 1,500 | ~900k | Use before manual production testing when query paths changed materially. |
| `stress` | Heavy opt-in lab | 100,000 | 2,500 | 2,500 | ~15M | Approximates the target personal-production stress profile, including ~1M payloads and at least 100M search postings. Do not run by default in CI. |

Perf reports record runtime metadata, git revision, seeded counts, target
profile dimensions, common search token, and scoped source/project values used
by the common-token search benchmark. Actual seeded counts are the source of
truth for a run; search postings are validated as a minimum floor because
tokenizer and payload-shape changes can legitimately push the actual count above
the 100M target. These are local validation gates, not public benchmark claims or
SLA evidence.

To compare branches, run the same command with different output directories:

```bash
PERF_LAB_OUTPUT_DIR=test-results/perf/lab/main make perf-lab-smoke
git switch feature-branch
PERF_LAB_OUTPUT_DIR=test-results/perf/lab/feature make perf-lab-smoke
```

Compare the Markdown summaries first, then inspect the JSON reports for exact
benchmark records and browser metric percentiles.

The smoke defaults are quick signal, not a statistical baseline. For branch
comparisons, raise `--fast-benchtime`, `--live-benchtime`, and
`--browser-repeats`, and compare reports from the same machine and ClickHouse
version.

Check the latest lab report against Beacon's built-in smoke budgets:

```bash
make perf-budget
```

`make perf-budget` reads `test-results/perf/lab/latest/perf-lab-report.json` by
default. Override the report path with `PERF_REPORT`, and pass checker flags with
`PERF_CHECK_ARGS`. Budget failures print the exact metric, observed value, and
limit. Missing budgeted metrics fail by default because a full smoke report
should include browser, fast Go, and live ClickHouse slices.

Compare two saved lab reports:

```bash
PERF_LAB_OUTPUT_DIR=test-results/perf/lab/main make perf-lab-smoke
git switch feature-branch
PERF_LAB_OUTPUT_DIR=test-results/perf/lab/feature make perf-lab-smoke
PERF_BASELINE=test-results/perf/lab/main/perf-lab-report.json \
PERF_REPORT=test-results/perf/lab/feature/perf-lab-report.json \
make perf-compare
```

`make perf-compare` checks the current report against budgets and flags material
regressions for overlapping metrics. Both reports must be passing perf lab JSON
reports, and at least one metric must overlap. By default, a comparison must
exceed both a 25% ratio increase and the minimum absolute delta (`5ms` for
browser millisecond metrics, `0.05ms/op` for Go benchmarks, `0.02` for
layout-shift score, and `1` for counts). Tune with `PERF_CHECK_ARGS`, for
example:

```bash
PERF_CHECK_ARGS='--max-regression-ratio=1.15 --min-browser-regression=10' make perf-compare
```

Smoke budgets intentionally cover the highest-signal local review paths:

| Area | Budgeted metrics | Smoke limit |
| --- | --- | ---: |
| Dashboard load | `dashboard.cold_load.ready` p95 | desktop `600ms`, mobile `700ms` |
| API waterfall | `dashboard.cold_load.api_max` p95 | desktop `250ms`, mobile `300ms` |
| Warm reload | `dashboard.warm_reload.ready` p95 | desktop `300ms`, mobile `350ms` |
| Dashboard search | `search.session.input_to_rows`, `search.event.input_to_rows` p95 | sessions `700/800ms`, events `800/900ms` desktop/mobile |
| Interactions | chart range, active sort, inspector open p95 | `150ms`-`400ms` depending on flow and viewport |
| Responsiveness | `browser.long_tasks.max`, `browser.layout_shift.cumulative` max | `50ms` long task, CLS `0.70` desktop / `0.25` mobile |
| Live ClickHouse search | `BenchmarkSearchBM25`, `BenchmarkSearchKeyword`, `BenchmarkSearchBrowse` | `30ms`, `25ms`, `8ms` per op |
| MCP tools | `BenchmarkMCPToolSearchSessions`, `BenchmarkMCPToolOpen`, `BenchmarkMCPToolListSessions` | `30ms`, `25ms`, `8ms` per op |
| Dashboard queries | `BenchmarkQueryDashboardData`, active/completed list queries | dashboard aggregate `200ms`; active/completed lists `15ms`-`20ms` |
| Fast Go families | capture parse/batch, search indexing, API shaping, MCP formatting, dashboard/chat rendering | exact per-benchmark limits in `cmd/perfcheck` |

The smoke budgets are local-review gates, not release claims. Treat one-sample
browser p95s as a regression signal that needs rerunning with higher
`--browser-repeats`; do not treat them as a statistically stable percentile.
For release or optimization decisions, compare repeated reports from the same
machine and ClickHouse version.

Recommended change-type validation:

| Change type | Required performance command |
| --- | --- |
| Parser, indexing, MCP formatting, API shaping, or view rendering hot paths | `make perf-fast` and `make perf-budget` if a lab report is generated |
| Dashboard JS, browser behavior, or user-facing workflow timing | `npm run test:perf:browser` or `make perf-lab-smoke`, then `make perf-budget` |
| ClickHouse query shape, projections, search, MCP live tools, or perf seed data | `go run ./cmd/beacon db up`, `PERF_SIZE=medium make perf-bench`, `PERF_SIZE=medium make perf-explain`, and `make perf-lab-smoke` |
| PRs intended to prove no end-to-end regression | `make perf-lab-smoke`, `make perf-budget`, and `make perf-compare` against a same-machine baseline |

Start local ClickHouse once:

```bash
go run ./cmd/beacon db up
```

Capture benchmark timings:

```bash
BEACON_TEST_CLICKHOUSE=127.0.0.1:9000 PERF_SIZE=medium make perf-bench
```

`make perf-bench` runs the live ClickHouse query/API suite, including MCP
`search_sessions`, `open`, and `list_sessions` tool flows through JSON-RPC
dispatch.

Capture ClickHouse plans for representative dashboard, search, transcript, and
MCP queries:

```bash
BEACON_TEST_CLICKHOUSE=127.0.0.1:9000 PERF_SIZE=medium make perf-explain
```

`make perf-explain` prints plans and asserts that dashboard paths stay on
projection tables, scoped common-token search stays on the search index with
source-name/project filters, and MCP/open transcript paths use the expected
tables. Set `BEACON_PERF_EXPLAIN_ASSERT=0` only when capturing diagnostic plans
from an incompatible ClickHouse version; do not use that override as PR
validation.

For `make perf-bench` and `make perf-explain`, use
`PERF_SIZE=small|medium|large|stress` for fixture scale. For PRs that touch
`internal/web/queries.go`, `internal/search/search.go`, `internal/mcp/tools.go`,
or ClickHouse schema/projection code, run at least `medium`; run `large` when
the change alters query shape, table order keys, projections, or search index
paths. Use `stress` only as a manual heavy preflight on a machine that can absorb
the ClickHouse seed. For noisy comparisons, prefer:

```bash
BEACON_TEST_CLICKHOUSE=127.0.0.1:9000 PERF_SIZE=medium PERF_BENCHTIME=3s PERF_COUNT=3 make perf-bench
```

The live perf package also includes a concurrent ingest/read smoke test. When
`BEACON_TEST_CLICKHOUSE` is set, it flushes ordinary row batches while dashboard
data, active-session APIs, scoped common-token search, and JSON API reads run
concurrently. Normal `make test` skips this test unless a ClickHouse perf
database is configured.

Capture failure harness coverage lives with the capture package tests:
rotation rollback, corrupt source handling, partial write artifacts,
and checkpoint advancement only after committed acknowledgements.

Compare the same benchmark names against `main` on the same machine and
ClickHouse version. Re-run outliers before treating them as regressions.

Capture browser page-load, search, and responsiveness timings with deterministic
Playwright fixtures:

```bash
npm run test:perf:browser
```

The browser run writes `test-results/perf/browser-performance.json` and prints a
compact per-viewport median/p95/max summary. It records desktop and mobile
viewport timings for dashboard cold load, warm reload, session search, event
search, chart range changes, active-session sorting, fixture-driven active SSE
refresh rendering, quick-view inspector open, API resource summaries, per-request
API waterfall entries, long tasks, and layout shifts.

To measure the actual local review server instead of fixture-routed responses,
start Beacon and point Playwright at it:

```bash
BEACON_BROWSER_PERF_FIXTURES=0 \
BEACON_E2E_BASE_URL=http://localhost:4600 \
npm run test:perf:browser
```

External live-server mode does not synthesize SSE events, so it omits the
fixture-only `interaction.active_sse_to_paint` metric unless future tooling adds
a deterministic live update trigger.

To run the integrated lab against an already-running Beacon server, skip live
ClickHouse benchmarks in that lab invocation and run `make perf-bench`
separately against a known disposable database:

```bash
PERF_LAB_ARGS='--base-url http://localhost:4600 --skip-live' make perf-lab
```

Useful browser perf controls:

| Variable | Default | Purpose |
| --- | --- | --- |
| `BEACON_BROWSER_PERF_REPEATS` | `3` | Repeats per viewport. |
| `BEACON_BROWSER_PERF_OUTPUT` | `test-results/perf/browser-performance.json` | JSON report path. |
| `BEACON_BROWSER_PERF_SEARCH_QUERY` | fixture mode: `dashboard payload`; live mode: `beacon` | Session-search query. |
| `BEACON_BROWSER_PERF_EVENT_QUERY` | fixture mode: `many`; live mode: search query | Event-search query. |

## Baseline environment

Captured on 2026-05-22 with:

- CPU: Apple M4
- Memory: 16 GiB
- OS/runtime: darwin/arm64
- Go: 1.26.0
- ClickHouse: 24.12.6.70
- Benchmark settings: `PERF_BENCHTIME=1s`, `PERF_COUNT=1`

Historical fixture seed counts from the earlier `internal/perf` baseline:

| Size | Sessions | Events | Tool payloads | Seed time |
| --- | ---: | ---: | ---: | ---: |
| small | 250 | 22,954 | 14,784 | 490 ms |
| medium | 2,500 | 259,679 | 166,844 | 5.587 s |
| large | 10,000 | 904,408 | 578,966 | 19.466 s |

## Timing baseline

Values are historical `ms/op` from `make perf-bench`. Current perf lab reports
record the exact seeded counts and stress profile metadata for each run.

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
