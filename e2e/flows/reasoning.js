// Full-row reasoning pickers persist globally and each bot can override its initial preference.
let app = await cua.getApp(APP);
const host = await app.getState({ query: "Settings", include_screenshot: false });
async function capabilities() {
    const state = await app.getState({ query: "Capabilities", tree_format: "elements", include_screenshot: false, max_elements: 120, timeout_ms: 5000 });
    const tabs = state.elements.filter(row => row.role === "AXButton" && row.label === "Capabilities");
    if (state.truncated || tabs.length !== 1) throw new Error("Capabilities tab lookup is incomplete or ambiguous");
    await app.click(tabs[0].element_token);
}
async function choose(level) {
    const trigger = { role: "button", name: "Reasoning" };
    const [control] = await app.query(trigger);
    const [model] = await app.query({ role: "button", name: "Model" });
    const [profileModel] = model ? [] : await app.query({ role: "button", name: "Model" });
    const row = model ?? profileModel;
    if (!control?.frame || !row?.frame || control.frame.x > row.frame.x + 2 ||
        control.frame.x + control.frame.w < row.frame.x + row.frame.w - 2) {
        throw new Error("Reasoning's clickable control does not span the model row");
    }
    await app.click(trigger);
    await app.waitFor({ role: "button", name: control.value, value: "Selected", timeout_ms: 5000 });
    const [first] = await app.query({ role: "button", name: "Off" });
    if (!first?.frame || first.frame.y < control.frame.y + control.frame.h) {
        throw new Error("The reasoning menu covers its setting row: " + JSON.stringify({ control: control.frame, first: first?.frame }));
    }
    await app.click({ role: "button", name: level });
    app = await cua.getApp({ pid: host.pid, windowId: host.window_id });
    await app.waitFor({ ...trigger, value: level, timeout_ms: 5000 });
}
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Models" });
for (const level of ["Minimal", "Xhigh", "Max"]) await choose(level);
await app.click({ role: "button", name: "Reasoning" });
await app.pressKey("up");
await app.pressKey("return");
await app.waitFor({ role: "button", name: "Reasoning", value: "Xhigh", timeout_ms: 5000 });
await choose("Max");
await app.click({ role: "button", name: "Reasoning" });
await app.pressKey("escape");
await app.waitFor({ role: "button", name: "Reasoning", value: "Max", timeout_ms: 5000 });
await app.click({ role: "button", name: "Close Settings" });
await app.click({ role: "button", name: "New chat" });
app = await pick(app, "New Bot");
await app.typeText("Reasoning QA", { role: "textfield", name: "Name" });
await app.click({ role: "button", name: "Create" });
app = await cua.getApp({ pid: host.pid, windowId: host.window_id });
await app.waitFor({ role: "button", name: "Edit Reasoning QA’s profile", timeout_ms: 10000 });
await app.click({ role: "button", name: "Edit Reasoning QA’s profile" });
await capabilities();
await app.waitFor({ role: "button", name: "Reasoning", value: "Max", timeout_ms: 10000 });
await choose("Xhigh");
await app.click({ role: "button", name: "Close side panel" });
await app.click({ role: "button", name: "Edit Reasoning QA’s profile" });
await capabilities();
await app.waitFor({ role: "button", name: "Reasoning", value: "Xhigh", timeout_ms: 10000 });
await choose("Off");
await app.click({ role: "button", name: "Close side panel" });
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Models" });
await app.waitFor({ role: "button", name: "Reasoning", value: "Max", timeout_ms: 5000 });
await choose("Off");
await app.click({ role: "button", name: "Close Settings" });
return "ok";
