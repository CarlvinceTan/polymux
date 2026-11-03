// Muting a bot's alerts keeps the switch off across a relaunch.
let app = await cua.getApp(APP);
const name = "Mutey" + Date.now();
await app.click({ role: "button", name: "New chat" });
app = await pick(app, "New Bot");
await app.waitFor({ role: "textfield", name: "Name", timeout_ms: 10000 });
await app.typeText(name, { role: "textfield", name: "Name" });
await app.click({ role: "button", name: "Create" });
await app.waitFor({ role: "button", name: name, timeout_ms: 10000 });
await app.click({ role: "button", name: `Edit ${name}’s profile` });
await app.waitFor({ role: "button", name: "Capabilities", timeout_ms: 10000 });
await app.click({ role: "button", name: "Mute notifications" });
await app.waitFor({ role: "button", name: "Unmute notifications" });
return name;

// @relaunch
let app = await cua.getApp(APP);
const name = PREVIOUS;
for (let i = 0; ; i++) {
  const rows = (await app.query({ role: "button", name })).filter(item => item.label.startsWith(name + ","));
  if (rows.length) { await app.click(rows[0].element_token); break; }
  if (i >= 30) throw new Error("Muted bot row is missing after relaunch");
  await cua.sleep(500);
}
await cua.sleep(800);
// The chat is still restoring after the relaunch; an early profile click can be swallowed.
let opened = false;
for (let i = 0; i < 10 && !opened; i++) {
  await app.click({ role: "button", name: `Edit ${name}’s profile` }).catch(() => {});
  await cua.sleep(600);
  const state = await app.getState({ query: "Capabilities", include_screenshot: false, timeout_ms: 5000, max_elements: 200, tree_format: "elements" });
  opened = state.elements.some(item => item.label === "Capabilities");
}
if (!opened) throw new Error("profile did not open after relaunch");
await app.waitFor({ role: "button", name: "Unmute notifications" });
await app.click({ role: "button", name: "Unmute notifications" });
await app.waitFor({ role: "button", name: "Mute notifications" });
// Later flows share this app and expect the side panel closed.
await app.click({ role: "button", name: "Close side panel" });
return "ok";
