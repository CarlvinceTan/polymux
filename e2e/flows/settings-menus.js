// Settings dropdowns stay below their row and keep the same right edge as selections change.
const window = (await cua.listWindows()).find(window => window.app_name === APP && window.layer === 0);
const target = { pid: window.pid, windowId: window.window_id };
const app = await cua.getApp(target);
async function choose(name, value) {
    const trigger = { role: "button", name };
    const [control] = await app.query(trigger);
    await app.click(trigger);
    await app.waitFor({ role: "button", name: value, timeout_ms: 3000 });
    const firstName = { Theme: "Light", "Voice fallbacks": "Off" }[name];
    const [first] = await app.query({ role: "button", name: firstName });
    if (!control?.frame || !first?.frame) throw new Error(name + " menu has no bounds");
    // A menu opens below its row, or above when that's where the room is; it never covers the row.
    if (first.frame.y < control.frame.y + control.frame.h && first.frame.y + first.frame.h > control.frame.y) {
        throw new Error(name + " menu covers its setting row: " + JSON.stringify({ control: control.frame, first: first.frame }));
    }
    const offset = first.frame.x + first.frame.w - control.frame.x - control.frame.w;
    if (Math.abs(offset + 4) > 1) throw new Error(name + " menu is offset by " + offset);
    await app.click({ role: "button", name: value });
    await app.waitFor({ ...trigger, value, timeout_ms: 5000 });
    return first.frame.w;
}
await app.click({ role: "button", name: "Settings" });
const theme = await choose("Theme", "Light");
for (const value of ["Dark", "System"]) {
    if (await choose("Theme", value) !== theme) throw new Error("Theme menu changed width");
}
await app.click({ role: "button", name: "Voice" });
if (await choose("Voice fallbacks", "Off") !== theme) throw new Error("Settings menus have different widths");
await choose("Voice fallbacks", "Auto");
await app.click({ role: "button", name: "Close Settings" });
return "ok";
