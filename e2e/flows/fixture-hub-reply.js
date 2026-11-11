// @fixture --verify-fixture --verify-plugins --verify-hub-long
// Replies work as in a bot's chat: the preview sits in the field, a quote jumps to its original, and latest brings you back.
let app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Hub", timeout_ms: 10000 });
await app.click({ role: "button", name: "Hub" });
await app.waitFor({ role: "button", name: "Priya Raman" });
await app.click({ role: "button", name: "Priya Raman" });
const source = "Morning! Did you see the deck?";
await app.waitFor({ text: source });
app = await pick(app, "Reply", (open) => open.rightClick({ role: "button", name: source }));
await app.waitFor({ role: "button", name: "Cancel reply" });
await app.waitFor({ text: "Reply to Priya Raman: " + source });
await app.typeText("On it", { role: "textfield", name: "Message" });
await app.click({ role: "button", name: "Send" });
await app.waitFor({ role: "button", name: "Cancel reply", gone: true });
await app.waitFor({ text: "On it" });
const quote = "Reply to Priya Raman: Message 2, the one this answers.";
await app.click({ role: "button", name: quote });
await app.waitFor({ role: "button", name: "Scroll to latest" });
await app.waitFor({ text: "Message 2, long enough" });
await app.click({ role: "button", name: "Scroll to latest" });
await app.waitFor({ role: "button", name: "Scroll to latest", gone: true });
return "ok";
