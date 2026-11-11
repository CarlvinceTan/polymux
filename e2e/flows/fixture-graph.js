// @fixture --verify-fixture --verify-plugins --verify-graph
// The Graph Visualiser sits above the chats, shows everything in Full, narrows in Compact,
// opens one level at a time, follows a chat from its button, and only ever reads.
const app = await cua.getApp(APP);
async function nodes(title) {
  const found = await app.query({ role: "button", name: title });
  return found.filter((item) => item.label === title || item.label.startsWith(`${title}, `));
}
async function shows(title) {
  for (let i = 0; i < 60; i++) {
    if ((await nodes(title)).length) return;
    await cua.sleep(250);
  }
  throw new Error(`${title} never appeared`);
}
async function hides(title) {
  for (let i = 0; i < 60; i++) {
    if (!(await nodes(title)).length) return;
    await cua.sleep(250);
  }
  throw new Error(`${title} never went away`);
}
await app.waitFor({ role: "button", name: "Graph Visualiser" });
const [row] = await app.query({ role: "button", name: "Graph Visualiser" });
await shows("Chief");
const [chief] = await nodes("Chief");
if (row.frame.y >= chief.frame.y) throw new Error("The global view isn't above the chats");
await app.click({ role: "button", name: "Graph Visualiser" });
// The map comes first; these checks are of the diagram behind it.
await app.waitFor({ role: "button", name: "Show diagram" });
await app.click({ role: "button", name: "Show diagram" });

// Full shows every level, and the chats stay beside it.
await app.waitFor({ role: "button", name: "Show compact view" });
for (const title of ["Safari", "Polymux docs", "Claude Code", "polymux", "Polymux server", "Add the graph"]) await shows(title);
if ((await nodes("Chief")).length < 2) throw new Error("The chats left the sidebar");

// Compact stops one step down, and each node opens on its own.
await app.click({ role: "button", name: "Show compact view" });
await app.waitFor({ role: "button", name: "Show full view" });
await hides("Safari");
await hides("Polymux docs");
await shows("Aria");
await shows("Claude Code");
const [more] = (await app.query({ role: "button", name: "more in Aria" }));
if (!more) throw new Error("Aria doesn't offer more");
await app.click(more.element_token);
await shows("Safari");
if ((await nodes("Polymux docs")).length) throw new Error("Opening Aria opened Safari too");
await app.waitFor({ role: "button", name: "Show 2 more in Safari" });

// Undo steps back through what was opened.
await app.click({ role: "button", name: "Undo" });
await hides("Safari");
await app.click({ role: "button", name: "Redo" });
await shows("Safari");

// Selecting something opens what's known about it.
const [safari] = await nodes("Safari");
await app.click(safari.element_token);
await app.waitFor({ role: "button", name: "Close details" });
await app.click({ role: "button", name: "Close details" });
await app.waitFor({ role: "button", name: "Close details", gone: true });
await app.click({ role: "button", name: "Task from Chief to Aria" });
await app.waitFor({ text: "Sent" });
await app.click({ role: "button", name: "Close details" });
// A reply keeps its own line, as on the old canvas, rather than merging with the task.
await app.waitFor({ role: "button", name: "Result from Aria to Chief" });

// The camera moves without changing anything.
const zoom = async () => (await app.query({ name: "Zoom" })).find((item) => item.label === "Zoom")?.value;
const before = await zoom();
await app.click({ role: "button", name: "Zoom out" });
for (let i = 0; i < 20 && await zoom() === before; i++) await cua.sleep(100);
if (await zoom() === before) throw new Error("Zoom out did nothing");
await app.click({ role: "button", name: "Fit to view" });
await app.click({ role: "button", name: "Auto layout" });
await shows("Safari");

// It's a view: nothing here starts or edits work.
for (const name of ["Run", "Stop", "Add node", "Draw wire", "Delete node"]) {
  if ((await app.query({ role: "button", name })).length) throw new Error(`${name} shouldn't exist`);
}

// A chat's button shows that chat's work, starting in Compact.
const rows = (await nodes("Chief")).sort((a, b) => a.frame.x - b.frame.x);
await app.click(rows[0].element_token);
await app.waitFor({ role: "button", name: "Close side panel", gone: true });
await app.click({ role: "button", name: "Graph Visualiser", nth: 1 });
await app.waitFor({ role: "button", name: "Close side panel" });
await app.waitFor({ role: "button", name: "Show full view" });
await shows("Aria");
await shows("polymux");
if ((await nodes("Milo")).filter((item) => item.frame.x > rows[0].frame.x + rows[0].frame.w).length) throw new Error("Milo isn't part of this chat");
await app.click({ role: "button", name: "Close side panel" });
await app.click({ role: "button", name: "Graph Visualiser", nth: 0 });
await app.waitFor({ role: "button", name: "Show full view" });
await cua.sleep(1500);
return "ok";
// @relaunch
// The view and its mode come back.
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Show full view" });
await app.waitFor({ role: "button", name: "Fit to view" });
return "ok";
