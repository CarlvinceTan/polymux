// @fixture --verify-fixture --verify-plugins
// The mention menu sends a real identity with the handle, and a group can be renamed.
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Hub", timeout_ms: 10000 });
await app.click({ role: "button", name: "Hub" });
await app.waitFor({ role: "button", name: "Launch team" });
await app.click({ role: "button", name: "Launch team" });
await app.waitFor({ role: "button", name: "Rename group" });
await app.typeText("Hello @pri", { role: "textfield", name: "Message" });
await app.waitFor({ role: "button", name: "Mention Priya Raman" });
const [person] = await app.query({ role: "button", name: "Mention Priya Raman" });
const [box] = await app.query({ role: "textfield", name: "Message" });
if (person.frame.y + person.frame.height > box.frame.y) throw new Error("The mention menu covers the message box");
await app.click({ role: "button", name: "Mention Priya Raman" });
await app.waitFor({ role: "button", name: "Send" });
await app.click({ role: "button", name: "Send" });
await app.waitFor({ text: "Hello @priya_raman" });
await app.waitFor({ text: "Mentioned @slack_1:hub" });
await app.click({ role: "button", name: "Rename group" });
await app.waitFor({ role: "button", name: "Save" });
await app.pressKey("cmd+a", { role: "textfield", name: "Name" });
await app.typeText("Launch crew", { role: "textfield", name: "Name" });
await app.click({ role: "button", name: "Save" });
await app.waitFor({ role: "button", name: "Launch crew" });
return "ok";
