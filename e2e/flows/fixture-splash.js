// @fixture --verify-fixture --verify-splash --verify-settings -serverAddress http://127.0.0.1:1
// The startup animation finishes before the app appears, even when the server is ready at once.
const app = await cua.getApp(APP);
const early = await app.getState({ include_screenshot: false, max_elements: 100 });
if (!early.tree_markdown.includes('AXImage = "Connecting" (Polymux)')) throw new Error("Startup logo is missing");
await cua.sleep(800);
const held = await app.getState({ include_screenshot: false, max_elements: 100 });
if (!held.tree_markdown.includes('AXImage = "Connecting" (Polymux)')) throw new Error("Startup left before the logo finished drawing");
await app.waitFor({ role: "button", name: "Settings", timeout_ms: 8000 });
const after = await app.getState({ include_screenshot: false, max_elements: 100 });
if (after.tree_markdown.includes('AXImage = "Connecting" (Polymux)')) throw new Error("Startup logo remained after connecting");
return "ok";
