// @fresh
// A new Mac app needs no terminal: it runs its own server, and what's on it survives a relaunch.
let app = await cua.getApp(APP);
// A new install opens with the welcome once its server is up.
for (let i = 0; i < 80 && !(await app.query({ role: "button", name: "Skip" })).length; i++) await cua.sleep(500);
await app.waitFor({ role: "statictext", name: "Get Started with Polymux" });
await app.click({ role: "button", name: "Skip" });
const name = "Fresh" + Date.now();
await app.waitFor({ role: "button", name: "New chat", timeout_ms: 10000 });
await app.click({ role: "button", name: "New chat" });
app = await pick(app, "New Bot");
await app.waitFor({ role: "textfield", name: "Name", timeout_ms: 10000 });
await app.typeText(name, { role: "textfield", name: "Name" });
// The button waits for the new server to answer, and goes once the bot exists.
for (let i = 0; i < 40; i++) {
  if ((await app.query({ role: "button", name })).length) break;
  if ((await app.query({ role: "button", name: "Create" })).length) await app.click({ role: "button", name: "Create" });
  await cua.sleep(500);
}
await app.waitFor({ role: "button", name, timeout_ms: 10000 });
return name;
// @relaunch
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: PREVIOUS, timeout_ms: 10000 });
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Network" });
await app.waitFor({ role: "checkbox", name: "Run a server here", timeout_ms: 10000 });
const [toggle] = await app.query({ role: "checkbox", name: "Run a server here" });
if (!toggle) throw new Error("This Mac's server toggle is missing");
if (!["1", "true"].includes(String(toggle.value))) throw new Error(`The toggle reads ${toggle.value}`);

// Stopping the server keeps what's on it, so turning it back on finds the same bot.
const value = async () => String((await app.query({ role: "checkbox", name: "Run a server here" }))[0]?.value);
async function settle(on) {
  for (let i = 0; i < 40; i++) {
    if (["1", "true"].includes(await value()) === on) return;
    await cua.sleep(250);
  }
  throw new Error(`The server didn't turn ${on ? "on" : "off"}`);
}
// Stopping the only server rebuilds Settings, so the toggle can't be pressed for a moment.
async function flip() {
  for (let i = 0; ; i++) {
    try { return await app.click({ role: "checkbox", name: "Run a server here" }); }
    catch (error) { if (i >= 20) throw error; await cua.sleep(250); }
  }
}
await flip();
await settle(false);
await flip();
await settle(true);
await app.pressKey("escape");
await app.waitFor({ role: "button", name: PREVIOUS, timeout_ms: 10000 });
return "ok";
