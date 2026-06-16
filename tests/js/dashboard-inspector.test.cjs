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
      if (selector === "[data-inspector-close]" || selector === "[aria-label=\"Close\"]") return fakeElement();
      return null;
    },
    querySelectorAll() {
      return [];
    },
  };
}

function loadInspectorSandbox(transcriptHTML = '<div id="chat-view" class="transcript-chat-view"><p>Loaded transcript</p></div>', options = {}) {
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
    if (selector === "[data-inspector-close]" || selector === "[aria-label=\"Close\"]") return closeButton;
    return null;
  };
  const listeners = {};
  const fetches = [];
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
      fetches.push(String(url));
      if (url === (options.transcriptURL || "/sessions/session-xss/conversation")) {
        return { ok: true, text: async () => transcriptHTML };
      }
      throw new Error(`unexpected fetch ${url}`);
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
  return { sandbox, elements, listeners, closeButton, fetches };
}

async function flushPromises() {
  await Promise.resolve();
  await Promise.resolve();
  await new Promise((resolve) => setImmediate(resolve));
}

test("inspector loads transcript conversation partial", async () => {
  const transcriptHTML = '<div id="chat-view" class="transcript-chat-view"><details open><summary>Transcript row</summary><p>Full transcript body</p></details></div>';
  const { sandbox, elements, fetches } = loadInspectorSandbox(transcriptHTML);

  sandbox.window.goToSession("/sessions/session-xss", null);
  await flushPromises();

  const html = elements["inspector-events"].innerHTML;
  assert.equal(html, transcriptHTML);
  assert.deepEqual(fetches, ["/sessions/session-xss/conversation"]);
});

test("inspector preserves dashboard scope when loading transcript partial", async () => {
  const previousLocation = global.location;
  global.location = { search: "?project_key=beacon&source_name=remote" };
  try {
    const transcriptHTML = '<div id="chat-view" class="transcript-chat-view"><p>Scoped transcript</p></div>';
    const { sandbox, elements, fetches } = loadInspectorSandbox(transcriptHTML, {
      transcriptURL: "/sessions/session-xss/conversation?source_name=remote&project_key=beacon",
    });

    sandbox.window.goToSession("/sessions/session-xss", null);
    await flushPromises();

    assert.equal(elements["inspector-events"].innerHTML, transcriptHTML);
    assert.equal(elements["inspector-full-link"].href, "/sessions/session-xss?source_name=remote&project_key=beacon");
    assert.deepEqual(fetches, ["/sessions/session-xss/conversation?source_name=remote&project_key=beacon"]);
  } finally {
    if (previousLocation === undefined) {
      delete global.location;
    } else {
      global.location = previousLocation;
    }
  }
});

test("outside inspector click closes quick view without swallowing dashboard click", async () => {
  const { sandbox, elements, listeners } = loadInspectorSandbox();

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
  const { sandbox, closeButton } = loadInspectorSandbox();

  sandbox.window.goToSession("/sessions/session-xss", null);

  assert.equal(closeButton.focusCalls, 1);
});
