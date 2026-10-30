// Settings dropdowns stay below their row and keep the same right edge as selections change.
const window = (await cua.listWindows()).find(window => window.app_name === APP && window.layer === 0);
const target = { pid: window.pid, windowId: window.window_id };
const app = await cua.getApp(target);
async function choose(name, value) {
    const trigger = { role: "button", name };
    const [control] = await app.query(trigger);
    await app.click(trigger);
    await app.waitFor({ role: "button", name: value, timeout_ms: 3000 });
    const firstName = { Theme: "Light", Language: "System", "Voice mode": "Take turns" }[name];
    const [first] = await app.query({ role: "button", name: firstName });
    if (!control?.frame || !first?.frame) throw new Error(name + " menu has no bounds");
    if (first.frame.y < control.frame.y + control.frame.h) {
        throw new Error(name + " menu covers its setting row: " + JSON.stringify({ control: control.frame, first: first.frame }));
    }
    const offset = first.frame.x + first.frame.w - control.frame.x - control.frame.w;
    if (Math.abs(offset + 4) > 1) throw new Error(name + " menu is offset by " + offset);
    await app.click({ role: "button", name: value });
    await app.waitFor({ ...trigger, value, timeout_ms: 5000 });
}
await app.click({ role: "button", name: "Settings" });
for (const theme of ["Light", "Dark", "System"]) await choose("Theme", theme);
await app.click({ role: "button", name: "Models" });
await choose("Language", "Spanish");
await choose("Language", "System");
await choose("Voice mode", "Live conversation");
await choose("Voice mode", "Take turns");
await app.click({ role: "button", name: "Close Settings" });
return "ok";
