// @fixture --verify-fixture --verify-pins
// @cua
// Closing and reopening the window keeps the process and titlebar controls usable.
let app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Settings" });
const initial = await app.getState({ include_screenshot: false, max_elements: 30 });
const { apps } = await cua.call("list_apps", {});
const running = apps.find(item => item.pid === initial.pid);
if (!running?.launch_path) throw new Error("Missing the isolated app's launch path");
for (let i = 0; i < 5; i++) {
    const before = await app.getState({ include_screenshot: false, max_elements: 30 });
    await app.click({ role: "button", name: "Close" });
    let closed;
    for (let attempt = 0; attempt < 20; attempt++) {
        closed = await cua.call("list_windows", { pid: initial.pid });
        if (!closed.windows.some(window => window.window_id === before.window_id && window.is_on_screen)) break;
        await cua.sleep(50);
    }
    if (closed.windows.some(window => window.window_id === before.window_id && window.is_on_screen)) {
        throw new Error("The chat window did not close: " + JSON.stringify(closed));
    }
    const opened = await cua.call("launch_app", { name: running.launch_path });
    if (opened.pid !== initial.pid) throw new Error("Closing the window unexpectedly stopped the app");
    app = await cua.getApp(APP);
    await app.waitFor({ role: "button", name: "Settings" });
    await app.click({ role: "button", name: "Settings" });
    await app.waitFor({ role: "button", name: "Close Settings" });
    await app.click({ role: "button", name: "Close Settings" });
    await app.waitFor({ role: "button", name: "Search" });
}
return "ok";
