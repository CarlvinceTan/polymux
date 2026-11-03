// @fresh
// @second-server
// @linked-job
// Joining with chats on this Mac can merge them into the joined network straight away.
let app = await cua.getApp(APP);
const name = "Mine" + Date.now();
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
let merge;
for (let i = 0; i < 40 && !merge; i++) {
  merge = (await app.query({ role: "button" })).find((item) => item.label?.startsWith("Merge into "));
  if (!merge) await cua.sleep(250);
}
if (!merge) throw new Error("The join screen offered no merge");
await app.click(merge.element_token);
await app.click({ role: "button", name: "Done" });
for (let i = 0; i < 30 && !(await app.query({ text: "Stays behind" })).length; i++) await cua.sleep(500);
await app.waitFor({ text: "Stays behind", timeout_ms: 5000 });
await app.click({ role: "button", name: "Merge server" });
await app.waitFor({ text: "Merging…", timeout_ms: 10000 });

// This Mac's chats end up on the joined network, which is then the only one.
for (let i = 0; i < 180 && (await app.query({ text: "Merging…" })).length; i++) await cua.sleep(500);
if ((await app.query({ text: "Merging…" })).length) throw new Error("The merge didn't finish");
await app.pressKey("escape");
for (let i = 0; i < 80 && (await entries()).length; i++) await cua.sleep(500);
if ((await entries()).length) throw new Error("This Mac's network stayed after merging");
for (let i = 0; i < 30 && !(await app.query({ role: "button", name })).length; i++) await cua.sleep(500);
await app.waitFor({ role: "button", name, timeout_ms: 5000 });

// A new server here sits beside the merged one, so later flows find this Mac's server again.
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Network" });
await app.waitFor({ role: "checkbox", name: "Run a server here", timeout_ms: 10000 });
await app.click({ role: "checkbox", name: "Run a server here" });
await app.pressKey("escape");
for (let i = 0; i < 80 && (await entries()).length !== 2; i++) await cua.sleep(500);
if ((await entries()).length !== 2) throw new Error("A new server here didn't join the merged one");
return "ok";
