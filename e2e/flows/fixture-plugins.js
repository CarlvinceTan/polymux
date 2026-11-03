// @fixture --verify-fixture --verify-plugins
// Scoped buttons follow the chat, keep the shared layout, and swap the side panel at its width.
const app = await cua.getApp(APP);
async function chat(name) {
  let rows = [];
  for (let i = 0; i < 60 && !rows.length; i++) {
    rows = (await app.query({ role: "button", name })).filter((item) => item.label.startsWith(`${name}, `));
    if (!rows.length) await cua.sleep(500);
  }
  if (!rows.length) throw new Error(`${name} row is missing`);
  await app.click(rows[0].element_token);
}
async function centre(name) {
  const [button] = await app.query({ role: "button", name });
  if (!button) throw new Error(`${name} is missing`);
  return button.frame.x + button.frame.w / 2;
}
await chat("Chief");
for (const name of ["Notes", "Tasks", "Drive", "Speech conversation"]) await app.waitFor({ role: "button", name });
if ((await app.query({ role: "button", name: "Quiet" })).length) throw new Error("A plugin with no UI has a button");
const order = [];
for (const name of ["Notes", "Tasks", "Drive", "Speech conversation"]) order.push(await centre(name));
if (order.some((value, index) => index && value <= order[index - 1])) throw new Error("Buttons left the shared order");

await app.click({ role: "button", name: "Notes" });
await app.waitFor({ text: "Notes for t-chief" });
await app.waitFor({ role: "button", name: "Close side panel" });
// The panel slides in, so its width only counts once the header stops moving.
async function settled(name) {
  let last = await centre(name);
  for (let attempt = 0; attempt < 20; attempt++) {
    await cua.sleep(100);
    const next = await centre(name);
    if (Math.abs(next - last) < 0.5) return next;
    last = next;
  }
  throw new Error(`${name} never settled`);
}
const panelled = await settled("Edit Chief’s profile");
await app.click({ role: "button", name: "Edit Chief’s profile" });
await app.waitFor({ role: "textfield", name: "Name", value: "Chief" });
if (Math.abs(await settled("Edit Chief’s profile") - panelled) > 1) throw new Error("The avatar changed the panel's width");
await app.click({ role: "button", name: "Drive" });
await app.waitFor({ text: "Drive for t-chief" });
await app.click({ role: "button", name: "Drive" });
await app.waitFor({ text: "Drive for t-chief", gone: true });

const middle = async (name) => {
  const [button] = await app.query({ role: "button", name });
  if (!button) throw new Error(`${name} is missing`);
  return button.frame.y + button.frame.h / 2;
};
if (Math.abs(await middle("Hub") - await middle("Add attachment")) > 1) throw new Error("The switcher isn't level with the composer");

await chat("Aria");
await app.waitFor({ role: "button", name: "Drive" });
if ((await app.query({ role: "button", name: "Notes" })).length) throw new Error("Notes isn't active in this chat");
await app.click({ role: "button", name: "Hub" });
await app.waitFor({ role: "button", name: "Priya Raman" });
await app.waitFor({ role: "button", name: "Search" });
await app.click({ role: "button", name: "Chats" });
await app.waitFor({ role: "button", name: "Priya Raman", gone: true });
return "ok";
