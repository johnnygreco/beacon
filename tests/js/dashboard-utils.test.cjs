const assert = require("node:assert/strict");
const test = require("node:test");

const utils = require("../../static/js/dashboard/utils.js");

test("dashboard text helpers escape HTML and attributes", () => {
  assert.equal(utils.escapeHTML(`<span data-x="1">&'</span>`), "&lt;span data-x=&quot;1&quot;&gt;&amp;&#39;&lt;/span&gt;");
  assert.equal(utils.escapeAttr("`quoted`"), "&#96;quoted&#96;");
  assert.equal(utils.escapeAttr(`" onmouseover="alert(1)`), "&quot; onmouseover=&quot;alert(1)");
});

test("dashboard numeric helpers normalize untrusted API fields", () => {
  assert.equal(utils.numericValue("12.5", 0), 12.5);
  assert.equal(utils.numericValue(`1"><script>alert(1)</script>`, 7), 7);
  assert.equal(utils.nonNegativeInt("4.8"), 4);
  assert.equal(utils.nonNegativeInt("-3"), 0);
  assert.equal(utils.formatTokens(`"><img src=x onerror=alert(1)>`), "0");
});

test("dashboard URL helper preserves empty range but omits empty filters", () => {
  assert.equal(
    utils.requestURL("/api/dashboard/sessions", { state: "completed", completed_range: "", event_kind: "" }),
    "/api/dashboard/sessions?state=completed&completed_range=",
  );
  assert.equal(
    utils.requestURL("/api/dashboard/charts", { chart_range: "", event_kind: "" }),
    "/api/dashboard/charts?chart_range=",
  );
});

test("dashboard formatters normalize IDs, models, tokens, and durations", () => {
  assert.equal(utils.shortID("abcdef123456"), "abcdef12");
  assert.equal(utils.shortModel("claude-sonnet-4-5-20250929"), "sonnet-4-5");
  assert.equal(utils.providerShort("openai"), "Codex");
  assert.equal(utils.formatTokens(1250), "1.3K");
  assert.equal(utils.durationSeconds({ started_at: "2026-05-22T10:00:00Z", ended_at: "2026-05-22T10:02:05Z" }), 125);
});

test("dashboard relative time is deterministic when a clock is supplied", () => {
  const now = Date.parse("2026-05-22T10:00:00Z");
  assert.equal(utils.relativeTime("2026-05-22T09:59:45Z", now), "just now");
  assert.equal(utils.relativeTime("2026-05-22T09:45:00Z", now), "15m ago");
  assert.equal(utils.relativeTime("2026-05-22T07:00:00Z", now), "3h ago");
  assert.equal(utils.relativeTime("2026-05-20T10:00:00Z", now), "2d ago");
});
