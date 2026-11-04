const assert = require("assert");
const fs = require("fs");
const vm = require("vm");

const html = fs.readFileSync(__dirname + "/trace.html", "utf8");
const src = fs.readFileSync(__dirname + "/trace.js", "utf8");
const home = fs.readFileSync(__dirname + "/index.html", "utf8");
const app = fs.readFileSync(__dirname + "/app.js", "utf8");

assert.match(home, /href="\/trace"/);
assert.doesNotMatch(home, /event_id|payload 字段/);
assert.match(html, /seq/);
assert.match(html, /暂停自动滚动/);
assert.doesNotMatch(src, /lh serve/);
assert.match(app, /noema\.web\.frames\.v1/);
const remember = app.slice(app.indexOf("function rememberFrame"), app.indexOf("function persist"));
assert.match(remember, /payload_keys/);
assert.doesNotMatch(remember, /payload\.text|payload\.message/);

const session = {};
const local = {
  "noema.web.sample.v1": JSON.stringify({ sessionId: "sess-trace" }),
  "noema.web.frames.v1": JSON.stringify([
    { seq: 2, type: "content", run_id: "run_1", event_id: "evt_2", created_at: "2026-11-04T09:00:00Z", payload_keys: ["text"], payload_bytes: 12 },
    { seq: 3, type: "done", run_id: "run_1", event_id: "evt_3", created_at: "", payload_keys: [], payload_bytes: 2 },
  ]),
};
const rows = [];
const clicks = {};
const sandbox = {
  sessionStorage: { getItem: (k) => session[k] || local[k] || null },
  localStorage: { getItem: (k) => local[k] || null },
  document: {
    querySelector(sel) {
      if (sel === "#rows") return {
        textContent: "",
        lastElementChild: null,
        appendChild(node) { this.lastElementChild = node; rows.push(node); },
      };
      if (sel === "#empty") return { hidden: false };
      if (sel === "#session") return { textContent: "" };
      if (sel === "#hold") return { checked: true, addEventListener() {} };
      if (sel === "#refresh") return { addEventListener(type, fn) { clicks[type] = fn; } };
      return null;
    },
    createElement() {
      const node = {
        className: "",
        textContent: "",
        kids: [],
        classList: { toggle() { node.open = !node.open; } },
      };
      node.appendChild = (child) => { node.kids.push(child); };
      node.addEventListener = () => {};
      return node;
    },
  },
  window: { addEventListener() {} },
  setInterval() {},
  console,
};
vm.createContext(sandbox);
vm.runInContext(src, sandbox);
assert.equal(rows.length, 2);
const first = rows[0];
const texts = [];
function walk(node) {
  if (!node) return;
  if (node.textContent) texts.push(node.textContent);
  (node.kids || []).forEach(walk);
}
walk(first);
assert.ok(texts.includes("content"));
assert.ok(texts.includes("evt_2"));
assert.ok(texts.includes("run_1"));
const payload = texts.find((t) => t.startsWith("payload"));
assert.match(payload, /字段 text/);
assert.doesNotMatch(payload, /用户|助手|今晚/);
console.log("trace sample ok");
