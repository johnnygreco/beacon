# Trace Annotation QA

Issue: #306
Branch: `issue-306-annotation-qa-polish`
Feature base: `feature/trace-annotations`
Date: 2026-06-17

## Scope

This QA pass covers the complete trace annotation workflow:

- human session, message, and event annotation from the transcript UI;
- agent annotation through MCP write/read tools;
- annotated-trace discovery and dataset export through JSON APIs;
- keyboard/focus, mobile failure, and axe accessibility checks;
- local installed Beacon review server smoke validation.

## Screenshot Matrix

| Image | Scenario | Evidence |
| --- | --- | --- |
| `images/01-desktop-transcript-empty-annotations.png` | Desktop transcript before annotation | Session strip starts at `0 annotations`; message/event controls are present without layout shifts. |
| `images/02-desktop-session-annotation-drawer.png` | Desktop session annotation | Session drawer saves structured fields, labels, and note; session summary count updates. |
| `images/03-desktop-message-annotation-drawer.png` | Desktop message annotation | Message-level annotation saves quality, confidence, labels, follow-up, and note. |
| `images/04-desktop-message-edit-state.png` | Edit state | Existing message annotation loads back into the form for editing. |
| `images/05-desktop-message-deleted-empty-state.png` | Delete/empty state | Soft delete removes the message annotation from the visible list and returns focus to New. |
| `images/06-desktop-timeline-event-annotation.png` | Timeline event annotation | Timeline event drawer saves an event-level annotation and displays event count state. |
| `images/07-mobile-annotation-failure-state.png` | Mobile error state | Annotation API failure is visible in the drawer without horizontal overflow. |
| `images/08-annotated-trace-discovery-json.png` | Annotated trace discovery API | `beacon.annotated_traces.index.v1` response includes counts, target summaries, paging, and scope. |
| `images/09-annotated-trace-export-json.png` | Dataset export API | `beacon.annotated_traces.export.v1` response includes session metadata, annotations, events, and truncation flags. |

Screenshots are reproducible with:

```bash
npx playwright test tests/e2e/trace-annotations-qa.spec.ts --reporter=line
```

## MCP Evidence

Concrete MCP request/response excerpts are recorded in
`mcp-annotation-workflow.json`.

Validated paths:

- `create_annotation` writes `author_type: "agent"` and `source: "mcp"`.
- Message targets return both `event_id` and `message_id`.
- Returned `open_ref` can be passed back to `list_annotations`.
- `update_annotation` increments revision and keeps source attribution as MCP.
- `delete_annotation` soft-deletes and returns `status: "deleted"`.

## API Evidence

Concrete annotated-trace API excerpts are recorded in
`annotated-trace-api-workflow.json`.

Validated paths:

- discovery uses schema `beacon.annotated_traces.index.v1`;
- export uses schema `beacon.annotated_traces.export.v1`;
- pagination fields are `limit`, `offset`, and `has_more`;
- export carries `event_limit`, per-trace `event_truncated`, and top-level
  `warnings`;
- filters can select by label plus the normal Beacon scope filters.

## Validation Log

Focused validation completed while producing this report:

| Command | Result |
| --- | --- |
| `npx playwright test tests/e2e/dashboard.spec.ts --grep "transcript annotation\|timeline event annotations"` | Passed |
| `npx playwright test tests/e2e/a11y.spec.ts --grep "annotation"` | Passed |
| `npx playwright test tests/e2e/trace-annotations-qa.spec.ts --reporter=line` | Passed |

Full validation required by #306:

| Command | Result |
| --- | --- |
| `make generate-check` | Passed |
| `make fmt-check` | Passed |
| `make test` | Passed |
| `make build` | Passed |
| `make lint` | Passed |
| `npm run test:frontend` | Passed |
| `npm run test:e2e` | Passed on rerun; first run had one transient dashboard-search scroll timeout while all annotation tests passed. |
| `npm run test:a11y` | Passed |
| `npm run test:visual` | Passed after updating transcript baselines for first-class annotation controls. |
| `make install-local INSTALL_DIR="$HOME/.local/bin"` | Passed |
| `tmux` review server restart and `curl -fsS http://localhost:4600/ >/dev/null` | Passed |

## Served Build Check

Review URL: `http://localhost:4600/`

The installed binary was started with:

```bash
$HOME/.local/bin/beacon --config /tmp/beacon-trace-annotations-dev.toml up
```

Server log evidence:

```text
starting beacon host=127.0.0.1 port=4600 database=beacon_trace_annotations_dev
server listening addr=127.0.0.1:4600
```

Manual browser checks against the served build:

- dashboard rendered at `http://localhost:4600/`;
- a real local transcript rendered from
  `session_1f4b481c206dc8098a3eb53c99a03236`;
- the transcript showed the session annotation strip and inline message
  annotation controls.

## Bugs Found And Fixed

- Timeline event annotation lacked a dedicated browser regression path. Added an
  E2E test that asserts `target_type: "event"` and the expected `event_uid`.
- Annotation drawer accessibility was only covered indirectly. Added axe checks
  for the open drawer and mobile annotation failure state.
- The mobile annotation panel used the general card background, which let
  underlying transcript content bleed through in QA screenshots. The drawer
  panel now uses the opaque dashboard background.

## Remaining Risk

No blocking risk is known. The committed screenshots use deterministic E2E
fixtures; final local-server smoke validation verifies the installed binary and
served dashboard separately.
