const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const vm = require("node:vm");

function loadNameSandbox() {
  const sandbox = createNameSandbox();
  runNameScript(sandbox);
  return sandbox.window.BeaconDashboard.name;
}

function runNameScript(sandbox) {
  vm.createContext(sandbox);
  const scriptPath = path.join(__dirname, "../../static/js/dashboard/name.js");
  vm.runInContext(fs.readFileSync(scriptPath, "utf8"), sandbox, { filename: scriptPath });
}

function createClassList(initial = []) {
  const classes = new Set(initial);
  return {
    add(name) {
      classes.add(name);
    },
    remove(name) {
      classes.delete(name);
    },
    contains(name) {
      return classes.has(name);
    },
    toggle(name, force) {
      if (force === undefined ? !classes.has(name) : force) {
        classes.add(name);
        return true;
      }
      classes.delete(name);
      return false;
    },
  };
}

function createElement({ id = "", textContent = "", value = "", attrs = {}, classes = [] } = {}) {
  const listeners = new Map();
  return {
    id,
    textContent,
    value,
    classList: createClassList(classes),
    focused: false,
    selected: false,
    attrs,
    getAttribute(name) {
      return this.attrs[name] || "";
    },
    setAttribute(name, value) {
      this.attrs[name] = value;
    },
    addEventListener(type, listener) {
      if (!listeners.has(type)) listeners.set(type, []);
      listeners.get(type).push(listener);
    },
    dispatch(type, event = {}) {
      for (const listener of listeners.get(type) || []) listener(event);
    },
    focus() {
      this.focused = true;
    },
    select() {
      this.selected = true;
    },
  };
}

function createNameSandbox(overrides = {}) {
  const storage = new Map(Object.entries(overrides.storage || {}));
  const sandbox = {
    window: {
      BeaconDashboard: {},
      localStorage: {
        getItem(key) {
          return storage.has(key) ? storage.get(key) : null;
        },
        setItem(key, value) {
          storage.set(key, String(value));
        },
        removeItem(key) {
          storage.delete(key);
        },
      },
    },
    document: {
      readyState: "complete",
      querySelector() {
        return null;
      },
      addEventListener() {},
    },
    storage,
  };
  sandbox.globalThis = sandbox.window;
  return sandbox;
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

test("dashboard name init supports empty commits without a clear button", () => {
  const control = createElement({
    attrs: {
      "data-dashboard-default-name": "Configured Station",
      "data-dashboard-fallback-heading": "Configured Station",
    },
  });
  const title = createElement({ id: "dashboard-title", textContent: "Configured Station" });
  const input = createElement({ id: "dashboard-name-input", value: "", classes: ["hidden"] });
  const elements = new Map([
    [title.id, title],
    [input.id, input],
  ]);
  const sandbox = createNameSandbox({ storage: { "beacon-dashboard-name": "Custom Name" } });
  sandbox.document.title = "";
  sandbox.document.querySelector = (selector) => selector === "[data-dashboard-name-control]" ? control : null;
  sandbox.document.getElementById = (id) => elements.get(id) || null;

  runNameScript(sandbox);

  assert.equal(sandbox.document.title, "Custom Name | Beacon");
  assert.equal(title.textContent, "Custom Name");
  title.dispatch("click");
  assert.equal(input.classList.contains("hidden"), false);
  input.value = "";
  input.dispatch("input");
  input.dispatch("keydown", {
    key: "Enter",
    preventDefault() {},
  });

  assert.equal(sandbox.storage.has("beacon-dashboard-name"), false);
  assert.equal(sandbox.document.title, "Configured Station | Beacon");
  assert.equal(title.textContent, "Configured Station");
  assert.equal(input.classList.contains("hidden"), true);
  assert.equal(title.focused, true);
  assert.equal(sandbox.document.getElementById("dashboard-name-clear"), null);
});
