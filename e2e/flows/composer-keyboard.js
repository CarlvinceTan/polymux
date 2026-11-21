// Cua's modified-key route verifies multiline entry through the real keyboard handler.
let app = await cua.getApp(APP);
const name = "Keyboard" + Date.now();
await app.click({ role: "button", name: "New chat" });
app = await pick(app, "New Bot");
await app.typeText(name, { role: "textfield", name: "Name" });
await app.click({ role: "button", name: "Create" });
await app.waitFor({ role: "textfield", name: "Message", timeout_ms: 10000 });
await app.typeText("First line", { role: "textfield", name: "Message" });
await app.pressKey("shift+return", { role: "textfield", name: "Message" });
await app.typeText("Second line", { role: "textfield", name: "Message" });
await app.waitFor({ role: "textfield", name: "Message", value: "First line\nSecond line" });
await app.pressKey("return", { role: "textfield", name: "Message" });
await app.waitFor({ role: "button", name: "First line\nSecond line", timeout_ms: 10000 });
const [composer] = await app.query({ role: "textfield", name: "Message" });
if (composer?.value?.includes("First line")) throw new Error("Sending did not clear the composer");
return "ok";
