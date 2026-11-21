"use strict";

const cardWidth = 192, cardHeight = 70;
const positions = new Map();
const boardURL = `api/board?chat=${encodeURIComponent(chat)}`;
const notice = document.querySelector("#notice");
let board, saved, queued, saving = false, fitted = false;
let retry;
let liveGraph = { nodes: [], wires: [] };
let camera = { x: 0, y: 0, scale: 1 };
let gesture, tool = "", source = "", chosenWire = "";
let undo = [], redo = [];

const icons = {
  add: "M4 4h16v16H4z M8 12h8 M12 8v8",
  wire: "M3 6h4v4H3z M17 14h4v4h-4z M7 8h5v8h5",
  fit: "M4 9V4h5 M15 4h5v5 M20 15v5h-5 M9 20H4v-5",
  plus: "M5 12h14 M12 5v14",
  minus: "M5 12h14",
  undo: "M9 5 4 10l5 5 M4 10h10a5 5 0 0 1 0 10",
  redo: "m15 5 5 5-5 5 M20 10H10a5 5 0 0 0 0 10",
};

function control(group, label, icon, action) {
  const button = el("button");
  button.type = "button";
  button.title = label;
  button.setAttribute("aria-label", label);
  const picture = svg("svg", { viewBox: "0 0 24 24", "aria-hidden": "true" });
  picture.append(svg("path", { d: icons[icon] }));
  button.append(picture);
  button.addEventListener("click", action);
  document.querySelector(group).append(button);
  return button;
}

const undoBtn = control("#history", "Undo", "undo", () => historyStep(undo, redo));
const redoBtn = control("#history", "Redo", "redo", () => historyStep(redo, undo));
const addBtn = control("#drawing", "Add node", "add", () => setTool(tool === "add" ? "" : "add"));
const wireBtn = control("#drawing", "Draw wire", "wire", () => setTool(tool === "wire" ? "" : "wire"));
control("#drawing", "Fit to view", "fit", fit);
control("#drawing", "Zoom in", "plus", () => zoom(1.2));
control("#drawing", "Zoom out", "minus", () => zoom(1 / 1.2));

function message(text) {
  notice.hidden = !text;
  notice.textContent = text;
}

function setTool(value) {
  tool = value;
  source = "";
  if (tool) viewport.dataset.tool = tool;
  else delete viewport.dataset.tool;
  addBtn.setAttribute("aria-pressed", String(tool === "add"));
  wireBtn.setAttribute("aria-pressed", String(tool === "wire"));
  message(tool === "add" ? "Choose where to place the node." : tool === "wire" ? "Choose the first node." : "");
  if (tool === "add") viewport.focus();
}

function controls() {
  undoBtn.disabled = !undo.length;
  redoBtn.disabled = !redo.length;
  wireBtn.disabled = current.size < 2;
}

function view() {
  map.style.transform = `translate(${camera.x}px, ${camera.y}px) scale(${camera.scale})`;
}

function fit() {
  if (!positions.size) { camera = { x: 0, y: 0, scale: 1 }; view(); return; }
  let left = Infinity, top = Infinity, right = -Infinity, bottom = -Infinity;
  for (const [x, y] of positions.values()) {
    left = Math.min(left, x);
    top = Math.min(top, y);
    right = Math.max(right, x + cardWidth);
    bottom = Math.max(bottom, y + cardHeight);
  }
  const gutter = 72, width = viewport.clientWidth, height = viewport.clientHeight;
  const scale = Math.max(.15, Math.min(1, (width - gutter - 20) / (right - left), (height - 48) / (bottom - top)));
  camera = { x: gutter + (width - gutter - (right + left) * scale) / 2, y: (height - (bottom + top) * scale) / 2, scale };
  view();
}

function zoom(factor, x = viewport.clientWidth / 2, y = viewport.clientHeight / 2) {
  const scale = Math.max(.15, Math.min(2.5, camera.scale * factor));
  const relative = scale / camera.scale;
  camera = { x: x - (x - camera.x) * relative, y: y - (y - camera.y) * relative, scale };
  view();
}

function redraw() { draw(); controls(); }

function change(apply) {
  undo.push(board);
  if (undo.length > 30) undo.shift();
  redo = [];
  board = structuredClone(board);
  apply(board);
  persist();
}

function historyStep(from, to) {
  if (!from.length || gesture) return;
  to.push(board);
  board = from.pop();
  persist();
}

function persist() {
  queued = board;
  redraw();
  void save();
}

function mergeRows(before, draft, latest) {
  const indexed = rows => new Map(rows.map(row => [row.id, row]));
  const old = indexed(before), local = indexed(draft), merged = indexed(latest);
  for (const id of old.keys()) {
    if (!local.has(id)) merged.delete(id);
  }
  for (const [id, row] of local) {
    const base = old.get(id);
    const edits = Object.entries(row).filter(([key, value]) => value !== base?.[key]);
    if (edits.length) merged.set(id, { ...(merged.get(id) ?? row), ...Object.fromEntries(edits) });
  }
  return [...merged.values()];
}

function mergeBoard(before, draft, latest) {
  const cards = mergeRows(before.cards, draft.cards, latest.cards);
  const ids = new Set(cards.map(card => card.id));
  const exists = id => !id.startsWith("card:") || ids.has(id);
  const wires = mergeRows(before.wires, draft.wires, latest.wires)
    .filter(wire => wire.from !== wire.to && exists(wire.from) && exists(wire.to));
  const positions = { ...latest.positions };
  for (const id of Object.keys(before.positions)) {
    if (!Object.hasOwn(draft.positions, id)) delete positions[id];
  }
  for (const [id, at] of Object.entries(draft.positions)) {
    const old = Object.hasOwn(before.positions, id) ? before.positions[id] : null;
    if (old?.x !== at.x || old?.y !== at.y) Object.defineProperty(positions, id,
      { value: at, enumerable: true, configurable: true, writable: true });
  }
  for (const id of Object.keys(positions)) {
    if (!exists(id)) delete positions[id];
  }
  return { revision: latest.revision, cards, wires, positions };
}

async function save() {
  if (saving) return;
  clearTimeout(retry);
  saving = true;
  try {
    while (queued) {
      const next = queued;
      queued = null;
      const response = await fetch(boardURL, { method: "PUT", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ ...next, revision: saved.revision }) });
      if (response.status === 409) {
        const latest = await response.json();
        cancelDrag();
        board = mergeBoard(saved, board, latest);
        undo = undo.map(step => mergeBoard(saved, step, latest));
        redo = redo.map(step => mergeBoard(saved, step, latest));
        saved = latest;
        queued = board;
        redraw();
        retry = setTimeout(save, 350);
        return;
      }
      if (!response.ok) {
        if (response.status >= 500) throw new Error("retry");
        message("Changes not saved.");
        return;
      }
      saved = await response.json();
    }
    if (!tool) message("");
  } catch {
    queued = board;
    message("Changes not saved.");
    retry = setTimeout(save, 2500);
  } finally {
    saving = false;
  }
}

function world(x, y) {
  const rect = viewport.getBoundingClientRect();
  const limit = n => Math.max(-100000, Math.min(100000, Math.round(n)));
  return { x: limit((x - rect.left - camera.x) / camera.scale), y: limit((y - rect.top - camera.y) / camera.scale) };
}

function newID(kind) {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  return kind + ":" + Array.from(bytes, value => value.toString(16).padStart(2, "0")).join("");
}

function createCard(x, y) {
  const id = newID("card");
  const pos = world(x, y);
  for (let step = 0; step < 64 && [...positions.values()].some(([x, y]) =>
    Math.abs(x - pos.x) < cardWidth + 16 && Math.abs(y - pos.y) < cardHeight + 16); step++) {
    pos.y = Math.min(100000, pos.y + cardHeight + 24);
  }
  setTool("");
  change(next => {
    next.cards.push({ id, title: "Untitled", note: "" });
    next.positions[id] = pos;
  });
  showDetails(id);
  details.querySelector("input")?.select();
}

function selectCard(id) {
  if (tool === "wire") {
    if (!source) {
      source = id;
      showDetails("");
      nodes.querySelector(`[data-id="${CSS.escape(id)}"]`)?.setAttribute("aria-pressed", "true");
      message("Choose the second node.");
      return;
    }
    if (source === id) return;
    const wire = { id: newID("wire"), from: source, to: id, label: "" };
    setTool("");
    change(next => next.wires.push(wire));
    showWire(wire.id);
    return;
  }
  chosenWire = "";
  paintWires();
  showDetails(id, true);
}

function editForm(item, title, extra, key, saveEdit) {
  const form = el("form"), titleLabel = el("label", title), input = el("input");
  const actions = el("div");
  input.name = "title";
  input.maxLength = 80;
  input.required = key === "note";
  input.value = item.title;
  titleLabel.append(input);
  form.append(titleLabel);
  let note;
  if (extra) {
    const label = el("label", extra);
    note = el("textarea");
    note.rows = 3;
    note.maxLength = 1000;
    note.value = item[key];
    label.append(note);
    form.append(label);
  }
  const submit = el("button", "Save");
  submit.type = "submit";
  const remove = el("button", "Delete");
  remove.type = "button";
  remove.addEventListener("click", removeSelection);
  actions.append(submit, remove);
  form.append(actions);
  form.addEventListener("submit", event => {
    event.preventDefault();
    if (input.required && !input.value.trim()) return;
    document.activeElement?.blur();
    saveEdit(item.id, input.value.trim(), note?.value ?? "");
  });
  return form;
}

function saveCard(id, title, note) {
  change(next => {
    const card = next.cards.find(card => card.id === id);
    if (card) Object.assign(card, { title, note });
  });
}

function showWire(id) {
  const wire = board.wires.find(wire => wire.id === id);
  chosenWire = wire ? id : "";
  showDetails("");
  if (!wire) return;
  inspector.hidden = false;
  details.replaceChildren(el("h1", "Connection"), editForm({ ...wire, title: wire.label }, "Label", "", "", (id, label) =>
    change(next => {
      const wire = next.wires.find(wire => wire.id === id);
      if (wire) wire.label = label;
    })));
}

function removeSelection() {
  const card = board.cards.find(card => card.id === selected);
  const wire = chosenWire;
  if (!card && !wire) return;
  chosenWire = "";
  showDetails("");
  change(next => {
    if (wire) next.wires = next.wires.filter(item => item.id !== wire);
    if (!card) return;
    next.cards = next.cards.filter(item => item.id !== card.id);
    next.wires = next.wires.filter(item => item.from !== card.id && item.to !== card.id);
    delete next.positions[card.id];
  });
}

viewport.addEventListener("pointerdown", event => {
  if (event.button !== 0 || gesture || event.target.closest("[data-wire]")) return;
  const button = event.target.closest(".node");
  if (tool === "add" || tool === "wire") {
    event.preventDefault();
    if (tool === "add") createCard(event.clientX, event.clientY);
    else if (button) selectCard(button.dataset.id);
    return;
  }
  const id = button?.dataset.id;
  const [x, y] = id ? positions.get(id) : [camera.x, camera.y];
  gesture = { pointer: event.pointerId, x, y, id, button, left: event.clientX, top: event.clientY, moved: false };
  viewport.setPointerCapture(event.pointerId);
});

viewport.addEventListener("pointermove", event => {
  if (gesture?.pointer !== event.pointerId) return;
  const dx = event.clientX - gesture.left, dy = event.clientY - gesture.top;
  if (!gesture.moved && Math.hypot(dx, dy) < 4) return;
  gesture.moved = true;
  viewport.classList.add("dragging");
  if (!gesture.id) {
    camera.x = gesture.x + dx;
    camera.y = gesture.y + dy;
    view();
    return;
  }
  const x = Math.max(-100000, Math.min(100000, Math.round(gesture.x + dx / camera.scale)));
  const y = Math.max(-100000, Math.min(100000, Math.round(gesture.y + dy / camera.scale)));
  positions.set(gesture.id, [x, y]);
  gesture.button.style.left = `${x}px`;
  gesture.button.style.top = `${y}px`;
  paintWires();
});

function endDrag() {
  const last = gesture;
  gesture = null;
  viewport.classList.remove("dragging");
  return last;
}

function cancelDrag() {
  if (!gesture) return;
  const last = endDrag();
  if (viewport.hasPointerCapture(last.pointer)) viewport.releasePointerCapture(last.pointer);
  if (last.id) redraw();
}

viewport.addEventListener("pointercancel", cancelDrag);
viewport.addEventListener("lostpointercapture", cancelDrag);
viewport.addEventListener("pointerup", event => {
  if (gesture?.pointer !== event.pointerId) return;
  const last = endDrag();
  if (last.moved && last.id) {
    const [x, y] = positions.get(last.id);
    change(next => { next.positions = { ...next.positions, [last.id]: { x, y } }; });
  } else if (!last.moved) {
    if (last.id) selectCard(last.id);
    else document.querySelector("#dismiss").click();
  }
});

viewport.addEventListener("wheel", event => {
  event.preventDefault();
  if (gesture) return;
  if (event.ctrlKey || event.metaKey) {
    const rect = viewport.getBoundingClientRect();
    zoom(Math.exp(-event.deltaY * .015), event.clientX - rect.left, event.clientY - rect.top);
  } else {
    camera.x -= event.deltaX;
    camera.y -= event.deltaY;
    view();
  }
}, { passive: false });

document.addEventListener("keydown", event => {
  if (event.key === "Escape" && tool) { setTool(""); redraw(); return; }
  if (event.key === "Enter" && event.target === viewport && tool === "add") {
    const rect = viewport.getBoundingClientRect();
    createCard(rect.left + rect.width / 2 - cardWidth / 2, rect.top + rect.height / 2 - cardHeight / 2);
    return;
  }
  if (event.target.matches("input, textarea") || gesture) return;
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "z") {
    event.preventDefault();
    if (event.shiftKey) historyStep(redo, undo);
    else historyStep(undo, redo);
  } else if (event.key === "Delete" || event.key === "Backspace") {
    event.preventDefault();
    removeSelection();
  }
});

async function startEditor() {
  try {
    saved = board = await read(boardURL);
    document.querySelector("#tools").hidden = false;
    setTool("");
    redraw();
    if (positions.size) { fit(); fitted = true; }
    void refresh();
  } catch {
    status.textContent = "Canvas is unavailable.";
    setTimeout(startEditor, 3000);
  }
}
void startEditor();
