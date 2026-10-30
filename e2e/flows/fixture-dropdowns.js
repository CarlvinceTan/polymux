// @fixture --verify-fixture --verify-profile
// Profile dropdowns stay below their whole row without moving the profile or closing it on Escape.
const app = await cua.getApp(APP);
let chief;
for (let i = 0; i < 30 && !chief; i++) {
    const state = await app.getState({ query: "Chief", tree_format: "elements", include_screenshot: false, max_elements: 300 });
    if (state.truncated) throw new Error("Chief lookup is incomplete");
    chief = state.elements.find(item => item.role === "AXButton" && item.label?.startsWith("Chief, "));
    if (!chief) await cua.sleep(100);
}
if (!chief) throw new Error("Chief row is missing");
await app.click(chief.element_token);
await app.click({ role: "button", name: "Edit Chief’s profile" });
await app.click({ role: "button", name: "Capabilities" });
for (const [name, content] of [["Reasoning", { role: "button", name: "Off" }], ["Model", { role: "textfield", name: "Search models" }]]) {
    const trigger = { role: "button", name };
    const [before] = await app.query(trigger);
    await app.click(trigger);
    await app.waitFor({ ...content, timeout_ms: 5000 });
    const [first] = await app.query(content);
    if (!before?.frame || !first?.frame || first.frame.y < before.frame.y + before.frame.h) {
        throw new Error(name + " dropdown covers its row: " + JSON.stringify({ before: before?.frame, first: first?.frame }));
    }
    if (name === "Model") await app.pressKey("escape", content);
    else await app.click({ role: "button", name: "Dismiss menu" });
    await app.waitFor({ ...trigger, timeout_ms: 5000 });
    const [after] = await app.query(trigger);
    if (Math.abs(before.frame.y - after.frame.y) > 1) throw new Error(name + " row moved after closing its dropdown");
}
await app.click({ role: "button", name: "Close side panel" });
return "ok";
