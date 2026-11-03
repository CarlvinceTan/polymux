// @fixture --verify-settings --verify-updates
// A refused appcast connection reports its result inside Settings without a Sparkle dialog.
const app = await cua.getApp(APP);
await app.click({ role: "button", name: "Settings" });
await app.waitFor({ role: "button", name: "Updates", timeout_ms: 10000 });
await app.click({ role: "button", name: "Updates" });
await app.click({ role: "button", name: "Check for updates" });
await app.waitFor({ role: "statictext", name: "Couldn’t check for updates.", timeout_ms: 10000 });
const button = await app.query({ role: "button", name: "Check for updates" });
if (!button.some(item => item.enabled)) throw new Error("Update check did not finish");
for (const name of ["Cancel Update", "Software Update"]) {
  if ((await app.query({ name })).length) throw new Error("Update check opened " + name);
}
const state = await app.getState({ include_screenshot: false });
const windows = (await cua.listWindows()).filter(window => window.pid === state.pid && window.layer === 0);
if (windows.length !== 1) throw new Error("Update check opened an extra window");
return "ok";
