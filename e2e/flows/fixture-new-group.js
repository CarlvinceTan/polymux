// @fixture --verify-fixture --verify-tools -lastCreation.http://127.0.0.1:7337 group
// Recipient chips and the optional group name survive a crash.
const app = await cua.getApp(APP);
await app.waitFor({ role: "textfield", name: "To", timeout_ms: 10000 });
await app.click({ role: "button", name: "Select Aria" });
await app.waitFor({ role: "button", name: "Remove Aria" });
await app.waitFor({ text: "Choose at least two bots." });
const [create] = await app.query({ role: "button", name: "Create" });
if (create && !create.display_only && create.enabled !== false) throw new Error("A one-bot group can be created");
await app.typeText("Milo", { role: "textfield", name: "To" });
await app.click({ role: "button", name: "Select Milo" });
await app.waitFor({ role: "button", name: "Remove Milo" });
await app.typeText("Crash Group", { role: "textfield", name: "Chat name (optional)" });
await app.click({ role: "button", name: "Remove Aria" });
await app.waitFor({ role: "button", name: "Select Aria" });
await app.click({ role: "button", name: "Select Aria" });
await app.waitFor({ role: "textfield", name: "Chat name (optional)", value: "Crash Group" });
await cua.sleep(800);
return "ok";
// @crash
const app = await cua.getApp(APP);
await app.waitFor({ role: "textfield", name: "Chat name (optional)", value: "Crash Group", timeout_ms: 10000 });
for (const name of ["Aria", "Milo"]) {
  await app.waitFor({ role: "button", name: "Remove " + name });
  if ((await app.query({ role: "button", name: "Select " + name })).length) throw new Error(name + " is still in the available list");
}
return "ok";
