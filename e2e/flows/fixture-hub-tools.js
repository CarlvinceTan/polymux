// @fixture --verify-fixture --verify-plugins
// Hub shares the sidebar's Search and + buttons, and both selector modes keep filters usable.
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
async function pillWidth() {
  const [pill] = await app.query({ role: "button", name: "WhatsApp" });
  return pill?.frame.w;
}
async function waitForWidth(matches, error) {
  for (let attempt = 0; attempt < 20; attempt++) {
    if (matches(await pillWidth())) return;
    await cua.sleep(150);
  }
  throw new Error(error);
}
const before = await pillWidth();
if (!before) throw new Error("WhatsApp filter is missing in Full");
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Plugins" });
await app.click({ role: "button", name: "Hub" });
await app.waitFor({ role: "button", name: "Platform selector", value: "Full" });
await app.click({ role: "button", name: "Platform selector" });
await app.click({ role: "button", name: "Compact" });
await app.click({ role: "checkbox", name: "Incognito" });
await app.click({ role: "button", name: "Back" });
await app.click({ role: "button", name: "Hub" });
await app.waitFor({ role: "checkbox", name: "Incognito", value: "1" });
await app.waitFor({ role: "button", name: "Platform selector", value: "Compact" });
await app.click({ role: "button", name: "Close Settings" });
await waitForWidth(width => width < before, "Compact wasn't narrower than Full");
await app.click({ role: "button", name: "WhatsApp" });
await app.waitFor({ role: "button", name: "Launch team", gone: true });
await app.waitFor({ role: "button", name: "Priya Raman" });
await app.click({ role: "button", name: "All" });
await app.waitFor({ role: "button", name: "Launch team" });
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Plugins" });
await app.click({ role: "button", name: "Hub" });
await app.click({ role: "button", name: "Platform selector" });
await app.click({ role: "button", name: "Full" });
await app.click({ role: "button", name: "Close Settings" });
await waitForWidth(width => width === before, "Selecting Full didn't restore the platform labels");
return "ok";
