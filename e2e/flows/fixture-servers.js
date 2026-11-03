// @fixture --verify-fixture --verify-plugins --verify-servers
// Every network has a switcher entry, each keeps its own place, and one can be left.
const app = await cua.getApp(APP);
const home = { role: "button", name: "Home server" };
const studio = { role: "button", name: "Studio" };
const hub = { role: "button", name: "Hub on Home server" };
const entry = (target) => ({ role: "button", name: `Chats on ${target.name}` });

async function selected(target) {
  for (let i = 0; i < 40; i++) {
    const [button] = await app.query(target);
    if (button?.selected) return;
    await cua.sleep(250);
  }
  throw new Error(`${target.name} is not selected`);
}

async function open(name) {
  let rows = [];
  for (let i = 0; i < 40 && !rows.length; i++) {
    rows = (await app.query({ role: "button", name })).filter((item) => item.label.startsWith(`${name}, `));
    if (!rows.length) await cua.sleep(250);
  }
  if (!rows.length) throw new Error(`${name} row is missing`);
  await app.click(rows[0].element_token);
}

for (const target of [entry(home), entry(studio), hub]) await app.waitFor({ ...target, timeout_ms: 10000 });
await selected(entry(home));
await open("Chief");

await app.click(entry(studio));
await selected(entry(studio));
await open("Aria");

// Hub belongs to the first server, so its entry moves the app back there.
await app.click(hub);
await selected(hub);
await app.waitFor({ text: "Priya Raman", timeout_ms: 10000 });

// Each server reopens on the chat it was left on.
await app.click(entry(home));
await selected(entry(home));
await app.waitFor({ role: "button", name: "Chief", timeout_ms: 10000 });
await app.click(entry(studio));
await selected(entry(studio));
await app.waitFor({ role: "button", name: "Aria", timeout_ms: 10000 });

await app.click(entry(home));
await selected(entry(home));
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Network" });
await app.waitFor(home);
await app.waitFor(studio);
await app.click(home);

// A restricted device is let back in from its page.
await app.click({ role: "button", name: "Old iPad" });
await app.waitFor({ role: "button", name: "Allow Old iPad", timeout_ms: 3000 });
await app.click({ role: "button", name: "Back" });

await app.click({ role: "button", name: "Back" });
await app.click(studio);
await app.click({ role: "button", name: "Leave Studio" });
for (let i = 0; i < 20; i++) {
  const leave = (await app.query({ role: "button" })).find((item) => item.label === "Leave");
  if (leave) { await app.click(leave.element_token); break; }
  await cua.sleep(250);
}
await app.pressKey("escape");

// One server left, so the switcher goes back to that server's own entries.
for (let i = 0; i < 40; i++) {
  if (!(await app.query(entry(home))).length && !(await app.query(entry(studio))).length) return "ok";
  await cua.sleep(250);
}
throw new Error("Leaving a network left it in the switcher");
