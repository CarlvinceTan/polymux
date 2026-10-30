// @fixture --verify-fixture --verify-settings --verify-providers --verify-providers-slow -serverAddress http://127.0.0.1:1
// Slow provider refreshes don't block the catalogue, sign-in, or an immediately available saved model.
const app = await cua.getApp(APP);
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Models" });
await app.click({ role: "button", name: "Add provider" });
await app.click({ role: "button", name: "Subscription · Subscription sign-in" });
await app.waitFor({ text: "TEST-1234", timeout_ms: 10000 });
await app.waitFor({ role: "button", name: "Copy code", timeout_ms: 5000 });
await app.typeText("http://localhost/callback?code=test", { role: "textfield", name: "http://localhost/callback" });
await app.click({ role: "button", name: "Continue" });
await app.waitFor({ role: "button", name: "Personal", timeout_ms: 5000 });
await app.click({ role: "button", name: "Personal" });
await app.waitFor({ role: "button", name: "Edit Subscription", timeout_ms: 5000 });
await app.click({ role: "button", name: "Default model" });
await app.typeText("subscription-chat", { role: "textfield", name: "Search models" });
await app.click({ role: "button", name: "Subscription Chat Subscription" });
await app.waitFor({ role: "button", name: "Default model", value: "Subscription Chat Subscription", timeout_ms: 5000 });
await app.click({ role: "button", name: "General" });
await app.click({ role: "button", name: "Models" });
await app.waitFor({ role: "button", name: "Edit Subscription", timeout_ms: 1500 });
await app.waitFor({ role: "button", name: "Default model", value: "Subscription Chat Subscription", timeout_ms: 1500 });
return "ok";
