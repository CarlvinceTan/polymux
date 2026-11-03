// @fixture --verify-fixture --verify-settings --verify-startup -serverAddress http://127.0.0.1:1
// Initial connection shows only the brand mark, then gives way to the connected app.
const app = await cua.getApp(APP);
const before = await app.getState({ include_screenshot: false, max_elements: 100 });
if (!before.tree_markdown.includes('AXImage = "Connecting" (Polymux)')) throw new Error("Startup logo is missing");
if ((await app.query({ role: "statictext", name: "Connecting…" })).length) throw new Error("startup still shows connecting text");
if ((await app.query({ role: "button", name: "Connections" })).length) throw new Error("startup still shows the centre button");
await app.waitFor({ role: "button", name: "Settings", timeout_ms: 10000 });
const after = await app.getState({ include_screenshot: false, max_elements: 100 });
if (after.tree_markdown.includes('AXImage = "Connecting" (Polymux)')) throw new Error("Startup logo remained after connecting");
return "ok";
