// @fixture --verify-approval
// A failed answer keeps the approval options available to retry.
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Allow once", timeout_ms: 10000 });
await app.click({ role: "button", name: "Allow once" });
await app.click({ role: "button", name: "Fail answer" });
await app.waitFor({ text: "Couldn’t record that answer.", timeout_ms: 10000 });
await app.waitFor({ role: "button", name: "Allow once" });
return "ok";
