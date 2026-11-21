// @fixture --verify-fixture --verify-tools --verify-call-ready
const app = await cua.getApp(APP);
const call = { role: "button", name: "Speech conversation" };
const dictation = { role: "button", name: "Start dictation" };
await app.waitFor({ role: "button", name: "Chief" });
const [chief] = (await app.query({ role: "button", name: "Chief" })).filter(item => item.label.startsWith("Chief, "));
await app.click(chief.element_token);
await app.waitFor(call);

async function chooseProvider(provider) {
    await app.click({ role: "button", name: "Settings" });
    await app.click({ role: "button", name: "Voice" });
    await app.click({ role: "button", name: "Voice provider" });
    await app.click({ role: "button", name: provider });
    await app.click({ role: "button", name: "Close Settings" });
}

await chooseProvider("OpenAI Realtime");
await app.waitFor({ ...call, gone: true });
await app.waitFor(dictation);
await chooseProvider("Gemini Live");
await app.waitFor(call);
await app.waitFor(dictation);
return "ok";
