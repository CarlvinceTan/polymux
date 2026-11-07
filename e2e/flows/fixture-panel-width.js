// @fixture --verify-fixture --verify-profile --verify-profile-empty --verify-panel t-aria
// Resizing keeps the panel within its limits and remembers the width across chats and relaunches.
const app = await cua.getApp(APP);
async function width(expected) {
    await app.waitFor({ role: "slider", name: "Resize side panel", value: String(expected) });
    const [divider] = await app.query({ role: "slider", name: "Resize side panel" });
    const state = await app.getState({ include_screenshot: false, query: "Resize side panel" });
    const windows = await cua.call("list_windows", { pid: state.pid });
    const window = windows.windows.find(item => item.window_id === state.window_id);
    if (!window || Math.abs(window.bounds.x + window.bounds.width - divider.frame.x - expected) > 2) {
        throw new Error("Panel width does not match its resize control");
    }
}
await app.waitFor({ role: "slider", name: "Resize side panel" });
for (const value of [480, 300, 440]) {
    await app.setValue({ role: "slider", name: "Resize side panel" }, String(value));
    await width(value);
}
await app.click({ role: "button", name: "Close side panel" });
await app.waitFor({ role: "slider", name: "Resize side panel", gone: true });
await app.click({ role: "button", name: "Edit Aria’s profile" });
await width(440);
const [chief] = (await app.query({ role: "button", name: "Chief" })).filter(item => item.label.startsWith("Chief, "));
if (!chief) throw new Error("Chief row is missing");
await app.click(chief.element_token);
await app.waitFor({ role: "textfield", name: "Name", value: "Chief" });
await width(440);
return "ok";
// @relaunch
const app = await cua.getApp(APP);
await app.waitFor({ role: "slider", name: "Resize side panel", value: "440" });
return "ok";
