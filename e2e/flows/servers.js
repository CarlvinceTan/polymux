// @second-server
// Joining a second network adds it beside the first, each with its own chats and name.
let app = await cua.getApp(APP);
const name = "Second" + Date.now();

async function entries() {
  for (let i = 0; i < 60; i++) {
    const found = (await app.query({ role: "button" })).filter((item) => item.label?.startsWith("Chats on "));
    if (found.length === 2) return found;
    await cua.sleep(500);
  }
  throw new Error("The switcher does not show both servers");
}

async function move(away) {
  const [entry] = (await entries()).filter((item) => !!item.selected !== away);
  await app.click(entry.element_token);
  for (let i = 0; i < 40; i++) {
    if ((await entries()).filter((item) => item.selected).length === 1) return;
    await cua.sleep(250);
  }
  throw new Error("No server is on show");
}

await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Network" });
await app.waitFor({ role: "button", name: "Join network", timeout_ms: 10000 });
await app.click({ role: "button", name: "Join network" });
await app.waitFor({ role: "textfield", name: "Pairing link", timeout_ms: 10000 });
await app.typeText(SECOND_LINK, { role: "textfield", name: "Pairing link" });
await app.pressKey("return", { role: "textfield", name: "Pairing link" });
await entries();

// Pairing moved the app to the new server, so this bot belongs to it alone.
await app.click({ role: "button", name: "New chat" });
app = await pick(app, "New Bot");
await app.waitFor({ role: "textfield", name: "Name", timeout_ms: 10000 });
await app.typeText(name, { role: "textfield", name: "Name" });
await app.click({ role: "button", name: "Create" });
await app.waitFor({ role: "button", name, timeout_ms: 10000 });

await move(true);
for (let i = 0; i < 40 && (await app.query({ role: "button", name })).length; i++) await cua.sleep(250);
if ((await app.query({ role: "button", name })).length) throw new Error("A bot appeared on the wrong server");

await move(true);
await app.waitFor({ role: "button", name, timeout_ms: 10000 });

// Each network gets the next number, and a new name reaches the switcher.
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Network" });
for (const number of [1, 2]) await app.waitFor({ role: "button", name: `Network ${number}`, timeout_ms: 10000 });
await app.click({ role: "button", name: "Network 2" });
await app.waitFor({ role: "textfield", name: "Name", timeout_ms: 5000 });
await app.typeText("Studio", { role: "textfield", name: "Name" });
await app.click({ role: "button", name: "Back" });
await app.waitFor({ role: "button", name: "Studio", timeout_ms: 5000 });
await app.pressKey("escape");
for (let i = 0; i < 40; i++) {
  if ((await entries()).some((item) => item.label === "Chats on Studio")) return "ok";
  await cua.sleep(250);
}
throw new Error("The new name didn't reach the switcher");
