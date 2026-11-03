// The image and video model order saves from Settings and survives a relaunch.
let app = await cua.getApp(APP);
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Models" });
await app.click({ role: "button", name: "Image models" });
await app.waitFor({ role: "button", name: "Mode", value: "Auto", timeout_ms: 5000 });
await app.click({ role: "button", name: "Mode" });
await app.click({ role: "button", name: "Custom" });
await app.waitFor({ text: "No models.", timeout_ms: 5000 });
await app.click({ role: "button", name: "Close Image models" });
await app.waitFor({ role: "button", name: "Image models", value: "Custom", timeout_ms: 5000 });
if (!(await app.query({ role: "button", name: "Video models" })).length) throw new Error("Video row is missing");
return "saved";

// @relaunch
const app = await cua.getApp(APP);
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Models" });
await app.waitFor({ role: "button", name: "Image models", value: "Custom", timeout_ms: 10000 });
await app.click({ role: "button", name: "Image models" });
await app.click({ role: "button", name: "Mode" });
await app.click({ role: "button", name: "Auto" });
await app.waitFor({ role: "button", name: "Mode", value: "Auto", timeout_ms: 5000 });
await app.click({ role: "button", name: "Close Image models" });
return "ok";
