// Chat-driven vault setup and manual entry remain available through private cards.
let app = await cua.getApp(APP);
async function ready() {
  for (let i = 0; i < 9; i++) {
    try { await app.waitFor({ role: "button", name: "Open private vault card", timeout_ms: 10000 }); return; }
    catch (error) { if (i === 8) throw error; }
  }
}
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Vault" });
await app.waitFor({ role: "textfield", name: "Search vault", timeout_ms: 5000 });
if ((await app.query({ role: "textfield", name: "Vault master password" })).length) throw new Error("Vault browsing is gated by a password");
await app.click({ role: "button", name: "Close Settings" });
const name = "Private" + Date.now();
await app.click({ role: "button", name: "New chat" });
app = await pick(app, "New Bot");
await app.waitFor({ role: "textfield", name: "Name", timeout_ms: 10000 });
await app.typeText(name, { role: "textfield", name: "Name" });
await app.click({ role: "button", name: "Create" });
await app.waitFor({ role: "button", name: name, timeout_ms: 10000 });
await app.typeText("Call vault_manage with operation add, kind login, title Private check, url https://example.com. Show the private entry card and stop. Never request secrets in chat.", { role: "textfield", name: "Message" });
await app.pressKey("return", { role: "textfield", name: "Message" });
await ready();
await app.click({ role: "button", name: "Open private vault card" });
await app.waitFor({ role: "textfield", name: "Password" });
if ((await app.query({ name: "Vault master password" })).length) throw new Error("Private card asks for authentication");
await app.typeText("e2e-private-value", { role: "textfield", name: "Password" });
await app.click({ role: "button", name: "Save private vault entry" });
await app.waitFor({ text: "Completed" });
return name;
// @relaunch
let app = await cua.getApp(APP);
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Vault" });
await app.waitFor({ role: "button", name: "Private check" });
await app.click({ role: "button", name: "Private check" });
await app.waitFor({ role: "textfield", name: "Password" });
const [field] = await app.query({ role: "textfield", name: "Password" });
if ((field.value ?? "").includes("e2e-private-value")) throw new Error("Private card value was read back");
if ((await app.query({ name: "Reveal" })).length) throw new Error("Reveal control remains");
await app.click({ role: "button", name: "Delete vault item" });
await app.waitFor({ role: "button", name: "Private check", gone: true });
await app.click({ role: "button", name: "Close Settings" });
return "ok";
