// Settings opens its pages inside one panel: the tabs stay beside the page, Back returns to the tab, and switching tabs leaves the page.
const app = await cua.getApp(APP);
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Models" });
await app.click({ role: "button", name: "Add provider" });
await app.waitFor({ role: "button", name: "Add custom provider", timeout_ms: 5000 });
await app.waitFor({ role: "heading", name: "Add provider", timeout_ms: 3000 });
if (!(await app.query({ role: "button", name: "General" })).length) throw new Error("The tabs left with the page");
await app.click({ role: "button", name: "Back" });
await app.waitFor({ role: "button", name: "Add custom provider", gone: true, timeout_ms: 3000 });
await app.waitFor({ role: "heading", name: "Models", timeout_ms: 3000 });
await app.click({ role: "button", name: "Image models" });
await app.waitFor({ role: "button", name: "Mode", timeout_ms: 5000 });
await app.click({ role: "button", name: "Network" });
await app.waitFor({ role: "button", name: "Join network", timeout_ms: 5000 });
if ((await app.query({ role: "button", name: "Back" })).length) throw new Error("Switching tabs kept the page");
await app.click({ role: "button", name: "Close Settings" });
await app.waitFor({ role: "button", name: "Join network", gone: true, timeout_ms: 3000 });
return "ok";
