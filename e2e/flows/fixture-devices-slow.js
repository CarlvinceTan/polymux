// @fixture --verify-fixture --verify-settings -serverAddress http://127.0.0.1:1 --verify-network-slow
// A slow device load shows progress without blocking the rest of Settings.
const app = await cua.getApp(APP);
const row = { role: "button", name: "127.0.0.1" };
await app.waitFor({ role: "button", name: "Settings", timeout_ms: 10000 });
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Network" });
await app.waitFor({ ...row, timeout_ms: 10000 });
await app.click(row);
await app.waitFor({ name: "Loading devices", timeout_ms: 10000 });
await app.click({ role: "button", name: "Back" });
await app.click({ role: "button", name: "Models" });
await app.waitFor({ role: "button", name: "Model", timeout_ms: 2000 });
await app.click({ role: "button", name: "Network" });
await app.click(row);
await app.waitFor({ name: "Loading devices", timeout_ms: 2000 });
return "ok";
