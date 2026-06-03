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
    AbortController: globalThis.AbortController,
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
    currentSearchEventKind: "session",
    currentSearchSessionID: "",
    currentSearchLimit: 30,
    currentCompletedRange: "",
    currentActivityRange: "",
    currentActivityRangePinned: false,
    currentRange: "",
    currentChartRange: "",
    currentChartMetric: "total_tokens",
    currentActiveSort: "recent",
    dashboardChartMetrics: ["total_tokens", "input_tokens", "output_tokens", "cache_read_tokens", "tool_calls", "errors", "error_rate"],
    dashboardActiveSorts: ["recent", "longest", "tokens", "tools", "errors"],
    completedPageSize: 50,
    sessionTableHeadHTML: "",
    dashboardRequestSeq: {},
    dashboardControllers: {},
    lastDashboardChartsPayload: null,
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
      tool_call_count: payload,
    }],
  });
  assertNoRawPayloadHTML(card);
  assert.match(card, /active-session-tracker/);
  assert.doesNotMatch(card, /active-tracker-label">CTX</);
  assert.doesNotMatch(card, /active-context/);
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

test("activity feed omits refresh-sensitive time labels", () => {
  const sandbox = loadRenderSandbox();
  const feed = {};
  sandbox.document.getElementById = (id) => (id === "activity-feed" ? feed : null);
  sandbox.renderActivity([{
    id: "event-1",
    type: "message",
    summary: "Stable activity copy",
    session_id: "session-stable",
    provider: "openai",
    timestamp: "2026-05-22T10:00:00Z",
    relative_time: "3h ago",
  }]);

  assertNoRawPayloadHTML(feed.innerHTML);
  assert.match(feed.innerHTML, /Stable activity copy/);
  assert.doesNotMatch(feed.innerHTML, /3h ago|2026|10:00/);
});

test("active cards render compact live stats", () => {
  const sandbox = loadRenderSandbox();
  const html = sandbox.activeCard({
    id: "active-run",
    title: "Active run",
    status: "active",
    provider: "openai",
    last_model: "gpt-5.4-codex",
    total_tokens: 1200000,
    turn_count: 9,
    tool_call_count: 3,
    duration: "12m",
    working_dir: "/tmp/beacon",
  });

  assertNoRawPayloadHTML(html);
  assert.match(html, /active-session-tracker/);
  assert.match(html, /data-active-session-action="toggle-pin"/);
  assert.match(html, /data-active-session-action="move-up"/);
  assert.match(html, /data-active-session-action="move-down"/);
  assert.doesNotMatch(html, /CTX/);
  assert.doesNotMatch(html, /active-context/);
  assert.doesNotMatch(html, /data-context-state/);
  assert.equal(html.includes('role="progressbar"'), false);
  assert.doesNotMatch(html, /Over window/);
});

test("active session sorting preserves grouping and handles metrics", () => {
  const sandbox = loadRenderSandbox();
  const sessions = [
    {
      id: "active-a",
      title: "Alpha",
      status: "active",
      ended_at: "2026-05-09T18:00:00.000Z",
      duration: "4m 22s",
      total_tokens: 42000,
      tool_call_count: 6,
      error_count: 0,
      child_sessions: [{ id: "active-a-child", status: "active", tool_call_count: 1 }],
    },
    {
      id: "active-b",
      title: "Bravo",
      status: "active",
      ended_at: "2026-05-09T18:00:00.000Z",
      duration: "2h 1m",
      total_tokens: 12000,
      tool_call_count: 40,
      error_count: 3,
      child_sessions: [],
    },
    {
      id: "active-c",
      title: "Charlie",
      status: "active",
      ended_at: "2026-05-09T18:00:00.000Z",
      duration: "9m",
      total_tokens: 99000,
      tool_call_count: 2,
      error_count: 1,
      child_sessions: [],
    },
  ];

  sandbox.currentActiveSort = "recent";
  assert.deepEqual(sandbox.sortActiveSessions(sessions).map((session) => session.id), ["active-a", "active-b", "active-c"]);
  sandbox.currentActiveSort = "longest";
  assert.deepEqual(sandbox.sortActiveSessions(sessions).map((session) => session.id), ["active-b", "active-c", "active-a"]);
  sandbox.currentActiveSort = "tokens";
  assert.deepEqual(sandbox.sortActiveSessions(sessions).map((session) => session.id), ["active-c", "active-a", "active-b"]);
  sandbox.currentActiveSort = "tools";
  assert.deepEqual(sandbox.sortActiveSessions(sessions).map((session) => session.id), ["active-b", "active-a", "active-c"]);
  sandbox.currentActiveSort = "errors";
  const byErrors = sandbox.sortActiveSessions(sessions);
  assert.deepEqual(byErrors.map((session) => session.id), ["active-b", "active-c", "active-a"]);
  assert.deepEqual(byErrors[2].child_sessions.map((session) => session.id), ["active-a-child"]);
});

test("dashboard fetches treat late failures from older panel requests as stale", async () => {
  const sandbox = loadRenderSandbox();
  let rejectFirst;
  sandbox.fetch = (url) => {
    if (url === "/api/dashboard/sessions?state=active&old=1") {
      return new Promise((resolve, reject) => {
        rejectFirst = reject;
      });
    }
    return Promise.resolve({
      ok: true,
      async json() {
        return { items: [{ id: "active-latest" }] };
      },
    });
  };

  const first = sandbox.fetchDashboardJSON("active", "/api/dashboard/sessions?state=active&old=1");
  const second = sandbox.fetchDashboardJSON("active", "/api/dashboard/sessions?state=active");
  rejectFirst(new Error("late failure"));

  const firstResult = await first;
  const secondResult = await second;
  assert.equal(firstResult.stale, true);
  assert.equal(secondResult.data.items[0].id, "active-latest");
});

test("pinned active sessions render above sorted unpinned sessions", () => {
  const sandbox = loadRenderSandbox();
  const sessions = [
    { id: "active-a", status: "active", ended_at: "2026-05-09T18:00:00.000Z", total_tokens: 10 },
    { id: "active-b", status: "active", ended_at: "2026-05-09T18:00:00.000Z", total_tokens: 90 },
    { id: "active-c", status: "active", ended_at: "2026-05-09T18:00:00.000Z", total_tokens: 50 },
  ];
  sandbox.currentActiveSort = "tokens";
  sandbox.activeSessionPinnedIDs = () => ["active-c", "active-a"];

  assert.deepEqual(
    Array.from(sandbox.sortActiveSessions(sessions).map((session) => session.id)),
    ["active-c", "active-a", "active-b"],
  );

  const pinned = sandbox.activeCard(sessions[2], { pinnedIndex: 0, pinnedCount: 2 });
  assert.match(pinned, /data-active-pinned="true"/);
  assert.match(pinned, /aria-label="Unpin/);
  assert.match(pinned, /data-active-session-action="move-down"/);
});

test("search mode ignores completed range alone but honors search state", () => {
  const sandbox = loadRenderSandbox();

  sandbox.currentCompletedRange = "7d";
  sandbox.currentRange = "7d";
  assert.equal(sandbox.isSearchMode(), false);

  sandbox.currentSearchSort = "newest";
  assert.equal(sandbox.isSearchMode(), false);

  sandbox.currentSearchSort = "relevance";
  sandbox.currentSearchLimit = 60;
  assert.equal(sandbox.isSearchMode(), false);

  sandbox.currentSearchLimit = 30;
  sandbox.currentSearchEventKind = "session";
  assert.equal(sandbox.isSearchMode(), false);

  sandbox.currentSearchQuery = "dashboard payload";
  assert.equal(sandbox.isSearchMode(), false);
  sandbox.currentSearchSessionID = "session-";
  assert.equal(sandbox.isSearchMode(), false);
  sandbox.currentSearchQuery = "";
  sandbox.currentSearchSessionID = "";

  sandbox.currentSearchEventKind = "event";
  assert.equal(sandbox.isSearchMode(), true);

  sandbox.currentSearchEventKind = "tool_call";
  assert.equal(sandbox.isSearchMode(), true);
});

test("analytics range caption stays stable during refresh states", () => {
  const sandbox = loadRenderSandbox();
  const caption = { textContent: "" };
  sandbox.document.getElementById = (id) => id === "dashboard-chart-range-caption" ? caption : null;

  sandbox.currentChartRange = "";
  sandbox.updateChartRangeCaption();
  assert.equal(caption.textContent, "All time");

  sandbox.updateChartRangeCaption("loading");
  assert.equal(caption.textContent, "All time");

  sandbox.updateChartRangeCaption("error");
  assert.equal(caption.textContent, "All time");

  sandbox.currentChartRange = "7d";
  sandbox.updateChartRangeCaption("empty");
  assert.equal(caption.textContent, "Last 7 days");
});

test("analytics busy state does not expose loading as panel label text", () => {
  const sandbox = loadRenderSandbox();
  const panel = {
    attributes: {},
    setAttribute(name, value) {
      this.attributes[name] = String(value);
    },
    removeAttribute(name) {
      delete this.attributes[name];
    },
  };
  sandbox.document.querySelector = (selector) => selector === ".dashboard-analytics-panel" ? panel : null;

  sandbox.setAnalyticsBusy(true);
  assert.equal(panel.attributes["data-loading"], "true");
  assert.equal(Object.prototype.hasOwnProperty.call(panel.attributes, "aria-busy"), false);

  sandbox.setAnalyticsBusy(false);
  assert.equal(panel.attributes["data-loading"], "false");
  assert.equal(Object.prototype.hasOwnProperty.call(panel.attributes, "aria-busy"), false);
});

test("completed table height floor stays stable during refresh", () => {
  const sandbox = loadRenderSandbox();
  sandbox.window.innerWidth = 1440;
  let measured = 0;
  const region = {
    style: { minHeight: "" },
    attrs: { "data-dashboard-height-floor": "480" },
    getAttribute(name) {
      return this.attrs[name] || "";
    },
    setAttribute(name, value) {
      this.attrs[name] = String(value);
    },
    removeAttribute(name) {
      delete this.attrs[name];
    },
    getBoundingClientRect() {
      measured += 1;
      return { height: 120 };
    },
  };
  const search = {
    closest(selector) {
      return selector === ".completed-table-surface" ? region : null;
    },
  };
  sandbox.document.getElementById = (id) => {
    if (id === "dashboard-wrap") return {};
    if (id === "dashboard-search") return search;
    return null;
  };

  sandbox.stabilizeCompletedTableRegion(false);

  assert.equal(region.style.minHeight, "480px");
  assert.equal(measured, 0);
});

test("analytics summaries describe the chart range independently", () => {
  const sandbox = loadRenderSandbox();
  const wrap = {};
  sandbox.document.getElementById = (id) => (id === "dashboard-analytics-summary" ? wrap : null);

  sandbox.currentCompletedRange = "7d";
  sandbox.currentRange = "7d";
  sandbox.currentChartRange = "1h";
  sandbox.renderAnalyticsSummary({
    total_tokens: 201500,
    model_count: 3,
    tool_call_count: 11,
    error_count: 1,
    error_rate: 9.1,
  });

  assert.match(wrap.innerHTML, /Last hour/);
  assert.doesNotMatch(wrap.innerHTML, /Last 7 days/);
});

test("analytics summaries foreground the selected chart metric", () => {
  const sandbox = loadRenderSandbox();
  const wrap = {};
  sandbox.document.getElementById = (id) => (id === "dashboard-analytics-summary" ? wrap : null);
  sandbox.currentChartMetric = "input_tokens";

  sandbox.renderAnalyticsSummary({
    total_tokens: 1000,
    model_count: 2,
    tool_call_count: 4,
    error_count: 1,
    error_rate: 12.5,
  }, {
    datasets: [
      { values: [100, 200] },
      { values: [50] },
    ],
  });

  assert.match(wrap.innerHTML, /Input Tokens/);
  assert.match(wrap.innerHTML, /350/);
  assert.match(wrap.innerHTML, /Total Tokens/);
  assert.match(wrap.innerHTML, /Error Rate/);
});

test("range helpers keep completed, activity, and chart state separate", () => {
  const sandbox = loadRenderSandbox();
  const completedCaption = {};
  const activityCaption = {};
  sandbox.document.getElementById = (id) => (id === "dashboard-range-caption" ? completedCaption : null);
  sandbox.document.querySelector = (selector) => (selector === "#timeline-sidebar .activity-bar-range" ? activityCaption : null);

  sandbox.currentCompletedRange = "30d";
  sandbox.currentActivityRange = "7d";
  sandbox.currentChartRange = "1h";
  sandbox.currentRange = "24h";

  assert.equal(sandbox.completedRangeValue(), "30d");
  assert.equal(sandbox.activityRangeValue(), "7d");
  assert.equal(sandbox.chartRangeValue(), "1h");

  sandbox.updateRangeCaption();
  assert.equal(completedCaption.textContent, "Last 30 days");
  assert.equal(activityCaption.textContent, "(7d)");
});

test("dashboard chart metric helpers select model activity series", () => {
  const sandbox = loadRenderSandbox();
  const payload = {
    token_cumulative: {
      labels: ["2026-06-02T10:00:00Z"],
      datasets: [{ label: "gpt", values: [10] }],
      summary: { total_tokens: 10 },
      time_unit: "hour",
      bucket_minutes: 60,
    },
    model_activity: {
      labels: ["2026-06-02T10:00:00Z"],
      summary: { total_tokens: 10, call_count: 8, error_count: 0 },
      time_unit: "hour",
      bucket_minutes: 60,
      metrics: {
        tool_calls: {
          label: "Tool Calls",
          unit: "tool calls",
          datasets: [{ label: "gpt", values: [3], tool_call_count: 3 }],
        },
        error_rate: {
          label: "Error Rate",
          unit: "%",
          datasets: [{ label: "gpt", values: [0, 0], error_count: 0, call_count: 8 }],
        },
      },
    },
  };

  sandbox.currentChartMetric = "tool_calls";
  const tools = sandbox.dashboardMetricPayload(payload);
  assert.equal(tools.label, "Tool Calls");
  assert.equal(tools.unit, "tool calls");
  assert.deepEqual(tools.datasets[0].values, [3]);
  assert.equal(sandbox.dashboardMetricHasSeriesData(tools), true);

  sandbox.currentChartMetric = "error_rate";
  const rate = sandbox.dashboardMetricPayload(payload);
  assert.equal(rate.unit, "%");
  assert.deepEqual(rate.datasets[0].values, [0, 0]);
  assert.equal(sandbox.dashboardMetricHasSeriesData(rate), true);
  assert.equal(sandbox.dashboardMetricHasSeriesData({
    metric: "error_rate",
    unit: "%",
    summary: { call_count: 0, error_count: 0 },
    datasets: [{ values: [0, 0] }],
  }), false);

  sandbox.currentChartMetric = "cache_read_tokens";
  const missing = sandbox.dashboardMetricPayload(payload);
  assert.equal(missing.label, "Cache Read Tokens");
  assert.equal(missing.unit, "tokens");
  assert.equal(missing.datasets.length, 0);
});

test("chart point values support Chart.js object points", () => {
  const sandbox = loadRenderSandbox();

  assert.equal(sandbox.chartPointValue({ x: "2026-06-02T10:00:00Z", y: 42 }), 42);
  assert.equal(sandbox.chartPointValue(17), 17);
  assert.equal(sandbox.chartPointValue({ x: "bad" }), 0);

  let captionState = "";
  sandbox.document.querySelector = () => ({ setAttribute() {}, removeAttribute() {} });
  sandbox.document.getElementById = (id) => {
    if (id === "dashboard-analytics-summary") return {};
    if (id === "dashboard-chart-range-caption") {
      return {
        set textContent(value) {
          captionState = value;
        },
      };
    }
    return null;
  };
  sandbox.updateDashboardCharts({
    token_cumulative: {
      datasets: [{ values: [0, 9] }],
      summary: { total_tokens: 9, model_count: 1 },
    },
  });
  assert.equal(captionState, "All time");
});
