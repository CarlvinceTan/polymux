// @fixture --verify-fixture --verify-profile
// Profile dropdowns line up with their row's trailing edge, stay below it, and don't move the profile or close on Escape.
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
for (const [name, content, row] of [
    ["Reasoning", { role: "button", name: "Off" }, { role: "button", name: "Off" }],
    ["Model", { role: "textfield", name: "Search models" }, { role: "button", name: "Not set" }],
]) {
    const trigger = { role: "button", name };
    const [before] = await app.query(trigger);
    await app.click(trigger);
    await app.waitFor({ ...content, timeout_ms: 5000 });
    const [first] = await app.query(row);
    if (!before?.frame || !first?.frame || first.frame.y < before.frame.y + before.frame.h) {
        throw new Error(name + " dropdown covers its row: " + JSON.stringify({ before: before?.frame, first: first?.frame }));
    }
    // The button frame excludes sectionRow's 8pt inset on each side; menu rows sit 4pt inside the menu.
    if (Math.abs(first.frame.x + first.frame.w - before.frame.x - before.frame.w - 4) > 1) {
        throw new Error(name + " dropdown isn't on its row's trailing edge: " + JSON.stringify({ before: before.frame, first: first.frame }));
    }
    if (name === "Model") await app.pressKey("escape", content);
    else await app.click({ role: "button", name: "Dismiss menu" });
    await app.waitFor({ ...trigger, timeout_ms: 5000 });
    const [after] = await app.query(trigger);
    if (Math.abs(before.frame.y - after.frame.y) > 1) throw new Error(name + " row moved after closing its dropdown");
}
await app.click({ role: "button", name: "Close side panel" });
return "ok";
