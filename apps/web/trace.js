const FRAMES = "noema.web.frames.v1";
const STORE = "noema.web.sample.v1";

const rowsEl = document.querySelector("#rows");
const emptyEl = document.querySelector("#empty");
const sessionEl = document.querySelector("#session");
const holdEl = document.querySelector("#hold");

function load() {
  let frames = [];
  try { frames = JSON.parse(sessionStorage.getItem(FRAMES) || "[]"); } catch { frames = []; }
  if (!Array.isArray(frames)) frames = [];
  let saved = {};
  try { saved = JSON.parse(localStorage.getItem(STORE) || "{}"); } catch { saved = {}; }
  return { frames, sessionId: saved.sessionId || "" };
}

function render() {
  const { frames, sessionId } = load();
  sessionEl.textContent = "session " + (sessionId || "—");
  rowsEl.textContent = "";
  emptyEl.hidden = frames.length > 0;
  frames.forEach((frame) => {
    const tr = document.createElement("tr");
    const cells = [
      frame.seq == null ? "—" : String(frame.seq),
      frame.type || "—",
      frame.run_id || "—",
      frame.event_id || "—",
      frame.created_at || "—",
    ];
    cells.forEach((text) => {
      const td = document.createElement("td");
      td.textContent = text;
      tr.appendChild(td);
    });
    const td = document.createElement("td");
    const btn = document.createElement("button");
    btn.type = "button";
    btn.textContent = "字段";
    const extra = document.createElement("div");
    extra.className = "payload";
    const keys = Array.isArray(frame.payload_keys) ? frame.payload_keys : [];
    extra.textContent = keys.length
      ? "payload 字段 " + keys.join(", ") + " · " + (frame.payload_bytes || 0) + " 字节"
      : "没有 payload";
    btn.addEventListener("click", () => tr.classList.toggle("open"));
    td.appendChild(btn);
    td.appendChild(extra);
    tr.appendChild(td);
    rowsEl.appendChild(tr);
  });
  if (!holdEl.checked) {
    const last = rowsEl.lastElementChild;
    if (last) last.scrollIntoView({ block: "nearest" });
  }
}

holdEl.addEventListener("change", () => {
  if (!holdEl.checked) render();
});
document.querySelector("#refresh").addEventListener("click", render);
window.addEventListener("storage", render);
render();
setInterval(() => { if (!holdEl.checked) render(); }, 1000);
