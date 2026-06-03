const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const vm = require("node:vm");

const utils = require("../../static/js/charts/utils.js");

function loadDashboardModelsSandbox() {
  const sandbox = {
    dashboardSeriesColor() {
      return { border: "#60a5fa", bg: "rgba(96, 165, 250, 0.14)" };
    },
    modelLineColors: ["#60a5fa"],
    providerDisplayName: utils.providerDisplayName,
  };
  vm.createContext(sandbox);
  const scriptPath = path.join(__dirname, "../../static/js/charts/dashboard-models.js");
  vm.runInContext(fs.readFileSync(scriptPath, "utf8"), sandbox, { filename: scriptPath });
  return sandbox;
}

test("chart color helper converts hex colors to rgba", () => {
  assert.equal(utils.colorWithAlpha("#60a5fa", 0.14), "rgba(96, 165, 250, 0.14)");
  assert.equal(utils.colorWithAlpha("#abc", 0.5), "rgba(170, 187, 204, 0.5)");
  assert.equal(utils.colorWithAlpha("var(--chart)", 0.5), "var(--chart)");
});

test("chart number formatters keep compact units stable", () => {
  assert.equal(utils.formatCompactNumber(250), "250");
  assert.equal(utils.formatCompactNumber(1250), "1.3K");
  assert.equal(utils.formatCompactNumber(1000000), "1M");
  assert.equal(utils.formatRateTick(0), "0%");
  assert.equal(utils.formatRateTick(1.25), "1.3%");
  assert.equal(utils.formatRateTick(12.5), "13%");
});

test("chart provider labels match dashboard terminology", () => {
  assert.equal(utils.providerDisplayName("anthropic"), "Claude Code");
  assert.equal(utils.providerDisplayName("openai"), "Codex");
  assert.equal(utils.providerDisplayName("unknown"), "Unknown");
  assert.equal(utils.providerDisplayName("local"), "local");
});

test("dashboard model datasets accept API values and Chart.js data points", () => {
  const sandbox = loadDashboardModelsSandbox();

  const apiDataset = sandbox.modelDatasetFromPayload({ label: "api", values: [1, 2, 3] }, 0, "tokens");
  assert.deepEqual(apiDataset.data, [1, 2, 3]);

  const chartDataset = sandbox.modelDatasetFromPayload({
    label: "chart",
    data: [{ x: "2026-06-02T10:00:00Z", y: 4 }],
  }, 0, "tokens");
  assert.deepEqual(chartDataset.data, [{ x: "2026-06-02T10:00:00Z", y: 4 }]);
});

test("dashboard model metric config maps selectable analytics metrics", () => {
  const sandbox = loadDashboardModelsSandbox();

  const total = sandbox.dashboardModelMetricConfig("tokens");
  assert.equal(total.key, "total_tokens");
  assert.equal(total.unit, "tokens");
  assert.equal(total.fill, true);
  assert.equal(sandbox.dashboardModelMetricConfig("bogus").key, "total_tokens");

  const errorRate = sandbox.dashboardModelMetricConfig("error_rate", { label: "Failure Rate", unit: "%" });
  assert.equal(errorRate.key, "error_rate");
  assert.equal(errorRate.title, "Failure Rate");
  assert.equal(errorRate.unit, "%");
  assert.equal(errorRate.fill, false);

  const tools = sandbox.modelDatasetFromPayload({ label: "tools", values: [2], tool_call_count: 9 }, 0, "tool_calls");
  assert.equal(tools.fill, false);
  assert.equal(tools.toolCallCount, 9);
});
