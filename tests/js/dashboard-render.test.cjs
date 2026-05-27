const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const vm = require("node:vm");

const utils = require("../../static/js/dashboard/utils.js");

function loadRenderSandbox() {
  const sandbox = {
    window: {
      BeaconDashboard: { utils },
      dashboardSessionIndex: {},
      AbortController: globalThis.AbortController,
    },
    document: {
      getElementById() {
        return null;
      },
      querySelector() {
        return null;
      },
    },
    currentSearchSort: "relevance",
    currentSearchQuery: "",
    currentSearchEventKind: "",
    currentSearchSessionID: "",
    currentSearchLimit: 30,
    currentRange: "24h",
    completedPageSize: 50,
    sessionTableHeadHTML: "",
    dashboardRequestSeq: {},
    dashboardControllers: {},
    updateCompletedSortIndicators() {},
    loadCompletedSessions() {},
  };
  vm.createContext(sandbox);
  const renderPath = path.join(__dirname, "../../static/js/dashboard/render.js");
  vm.runInContext(fs.readFileSync(renderPath, "utf8"), sandbox, { filename: renderPath });
  return sandbox;
}

function assertNoRawPayloadHTML(html) {
  for (const fragment of ["<script", "<img", "<iframe", "<object", "javascript:alert"]) {
    assert.equal(html.toLowerCase().includes(fragment), false, `unexpected raw fragment ${fragment} in ${html}`);
  }
  for (const fragment of [`onclick="alert`, `onmouseover="alert`, `onerror="alert`]) {
    assert.equal(html.toLowerCase().includes(fragment), false, `unexpected raw handler ${fragment} in ${html}`);
  }
  assert.equal(html.includes("NaN"), false, `unexpected NaN in ${html}`);
}

test("completed session rows escape malicious payload fields", () => {
  const sandbox = loadRenderSandbox();
  const payload = `"><img src=x onerror="alert(1)"><script>alert(1)</script>`;
  const html = sandbox.completedRow({
    id: `session-${payload}`,
    title: payload,
    provider: payload,
    last_model: payload,
    total_tokens: payload,
    context_tokens: payload,
    context_window_tokens: payload,
    turn_count: payload,
    tool_call_count: payload,
    duration: payload,
    working_dir: `/tmp/${payload}`,
    ended_at: payload,
    subagent_count: payload,
  }, false, "");

  assertNoRawPayloadHTML(html);
  assert.match(html, /&lt;img src=x onerror=&quot;alert\(1\)&quot;&gt;/);
  assert.match(html, /data-sort-tokens="0"/);
  assert.match(html, /data-sort-turns="0"/);
  assert.match(html, /data-sort-tools="0"/);
});

test("dashboard search rows escape snippets and metadata", () => {
  const sandbox = loadRenderSandbox();
  const payload = `"><img src=x onerror="alert(1)"><script>alert(1)</script>`;
  const html = sandbox.searchRow({
    session_id: `session-${payload}`,
    event_uid: `event-${payload}`,
    event_kind: payload,
    snippet: `match ${payload}`,
    tool_name: payload,
    provider: payload,
    model: payload,
    session_title: payload,
    working_dir: payload,
    relative_time: payload,
    score: `9${payload}`,
  });

  assertNoRawPayloadHTML(html);
  assert.match(html, /match &quot;&gt;&lt;img src=x onerror=&quot;alert\(1\)&quot;&gt;/);
});

test("active cards and activity feed escape JSON-rendered payloads", () => {
  const sandbox = loadRenderSandbox();
  const payload = `"><img src=x onerror="alert(1)"><script>alert(1)</script>`;
  const card = sandbox.activeCard({
    id: `session-${payload}`,
    title: payload,
    status: "active",
    provider: payload,
    last_model: payload,
    total_tokens: payload,
    turn_count: payload,
    tool_call_count: payload,
    duration: payload,
    working_dir: payload,
    child_sessions: [{
      id: `child-${payload}`,
      status: "active",
      last_model: payload,
      duration: payload,
      total_tokens: payload,
      context_tokens: payload,
      context_window_tokens: payload,
      tool_call_count: payload,
    }],
  });
  assertNoRawPayloadHTML(card);
  assert.match(card, /data-context-state="unknown"/);
  assert.equal(card.includes('role="progressbar"'), false);

  const feed = {};
  sandbox.document.getElementById = (id) => (id === "activity-feed" ? feed : null);
  sandbox.renderActivity([{
    id: `event-${payload}`,
    type: payload,
    summary: `activity ${payload}`,
    session_id: `session-${payload}`,
    provider: payload,
    relative_time: payload,
  }]);

  assertNoRawPayloadHTML(feed.innerHTML);
  assert.match(feed.innerHTML, /activity &quot;&gt;&lt;img src=x onerror=&quot;alert\(1\)&quot;&gt;/);
});

test("active cards render bounded accessible context progress", () => {
  const sandbox = loadRenderSandbox();
  const html = sandbox.activeCard({
    id: "active-context",
    title: "Context run",
    status: "active",
    provider: "openai",
    last_model: "gpt-5.4-codex",
    total_tokens: 1200000,
    context_tokens: 1200000,
    context_window_tokens: 1050000,
    context_estimate: true,
    turn_count: 9,
    tool_call_count: 3,
    duration: "12m",
    working_dir: "/tmp/beacon",
  });

  assertNoRawPayloadHTML(html);
  assert.match(html, /data-context-state="over"/);
  assert.match(html, /role="progressbar"/);
  assert.match(html, /aria-valuemax="1050000"/);
  assert.match(html, /aria-valuenow="1050000"/);
  assert.match(html, /Over window/);
  assert.match(html, /over context window/);
});

test("search mode ignores dashboard range alone but honors search state", () => {
  const sandbox = loadRenderSandbox();

  sandbox.currentRange = "7d";
  assert.equal(sandbox.isSearchMode(), false);

  sandbox.currentSearchSort = "newest";
  assert.equal(sandbox.isSearchMode(), true);

  sandbox.currentSearchSort = "relevance";
  sandbox.currentSearchLimit = 60;
  assert.equal(sandbox.isSearchMode(), true);

  sandbox.currentSearchLimit = 30;
  sandbox.currentSearchEventKind = "tool_call";
  assert.equal(sandbox.isSearchMode(), true);
});
