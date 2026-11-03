// @cua
// The compact native creation menu clears its trigger and opens both forms. APP is injected by scripts/run-e2e.sh.
let app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Search" });
await app.waitFor({ role: "button", name: "New chat" });
await app.waitFor({ role: "button", name: "Settings" });
const [trigger] = await app.query({ role: "button", name: "New chat" });
const main = await app.getState({ include_screenshot: false, max_elements: 50 });
// Input elsewhere can close the menu mid-check; reopen it, as pick() does.
let menu;
for (let attempt = 0; !menu; attempt++) {
    try {
        if (!(await app.query({ role: "menuitem", name: "New Bot" })).length) await app.click({ role: "button", name: "New chat" });
        await app.waitFor({ role: "menuitem", name: "New Bot" });
        await app.waitFor({ role: "menuitem", name: "New Group Chat" });
        const state = await cua.call("get_window_state", { pid: main.pid, window_id: main.window_id,
            query: "New Bot", include_screenshot: false, tree_format: "elements", timeout_ms: 5000 });
        if (state.truncated) throw new Error("Incomplete creation menu lookup");
        menu = state.elements.find(item => item.role === "AXMenu");
        if (!menu) throw new Error("Native creation menu is missing");
    } catch (error) {
        if (attempt >= 2) throw error;
        await cua.sleep(250);
        app = await cua.getApp(APP);
    }
}
if (!trigger?.frame || !menu?.frame) throw new Error("Native creation menu bounds are unavailable");
if (menu.frame.y <= trigger.frame.y + trigger.frame.h) {
    throw new Error("The creation menu overlaps its trigger");
}
app = await pick(app, "New Bot");
await app.waitFor({ role: "button", name: "Close New Bot" });
await app.click({ role: "button", name: "Close New Bot" });
await app.click({ role: "button", name: "New chat" });
app = await pick(app, "New Group Chat");
await app.waitFor({ role: "button", name: "Close New Group Chat" });
await app.click({ role: "button", name: "Close New Group Chat" });
return "ok";
