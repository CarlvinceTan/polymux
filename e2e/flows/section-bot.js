// A section's New Bot action retains its destination across relaunch without affecting global creation.
let app = await cua.getApp(APP);
const state = await app.getState({ include_screenshot: false });
const [close] = await app.query({ role: "button", name: "Close side panel" });
if (close) {
    await app.click(close.element_token);
    await app.waitFor({ role: "button", name: "Close side panel", gone: true });
}
const stamp = Date.now();
const seed = "Seed" + stamp;
const section = "Bots" + stamp;
const bot = "SectionBot" + stamp;
const global = "GlobalBot" + stamp;
await app.click({ role: "button", name: "New chat" });
app = await pick(app, "New Bot");
await app.waitFor({ role: "textfield", name: "Name" });
await app.typeText(seed, { role: "textfield", name: "Name" });
await app.click({ role: "button", name: "Create" });
await app.waitFor({ role: "button", name: "Close New Bot", gone: true });
await app.waitFor({ role: "button", name: seed });
const [row] = (await app.query({ role: "button", name: seed })).filter(item => item.label.startsWith(seed + ", "));
if (!row) throw new Error("Seed chat missing");
await app.rightClick(row.element_token);
await pick(app, "Move to", open => open.rightClick({ role: "button", name: seed }));
await app.click({ role: "menuitem", name: "Create section" });
// pick() can return a handle on the menu's window, which closes with it.
app = await cua.getApp({ pid: state.pid, windowId: state.window_id });
await app.waitFor({ role: "textfield", name: "Section name" });
await app.setValue({ role: "textfield", name: "Section name" }, section);
await app.pressKey("return", { role: "textfield", name: "Section name" });
await app.waitFor({ role: "button", name: "Collapse " + section });
await app.click({ role: "button", name: "Collapse " + section });
await app.rightClick({ role: "button", name: "Expand " + section });
await app.click({ role: "menuitem", name: "New Bot" });
await app.waitFor({ role: "textfield", name: "Name" });
return { section, bot, global };

// @relaunch
let app = await cua.getApp(APP);
await app.waitFor({ role: "textfield", name: "Name" });
await app.typeText(PREVIOUS.bot, { role: "textfield", name: "Name" });
await app.click({ role: "button", name: "Create" });
await app.waitFor({ role: "button", name: "Close New Bot", gone: true });
const expand = { role: "button", name: "Expand " + PREVIOUS.section };
const collapse = { role: "button", name: "Collapse " + PREVIOUS.section };
await app.waitFor(expand);
await app.click(expand);
const row = async name => (await app.query({ role: "button", name })).find(item => item.label.startsWith(name + ", "));
for (let i = 0; !(await row(PREVIOUS.bot)); i++) {
    if (i >= 40) throw new Error("The new bot did not appear in its section");
    await cua.sleep(100);
}
await app.click(collapse);
if (await row(PREVIOUS.bot)) throw new Error("The new bot was created outside the requested section");
await app.click({ role: "button", name: "New chat" });
app = await pick(app, "New Bot");
await app.waitFor({ role: "textfield", name: "Name" });
await app.typeText(PREVIOUS.global, { role: "textfield", name: "Name" });
await app.click({ role: "button", name: "Create" });
await app.waitFor({ role: "button", name: "Close New Bot", gone: true });
for (let i = 0; !(await row(PREVIOUS.global)); i++) {
    if (i >= 40) throw new Error("Global creation reused the previous section destination");
    await cua.sleep(100);
}
await app.click(expand);
await app.click(collapse);
if (!(await row(PREVIOUS.global))) throw new Error("Global creation placed the bot in the section");
return "ok";
