// @fixture --verify-fixture --verify-settings -serverAddress http://127.0.0.1:1
// Invalid links stay open; an unreachable invite must not replace the existing server.
let app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Settings", timeout_ms: 10000 });
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "General" });
await app.waitFor({ text: "Devices", timeout_ms: 10000 });
await app.waitFor({ role: "button", name: "Home server", timeout_ms: 10000 });
if ((await app.query({ role: "button", name: "Server" })).length) throw new Error("Expected one saved server before pairing");
await app.click({ role: "button", name: "Add device" });
await app.waitFor({ role: "button", name: "Copy link", timeout_ms: 10000 });
await app.typeText("not-a-pairing-link", { role: "textfield", name: "Pairing link" });
await app.pressKey("return", { role: "textfield", name: "Pairing link" });
await app.waitFor({ text: "Paste a valid Polymux pairing link.", timeout_ms: 5000 });
for (const link of [
  "polymux://connect?url=not-a-url&token=test",
  "polymux://connect?url=http://127.0.0.1:7337&token=",
  "polymux://connect?url=http://one&url=http://two&token=test",
]) {
  await app.setValue({ role: "textfield", name: "Pairing link" }, "");
  await app.typeText(link, { role: "textfield", name: "Pairing link" });
  await app.pressKey("return", { role: "textfield", name: "Pairing link" });
  await app.waitFor({ text: "Paste a valid Polymux pairing link.", timeout_ms: 5000 });
  await app.waitFor({ role: "button", name: "Copy link" });
}
// Loopback refusal is deterministic and needs no relay or device credential.
const link = "polymux://connect?url=http%3A%2F%2F127.0.0.1%3A1&token=fixture-only";
await app.pressKey("cmd+a", { role: "textfield", name: "Pairing link" });
await app.typeText(link, { role: "textfield", name: "Pairing link" });
await app.pressKey("return", { role: "textfield", name: "Pairing link" });
await app.waitFor({ text: "Can’t reach this server. Check its connection and try the link again.", timeout_ms: 10000 });
await app.waitFor({ role: "textfield", name: "Pairing link" });
await app.click({ role: "button", name: "Back" });
await app.waitFor({ role: "button", name: "Home server", timeout_ms: 5000 });
// A second saved server would reveal the otherwise hidden Server picker.
if ((await app.query({ role: "button", name: "Server" })).length) throw new Error("Failed pairing added a saved server");
return "ok";
