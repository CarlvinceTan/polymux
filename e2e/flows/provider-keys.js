// Provider models stay on the main page; its keys have names and immutable masked secrets.
const app = await cua.getApp(APP);
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
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Models" });
await app.click({ role: "button", name: "Add provider" });
await app.typeText("DeepSeek", { role: "textfield", name: "Search providers" });
await app.click({ role: "button", name: "DeepSeek · DeepSeek API key" });
await app.typeText("abcd-private-first-wxyz", { role: "textfield", name: "API key" });
if ((await app.query({ text: "abcd-private-first-wxyz" })).length) throw new Error("the unsaved secret was exposed");
await app.click({ role: "button", name: "Continue" });
await app.waitFor({ role: "button", name: "Edit DeepSeek", timeout_ms: 10000 });
await app.click({ role: "button", name: "Edit DeepSeek" });
await app.waitFor({ text: "abcd••••wxyz", timeout_ms: 5000 });
await app.click({ role: "checkbox", name: "All models" });
await app.waitFor({ role: "checkbox", name: "All models", value: "0", timeout_ms: 5000 });
await app.click({ role: "button", name: "Edit API key 1" });
if ((await app.query({ role: "textfield", name: "API key" })).length) throw new Error("saved secret is editable");
await app.typeText("Work", { role: "textfield", name: "Name" });
if ((await app.query({ role: "checkbox", name: "All models" })).length) throw new Error("provider models moved into the key editor");
await app.click({ role: "button", name: "Save" });
await app.waitFor({ role: "button", name: "Edit Work", timeout_ms: 5000 });
await app.click({ role: "button", name: "Add API key" });
await app.typeText("short-key", { role: "textfield", name: "API key" });
await app.click({ role: "button", name: "Continue" });
await app.waitFor({ role: "button", name: "Edit API key 2", timeout_ms: 10000 });
await app.waitFor({ role: "checkbox", name: "All models", value: "0", timeout_ms: 5000 });
await app.waitFor({ text: "••••••••", timeout_ms: 5000 });
if ((await app.query({ text: "short-key" })).length) throw new Error("short key was exposed");
await app.click({ role: "button", name: "Edit Work" });
await app.waitFor({ role: "textfield", name: "Name", value: "Work", timeout_ms: 5000 });
await app.click({ role: "button", name: "Back" });
await app.waitFor({ role: "checkbox", name: "All models", value: "0", timeout_ms: 5000 });
await app.click({ role: "button", name: "Remove Work" });
await confirm("Cancel");
await app.waitFor({ role: "button", name: "Edit Work" });
await app.click({ role: "button", name: "Remove Work" });
await confirm("Remove");
await app.waitFor({ role: "button", name: "Edit API key 2", timeout_ms: 5000 });
await app.waitFor({ role: "button", name: "Edit Work", gone: true, timeout_ms: 5000 });
await app.click({ role: "button", name: "Remove API key 2" });
await confirm("Remove");
await app.waitFor({ text: "No API keys yet.", timeout_ms: 5000 });
await app.waitFor({ role: "button", name: "Add API key", timeout_ms: 5000 });
await app.click({ role: "button", name: "Back" });
await app.waitFor({ role: "button", name: "Edit DeepSeek", gone: true, timeout_ms: 5000 });
await app.click({ role: "button", name: "Close Settings" });
return "ok";
