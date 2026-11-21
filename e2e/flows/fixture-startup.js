// @fixture --verify-fixture --verify-settings --verify-startup -serverAddress http://127.0.0.1:1
// A slow start shows no splash or "Connecting…" text, then opens the app.
const app = await cua.getApp(APP);
const before = await app.getState({ include_screenshot: false, max_elements: 100 });
if (before.tree_markdown.includes('AXImage = "Connecting" (Polymux)')) throw new Error("Startup still shows the splash logo");
if (before.tree_markdown.includes("Connecting…")) throw new Error("Startup shows connecting text");
// The fixture holds startup for 8 seconds; waits are capped at 10, so allow two.
try { await app.waitFor({ role: "button", name: "Settings", timeout_ms: 10000 }); }
catch { await app.waitFor({ role: "button", name: "Settings", timeout_ms: 5000 }); }
const after = await app.getState({ include_screenshot: false, max_elements: 100 });
if (after.tree_markdown.includes('AXImage = "Connecting" (Polymux)')) throw new Error("Startup returned to the splash logo");
if ((await app.query({ role: "statictext", name: "Connecting…" })).length) throw new Error("Startup shows connecting text");
return "ok";
