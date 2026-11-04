const assert = require("assert");
const fs = require("fs");
const vm = require("vm");

const html = fs.readFileSync(__dirname + "/index.html", "utf8");
const src = fs.readFileSync(__dirname + "/app.js", "utf8");

assert.match(html, /灯織/);
assert.match(html, /静态立绘位/);
assert.match(html, /未接入 \/ 模拟/);
assert.match(html, /ws:\/\/127\.0\.0\.1:8787\/v1\/ws/);
assert.doesNotMatch(src, /lh serve|localhost:8787\/api/);

const store = {};
const log = { children: [] };
const note = { textContent: "" };
const status = { textContent: "" };
const buttons = {
  send: { hidden: false, addEventListener() {} },
  stop: { hidden: true, addEventListener(type, fn) { listeners["stop:" + type] = fn; } },
};
const listeners = {};
function el(id) {
  return {
    value: id === "ws" ? "ws://127.0.0.1:8787/v1/ws" : "",
    textContent: "",
    classList: { toggle() {}, add() {}, remove() {} },
    addEventListener(type, fn) { listeners[id + ":" + type] = fn; },
    dataset: {},
  };
}
const nodes = {
  "#ws": el("ws"),
  "#key": Object.assign(el("key"), { value: "dev-loopback-key" }),
  "#status": status,
  "#log": {
    lastElementChild: null,
    appendChild(node) { this.lastElementChild = node; log.children.push(node); },
  },
  "#note": note,
  "#draft": Object.assign(el("draft"), { value: "你好" }),
  "#send": buttons.send,
  "#stop": buttons.stop,
  "#stage": { classList: { toggle(name, on) { this[name] = on; } } },
  "#mode-label": { textContent: "" },
  "#connect": { addEventListener(type, fn) { listeners["connect:" + type] = fn; } },
  "#form": { addEventListener(type, fn) { listeners["form:" + type] = fn; } },
  "#mode-chat": { classList: { toggle() {} }, addEventListener(type, fn) { listeners["chat:" + type] = fn; } },
  "#mode-gal": { classList: { toggle() {} }, addEventListener(type, fn) { listeners["gal:" + type] = fn; } },
};

let opened = null;
class FakeSocket {
  static OPEN = 1;
  constructor(url) {
    this.url = url;
    this.readyState = 1;
    this.sent = [];
    this.handlers = {};
    opened = this;
  }
  addEventListener(type, fn) { this.handlers[type] = fn; }
  send(data) { this.sent.push(JSON.parse(data)); }
  close() {}
}

const sandbox = {
  localStorage: {
    getItem: (k) => store[k] || null,
    setItem: (k, v) => { store[k] = v; },
  },
  document: {
    createElement() {
      const node = {
        className: "",
        dataset: {},
        textContent: "",
        scrollIntoView() {},
      };
      node.classList = {
        contains(name) { return node.className.split(/\s+/).includes(name); },
      };
      return node;
    },
    querySelector: (sel) => nodes[sel],
    querySelectorAll: () => [{
      dataset: { empty: "回忆" },
      addEventListener(type, fn) { listeners["empty:" + type] = fn; },
    }],
  },
  WebSocket: FakeSocket,
  crypto: { randomUUID: () => "req-web-1" },
  URL,
  console,
};
vm.createContext(sandbox);
vm.runInContext(src, sandbox);

assert.equal(typeof listeners["connect:click"], "function");
listeners["connect:click"]();
const openedURL = String(opened.url);
assert.ok(openedURL.includes("access_token=dev-loopback-key"));
assert.ok(openedURL.includes("/v1/ws"));

opened.handlers.open();
opened.handlers.message({ data: JSON.stringify({
  protocol: "noema.v1", type: "hello", session_id: "sess-web", seq: 1, event_id: "evt_1",
  payload: { session_id: "sess-web", character_id: "hiori" },
}) });
const saved = JSON.parse(store["noema.web.sample.v1"]);
assert.equal(saved.sessionId, "sess-web");
assert.equal(saved.lastSeq, 1);

listeners["form:submit"]({ preventDefault() {} });
if (!opened.sent.length) throw new Error("chat was not sent; note=" + note.textContent);
const chat = opened.sent[opened.sent.length - 1];
assert.equal(chat.type, "chat");
assert.equal(chat.payload.message, "你好");
assert.equal(chat.client_request_id, "req-web-1");

opened.handlers.message({ data: JSON.stringify({
  type: "content", seq: 2, event_id: "evt_2", session_id: "sess-web", payload: { text: "嗯。" },
}) });
assert.equal(buttons.stop.hidden, false);
opened.handlers.message({ data: JSON.stringify({
  type: "content", seq: 2, event_id: "evt_2", session_id: "sess-web", payload: { text: "重复" },
}) });
assert.equal(log.children.filter((n) => n.className.includes("assistant")).length, 1);

listeners["stop:click"]();
const cancel = opened.sent[opened.sent.length - 1];
assert.equal(cancel.type, "cancel");
assert.equal(cancel.client_request_id, "req-web-1");
opened.handlers.message({ data: JSON.stringify({
  type: "cancelled", seq: 3, event_id: "evt_3", payload: {},
}) });
assert.equal(note.textContent, "已取消");
assert.equal(buttons.send.hidden, false);
const again = JSON.parse(store["noema.web.sample.v1"]);
assert.equal(again.sessionId, "sess-web");
assert.equal(again.lastSeq, 3);

listeners["empty:click"]();
assert.match(note.textContent, /未接入/);
assert.doesNotMatch(JSON.stringify(opened.sent), /memory|remember/i);

console.log("web sample ok");
