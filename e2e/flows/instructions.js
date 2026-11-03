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
// A new bot shows the default instructions, ready to edit.
let shown = "";
for (let tries = 0; tries < 50 && !shown.includes("How you talk"); tries++) {
  shown = (await app.query(editor))[0]?.value ?? "";
  await cua.sleep(200);
}
if (!shown.includes("How you talk")) throw new Error("the default instructions are not shown");
await app.setValue(editor, "Reply in plain words.");
await app.waitFor({ ...editor, value: "Reply in plain words." });
// The heading's button opens the same text, and closing keeps what was typed there.
await app.click({ role: "button", name: "Expand instructions" });
await app.waitFor({ role: "button", name: "Close Instructions", timeout_ms: 5000 });
await app.waitFor({ ...editor, value: "Reply in plain words." });
await app.setValue(editor, "Reply in plain words. Stay brief.");
await app.click({ role: "button", name: "Close Instructions" });
await app.waitFor({ role: "button", name: "Close Instructions", gone: true });
await app.waitFor({ ...editor, value: "Reply in plain words. Stay brief." });
// Closing the profile sends an edit still waiting to save.
await app.click({ role: "button", name: "Edit Brief QA’s profile" });
await app.waitFor({ ...editor, gone: true });
return "ok";

// @relaunch
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Edit Brief QA’s profile", timeout_ms: 10000 });
await app.click({ role: "button", name: "Edit Brief QA’s profile" });
await app.click({ role: "button", name: "Capabilities" });
await app.waitFor({ role: "textarea", name: "Instructions", value: "Reply in plain words. Stay brief.", timeout_ms: 10000 });
return "ok";
