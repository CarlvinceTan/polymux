// @fixture --verify-fixture --verify-settings -serverAddress http://127.0.0.1:1 --verify-network-fail
// A device list that can’t be reached says so plainly, without an error.
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Settings", timeout_ms: 10000 });
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Network" });
await app.waitFor({ role: "button", name: "127.0.0.1", timeout_ms: 10000 });
await app.click({ role: "button", name: "127.0.0.1" });
await app.waitFor({ text: "Offline", timeout_ms: 10000 });
return "ok";
