// Voice fallbacks save from Settings and survive a relaunch.
let app = await cua.getApp(APP);
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Models" });
await app.click({ role: "button", name: "Voice fallbacks" });
await app.waitFor({ role: "button", name: "Mode", value: "Auto", timeout_ms: 5000 });
await app.click({ role: "button", name: "Mode" });
await app.click({ role: "button", name: "Custom" });
await app.waitFor({ text: "No fallbacks.", timeout_ms: 5000 });
await app.click({ role: "button", name: "Add voice fallback" });
await app.click({ role: "button", name: "OpenAI Realtime · gpt-realtime-mini" });
await app.waitFor({ role: "button", name: "Voice fallback 1", value: "OpenAI Realtime · gpt-realtime-mini", timeout_ms: 5000 });
await app.click({ role: "button", name: "Voice for OpenAI Realtime · gpt-realtime-mini" });
await app.click({ role: "button", name: "Cedar" });
await app.waitFor({ role: "button", name: "Voice for OpenAI Realtime · gpt-realtime-mini", value: "Cedar", timeout_ms: 5000 });
await app.click({ role: "button", name: "Close Voice fallbacks" });
return "saved";

// @relaunch
const app = await cua.getApp(APP);
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Models" });
await app.click({ role: "button", name: "Voice fallbacks" });
await app.waitFor({ role: "button", name: "Mode", value: "Custom", timeout_ms: 10000 });
await app.waitFor({ role: "button", name: "Voice fallback 1", value: "OpenAI Realtime · gpt-realtime-mini", timeout_ms: 5000 });
await app.waitFor({ role: "button", name: "Voice for OpenAI Realtime · gpt-realtime-mini", value: "Cedar", timeout_ms: 5000 });
await app.click({ role: "button", name: "Close Voice fallbacks" });
return "ok";
