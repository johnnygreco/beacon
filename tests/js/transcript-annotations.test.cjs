const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const vm = require("node:vm");

const transcriptScript = fs.readFileSync(path.join(__dirname, "../../internal/assets/static/js/transcript.js"), "utf8");

function plain(value) {
  return JSON.parse(JSON.stringify(value));
}

function loadAnnotationHelpers() {
  const sandbox = {
    URL,
    URLSearchParams,
    fetch() {
      throw new Error("fetch should not be called by helper tests");
    },
    window: {
      location: {
        origin: "http://127.0.0.1:4610",
        pathname: "/sessions/session-1",
        search: "",
        hash: "",
      },
      sessionStorage: {
        getItem() {
          return null;
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
      querySelectorAll() {
        return [];
      },
    },
  };
  sandbox.window.window = sandbox.window;
  sandbox.window.document = sandbox.document;
  vm.runInNewContext(transcriptScript, sandbox);
  return sandbox.window.__beaconTranscriptAnnotations;
}

test("transcript annotation helpers normalize labels and target keys", () => {
  const helpers = loadAnnotationHelpers();

  assert.deepEqual(
    plain(helpers.normalizeAnnotationLabels(" Dataset:Train, quality good, dataset:train, Needs_Followup ")),
    ["dataset:train", "needs_followup", "quality-good"],
  );
  assert.equal(helpers.annotationTargetKey({ target_type: "session", session_id: "session-1" }), "session:session-1");
  assert.equal(helpers.annotationTargetKey({ target_type: "event", event_uid: "event-1" }), "event:event-1");
  assert.equal(helpers.annotationCountText(1), "1 annotation");
  assert.equal(helpers.annotationCountText(2), "2 annotations");
});

test("transcript annotation helpers build create and update payloads", () => {
  const helpers = loadAnnotationHelpers();
  const target = { target_type: "event", session_id: "session-1", event_uid: "event-1" };

  const createPayload = helpers.buildAnnotationPayload(target, {
    category: " Quality ",
    outcome: " Useful ",
    quality_score: "9",
    confidence: "-4",
    needs_followup: true,
    labels: "dataset:train, quality:good",
    note: " Keep this trace. ",
  }, true);

  assert.deepEqual(plain(createPayload), {
    author_type: "human",
    source: "ui",
    target_type: "event",
    session_id: "session-1",
    event_uid: "event-1",
    category: "Quality",
    outcome: "Useful",
    quality_score: 5,
    confidence: 0,
    needs_followup: true,
    labels: ["dataset:train", "quality:good"],
    note: "Keep this trace.",
  });

  const updatePayload = helpers.buildAnnotationPayload(target, {
    category: "",
    outcome: "",
    quality_score: "3",
    confidence: "80",
    needs_followup: false,
    labels: [],
    note: "Updated",
  }, false);

  assert.equal(updatePayload.target_type, undefined);
  assert.equal(updatePayload.session_id, undefined);
  assert.equal(updatePayload.event_uid, undefined);
  assert.equal(updatePayload.quality_score, 3);
  assert.equal(updatePayload.confidence, 80);
});
