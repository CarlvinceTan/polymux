// Live model choices follow the voice provider and persist without a free-text field.
let app = await cua.getApp(APP);
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Models" });
async function choose(name, item) {
    await app.click({ role: "button", name });
    await app.click({ role: "button", name: item });
}
await choose("Voice mode", "Live conversation");
await app.waitFor({ role: "button", name: "Voice model", value: "gemini-3.8-live", timeout_ms: 5000 });
await choose("Voice provider", "OpenAI Realtime");
await app.waitFor({ role: "button", name: "Voice model", value: "gpt-realtime", timeout_ms: 5000 });
await choose("Voice model", "gpt-realtime-mini");
await app.waitFor({ role: "button", name: "Voice model", value: "gpt-realtime-mini", timeout_ms: 5000 });
await app.click({ role: "button", name: "Close Settings" });
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Models" });
await app.waitFor({ role: "button", name: "Voice model", value: "gpt-realtime-mini", timeout_ms: 5000 });
await choose("Voice provider", "Gemini Live");
await app.waitFor({ role: "button", name: "Voice model", value: "gemini-3.8-live", timeout_ms: 5000 });
await app.click({ role: "button", name: "Voice model" });
await app.waitFor({ role: "button", name: "gemini-3.8-live", timeout_ms: 5000 });
if ((await app.query({ role: "button", name: "gpt-realtime-mini" })).length) throw new Error("OpenAI models leaked into Gemini's picker");
await app.click({ role: "button", name: "gemini-3.8-live" });
if ((await app.query({ role: "textfield", name: "Model" })).length) throw new Error("voice model still requires typing");
await choose("Voice mode", "Take turns");
await app.click({ role: "button", name: "Close Settings" });
return "ok";
