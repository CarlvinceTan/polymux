// Provider methods stay distinct; keys and manual endpoints feed the searchable default model picker.
let app = await cua.getApp(APP);
async function confirm(label) {
  await app.waitFor({ role: "button", name: label });
  for (let attempt = 0; attempt < 3; attempt++) {
    const buttons = await app.query({ role: "button", name: label });
    const button = buttons.find(row => row.label === label && row.frame?.w > 10 && row.frame?.h > 10);
    if (!button && attempt > 0) return;
    if (!button) throw new Error("Confirmation button missing: " + label);
    try { await app.click(button.element_token); return; }
    catch (error) {
      if (!String(error.message).includes("-25205") || attempt === 2) throw error;
      // Native alerts can replace their accessibility objects while appearing.
      await cua.sleep(350);
    }
  }
}
const host = await app.getState({ query: "Settings", include_screenshot: false });
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Models" });
await app.click({ role: "button", name: "Add provider" });
await app.typeText("OpenAI", { role: "textfield", name: "Search providers" });
await app.click({ role: "button", name: "OpenAI · OpenAI (ChatGPT subscription)" });
await app.waitFor({ role: "textfield", name: "http://127.0.0.1:1455/auth/callback", timeout_ms: 10000 });
await app.waitFor({ role: "button", name: "Copy link", timeout_ms: 5000 });
await app.click({ role: "button", name: "Back" });
await app.typeText("OpenAI", { role: "textfield", name: "Search providers" });
await app.waitFor({ role: "button", name: "OpenAI · OpenAI API key", timeout_ms: 10000 });
await app.click({ role: "button", name: "OpenAI · OpenAI API key" });
await app.waitFor({ role: "textfield", name: "API key", timeout_ms: 10000 });
await app.typeText("polymux-test-key", { role: "textfield", name: "API key" });
await app.click({ role: "button", name: "Continue" });
await app.waitFor({ role: "button", name: "Edit OpenAI", timeout_ms: 10000 });
await app.click({ role: "button", name: "Model" });
await app.typeText("gpt-5", { role: "textfield", name: "Search models" });
const models = await app.query({ role: "button" });
const chosen = models.find(row => row.label?.startsWith("Gpt 5") && row.label.endsWith(" OpenAI"));
if (!chosen) throw new Error("no connected OpenAI models found");
if ((await app.query({ text: "Server default" })).length) throw new Error("development fallback leaked into the picker");
await app.click({ role: "button", name: chosen.label });
await app.waitFor({ role: "button", name: "Model", value: chosen.label, timeout_ms: 5000 });
app = await cua.getApp({ pid: host.pid, windowId: host.window_id });
await app.click({ role: "button", name: "Add provider" });
await app.click({ role: "button", name: "Add custom provider" });
await app.typeText("Manual test", { role: "textfield", name: "Name" });
await app.typeText("http://127.0.0.1:1/v1", { role: "textfield", name: "Base URL" });
const sharedModel = "qa-6-1-model";
const sharedTitle = "Qa 6.1 Model Custom";
await app.typeText(sharedModel, { role: "textfield", name: "Model ID" });
await app.click({ role: "button", name: "Add" });
await app.click({ role: "button", name: "Save" });
await app.waitFor({ role: "button", name: "Edit Custom", timeout_ms: 10000 });
await app.click({ role: "button", name: "Model" });
await app.typeText("Custom " + sharedModel, { role: "textfield", name: "Search models" });
await app.click({ role: "button", name: sharedTitle });
await app.waitFor({ role: "button", name: "Model", value: sharedTitle, timeout_ms: 5000 });
app = await cua.getApp({ pid: host.pid, windowId: host.window_id });
await app.click({ role: "button", name: "Edit Custom" });
await app.waitFor({ role: "textfield", name: "Name", value: "Manual test", timeout_ms: 5000 });
await app.click({ role: "button", name: "Delete" });
await app.waitFor({ role: "button", name: "Model", value: "Server default", timeout_ms: 5000 });
await app.click({ role: "button", name: "Edit OpenAI" });
await app.click({ role: "button", name: "Remove API key 1" });
await confirm("Remove");
await app.click({ role: "button", name: "Back" });
await app.click({ role: "button", name: "Close Settings" });
return "ok";
