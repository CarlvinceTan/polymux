// @fixture --verify-fixture --verify-profile --verify-profile-media --verify-profile-scroll
// Gallery and file rows scroll inside a card while the profile tabs stay fixed.
const app = await cua.getApp(APP);
let rows = [];
for (let i = 0; i < 60 && !rows.length; i++) {
    rows = (await app.query({ role: "button", name: "Chief" })).filter(item => item.label.startsWith("Chief, "));
    if (!rows.length) await cua.sleep(500);
}
if (!rows.length) throw new Error("Chief row missing");
await app.click(rows[0].element_token);
await app.waitFor({ role: "button", name: "Outgoing media spacing", timeout_ms: 10000 });
await app.click({ role: "button", name: "Edit Chief’s profile" });
await app.waitFor({ text: "Reviewing calendar", timeout_ms: 10000 });
async function button(name) {
    const state = await app.getState({ query: name, include_screenshot: false, timeout_ms: 5000, max_elements: 500, tree_format: "elements" });
    const items = state.elements.filter(item => item.role === "AXButton" && item.label === name);
    if (state.truncated || items.length !== 1) throw new Error("Missing or ambiguous button: " + name);
    return items[0];
}
for (const [tab, item] of [["Media", "Show Landscape 1.png in chat"], ["Files", "View Plan.txt"]]) {
    await app.click((await button(tab)).element_token);
    await app.waitFor({ role: "button", name: item });
    const header = (await button(tab)).frame;
    const first = await button(item);
    await app.scroll(first.element_token, "down", 4);
    const after = await button(item);
    if (after.frame.y >= first.frame.y) throw new Error(tab + " contents did not scroll");
    if ((await button(tab)).frame.y !== header.y) throw new Error(tab + " scrolling moved the tabs");
    if ((await app.query({ role: "button", name: "View more" })).length) throw new Error(tab + " still requires expansion");
}
return "ok";
