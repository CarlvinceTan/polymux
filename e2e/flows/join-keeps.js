// @fresh
// @second-server
// A server with something on it stays when this Mac joins another, and merging them is a choice.
let app = await cua.getApp(APP);
const name = "Keep" + Date.now();
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

let entries = [];
for (let i = 0; i < 60 && entries.length !== 2; i++) {
  entries = (await app.query({ role: "button" })).filter((item) => item.label?.startsWith("Chats on "));
  if (entries.length !== 2) await cua.sleep(500);
}
if (entries.length !== 2) throw new Error("Joining replaced a server that had data");

await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Network" });
await app.waitFor({ role: "checkbox", name: "Run a server here", timeout_ms: 10000 });
const [toggle] = await app.query({ role: "checkbox", name: "Run a server here" });
if (!["1", "true"].includes(String(toggle?.value))) throw new Error("This Mac's server stopped");
await app.pressKey("escape");
return "ok";
