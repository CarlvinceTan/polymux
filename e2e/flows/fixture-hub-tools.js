// @fixture --verify-fixture --verify-plugins
// Hub shares the sidebar's Search and + buttons, and its selector can show logos alone.
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Hub", timeout_ms: 10000 });
await app.click({ role: "button", name: "Hub" });
await app.waitFor({ role: "button", name: "Priya Raman" });
if ((await app.query({ role: "textfield", name: "Search" })).length) throw new Error("Hub still has a search bar");
await app.click({ role: "button", name: "Unread" });
await app.waitFor({ role: "button", name: "Sam", gone: true });
await app.click({ role: "button", name: "All" });
await app.click({ role: "button", name: "Search" });
await app.typeText("Sam", { role: "textfield", name: "Search Hub" });
await app.click({ role: "button", name: "Sam" });
await app.waitFor({ role: "textfield", name: "Search Hub", gone: true });
const before = (await app.query({ role: "button", name: "WhatsApp" }))[0].frame.w;
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Plugins" });
await app.click({ role: "button", name: "Hub" });
await app.click({ role: "button", name: "Platform selector" });
await app.click({ role: "button", name: "Logo" });
await app.click({ role: "checkbox", name: "Incognito" });
await app.click({ role: "button", name: "Back" });
await app.click({ role: "button", name: "Hub" });
await app.waitFor({ role: "checkbox", name: "Incognito", value: "1" });
await app.click({ role: "button", name: "Close Settings" });
for (let attempt = 0; attempt < 20; attempt++) {
  const [pill] = await app.query({ role: "button", name: "WhatsApp" });
  if (pill.frame.w < before) return "ok";
  await cua.sleep(150);
}
const [pill] = await app.query({ role: "button", name: "WhatsApp" });
throw new Error(`The selector didn't change style: ${before} then ${pill.frame.w}`);
