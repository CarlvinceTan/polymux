// @fresh
// @second-server
// @linked-job
// Two networks become one: both sets of bots stay, same names told apart, and the old server goes.
let app = await cua.getApp(APP);
const stamp = Date.now();
const here = "Here" + stamp, there = "There" + stamp, same = "Same" + stamp;
const entries = async () => (await app.query({ role: "button" })).filter((item) => item.label?.startsWith("Chats on "));

async function bot(name) {
  await app.click({ role: "button", name: "New chat" });
  app = await pick(app, "New Bot");
  await app.waitFor({ role: "textfield", name: "Name", timeout_ms: 10000 });
  await app.typeText(name, { role: "textfield", name: "Name" });
  // The button goes once the bot exists, which can beat the row to the screen.
  for (let i = 0; i < 40; i++) {
    if ((await app.query({ role: "button", name })).length) return;
    if ((await app.query({ role: "button", name: "Create" })).length) await app.click({ role: "button", name: "Create" });
    await cua.sleep(500);
  }
  throw new Error(`${name} wasn't created`);
}

// Both entries read the same on one machine, so the move shows in which is selected. A rebuilt
// switcher can refuse a click on what it just listed, so each try reads it fresh.
async function other() {
  const from = (await entries()).findIndex((item) => item.selected);
  for (let i = 0; i < 20; i++) {
    const list = await entries();
    if (list.length === 2 && list.findIndex((item) => item.selected) === 1 - from) return;
    try { await app.click(list[1 - from].element_token); } catch (error) { await cua.sleep(250); }
    await cua.sleep(500);
  }
  throw new Error("Couldn't move to the other server");
}

await app.waitFor({ role: "button", name: "New chat", timeout_ms: 10000 });
await bot(here);
await bot(same);
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Network" });
await app.waitFor({ role: "button", name: "Join network", timeout_ms: 10000 });
await app.click({ role: "button", name: "Join network" });
await app.waitFor({ role: "textfield", name: "Pairing link", timeout_ms: 10000 });
await app.typeText(SECOND_LINK, { role: "textfield", name: "Pairing link" });
await app.pressKey("return", { role: "textfield", name: "Pairing link" });
await app.waitFor({ role: "button", name: "Keep separate", timeout_ms: 10000 });
await app.click({ role: "button", name: "Done" });
for (let i = 0; i < 60 && (await entries()).length !== 2; i++) await cua.sleep(500);
if ((await entries()).length !== 2) throw new Error("The other network did not appear");
await cua.sleep(1000);
await bot(there);
await bot(same);
await other();
await app.waitFor({ role: "button", name: here, timeout_ms: 10000 });

async function network(here) {
  await app.click({ role: "button", name: "Settings" });
  await app.click({ role: "button", name: "Network" });
  for (let i = 0; i < 40; i++) {
    const rows = (await app.query({ role: "button" })).filter((item) => item.label?.startsWith("Network "));
    const row = rows.find((item) => (item.value || "").startsWith("This Mac") === here);
    if (row) return app.click(row.element_token);
    await cua.sleep(250);
  }
  throw new Error("The network's row is missing");
}

await network(false);
await app.waitFor({ role: "button", name: "Merge network", timeout_ms: 10000 });
await app.click({ role: "button", name: "Merge network" });
await app.waitFor({ text: "Stays behind", timeout_ms: 10000 });
await app.click({ role: "button", name: "Merge server" });
await app.waitFor({ text: "Merging…", timeout_ms: 10000 });

// The page comes back by itself once the other server has gone from this device.
for (let i = 0; i < 180 && (await app.query({ text: "Merging…" })).length; i++) await cua.sleep(500);
if ((await app.query({ text: "Merging…" })).length) throw new Error("The merge didn't finish");
await app.pressKey("escape");
for (let i = 0; i < 80 && (await entries()).length; i++) await cua.sleep(500);
if ((await entries()).length) throw new Error("The merged server stayed");

// All of it is here, on one server, and the clash is told apart.
for (const name of [here, there, same]) await app.waitFor({ role: "button", name, timeout_ms: 10000 });
const twins = (await app.query({ role: "button" })).filter((item) => item.label?.includes(same));
if (twins.length < 2) throw new Error(`Bots with the same name didn't both stay: ${twins.map((item) => item.label)}`);
return "ok";
