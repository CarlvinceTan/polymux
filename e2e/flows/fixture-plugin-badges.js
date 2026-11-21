// @fixture --verify-fixture --verify-plugins --verify-providers
// Switcher badges sit on the icon's corner and can be turned off, leaving the chat rows' counts alone.
const app = await cua.getApp(APP);
async function count(shown) {
  for (let attempt = 0; attempt < 60; attempt++) {
    const [hub] = await app.query({ role: "button", name: "Hub" });
    if (hub && Boolean(hub.value) === shown) return;
    await cua.sleep(150);
  }
  throw new Error(shown ? "Hub's count is missing with Hub closed" : "Badges stayed on");
}
await count(true);
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "checkbox", name: "Switcher badges" });
await app.click({ role: "button", name: "Close Settings" });
await count(false);
await app.click({ role: "button", name: "Hub" });
await app.waitFor({ role: "button", name: "Priya Raman", value: "3 unread" });
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "checkbox", name: "Switcher badges" });
await app.click({ role: "button", name: "Close Settings" });
await count(true);
return "ok";
