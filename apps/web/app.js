const PROTOCOL = "noema.v1";
const STORE = "noema.web.sample.v1";
const FRAMES = "noema.web.frames.v1";

const wsInput = document.querySelector("#ws");
const keyInput = document.querySelector("#key");
const statusEl = document.querySelector("#status");
const logEl = document.querySelector("#log");
const noteEl = document.querySelector("#note");
const draftEl = document.querySelector("#draft");
const sendBtn = document.querySelector("#send");
const stopBtn = document.querySelector("#stop");
const stage = document.querySelector("#stage");
const workEl = document.querySelector("#work");
const workBody = document.querySelector("#work-body");

const saved = JSON.parse(localStorage.getItem(STORE) || "{}");
if (saved.ws) {
  wsInput.value = saved.ws;
} else if (typeof location !== "undefined" && location.host) {
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  wsInput.value = proto + "//" + location.host + "/v1/ws";
}
if (saved.key) keyInput.value = saved.key;

let socket = null;
let sessionId = saved.sessionId || "";
let lastSeq = Number(saved.lastSeq || 0);
let requestId = "";
let runId = "";
let running = false;
const seen = new Set(saved.seen || []);

function rememberFrame(env) {
  if (typeof sessionStorage === "undefined") return;
  let list = [];
  try { list = JSON.parse(sessionStorage.getItem(FRAMES) || "[]"); } catch { list = []; }
  if (!Array.isArray(list)) list = [];
  const payload = env.payload && typeof env.payload === "object" ? env.payload : {};
  const keys = Object.keys(payload);
  list.push({
    seq: typeof env.seq === "number" ? env.seq : null,
    type: env.type || "",
    run_id: env.run_id || "",
    event_id: env.event_id || "",
    created_at: env.created_at || "",
    session_id: env.session_id || "",
    payload_keys: keys,
    payload_bytes: JSON.stringify(payload).length,
  });
  sessionStorage.setItem(FRAMES, JSON.stringify(list.slice(-400)));
}

function persist() {
  localStorage.setItem(STORE, JSON.stringify({
    ws: wsInput.value.trim(),
    sessionId,
    lastSeq,
    seen: [...seen].slice(-400),
  }));
}

function setStatus(text) { statusEl.textContent = text; }

function showWork(on) {
  if (!workEl) return;
  workEl.hidden = !on;
  if (!on) {
    workEl.open = false;
    if (workBody) workBody.textContent = "";
    return;
  }
  if (workBody) workBody.textContent = runId ? runId : "还没有 run_id";
}

function setNote(text, err) {
  noteEl.textContent = text || "";
  noteEl.classList.toggle("err", Boolean(err && text));
}

function markCurrentLine() {
  const asst = logEl.querySelectorAll(".bubble.assistant");
  asst.forEach((n, i) => n.classList.toggle("current", i === asst.length - 1));
}

function line(role, text, id) {
  const node = document.createElement("div");
  node.className = "bubble " + role;
  node.dataset.id = id || "";
  const meta = document.createElement("div");
  meta.className = "meta";
  meta.textContent = role === "user" ? "你" : "灯織";
  const body = document.createElement("p");
  body.className = "body";
  body.textContent = text;
  node.appendChild(meta);
  node.appendChild(body);
  logEl.appendChild(node);
  node.scrollIntoView({ block: "nearest" });
  markCurrentLine();
  return node;
}

function appendAssistant(text, id) {
  if (!text) return;
  const last = logEl.lastElementChild;
  if (last && last.classList.contains("assistant") && !last.dataset.closed) {
    const body = last.querySelector(".body");
    if (body) body.textContent += text;
    else last.textContent += text;
    markCurrentLine();
  } else {
    line("assistant", text, id);
  }
}

function wsURL() {
  const raw = wsInput.value.trim() || "ws://127.0.0.1:8787/v1/ws";
  const base = raw.includes("/v1/ws") ? raw : raw.replace(/\/$/, "") + "/v1/ws";
  if (!sessionId) return base;
  const sep = base.includes("?") ? "&" : "?";
  return base + sep + "session=" + encodeURIComponent(sessionId);
}

function send(obj) {
  if (!socket || socket.readyState !== WebSocket.OPEN) return false;
  socket.send(JSON.stringify(obj));
  return true;
}

function connect() {
  setNote("");
  const previous = socket;
  const key = keyInput.value.trim();
  const url = new URL(wsURL());
  if (key) url.searchParams.set("access_token", key);
  persist();
  setStatus("连接中");
  socket = new WebSocket(url);
  if (previous && previous !== socket) previous.close();
  socket.addEventListener("open", () => {
    setStatus(sessionId ? "已连接 · " + sessionId : "已连接");
    if (lastSeq > 0 || sessionId) {
      send({ protocol: PROTOCOL, type: "replay", session_id: sessionId || undefined, payload: { last_seq: lastSeq } });
    }
  });
  socket.addEventListener("message", (ev) => onFrame(ev.data));
  const mine = socket;
  socket.addEventListener("close", () => {
    if (socket !== mine) return;
    setStatus("已断开");
    running = false;
    showWork(false);
    stopBtn.hidden = true;
    sendBtn.hidden = false;
  });
  socket.addEventListener("error", () => {
    setNote("连不上。展开底部连接，核对地址和密钥。", true);
  });
}

function onFrame(raw) {
  let env;
  try { env = JSON.parse(raw); } catch { setNote("无法解析的帧", true); return; }
  rememberFrame(env);
  if (env.event_id && seen.has(env.event_id)) return;
  if (env.event_id) seen.add(env.event_id);
  if (typeof env.seq === "number" && env.seq > lastSeq) lastSeq = env.seq;
  if (env.session_id) sessionId = env.session_id;
  if (env.run_id) {
    runId = env.run_id;
    if (running) showWork(true);
  }
  const helloSession = env.payload && env.payload.session_id;
  if (env.type === "hello" && helloSession) sessionId = helloSession;
  persist();
  const text = env.payload && (env.payload.text || env.payload.error || "");
  if (env.type === "hello") setStatus("已连接 · " + (sessionId || "未分配"));
  if (env.type === "content") {
    running = true;
    stopBtn.hidden = false;
    sendBtn.hidden = true;
    showWork(true);
    appendAssistant(text, env.event_id);
  }
  if (env.type === "done") {
    if (text) appendAssistant(text, env.event_id);
    const last = logEl.lastElementChild;
    if (last) last.dataset.closed = "1";
    running = false;
    showWork(false);
    stopBtn.hidden = true;
    sendBtn.hidden = false;
  }
  if (env.type === "cancelled") {
    const last = logEl.lastElementChild;
    if (last) last.dataset.closed = "1";
    running = false;
    showWork(false);
    stopBtn.hidden = true;
    sendBtn.hidden = false;
    setNote("已取消");
  }
  if (env.type === "error") setNote(text || "error", true);
  if (env.type === "replay_complete") setStatus("已重放 · " + (sessionId || ""));
}

document.querySelector("#connect").addEventListener("click", connect);
document.querySelector("#form").addEventListener("submit", (ev) => {
  ev.preventDefault();
  const message = draftEl.value.trim();
  if (!message || running) return;
  requestId = crypto.randomUUID();
  const ok = send({
    protocol: PROTOCOL,
    type: "chat",
    client_request_id: requestId,
    session_id: sessionId || undefined,
    character_id: "hiori",
    payload: { message },
  });
  if (!ok) { setNote("还没连上。展开底部连接。", true); return; }
  line("user", message, requestId);
  draftEl.value = "";
  runId = "";
  running = true;
  showWork(true);
  stopBtn.hidden = false;
  sendBtn.hidden = true;
  setNote("");
});
stopBtn.addEventListener("click", () => {
  if (!requestId) return;
  send({
    protocol: PROTOCOL,
    type: "cancel",
    client_request_id: requestId,
    session_id: sessionId || undefined,
  });
});

function setMode(gal) {
  stage.classList.toggle("gal", gal);
  stage.classList.toggle("chat", !gal);
  document.querySelector("#mode-chat").classList.toggle("on", !gal);
  document.querySelector("#mode-gal").classList.toggle("on", gal);
  markCurrentLine();
}
document.querySelector("#mode-chat").addEventListener("click", () => setMode(false));
document.querySelector("#mode-gal").addEventListener("click", () => setMode(true));

if (sessionId && keyInput.value.trim()) {
  connect();
} else if (sessionId) {
  setStatus("已记住 session " + sessionId + " · 点连接后续上");
}
