const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const vm = require("node:vm");

const utils = require("../../static/js/dashboard/utils.js");

function fakeClassList(initial = []) {
  const values = new Set(initial);
  return {
    add(...names) {
      names.forEach((name) => values.add(name));
    },
    remove(...names) {
      names.forEach((name) => values.delete(name));
    },
    contains(name) {
      return values.has(name);
    },
    toggle(name, force) {
      if (force === undefined ? !values.has(name) : force) {
        values.add(name);
        return true;
      }
      values.delete(name);
      return false;
    },
  };
}

function fakeElement(initialClasses = []) {
  return {
    isConnected: true,
    classList: fakeClassList(initialClasses),
    style: {},
    attributes: {},
    focusCalls: 0,
    textContent: "",
    innerHTML: "",
    href: "",
    setAttribute(name, value) {
      this.attributes[name] = String(value);
    },
    removeAttribute(name) {
      delete this.attributes[name];
    },
    toggleAttribute(name, enabled) {
      if (enabled) this.setAttribute(name, "");
      else this.removeAttribute(name);
    },
    contains() {
      return false;
    },
    closest() {
      return null;
    },
    focus() {
      this.focusCalls += 1;
    },
    querySelector(selector) {
      if (selector === "[aria-label=\"Close\"]") return fakeElement();
      return null;
    },
    querySelectorAll() {
      return [];
    },
  };
}

function loadInspectorSandbox(events) {
  const closeButton = fakeElement();
  const elements = {
    "session-inspector": fakeElement(["hidden"]),
    "inspector-title": fakeElement(),
    "inspector-subtitle": fakeElement(),
    "inspector-summary": fakeElement(),
    "inspector-events": fakeElement(),
    "inspector-full-link": fakeElement(),
    "dashboard-main": fakeElement(),
    "sidebar-divider": fakeElement(),
    "timeline-sidebar": fakeElement(),
  };
  elements["session-inspector"].querySelector = (selector) => {
    if (selector === "[aria-label=\"Close\"]") return closeButton;
    return null;
  };
  const listeners = {};
  const sandbox = {
    window: {
      BeaconDashboard: { utils },
      dashboardSessionIndex: {
        "session-xss": {
          id: "session-xss",
          duration: "1s",
          total_tokens: 12,
          turn_count: 1,
          tool_call_count: 1,
        },
      },
      AbortController: globalThis.AbortController,
      location: { origin: "http://127.0.0.1:4610" },
    },
    document: {
      activeElement: null,
      getElementById(id) {
        return elements[id] || null;
      },
      querySelectorAll() {
        return [];
      },
      addEventListener(type, handler) {
        listeners[type] = listeners[type] || [];
        listeners[type].push(handler);
      },
      documentElement: {
        getAttribute() {
          return "";
        },
      },
    },
    fetch: async (url) => {
      assert.equal(url, "/api/sessions/session-xss/events?limit=200&tail=1");
      return { ok: true, json: async () => events };
    },
    AbortController: globalThis.AbortController,
    Array,
    Promise,
    String,
    URL,
  };
  vm.createContext(sandbox);
  const inspectorPath = path.join(__dirname, "../../static/js/dashboard/inspector.js");
  vm.runInContext(fs.readFileSync(inspectorPath, "utf8"), sandbox, { filename: inspectorPath });
  return { sandbox, elements, listeners, closeButton };
}

async function flushPromises() {
  await Promise.resolve();
  await Promise.resolve();
  await new Promise((resolve) => setImmediate(resolve));
}

test("inspector event rows escape malicious event payloads", async () => {
  const payload = `"><img src=x onerror="alert(1)"><script>alert(1)</script>`;
  const { sandbox, elements } = loadInspectorSandbox([{
    event_uid: `event-${payload}`,
    event_kind: payload,
    actor_role: payload,
    tool_name: payload,
    model: payload,
    text_preview: `preview ${payload}`,
  }]);

  sandbox.window.goToSession("/sessions/session-xss", null);
  await flushPromises();

  const html = elements["inspector-events"].innerHTML;
  for (const raw of ["<script", "<img", "<iframe", "<object"]) {
    assert.equal(html.toLowerCase().includes(raw), false, `unexpected raw tag ${raw} in ${html}`);
  }
  for (const raw of [`onclick="alert`, `onmouseover="alert`, `onerror="alert`]) {
    assert.equal(html.toLowerCase().includes(raw), false, `unexpected raw handler ${raw} in ${html}`);
  }
  assert.match(html, /preview &quot;&gt;&lt;img src=x onerror=&quot;alert\(1\)&quot;&gt;/);
  assert.match(html, /data-event="event-&quot;&gt;&lt;img src=x/);
});

test("outside inspector click closes quick view without swallowing dashboard click", async () => {
  const { sandbox, elements, listeners } = loadInspectorSandbox([]);

  sandbox.window.goToSession("/sessions/session-xss", null);
  await flushPromises();

  assert.equal(elements["session-inspector"].classList.contains("hidden"), false);

  let prevented = false;
  let stopped = false;
  listeners.click[0]({
    target: fakeElement(),
    preventDefault() {
      prevented = true;
    },
    stopPropagation() {
      stopped = true;
    },
  });

  assert.equal(elements["session-inspector"].classList.contains("hidden"), true);
  assert.equal(prevented, false);
  assert.equal(stopped, false);
});

test("inspector focuses close control immediately on open", () => {
  const { sandbox, closeButton } = loadInspectorSandbox([]);

  sandbox.window.goToSession("/sessions/session-xss", null);

  assert.equal(closeButton.focusCalls, 1);
});
