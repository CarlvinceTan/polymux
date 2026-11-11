// @fixture --verify-fixture --verify-plugins
// Hub replaces the sidebar and content, shows unread counts as pills, and sends optimistically.
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Hub", timeout_ms: 10000 });
await app.click({ role: "button", name: "Hub" });
await app.waitFor({ role: "button", name: "Priya Raman" });
const list = await app.getState({ include_screenshot: false, max_elements: 300 });
for (const count of ["3 unread", "124 unread"]) {
  if (!list.tree_markdown.includes(count)) throw new Error(`${count} is missing`);
}
await app.click({ role: "button", name: "Priya Raman" });
await app.waitFor({ text: "Morning! Did you see the deck?" });
const [tab] = await app.query({ role: "button", name: "Hub" });
const [attach] = await app.query({ role: "button", name: "Add attachment" });
if (Math.abs(tab.frame.y + tab.frame.h / 2 - attach.frame.y - attach.frame.h / 2) > 1) throw new Error("The switcher isn't level with Hub's composer");
await app.typeText("Thanks, on it", { role: "textfield", name: "Message" });
await app.click({ role: "button", name: "Send" });
await app.waitFor({ text: "Thanks, on it" });
await app.click({ role: "button", name: "Telegram" });
await app.waitFor({ role: "button", name: "Sam" });
if ((await app.query({ role: "button", name: "Launch team" })).length) throw new Error("Another platform's chat is listed");
return "ok";
