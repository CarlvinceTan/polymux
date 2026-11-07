// @fixture --verify-fixture --verify-profile --verify-model-levels
// The reasoning menu offers only the bot's model's levels, and a saved level it lacks shows as the one it runs at.
const app = await cua.getApp(APP);
let chief;
for (let i = 0; i < 30 && !chief; i++) {
    const state = await app.getState({ query: "Chief", tree_format: "elements", include_screenshot: false, max_elements: 300, timeout_ms: 5000 });
    if (state.truncated) throw new Error("Chief lookup is incomplete");
    chief = state.elements.find(item => item.role === "AXButton" && item.label?.startsWith("Chief, "));
    if (!chief) await cua.sleep(100);
}
if (!chief) throw new Error("Chief row is missing");
await app.click(chief.element_token);
await app.click({ role: "button", name: "Edit Chief’s profile" });
await app.click({ role: "button", name: "Capabilities" });
await app.waitFor({ role: "button", name: "Reasoning", value: "High", timeout_ms: 5000 });
await app.click({ role: "button", name: "Reasoning" });
await app.waitFor({ role: "button", name: "Max", timeout_ms: 5000 });
for (const level of ["Off", "Low"]) await app.waitFor({ role: "button", name: level, timeout_ms: 1000 });
for (const level of ["Minimal", "Medium", "Xhigh"]) {
    if ((await app.query({ role: "button", name: level })).length) throw new Error(level + " isn't one of this model's levels");
}
return "ok";
