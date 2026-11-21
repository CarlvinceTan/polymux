// @fixture --verify-fixture --verify-plugins
// A mailbox signs in with its provider, a provider without a client asks for one, and Incognito still marks chats read for you.
let app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Hub", timeout_ms: 10000 });
await app.click({ role: "button", name: "Hub" });
await app.waitFor({ role: "button", name: "Launch team" });
app = await pick(app, "Add account");
await app.waitFor({ role: "button", name: "Sign in with Google" });
await app.click({ role: "button", name: "Sign in with Google" });
await app.waitFor({ text: "me@gmail.example" });
await app.click({ role: "button", name: "Sign in with Microsoft" });
await app.waitFor({ text: "Client ID" });
await app.click({ role: "button", name: "Back" });
await app.waitFor({ role: "button", name: "Sign in with Google" });
await app.click({ role: "button", name: "Close Accounts" });
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Plugins" });
await app.click({ role: "button", name: "Hub" });
await app.click({ role: "checkbox", name: "Incognito" });
await app.click({ role: "button", name: "Close Settings" });
await app.waitFor({ role: "button", name: "Launch team" });
await app.click({ role: "button", name: "Priya Raman" });
await app.waitFor({ text: "Morning! Did you see the deck?" });
const state = await app.getState({ include_screenshot: false, max_elements: 300 });
if (state.tree_markdown.includes("3 unread")) throw new Error("Opening a chat left it unread while incognito");
return "ok";
