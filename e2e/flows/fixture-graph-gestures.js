// @fixture --verify-fixture --verify-plugins --verify-graph
// Dragging a node moves it and undo puts it back; dragging the background selects.
// Scrolling and a right drag need a real pointer over the canvas, which background input doesn't give.
let app = await cua.getApp(APP);
const graphNodes = ["Claude Code", "Safari", "Polymux docs", "Add the graph", "Polymux server"];
async function node(title) {
  for (let i = 0; i < 40; i++) {
    const found = await app.query({ role: "button", name: title });
    const match = found.find((item) => (item.label === title || item.label.startsWith(`${title}, `)) && item.frame);
    if (match) return match;
    await cua.sleep(250);
  }
  throw new Error(`${title} never appeared`);
}
async function moved(title, from, dx, dy) {
  for (let i = 0; i < 20; i++) {
    const { frame } = await node(title);
    // Positions snap to the grid.
    if (Math.abs(frame.x - from.x - dx) <= 8 && Math.abs(frame.y - from.y - dy) <= 8) return frame;
    await cua.sleep(100);
  }
  const { frame } = await node(title);
  throw new Error(`${title} went to ${frame.x},${frame.y}, not ${from.x + dx},${from.y + dy}`);
}

await app.waitFor({ role: "button", name: "Graph Visualiser" });
await app.click({ role: "button", name: "Graph Visualiser" });
// The map comes first; these checks are of the diagram behind it.
await app.waitFor({ role: "button", name: "Show diagram" });
await app.click({ role: "button", name: "Show diagram" });
await app.waitFor({ role: "button", name: "Show compact view" });
await node("Claude Code");
await cua.sleep(1000);
const host = await app.getState({ query: "Show compact view", include_screenshot: true, max_elements: 50, tree_format: "elements" });
app = await cua.getApp({ pid: host.pid, windowId: host.window_id });
// Frames are on the screen; the driver takes the window's screenshot pixels, which can be smaller.
const bounds = host.window_bounds, scale = host.screenshot_width / bounds.width;
const pixel = (point) => ({ x: (point.x - bounds.x) * scale, y: (point.y - bounds.y) * scale });
const drag = (from, to, extra = {}) => {
  const [start, end] = [pixel(from), pixel(to)];
  return cua.call("drag", { pid: host.pid, window_id: host.window_id, from_x: start.x, from_y: start.y, to_x: end.x, to_y: end.y,
    steps: 16, duration_ms: 300, ...extra });
};
const middle = (frame) => ({ x: frame.x + frame.w / 2, y: frame.y + frame.h / 2 });

// A node follows the pointer, and undo returns it.
const before = (await node("Claude Code")).frame;
const start = middle(before);
await drag(start, { x: start.x + 60, y: start.y + 24 });
await moved("Claude Code", before, 60, 24);
await app.click({ role: "button", name: "Undo" });
await moved("Claude Code", before, 0, 0);

// The background draws a selection band.
const frames = [];
for (const title of graphNodes) frames.push((await node(title)).frame);
const right = Math.max(...frames.map((frame) => frame.x + frame.w));
const bottom = Math.max(...frames.map((frame) => frame.y + frame.h));
const left = Math.min(...frames.map((frame) => frame.x));
await drag({ x: right + 12, y: bottom + 12 }, { x: left + 4, y: frames[0].y });
let selected;
for (let i = 0; i < 20 && !selected; i++) {
  selected = (await app.query({ text: " selected" })).find((item) => /^\d+ selected$/.test(item.value ?? item.label ?? ""));
  if (!selected) await cua.sleep(100);
}
if (!selected) throw new Error("The background drag selected nothing");
await app.click({ role: "button", name: "Clear" });

return "ok";
