const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const vm = require("node:vm");

const tableScript = fs.readFileSync(path.join(__dirname, "../../internal/assets/static/js/dashboard/table.js"), "utf8");

class FakeRow {
  constructor(id, attrs = {}) {
    this.id = id;
    this.attrs = attrs;
    this.parent = null;
  }

  getAttribute(name) {
    return this.attrs[name] || "";
  }

  remove() {
    if (!this.parent) return;
    this.parent.removeChild(this);
  }

  after(row) {
    if (!this.parent) return;
    this.parent.insertAfter(row, this);
  }
}

class FakeTBody {
  constructor(rows) {
    this.children = [];
    rows.forEach((row) => this.appendChild(row));
  }

  querySelectorAll(selector) {
    if (selector === "tr[data-sort-ended]:not([data-parent])") {
      return this.children.filter((row) => row.attrs["data-sort-ended"] && !row.attrs["data-parent"]);
    }
    if (selector === "tr[data-parent]") {
      return this.children.filter((row) => row.attrs["data-parent"]);
    }
    return [];
  }

  querySelector(selector) {
    if (selector === "tr[data-pagination-row]") {
      return this.children.find((row) => row.attrs["data-pagination-row"]) || null;
    }
    return null;
  }

  appendChild(row) {
    this.removeChild(row);
    row.parent = this;
    this.children.push(row);
  }

  insertBefore(row, before) {
    this.removeChild(row);
    row.parent = this;
    const index = this.children.indexOf(before);
    if (index < 0) this.children.push(row);
    else this.children.splice(index, 0, row);
  }

  insertAfter(row, after) {
    this.removeChild(row);
    row.parent = this;
    const index = this.children.indexOf(after);
    if (index < 0 || index === this.children.length - 1) this.children.push(row);
    else this.children.splice(index + 1, 0, row);
  }

  removeChild(row) {
    const index = this.children.indexOf(row);
    if (index >= 0) this.children.splice(index, 1);
  }
}

function loadTableSandbox(tbody) {
  const sandbox = {
    document: {
      getElementById(id) {
        return id === "completed-sessions" ? tbody : null;
      },
      querySelectorAll() {
        return [];
      },
      querySelector() {
        return null;
      },
    },
    sortAsc: false,
    sortColumn: "ended",
    updateCompletedSortIndicators() {},
    loadCompletedSessions() {},
  };
  vm.createContext(sandbox);
  vm.runInContext(tableScript, sandbox, { filename: "table.js" });
  return sandbox;
}

test("completed table client pre-sort treats errors as numeric", () => {
  const rows = [
    new FakeRow("session-row-a", { "data-sort-ended": "1", "data-sort-errors": "2" }),
    new FakeRow("session-row-b", { "data-sort-ended": "1", "data-sort-errors": "10" }),
    new FakeRow("session-row-c", { "data-sort-ended": "1", "data-sort-errors": "1" }),
  ];
  const tbody = new FakeTBody(rows);
  const sandbox = loadTableSandbox(tbody);

  sandbox.sortCurrentCompletedRows("errors");

  assert.deepEqual(tbody.children.map((row) => row.id), ["session-row-b", "session-row-a", "session-row-c"]);
});
