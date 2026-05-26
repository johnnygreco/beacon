# Dashboard Battle Test Findings

Final dashboard epic run: 2026-05-26, deterministic local fixtures plus CI.

## Environment

- Branch: final dashboard stability, UX, naming, transcript-return, and visual QA work for epic #131.
- Browser matrix: Playwright Chromium desktop, narrow desktop, mobile-sized viewports, accessibility scans, and Darwin visual snapshots.
- Data source: deterministic Playwright route fixtures in `tests/e2e/fixtures/dashboard.ts`, with active, many-active, empty, error-heavy, delayed search, stale search, failed API, and transcript scenarios.
- Prior isolated install run from 2026-05-11 remains the real-capture regression reference for JSONL ingestion, ClickHouse projections, transcript SSE refresh, and nonblank token charts.

## QA Matrix

- Scroll stability: search typing, delayed responses, stale unabortable responses, Escape clear, input clear, filter changes, range changes, sort, pagination, SSE updates, timeline toggle, and active-session height changes.
- Search table: query, filters, reset, no custom clear button, show-more pagination, unavailable/error states, narrow-screen containment, and no row/header overlap.
- Active sessions: default, no-active, many-active, high/over/unknown context states, accessible progress bars, active section prominence, and context-bar visual baseline.
- Analytics: single tokens-by-model time chart, range reloads, model dropdown, log toggle, tooltip anchoring, theme retinting, and nonblank chart geometry.
- Timeline: activity filters, collapse/expand, persisted collapse before paint, resize drag, keyboard resize, inert/aria states, and transcript event deep links.
- Dashboard identity: editable dashboard name, tab title persistence, configured fallback, local clear behavior, and unsafe text escaping.
- Transcript return: inspector, search result, and activity transcript paths restore dashboard URL state, including sort/page state; direct transcript loads fall back to `/`; unsafe saved state is rejected.
- Responsive and visual QA: desktop `1440x900`, narrow desktop, `390x844`, `320x568`, light theme, fixed-dark theme, dashboard mobile, transcript mobile, and themed transcript snapshots.

## Added In Final Pass

- Added an input clear-path test to verify clearing the `type="search"` field does not move `window.scrollY`, `#main-content.scrollTop`, or `#dashboard-main.scrollTop`.
- Extended transcript-return coverage so sort direction is preserved alongside range, pagination, and scroll state.
- Added a direct transcript fallback assertion before the unsafe saved-state check, so the breadcrumb stays `/` when no dashboard return state exists.
- Updated this findings document to describe the final battle-test matrix and validation commands used for the dashboard epic.

## Commands

```bash
npm run lint:js
npm run test:unit
npx playwright test tests/e2e/dashboard.spec.ts -g "initializes dashboard"
npx playwright test tests/e2e/dashboard-search.spec.ts -g "input clear path"
make test
make generate-check
npm run test:e2e
npm run test:a11y
npm run test:visual
git diff --check
```

## Final Result

- The dashboard search area stays anchored during typing, clearing, delayed searches, stale responses, filters, range changes, sorting, pagination, and live updates.
- Active sessions are the first dashboard content when present, with prominent stats and accessible context progress indicators.
- The chart surface is reduced to one tokens-by-model time chart with stable controls and visual baselines.
- Dashboard names update the visible heading and browser tab safely, persist locally, and fall back cleanly.
- Transcript breadcrumbs restore dashboard range, search, activity, sort, pagination, and scroll state for dashboard-originated navigation while preserving safe fallback behavior for direct transcript loads.
