// @fixture --verify-fixture --verify-settings --verify-startup -serverAddress http://127.0.0.1:1
// Initial setup stays quiet, without a splash, then opens the connected app.
const app = await cua.getApp(APP);
const before = await app.getState({ include_screenshot: false, max_elements: 100 });
if (before.tree_markdown.includes('AXImage = "Connecting" (Polymux)')) throw new Error("Startup still shows the splash logo");
await app.waitFor({ role: "statictext", name: "Connecting…", timeout_ms: 1000 });
await app.waitFor({ role: "button", name: "Settings", timeout_ms: 10000 });
const after = await app.getState({ include_screenshot: false, max_elements: 100 });
if (after.tree_markdown.includes('AXImage = "Connecting" (Polymux)')) throw new Error("Startup returned to the splash logo");
if ((await app.query({ role: "statictext", name: "Connecting…" })).length) throw new Error("Startup stayed in the connecting state");
return "ok";
