"use strict";

const chat = window.polymux?.chat ?? new URLSearchParams(location.search).get("chat") ?? "";
const states = { working: "Working", waiting: "Waiting", done: "Done", failed: "Failed", idle: "Idle" };
const status = document.querySelector("#status");
const viewport = document.querySelector("#viewport");
const map = document.querySelector("#map");
const nodes = document.querySelector("#nodes");
const wires = document.querySelector("#wires");
const inspector = document.querySelector("#inspector");
const details = document.querySelector("#details");
let selected = "";
let previous = "";
let current = new Map();

function el(tag, text) {
  const node = document.createElement(tag);
  node.textContent = text ?? "";
  return node;
}

function svg(tag, attrs) {
  const node = document.createElementNS("http://www.w3.org/2000/svg", tag);
  for (const [key, value] of Object.entries(attrs)) node.setAttribute(key, value);
  return node;
}

function showDetails(id, focus = false) {
  selected = id;
  for (const button of nodes.children) button.setAttribute("aria-pressed", String(button.dataset.id === id));
  const node = current.get(id);
  inspector.hidden = !node;
  if (!node) return;
  const content = [el("h1", node.title)];
  if (node.subtitle) content.push(el("p", node.subtitle));
  if (node.preview) {
    const img = el("img");
    img.alt = `Preview of ${node.title}`;
    img.src = `api/preview?id=${encodeURIComponent(node.id)}`;
    content.push(img);
  }
  for (const block of node.blocks ?? []) {
    if (block.kind === "progress") {
      const progress = el("progress");
      progress.max = 1;
      progress.value = block.value ?? 0;
      progress.setAttribute("aria-label", block.text || "Progress");
      content.push(progress);
    }
    if (block.text) content.push(el("p", block.text));
  }
  if (node.details?.length) {
    const list = el("dl");
    for (const detail of node.details) list.append(el("dt", detail.label), el("dd", detail.value));
    content.push(list);
  }
  details.replaceChildren(...content);
  if (focus) document.querySelector("#dismiss").focus();
}

function draw(graph) {
  const active = document.activeElement?.dataset.id;
  current = new Map((graph.nodes ?? []).map(node => [node.id, node]));
  const positions = new Map();
  const columns = [];
  const pad = 24, dx = 236, dy = 104, width = 192, height = 70;
  for (const [id, node] of current) {
    let level = 0;
    let parent = node.parent;
    const seen = new Set([id]);
    while (parent && current.has(parent) && !seen.has(parent) && level < 12) {
      seen.add(parent);
      level++;
      parent = current.get(parent).parent;
    }
    const row = columns[level] ?? 0;
    columns[level] = row + 1;
    positions.set(id, [pad + level * dx, pad + row * dy]);
  }
  map.style.width = `${pad * 2 + Math.max(0, columns.length - 1) * dx + width}px`;
  map.style.height = `${pad * 2 + Math.max(0, ...columns.filter(Boolean).map(rows => rows - 1)) * dy + height}px`;
  const arrow = svg("marker", { id: "arrow", viewBox: "0 0 10 10", refX: 9, refY: 5, markerWidth: 6, markerHeight: 6, orient: "auto-start-reverse" });
  arrow.append(svg("path", { d: "M 0 0 L 10 5 L 0 10 z", class: "arrow" }));
  const defs = svg("defs", {});
  defs.append(arrow);
  const paths = [defs];
  for (const wire of graph.wires ?? []) {
    const a = positions.get(wire.from), b = positions.get(wire.to);
    if (!a || !b || a === b) continue;
    const right = a[0] < b[0];
    const start = a[0] + (right || a[0] === b[0] ? width : 0);
    const end = b[0] + (right ? 0 : width);
    const middle = start === end ? start + 16 : (start + end) / 2;
    const y1 = a[1] + height / 2, y2 = b[1] + height / 2;
    const attrs = {
      d: `M ${start} ${y1} C ${middle} ${y1}, ${middle} ${y2}, ${end} ${y2}`,
      class: [wire.state === "active" ? "active" : "", ["dashed", "dotted"].includes(wire.line) ? wire.line : ""].join(" "),
    };
    if (["forward", "both"].includes(wire.direction)) attrs["marker-end"] = "url(#arrow)";
    if (["back", "both"].includes(wire.direction)) attrs["marker-start"] = "url(#arrow)";
    paths.push(svg("path", attrs));
  }
  const buttons = [];
  for (const [id, node] of current) {
    const [x, y] = positions.get(id);
    const button = el("button");
    button.type = "button";
    button.className = "node";
    button.dataset.id = id;
    button.style.left = `${x}px`;
    button.style.top = `${y}px`;
    const caption = [states[node.state], node.subtitle].filter(Boolean).join(" · ");
    button.append(el("strong", node.title), el("span", caption));
    button.setAttribute("aria-label", [node.title, caption].filter(Boolean).join(", "));
    button.addEventListener("click", () => showDetails(id, true));
    buttons.push(button);
  }
  wires.replaceChildren(...paths);
  nodes.replaceChildren(...buttons);
  viewport.hidden = buttons.length === 0;
  status.hidden = buttons.length > 0;
  status.textContent = "No activity yet.";
  buttons.find(button => button.dataset.id === active)?.focus({ preventScroll: true });
  showDetails(selected);
}

document.querySelector("#dismiss").addEventListener("click", () => {
  const match = Array.from(nodes.children).find(button => button.dataset.id === selected);
  showDetails("");
  match?.focus({ preventScroll: true });
});
document.addEventListener("keydown", event => {
  if (event.key === "Escape" && !inspector.hidden) document.querySelector("#dismiss").click();
});

function withActivity(graph, activity) {
  const runs = new Map((activity.runs ?? []).map(run => [run.id, run]));
  function inChat(run) {
    const seen = new Set();
    while (run && !seen.has(run.id)) {
      if (run.conversationID === chat) return true;
      seen.add(run.id);
      run = runs.get(run.parentRunID);
    }
    return false;
  }
  const recent = new Map();
  const shown = new Set();
  for (const run of runs.values()) {
    if (chat && !inChat(run)) continue;
    shown.add(run.id);
    if (!recent.has(run.agentID)) recent.set(run.agentID, run);
  }
  const deliveries = (activity.deliveries ?? []).filter(delivery =>
    !chat || delivery.conversationID === chat || shown.has(delivery.parentRunID));
  const participants = new Set(deliveries.flatMap(delivery => [delivery.sourceAgentID, delivery.targetAgentID]));
  const state = new Map(Object.entries({
    queued: "waiting", waiting_input: "waiting", waiting_peer: "waiting",
    running: "working", resuming: "working", completed: "done", failed: "failed",
  }));
  const merged = new Map((graph.nodes ?? []).map(node => [node.id, node]));
  const id = agent => `agent:${agent}`;
  for (const bot of activity.bots ?? []) {
    if (chat && !recent.has(bot.id) && !participants.has(bot.id) && bot.id !== chat) continue;
    const report = merged.get(id(bot.id));
    merged.set(id(bot.id), { ...report, id: id(bot.id), title: bot.name,
      kind: "agent", state: state.get(recent.get(bot.id)?.status) ?? report?.state ?? "idle" });
  }
  const edges = new Map((graph.wires ?? []).map(wire => [wire.id, wire]));
  function link(kind, from, to, active) {
    if (from === to || !merged.has(id(from)) || !merged.has(id(to))) return;
    const key = JSON.stringify([kind, from, to]);
    if (!edges.has(key)) edges.set(key, { id: key, from: id(from), to: id(to), direction: "forward",
      line: kind === "task" ? "dashed" : "solid", state: active ? "active" : "idle" });
  }
  for (const key of shown) {
    const run = runs.get(key);
    const parent = runs.get(run.parentRunID);
    if (parent) link("task", parent.agentID, run.agentID, state.get(run.status) === "working");
  }
  for (const delivery of deliveries) {
    link("handoff", delivery.sourceAgentID, delivery.targetAgentID, ["queued", "running"].includes(delivery.status));
  }
  return { nodes: Array.from(merged.values()), wires: Array.from(edges.values()) };
}

async function read(path) {
  const response = await fetch(path, { cache: "no-store" });
  if (!response.ok) throw new Error("Canvas data is unavailable");
  return response.json();
}

async function refresh() {
  try {
    const [reports, activity] = await Promise.all([
      read(`api/graph?chat=${encodeURIComponent(chat)}`),
      read("api/_host/activity"),
    ]);
    const graph = withActivity(reports, activity);
    const signature = JSON.stringify(graph);
    if (signature !== previous) {
      draw(graph);
      previous = signature;
    }
  } catch {
    if (!previous) status.textContent = "Canvas is unavailable.";
  } finally {
    setTimeout(refresh, 3000);
  }
}
refresh();
