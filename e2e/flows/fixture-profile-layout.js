// @fixture --verify-fixture --verify-profile --verify-profile-media
// Work and Capabilities divide their fixed space among independently scrolling cards.
const app = await cua.getApp(APP);
let rows = [];
for (let i = 0; i < 60 && !rows.length; i++) {
    rows = (await app.query({ role: "button", name: "Chief" })).filter(item => item.label.startsWith("Chief, "));
    if (!rows.length) await cua.sleep(500);
}
if (!rows.length) throw new Error("Chief row missing");
await app.click(rows[0].element_token);
await app.waitFor({ text: "Profile fixture reply", timeout_ms: 10000 });
await app.click({ role: "button", name: "Edit Chief’s profile" });
await app.waitFor({ text: "Reviewing calendar", timeout_ms: 10000 });
async function element(role, label) {
    const state = await app.getState({ query: label, include_screenshot: false, timeout_ms: 5000, max_elements: 500, tree_format: "elements" });
    const matches = state.elements.filter(item => item.role === role && item.label === label);
    if (state.truncated || matches.length !== 1) throw new Error("Missing or ambiguous " + label + ": " + JSON.stringify(state.elements));
    return matches[0];
}
await app.click({ role: "button", name: "Mute notifications" });
await app.waitFor({ role: "button", name: "Unmute notifications" });
await app.waitFor({ text: "Notifications turned off" });
await app.click({ role: "button", name: "Unmute notifications" });
await app.waitFor({ text: "Notifications turned on" });
await app.waitFor({ text: "Notifications turned on", gone: true, timeout_ms: 5000 });
// Instructions is an editor that scrolls itself, so it isn't one of the card scroll areas.
for (const [tab, names] of [["Work", ["Schedule", "Tasks"]], ["Capabilities", ["Skills", "MCPs"]]]) {
    await app.click((await element("AXButton", tab)).element_token);
    if (tab === "Capabilities") await element("AXTextArea", "Instructions");
    const header = (await element("AXButton", tab)).frame;
    const [divider] = await app.query({ name: "Resize side panel" });
    if (!divider?.frame) throw new Error("Side panel divider is missing");
    const left = divider.frame.x;
    const state = await app.getState({ include_screenshot: false, timeout_ms: 5000, max_elements: 1000, tree_format: "elements" });
    const areas = state.elements.filter(item => item.actions?.includes("AXScrollToTop") && item.frame?.x >= left);
    if (state.truncated || areas.length !== names.length) throw new Error("Wrong scroll sections in " + tab + ": " + JSON.stringify(areas));
    await app.scroll(areas[0].element_token, "down", 4);
    if ((await element("AXButton", tab)).frame.y !== header.y) throw new Error(tab + " header scrolled");
}
for (const text of ["Background reviews", "Alerts"]) {
    if ((await app.query({ text })).length) throw new Error("Old toggle remains: " + text);
}
return "ok";
