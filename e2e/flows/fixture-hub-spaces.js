// @fixture --verify-fixture --verify-plugins
// A Space folds its chats into one row, opens in place, and goes back to the platform.
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Hub", timeout_ms: 10000 });
await app.click({ role: "button", name: "Hub" });
await app.waitFor({ role: "button", name: "Open Acme, 2 chats" });
if ((await app.query({ role: "button", name: "general" })).length) throw new Error("A Space's chat is listed at the top level");
const list = await app.getState({ include_screenshot: false, max_elements: 300 });
if (!list.tree_markdown.includes("7 unread")) throw new Error("The Space didn't add up its chats' unread counts");
await app.click({ role: "button", name: "Open Acme, 2 chats" });
await app.waitFor({ role: "button", name: "general" });
await app.waitFor({ role: "button", name: "random" });
await app.waitFor({ role: "button", name: "Back to Slack" });
if ((await app.query({ role: "button", name: "Launch team" })).length) throw new Error("A chat outside the Space is listed in it");
await app.click({ role: "button", name: "Back to Slack" });
await app.waitFor({ role: "button", name: "Launch team" });
await app.waitFor({ role: "button", name: "Open Acme, 2 chats" });
return "ok";
