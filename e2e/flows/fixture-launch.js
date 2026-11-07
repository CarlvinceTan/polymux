// @fixture --verify-fixture --verify-settings -serverAddress http://127.0.0.1:1
// A ready session opens the shell without a splash or animation delay.
const app = await cua.getApp(APP);
const early = await app.getState({ include_screenshot: false, max_elements: 100 });
if (early.tree_markdown.includes('AXImage = "Connecting" (Polymux)')) throw new Error("Startup still shows the splash logo");
await app.waitFor({ role: "button", name: "Settings", timeout_ms: 3000 });
const after = await app.getState({ include_screenshot: false, max_elements: 100 });
if (after.tree_markdown.includes('AXImage = "Connecting" (Polymux)')) throw new Error("Startup returned to the splash logo");
return "ok";
