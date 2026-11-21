// @fixture --verify-fixture --verify-plugins --verify-hub-slow
// A chat seen before opens from what's saved, without waiting for a slow server.
let app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Hub", timeout_ms: 10000 });
await app.click({ role: "button", name: "Hub" });
await app.waitFor({ role: "button", name: "Priya Raman" });
await app.click({ role: "button", name: "Priya Raman" });
await app.waitFor({ text: "Morning! Did you see the deck?", timeout_ms: 10000 });
return "ok";
// @relaunch
app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Hub", timeout_ms: 10000 });
await app.click({ role: "button", name: "Hub" });
await app.waitFor({ role: "button", name: "Priya Raman" });
await app.click({ role: "button", name: "Priya Raman" });
await app.waitFor({ text: "Morning! Did you see the deck?", timeout_ms: 2000 });
return "ok";
