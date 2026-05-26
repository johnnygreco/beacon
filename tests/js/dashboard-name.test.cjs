const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const vm = require("node:vm");

function loadNameSandbox() {
  const sandbox = {
    window: {
      BeaconDashboard: {},
      localStorage: {
        getItem() {
          return "";
        },
        setItem() {},
        removeItem() {},
      },
    },
    document: {
      readyState: "complete",
      querySelector() {
        return null;
      },
      addEventListener() {},
    },
  };
  sandbox.globalThis = sandbox.window;
  vm.createContext(sandbox);
  const scriptPath = path.join(__dirname, "../../static/js/dashboard/name.js");
  vm.runInContext(fs.readFileSync(scriptPath, "utf8"), sandbox, { filename: scriptPath });
  return sandbox.window.BeaconDashboard.name;
}

test("dashboard name normalization strips C0 and C1 controls", () => {
  const name = loadNameSandbox();
  assert.equal(name.normalize("  Work\u0000station\u0085A\n "), "Workstation A");
});

test("dashboard name normalization clamps by Unicode code point", () => {
  const name = loadNameSandbox();
  const normalized = name.normalize("x".repeat(79) + "🚦extra");
  assert.equal(Array.from(normalized).length, 80);
  assert.equal(normalized.endsWith("🚦"), true);
});
