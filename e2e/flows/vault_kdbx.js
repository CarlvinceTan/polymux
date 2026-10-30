// Import encrypted KDBX directly; editors never receive imported values.
let app = await cua.getApp(APP);
for (let i = 0; i < 20; i++) {
  try { await app.waitFor({ role: "button", name: "New chat", timeout_ms: 5000 }); break; }
  catch (error) { if (i === 19) throw error; await cua.sleep(250); }
}
async function openCard() {
  for (let i = 0; i < 12; i++) {
    try { await app.waitFor({ role: "button", name: "Open private vault card", timeout_ms: 10000 }); return; }
    catch (error) { if (i === 11) throw error; }
  }
}
await app.click({ role: "button", name: "New chat" });
app = await pick(app, "New Bot");
await app.waitFor({ role: "textfield", name: "Name", timeout_ms: 10000 });
await app.typeText("KDBX" + Date.now(), { role: "textfield", name: "Name" });
await app.click({ role: "button", name: "Create" });
await app.waitFor({ role: "textfield", name: "Message", timeout_ms: 10000 });
await app.typeText(`Call vault_import now with exactly this JSON: {"path":"${KDBX}"}. The vault initializes automatically; do not use setup or any other tool. You must call vault_import rather than just replying that a card is ready. Stop immediately after the tool returns.`, { role: "textfield", name: "Message" });
await app.pressKey("return", { role: "textfield", name: "Message" });
await openCard();
await app.click({ role: "button", name: "Open private vault card" });
await app.waitFor({ role: "textfield", name: "KDBX password", timeout_ms: 10000 });
await app.typeText("e2e-kdbx-password", { role: "textfield", name: "KDBX password" });
await app.click({ role: "button", name: "Import encrypted KDBX" });
await app.waitFor({ text: "Completed", timeout_ms: 10000 });
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Vault" });
await app.waitFor({ role: "button", name: "KDBX E2E" });
await app.click({ role: "button", name: "KDBX E2E" });
await app.waitFor({ role: "textfield", name: "Password" });
const [field] = await app.query({ role: "textfield", name: "Password" });
if ((field.value ?? "").includes("e2e-kdbx-secret")) throw new Error("Imported password was read back");
await app.waitFor({ role: "button", name: "Clear Password" });
await app.click({ role: "button", name: "Back" });
await app.click({ role: "button", name: "Close Settings" });
return "ok";
