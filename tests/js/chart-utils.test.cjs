const assert = require("node:assert/strict");
const test = require("node:test");

const utils = require("../../static/js/charts/utils.js");

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
