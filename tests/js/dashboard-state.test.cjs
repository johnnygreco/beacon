const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const vm = require("node:vm");

const stateScript = fs.readFileSync(path.join(__dirname, "../../static/js/dashboard/state.js"), "utf8");

function loadStateSandbox(search) {
  const location = {
    origin: "http://127.0.0.1:4610",
    pathname: "/",
    search,
  };
  const sandbox = {
    Date,
    URL,
    URLSearchParams,
    clearTimeout,
    setTimeout,
    window: {
      location,
      history: {
        state: {},
        replaceState(state, title, url) {
          const parsed = new URL(url, location.origin);
          location.pathname = parsed.pathname;
          location.search = parsed.search;
        },
      },
      sessionStorage: {
        getItem() {
          return null;
        },
        setItem() {},
        removeItem() {},
      },
      localStorage: {
        getItem() {
          return null;
        },
        setItem() {},
        removeItem() {},
      },
      requestAnimationFrame(callback) {
        return callback();
      },
    },
    document: {
      addEventListener() {},
    },
  };
  sandbox.window.window = sandbox.window;
  sandbox.window.document = sandbox.document;
  vm.runInNewContext(stateScript, sandbox, { filename: "state.js" });
  return sandbox;
}

test("dashboard scope chips clear only their own value", () => {
  const sandbox = loadStateSandbox("?source_names=source-a,source-b&source_name=source-c&runtime=runtime-a");

  sandbox.clearDashboardScope("source_name", "source-a");

  const params = new URLSearchParams(sandbox.window.location.search);
  assert.deepEqual(params.getAll("source_names"), ["source-b"]);
  assert.deepEqual(params.getAll("source_name"), ["source-c"]);
  assert.equal(params.get("runtime"), "runtime-a");
});

test("dashboard scope clear all still clears every scope field", () => {
  const sandbox = loadStateSandbox("?source_names=source-a,source-b&runtime=runtime-a&project_key=project-a");

  sandbox.clearDashboardScope("");

  const params = new URLSearchParams(sandbox.window.location.search);
  assert.equal(params.has("source_names"), false);
  assert.equal(params.has("runtime"), false);
  assert.equal(params.has("project_key"), false);
});
