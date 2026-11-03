// @fresh
// @second-server
// @linked-job
// A server moves to another network: its chats go with it, and the app follows without pairing again.
let app = await cua.getApp(APP);
const name = "Moved" + Date.now();
const entries = async () => (await app.query({ role: "button" })).filter((item) => item.label?.startsWith("Chats on "));

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

await network(true);
await app.waitFor({ role: "button", name: "Move server", timeout_ms: 10000 });
await app.click({ role: "button", name: "Move server" });
await app.waitFor({ text: "Stays on this Mac", timeout_ms: 10000 });
await app.click({ role: "button", name: "Move server" });
await app.waitFor({ text: "Moving…", timeout_ms: 10000 });

// The page comes back by itself, the two servers become one, and the bot is on it.
for (let i = 0; i < 160 && (await app.query({ text: "Moving…" })).length; i++) await cua.sleep(500);
if ((await app.query({ text: "Moving…" })).length) throw new Error("The move didn't finish");
await app.pressKey("escape");
for (let i = 0; i < 80 && (await entries()).length; i++) await cua.sleep(500);
if ((await entries()).length) throw new Error("The old server stayed");
await app.waitFor({ role: "button", name, timeout_ms: 10000 });

// This Mac's address is free again, so a new server here sits beside the one that moved.
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Network" });
await app.waitFor({ role: "button", name: "Delete old server copy", timeout_ms: 10000 });
await app.click({ role: "checkbox", name: "Run a server here" });
await app.pressKey("escape");
for (let i = 0; i < 80 && (await entries()).length !== 2; i++) await cua.sleep(500);
if ((await entries()).length !== 2) throw new Error("A new server here didn't join the one that moved");
return "ok";
