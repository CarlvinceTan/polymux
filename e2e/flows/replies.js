// Replies and reactions keep their identity after an app relaunch.
let app = await cua.getApp(APP);
const name = "Replies" + Date.now();
const source = "Reply target " + name;
async function message() {
  for (let i = 0; i < 30; i++) {
    const matches = (await app.query({ role: "button", name: source })).filter(row => row.label === source);
    if (matches.length === 1 && matches[0].element_token) return matches[0].element_token;
    if (matches.length > 1) throw new Error("Message target is ambiguous");
    await cua.sleep(100);
  }
  throw new Error("Exact message target missing; refusing to target the chat preview");
}
// Row two of the reaction menu: picks, then system recents, then defaults.
async function recents() {
  const labels = (await app.query({ role: "menuitem" })).map(row => row.label?.replace(/^React /, "") ?? "");
  return labels.filter(label => /\p{Extended_Pictographic}/u.test(label) && !["❤️", "👍", "👎", "😂", "‼️", "❓"].includes(label));
}
async function choose(item) {
  await cua.sleep(150);
  await app.click({ role: "menuitem", name: item });
  await cua.sleep(250);
  app = await cua.getApp({ pid: host.pid, windowId: host.window_id });
}
await app.click({ role: "button", name: "New chat" });
await app.waitFor({ role: "menuitem", name: "New Bot" });
app = await pick(app, "New Bot");
await app.typeText(name, { role: "textfield", name: "Name" });
await app.click({ role: "button", name: "Create" });
await app.typeText(source, { role: "textfield", name: "Message" });
await app.click({ role: "button", name: "Send message" });
await app.waitFor({ role: "button", name: source });
const host = await app.getState({ include_screenshot: false });
app = await cua.getApp({ pid: host.pid, windowId: host.window_id });
await app.rightClick(await message());
await choose("Reply");
await app.click({ role: "button", name: "Cancel reply" });
await app.waitFor({ role: "button", name: "Cancel reply", gone: true });
await app.rightClick(await message());
const menu = await app.query({ role: "menuitem" });
for (const emoji of ["❤️", "👍", "👎", "😂", "‼️", "❓"]) {
  if (!menu.some(row => row.label === "React " + emoji)) throw new Error("Quick reaction missing: " + emoji);
}
await choose("👍");
await app.waitFor({ role: "checkbox", name: "Reaction 👍, 1, You" });
await app.click({ role: "checkbox", name: "Reaction 👍, 1, You" });
await app.waitFor({ role: "checkbox", name: "Reaction 👍, 1, You", gone: true });
await app.rightClick(await message());
const recent = (await recents())[0];
if (!recent) throw new Error("Recent reactions missing");
await choose(recent);
await app.waitFor({ role: "checkbox", name: "Reaction " + recent + ", 1, You" });
const draft = "Keep this draft " + name;
await app.typeText(draft, { role: "textfield", name: "Message" });
await app.rightClick(await message());
await choose("More");
await app.typeText("unicorn", { role: "textfield", name: "Search Emoji" });
await app.click({ role: "button", name: "Flags" });
await app.waitFor({ role: "button", name: "React 🏁" });
await app.typeText("unicorn", { role: "textfield", name: "Search Emoji" });
await app.click({ role: "button", name: "React 🦄" });
await cua.sleep(250);
app = await cua.getApp({ pid: host.pid, windowId: host.window_id });
await app.waitFor({ role: "checkbox", name: "Reaction 🦄, 1, You" });
const composer = await app.query({ role: "textfield", name: "Message" });
if (composer.length !== 1 || composer[0].value !== draft) throw new Error("Picking a reaction changed the draft");
await app.pressKey("cmd+a", { role: "textfield", name: "Message" });
await app.pressKey("delete", { role: "textfield", name: "Message" });
await app.waitFor({ role: "button", name: "Send message", gone: true });
await app.rightClick(await message());
if ((await recents())[0] !== "🦄") throw new Error("A picked reaction must lead the recents");
await choose("❤️");
await app.waitFor({ role: "checkbox", name: "Reaction ❤️, 1, You" });
await app.rightClick(await message());
await choose("Reply");
await app.typeText("Quoted reply " + name, { role: "textfield", name: "Message" });
await app.click({ role: "button", name: "Send message" });
await app.waitFor({ role: "button", name: "Reply to You: " + source });
return { name, source, recent };

// @relaunch
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: PREVIOUS.name + ", ", timeout_ms: 10000 });
await app.click({ role: "button", name: PREVIOUS.name + ", ", nth: 0 });
await app.waitFor({ role: "button", name: "Reply to You: " + PREVIOUS.source });
await app.waitFor({ role: "checkbox", name: "Reaction ❤️, 1, You" });
await app.waitFor({ role: "checkbox", name: "Reaction " + PREVIOUS.recent + ", 1, You" });
await app.waitFor({ role: "checkbox", name: "Reaction 🦄, 1, You" });
return "ok";
