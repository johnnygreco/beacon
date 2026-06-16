const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const vm = require("node:vm");

const transcriptScript = fs.readFileSync(path.join(__dirname, "../../static/js/transcript.js"), "utf8");

function loadTranscriptSandbox({ currentSearch, transcriptPath }) {
  const backLink = {
    attributes: { href: "/" },
    setAttribute(name, value) {
      this.attributes[name] = String(value);
    },
  };
  const state = {
    savedAt: Date.now(),
    url: "/?q=scoped&project_key=beacon",
    transcriptPath,
  };
  const sandbox = {
    URL,
    URLSearchParams,
    window: {
      location: {
        origin: "http://127.0.0.1:4610",
        pathname: "/sessions/session-1",
        search: currentSearch,
      },
      sessionStorage: {
        getItem(key) {
          return key === "beacon-dashboard-return-state-v1" ? JSON.stringify(state) : null;
        },
      },
      requestAnimationFrame(callback) {
        return callback();
      },
    },
    document: {
      addEventListener() {},
      getElementById() {
        return null;
      },
      querySelectorAll(selector) {
        return selector === ".transcript-back-link" ? [backLink] : [];
      },
    },
  };
  sandbox.window.window = sandbox.window;
  sandbox.window.document = sandbox.document;
  vm.runInNewContext(transcriptScript, sandbox);
  return backLink;
}

test("transcript dashboard return state requires matching scope search params", () => {
  const matching = loadTranscriptSandbox({
    currentSearch: "?source_name=remote&project_key=beacon",
    transcriptPath: "/sessions/session-1?project_key=beacon&source_name=remote#event-1",
  });
  assert.equal(matching.attributes.href, "/?q=scoped&project_key=beacon");

  const mismatched = loadTranscriptSandbox({
    currentSearch: "?project_key=other",
    transcriptPath: "/sessions/session-1?project_key=beacon",
  });
  assert.equal(mismatched.attributes.href, "/");
});
