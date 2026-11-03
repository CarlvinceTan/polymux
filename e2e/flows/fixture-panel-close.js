// @fixture --verify-fixture --verify-tools --verify-profile
// The side panel closes without losing the chat window or displacing its phone button.
const app = await cua.getApp(APP);
const main = await app.getState({ include_screenshot: false, max_elements: 50 });
async function row(name) {
    let matches = [];
    for (let i = 0; i < 40 && !matches.length; i++) {
        matches = (await app.query({ role: "button", name })).filter(item => item.label.startsWith(name + ", "));
        if (!matches.length) await cua.sleep(250);
    }
    if (matches.length !== 1) throw new Error("Chat row is missing or ambiguous: " + name);
    await app.click(matches[0].element_token);
}
async function phone() {
    for (let i = 0; i < 20; i++) {
        const buttons = await app.query({ role: "button", name: "Speech conversation" });
        const [mic] = await app.query({ role: "button", name: "Start dictation" });
        const button = buttons[0];
        if (buttons.length === 1 && button.enabled && button.frame && mic?.frame &&
            Math.abs(button.frame.x + button.frame.w - mic.frame.x - mic.frame.w) <= 8) return;
        await cua.sleep(50);
    }
    throw new Error("The phone button is not attached to the chat pane’s right edge");
}
async function closePanel() {
    await phone();
    const [search] = await app.query({ role: "button", name: "Search this chat" });
    const [close] = await app.query({ role: "button", name: "Close side panel" });
    if (!search?.frame || !close?.frame || close.frame.x < search.frame.x + search.frame.w ||
        Math.abs(close.frame.y - search.frame.y) > 2) {
        throw new Error("Close is not to the right of Search in the side panel toolbar");
    }
    await app.click(close.element_token);
    await app.waitFor({ role: "button", name: "Close side panel", gone: true });
    const state = await cua.call("list_windows", { pid: main.pid });
    if (!state.windows.some(window => window.window_id === main.window_id)) throw new Error("Closing the side panel closed the chat window");
    await app.waitFor({ role: "textfield", name: "Message" });
    await phone();
}
await row("Chief");
await phone();
for (let i = 0; i < 3; i++) {
    await app.click({ role: "button", name: "Edit Chief’s profile" });
    await app.waitFor({ role: "textfield", name: "Role" });
    await closePanel();
    await app.waitFor({ role: "textfield", name: "Role", gone: true });
}
await row("grp-trip");
await app.click({ role: "button", name: "Edit chat" });
await app.waitFor({ role: "textfield", name: "Chat name" });
await closePanel();
await app.waitFor({ role: "textfield", name: "Chat name", gone: true });
await app.click({ role: "button", name: "Edit chat" });
await app.click({ role: "button", name: "Add agents to chat" });
await app.waitFor({ role: "textfield", name: "Search agents to add" });
await app.click({ role: "button", name: "Close side panel" });
await app.waitFor({ role: "textfield", name: "Search agents to add", gone: true });
await app.waitFor({ role: "textfield", name: "Message" });
await phone();
return "ok";
