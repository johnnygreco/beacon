# Dashboard Battle Test Findings

Final isolated install run: 2026-05-11, local temporary install.

## Environment

- Installed current branch binary into `/tmp/beacon-battle-64973/install/bin`.
- Used isolated HOME/config/trace data under `/tmp/beacon-battle-64973`.
- Used fresh ClickHouse database `beacon_battle_fix2_65143`.
- Served the installed binary on `http://127.0.0.1:4612` with capture enabled, backfill enabled, 2s reconcile, and 2s search rebuild.

## Coverage

- Real JSONL capture for 30 completed Claude-style sessions, one active session, one sidechain subagent, tool calls, an MCP-style tool call, a tool error, multi-model token usage, and an unattributed model token row.
- Dashboard API projections for active/completed sessions, charts, activity feed, pagination, token totals, and subagent counts.
- Desktop dashboard controls: completed search, search clear, theme select, dark/light switch, fixed dark theme lockout, timeline collapse/restore, chart canvas rendering, completed row keyboard open, inspector close.
- Transcript controls: chat/timeline switch, session Tokens by Model chart, real SSE-driven partial refresh after appending to the watched JSONL file, and preservation of the active timeline view after refresh.
- Mobile dashboard viewport check for horizontal overflow.

## Findings And Fixes

- Found a token attribution bug: multiple Claude content blocks parsed from one JSONL line shared the same deterministic event UID, so ClickHouse replacement could collapse the token-bearing event against a later tool event.
- Found the paired parser/dedup bug: the Claude parser assigned usage only to the first block, but token dedup keeps the last block for a shared message UUID. Multi-block assistant messages with tools could therefore lose token totals and leave session/model charts empty.
- Fixed event UIDs to include a deterministic per-line ordinal for secondary events while preserving ordinal-0 IDs.
- Fixed Claude parsing so every block carries usage before dedup, leaving exactly one token-bearing event after dedup.
- Added unit coverage for per-line event UID ordinals and multi-block Claude usage surviving dedup.

## Final Result

- Real captured completed sessions show nonzero token totals.
- Session detail Tokens by Model chart renders nonblank with `sonnet-4` and `opus-4`.
- Dashboard model/token charts render nonblank.
- Live append to the watched JSONL updates the open transcript through session SSE and preserves the Timeline view.
- Completed search, theme controls, fixed dark lockout, timeline collapse/restore, keyboard session open, inspector close, and mobile overflow checks pass.
