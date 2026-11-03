// @fixture --verify-fixture --verify-plugins
// Arranging keeps Chats in the switcher and the call button last, and reaches the sidebar and chat.
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Settings", timeout_ms: 10000 });
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Plugins" });
await app.click({ role: "button", name: "Arrange plugins" });
await app.click({ role: "button", name: "Move Drive up" });
await app.click({ role: "button", name: "Move Drive up" });
await app.click({ role: "button", name: "Move Hub up" });
await app.click({ role: "button", name: "Close Settings" });
let rows = [];
for (let i = 0; i < 40 && !rows.length; i++) {
  rows = (await app.query({ role: "button", name: "Chief" })).filter((item) => item.label.startsWith("Chief, "));
  if (!rows.length) await cua.sleep(300);
}
await app.click(rows[0].element_token);
await app.waitFor({ role: "button", name: "Drive" });
async function centre(name) {
  const [button] = await app.query({ role: "button", name });
  if (!button) throw new Error(`${name} is missing`);
  return button.frame.x + button.frame.w / 2;
}
const order = [];
for (const name of ["Drive", "Notes", "Tasks", "Speech conversation"]) order.push(await centre(name));
if (order.some((value, index) => index && value <= order[index - 1])) throw new Error("The new order didn't reach the chat");
if (await centre("Hub") >= await centre("Chats")) throw new Error("The switcher didn't reorder");
return "ok";
