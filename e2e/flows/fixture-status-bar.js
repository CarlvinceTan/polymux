// @fixture --verify-fixture --verify-blocked --verify-markers --verify-still --verify-notch
// The notch pill appears only once turned on in Settings, and leaves when turned off.
// The menu bar icon sits outside the app's accessibility tree, so it is checked by hand.
const app = await cua.getApp(APP);
const state = await app.getState({ include_screenshot: false });
const pid = state.pid;
const others = async () => (await cua.listWindows({ pid })).filter(item => item.window_id !== state.window_id);
const notch = async () => (await others()).find(item => item.title === "Polymux Activity");
async function until(check, message) {
    for (let attempt = 0; attempt < 50; attempt++) {
        if (await check()) return;
        await cua.sleep(100);
    }
    throw new Error(message);
}
if (await notch()) throw new Error("The notch pill shows before it is turned on");
await app.click({ role: "button", name: "Settings" });
await app.waitFor({ name: "Menu bar icon", timeout_ms: 5000 });
const toggle = async (name) => app.click({ role: "checkbox", name, exact: true });
await toggle("Notch activity");
await until(notch, "The notch pill did not appear while a bot works");
const pill = await cua.getApp({ pid, windowId: (await notch()).window_id });
await pill.waitFor({ text: "1 working", timeout_ms: 5000 });
await toggle("Notch activity");
await until(async () => !(await notch())?.is_on_screen, "The notch pill stayed after it was turned off");
return "ok";
