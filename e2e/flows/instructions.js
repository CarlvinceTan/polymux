// A bot's instructions edit in place on its profile and survive relaunch.
let app = await cua.getApp(APP);
await app.click({ role: "button", name: "New chat" });
app = await pick(app, "New Bot");
await app.typeText("Brief QA", { role: "textfield", name: "Name" });
await app.click({ role: "button", name: "Create" });
await app.waitFor({ role: "button", name: "Edit Brief QA’s profile", timeout_ms: 10000 });
await app.click({ role: "button", name: "Edit Brief QA’s profile" });
await app.click({ role: "button", name: "Capabilities" });
const editor = { role: "textarea", name: "Instructions" };
await app.waitFor({ ...editor, timeout_ms: 10000 });
await app.typeText("Reply in plain words.", editor);
await app.waitFor({ ...editor, value: "Reply in plain words." });
// Closing the profile sends an edit still waiting to save.
await app.click({ role: "button", name: "Edit Brief QA’s profile" });
await app.waitFor({ ...editor, gone: true });
return "ok";

// @relaunch
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Edit Brief QA’s profile", timeout_ms: 10000 });
await app.click({ role: "button", name: "Edit Brief QA’s profile" });
await app.click({ role: "button", name: "Capabilities" });
await app.waitFor({ role: "textarea", name: "Instructions", value: "Reply in plain words.", timeout_ms: 10000 });
return "ok";
