// Bulk changes do not gate individual models, and apply to models hidden by search too.
const app = await cua.getApp(APP);
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Models" });
await app.click({ role: "button", name: "Add provider" });
await app.click({ role: "button", name: "Add custom provider" });
await app.typeText("Model controls", { role: "textfield", name: "Name" });
await app.typeText("http://127.0.0.1:1/v1", { role: "textfield", name: "Base URL" });
for (const model of ["alpha", "beta"]) {
    await app.typeText(model, { role: "textfield", name: "Model ID" });
    await app.click({ role: "button", name: "Add" });
}
async function check(name, value) {
    await app.waitFor({ role: "checkbox", name, value, timeout_ms: 5000 });
    const [item] = await app.query({ role: "checkbox", name });
    if (!item.enabled) throw new Error(name + " is disabled by the bulk switch");
}
await app.click({ role: "checkbox", name: "All models" });
await check("alpha", "0");
await check("beta", "0");
await app.click({ role: "checkbox", name: "alpha" });
await check("alpha", "1");
await check("All models", "0");
await app.click({ role: "button", name: "Save" });
await app.waitFor({ role: "button", name: "Edit Custom", value: "2 models (1 active)", timeout_ms: 5000 });
await app.click({ role: "button", name: "Edit Custom" });
await check("alpha", "1");
await check("beta", "0");
await app.typeText("beta", { role: "textfield", name: "Search models" });
await app.click({ role: "checkbox", name: "All models" });
await check("beta", "1");
await app.click({ role: "button", name: "Save" });
await app.waitFor({ role: "button", name: "Edit Custom", value: "2 models (all active)", timeout_ms: 5000 });
await app.click({ role: "button", name: "Edit Custom" });
await check("alpha", "1");
await check("beta", "1");
await app.click({ role: "button", name: "Delete" });
await app.click({ role: "button", name: "Close Settings" });
return "ok";
