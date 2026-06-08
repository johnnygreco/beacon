const assert = require("node:assert/strict");
const test = require("node:test");

const { contracts, validateContract } = require("../contracts/api-contracts.cjs");

test("shared API contracts reject drifted fixture payloads", () => {
  assert.ok(contracts.APIDashboardSessionsResponse);
  assert.doesNotThrow(() => validateContract("APIDashboardSessionsResponse", {
    state: "completed",
    range: "24h",
    offset: 0,
    limit: 30,
    scope: {
      auth_scope_applied: false,
      filters: {},
    },
    has_more: false,
    items: [{
      id: "session-1",
      title: "Session",
      source: "codex",
      provider: "openai",
      status: "completed",
      started_at: "2026-05-22T10:00:00Z",
      ended_at: "2026-05-22T10:05:00Z",
      duration: "5m",
      turn_count: 1,
      total_tokens: 100,
      input_tokens: 50,
      output_tokens: 40,
      cache_read_tokens: 10,
      cache_create_tokens: 0,
      tool_call_count: 2,
      mcp_call_count: 0,
      error_count: 0,
      last_model: "gpt-5.4",
      working_dir: "/tmp/beacon",
      has_session_end: true,
      subagent_count: 0,
    }],
  }));

  assert.throws(() => validateContract("APIDashboardSessionsResponse", {
    State: "completed",
    items: [],
  }), /missing required field|unexpected field/);
});
