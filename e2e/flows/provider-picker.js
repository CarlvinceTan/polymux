// Provider tabs filter one headerless list; the plus action opens manual setup directly.
let app;
for (let i = 0; ; i++) {
    try { app = await cua.getApp(APP); break; }
    catch (error) { if (i >= 20) throw error; await cua.sleep(500); }
}
const subscription = "OpenAI · OpenAI (ChatGPT subscription)";
const apiKey = "OpenAI · OpenAI API key";
async function absent(name) {
    if ((await app.query({ role: "button", name })).some(row => row.label === name)) {
        throw new Error("Unrelated provider is visible: " + name);
    }
}
async function noHeadings() {
    const state = await app.getState({ max_elements: 800, timeout_ms: 5000, include_screenshot: false });
    if (state.truncated) throw new Error("Provider list lookup is incomplete");
    for (const title of ["All", "API key", "Sign in", "Manual"]) {
        if (state.tree_markdown.includes("AXHeading (" + title + ")")) {
            throw new Error("Redundant provider heading: " + title);
        }
    }
}
async function searchCleared() {
    for (let i = 0; i < 20; i++) {
        const [field] = await app.query({ role: "textfield", name: "Search providers" });
        if (field && (!field.value || field.value === "Search providers")) return;
        await cua.sleep(250);
    }
    throw new Error("Provider search did not clear");
}
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Models" });
await app.click({ role: "button", name: "Add provider" });
await app.waitFor({ role: "textfield", name: "Search providers", timeout_ms: 5000 });
await app.waitFor({ role: "button", name: subscription, timeout_ms: 10000 });
await app.waitFor({ role: "button", name: apiKey, timeout_ms: 5000 });
await absent("Manual");
await absent("Custom");
await noHeadings();
await app.click({ role: "button", name: "API key" });
await absent(subscription);
await noHeadings();
// The list renders lazily and OpenAI sits below the fold here; searching brings it into view.
await app.typeText(" OPENAI ", { role: "textfield", name: "Search providers" });
await app.waitFor({ role: "button", name: apiKey, timeout_ms: 5000 });
await absent(subscription);
await app.click({ role: "button", name: "Sign in" });
await searchCleared();
await app.waitFor({ role: "button", name: subscription, timeout_ms: 5000 });
await absent(apiKey);
await noHeadings();
await app.typeText("key openai", { role: "textfield", name: "Search providers" });
await app.waitFor({ role: "statictext", name: "No providers found.", timeout_ms: 5000 });
await absent(apiKey);
await app.click({ role: "button", name: "All" });
await searchCleared();
await app.waitFor({ role: "button", name: subscription, timeout_ms: 5000 });
await app.waitFor({ role: "button", name: apiKey, timeout_ms: 5000 });
await app.typeText("key openai", { role: "textfield", name: "Search providers" });
await app.waitFor({ role: "button", name: apiKey, timeout_ms: 5000 });
await absent(subscription);
await app.click({ role: "button", name: "All" });
await searchCleared();
await app.typeText("no-such-provider", { role: "textfield", name: "Search providers" });
await app.waitFor({ role: "statictext", name: "No providers found.", timeout_ms: 5000 });
await app.click({ role: "button", name: "Add custom provider" });
await app.waitFor({ role: "textfield", name: "Name", value: "Custom", timeout_ms: 5000 });
await app.waitFor({ role: "textfield", name: "Base URL", timeout_ms: 5000 });
await app.waitFor({ role: "textfield", name: "Model ID", timeout_ms: 5000 });
await app.click({ role: "button", name: "Back" });
await app.waitFor({ role: "button", name: "Add provider", timeout_ms: 5000 });
await app.click({ role: "button", name: "Close Settings" });
return "ok";
