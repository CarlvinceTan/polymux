// @fresh
// Setup connects Ollama on the page; when it isn't reachable (this flow needs nothing on port 11434), nothing is left behind.
let app = await cua.getApp(APP);
for (let i = 0; i < 80 && !(await app.query({ role: "statictext", name: "Get Started with Polymux" })).length; i++) await cua.sleep(250);
await app.click({ role: "button", name: "Continue" });
await app.click({ role: "button", name: "Continue" });
await app.click({ role: "button", name: "Add a provider" });
await app.click({ role: "button", name: "Ollama · Models on this computer" });
await app.waitFor({ role: "textfield", name: "Base URL", timeout_ms: 5000 });
await app.click({ role: "button", name: "Connect" });
for (let i = 0; i < 60 && !(await app.query({ text: "Couldn’t reach Ollama. Open it on this computer, then connect again." })).length; i++) await cua.sleep(250);
await app.waitFor({ text: "Couldn’t reach Ollama. Open it on this computer, then connect again.", timeout_ms: 1000 });
await app.click({ role: "button", name: "Back" });
await app.click({ role: "button", name: "Back" });
await app.waitFor({ role: "button", name: "Set up a model later", timeout_ms: 5000 });
await cua.sleep(2500);
if ((await app.query({ text: "Ollama" })).length) throw new Error("A failed Ollama connection stayed");
await app.waitFor({ role: "button", name: "Add a provider", timeout_ms: 1000 });
return "ok";
