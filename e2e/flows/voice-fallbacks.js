// Voice fallbacks save from Settings and survive a relaunch.
let app = await cua.getApp(APP);
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Voice" });
const mode = { role: "button", name: "Voice fallbacks" };
await app.waitFor({ ...mode, value: "Auto", timeout_ms: 5000 });
await app.click(mode);
for (const name of ["Off", "Custom", "Auto"]) {
    await app.waitFor({ role: "button", name, timeout_ms: 5000 });
}
await app.click({ role: "button", name: "Custom" });
await app.waitFor({ ...mode, value: "Custom", timeout_ms: 5000 });
await app.waitFor({ role: "button", name: "Voice language", timeout_ms: 5000 });
await app.waitFor({ text: "No fallbacks.", timeout_ms: 5000 });
await app.click({ role: "button", name: "Add voice fallback" });
await app.click({ role: "button", name: "OpenAI Realtime · gpt-realtime-mini" });
await app.waitFor({ role: "button", name: "Voice fallback 1", value: "OpenAI Realtime · gpt-realtime-mini", timeout_ms: 5000 });
await app.click({ role: "button", name: "Voice for OpenAI Realtime · gpt-realtime-mini" });
await app.click({ role: "button", name: "Cedar" });
await app.waitFor({ role: "button", name: "Voice for OpenAI Realtime · gpt-realtime-mini", value: "Cedar", timeout_ms: 5000 });
await app.click({ role: "button", name: "Close Settings" });
return "saved";

// @relaunch
const app = await cua.getApp(APP);
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Voice" });
const mode = { role: "button", name: "Voice fallbacks" };
await app.waitFor({ ...mode, value: "Custom", timeout_ms: 10000 });
await app.waitFor({ role: "button", name: "Voice fallback 1", value: "OpenAI Realtime · gpt-realtime-mini", timeout_ms: 5000 });
await app.waitFor({ role: "button", name: "Voice for OpenAI Realtime · gpt-realtime-mini", value: "Cedar", timeout_ms: 5000 });
await app.click(mode);
await app.click({ role: "button", name: "Off" });
await app.waitFor({ ...mode, value: "Off", timeout_ms: 5000 });
if ((await app.query({ role: "button", name: "Voice fallback 1" })).length) throw new Error("Custom order is still visible");
await app.click(mode);
await app.click({ role: "button", name: "Custom" });
await app.waitFor({ role: "button", name: "Voice for OpenAI Realtime · gpt-realtime-mini", value: "Cedar", timeout_ms: 5000 });
await app.click({ role: "button", name: "Close Settings" });
return "ok";
