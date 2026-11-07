// @fixture --verify-approval
// A failed answer keeps the options available to retry.
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "A. Plan my day", timeout_ms: 10000 });
await app.click({ role: "button", name: "A. Plan my day" });
await app.click({ role: "button", name: "Fail answer" });
await app.waitFor({ text: "Couldn’t record that choice.", timeout_ms: 10000 });
await app.waitFor({ role: "button", name: "A. Plan my day" });
return "ok";
