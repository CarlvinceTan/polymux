// @fresh
// @second-server
// Joining a network from a new install leaves no extra server behind.
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Settings", timeout_ms: 10000 });
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Network" });
await app.waitFor({ role: "checkbox", name: "Run a server here", timeout_ms: 10000 });
await app.click({ role: "button", name: "Join network" });
await app.waitFor({ role: "textfield", name: "Pairing link", timeout_ms: 10000 });
await app.typeText(SECOND_LINK, { role: "textfield", name: "Pairing link" });
await app.pressKey("return", { role: "textfield", name: "Pairing link" });

await app.waitFor({ role: "button", name: "Settings", timeout_ms: 10000 });
for (let i = 0; i < 40; i++) {
  await cua.sleep(250);
  const entries = (await app.query({ role: "button" })).filter((item) => item.label?.startsWith("Chats on "));
  if (!entries.length && i > 8) break;
  if (i === 39) throw new Error("The empty server stayed");
}
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Network" });
await app.waitFor({ role: "checkbox", name: "Run a server here", timeout_ms: 10000 });
const [toggle] = await app.query({ role: "checkbox", name: "Run a server here" });
if (["1", "true"].includes(String(toggle.value))) throw new Error("This Mac's server is still running");
await app.pressKey("escape");
return "ok";
