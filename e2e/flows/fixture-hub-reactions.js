// @fixture --verify-fixture --verify-plugins
// Reactions name who made them and toggle at once; a read mark names who reached the message.
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Hub", timeout_ms: 10000 });
await app.click({ role: "button", name: "Hub" });
await app.waitFor({ role: "button", name: "Priya Raman" });
await app.click({ role: "button", name: "Priya Raman" });
await app.waitFor({ role: "button", name: "Reaction 👍, 2, You and Priya Raman" });
await app.waitFor({ role: "button", name: "Read by Priya Raman" });
await app.click({ role: "button", name: "Reaction 🎉, 1, Sam Lee" });
await app.waitFor({ role: "button", name: "Reaction 🎉, 2, You and Sam Lee" });
await app.click({ role: "button", name: "Reaction 🎉, 2, You and Sam Lee" });
await app.waitFor({ role: "button", name: "Reaction 🎉, 1, Sam Lee" });
await app.click({ role: "button", name: "Add reaction" });
await app.waitFor({ role: "button", name: "React 🔥" });
await app.click({ role: "button", name: "React 🔥" });
await app.waitFor({ role: "button", name: "Reaction 🔥, 1, You" });
await app.waitFor({ role: "button", name: "React 🔥", gone: true });
await app.click({ role: "button", name: "Read by Priya Raman" });
await app.waitFor({ text: "Read by Priya Raman" });
return "ok";
