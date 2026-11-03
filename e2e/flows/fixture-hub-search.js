// @fixture --verify-fixture --verify-plugins
// Chat search reaches past the loaded page, filters what a chat shared, and returns to the message.
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Hub", timeout_ms: 10000 });
await app.click({ role: "button", name: "Hub" });
await app.waitFor({ role: "button", name: "Priya Raman" });
await app.click({ role: "button", name: "Priya Raman" });
await app.waitFor({ text: "Morning! Did you see the deck?" });
await app.click({ role: "button", name: "Search chat" });
await app.typeText("budget", { role: "textfield", name: "Search Priya Raman" });
await app.waitFor({ text: "Budget notes are at https://www.example.com/budget" });
await app.click({ role: "button", name: "Links" });
await app.waitFor({ text: "example.com" });
await app.click({ role: "button", name: "Files" });
await app.waitFor({ text: "No results found." });
await app.click({ role: "button", name: "All", nth: 1 });
const [row] = await app.query({ role: "button", name: "Budget notes" });
if (!row) throw new Error("The result isn't a button");
await app.click(row.element_token);
await app.waitFor({ role: "textfield", name: "Message" });
await app.waitFor({ role: "textfield", name: "Search Priya Raman", gone: true });
return "ok";
