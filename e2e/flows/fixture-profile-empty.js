// @fixture --verify-fixture --verify-profile --verify-profile-empty
// Empty cards centre their placeholders on the profile's horizontal centre.
const app = await cua.getApp(APP);
let rows = [];
for (let i = 0; i < 60 && !rows.length; i++) {
    rows = (await app.query({ role: "button", name: "Aria" })).filter(item => item.label.startsWith("Aria, "));
    if (!rows.length) await cua.sleep(500);
}
if (!rows.length) throw new Error("Aria row missing");
await app.click(rows[0].element_token);
await app.click({ role: "button", name: "Edit Aria’s profile" });
await app.waitFor({ text: "No scheduled work", timeout_ms: 10000 });
const [avatar] = await app.query({ role: "button", name: "Edit avatar" });
const centre = avatar.frame.x + avatar.frame.w / 2;
async function centred(text) {
    await app.waitFor({ text });
    const state = await app.getState({ query: text, tree_format: "elements", include_screenshot: false, max_elements: 500 });
    if (state.truncated) throw new Error("Incomplete placeholder lookup");
    const matches = state.elements;
    const item = matches.find(item => (item.label === text || item.value === text) && item.frame);
    if (!item || Math.abs(item.frame.x + item.frame.w / 2 - centre) > 2) throw new Error("Placeholder is not centred: " + text + " " + JSON.stringify(matches));
}
await centred("No scheduled work");
await centred("No tasks yet");
for (const [tab, text] of [["Media", "No media yet"], ["Files", "No files yet"]]) {
    await app.click({ role: "button", name: tab });
    await centred(text);
}
return "ok";
