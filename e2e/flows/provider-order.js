// Accounts stay within one provider row; reordering moves and persists whole provider groups.
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
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Models" });
async function finishKey(key) {
    await app.waitFor({ role: "textfield", name: "API key", timeout_ms: 10000 });
    await app.typeText(key, { role: "textfield", name: "API key" });
    await app.click({ role: "button", name: "Continue" });
}
async function add(type) {
    await app.click({ role: "button", name: "Add provider" });
    await app.typeText(type, { role: "textfield", name: "Search providers" });
    await app.click({ role: "button", name: type + " · " + type + " API key" });
    await finishKey("priority-test-key");
    await app.waitFor({ role: "button", name: "Edit " + type, timeout_ms: 10000 });
}
await add("OpenAI");
const [initial] = await app.query({ role: "button", name: "Edit OpenAI" });
const single = initial.value;
await app.click({ role: "button", name: "Edit OpenAI" });
await app.waitFor({ role: "button", name: "Edit API key 1", timeout_ms: 5000 });
if ((await app.query({ role: "button", name: "Change API key" })).length) throw new Error("saved key can be replaced");
await app.waitFor({ text: "prio••••-key", timeout_ms: 5000 });
await app.click({ role: "button", name: "Add API key" });
await finishKey("second-priority-test-key");
await app.waitFor({ role: "button", name: "Edit API key 2", timeout_ms: 10000 });
await app.click({ role: "button", name: "Edit API key 2" });
await app.waitFor({ role: "textfield", name: "Name", value: "API key 2", timeout_ms: 5000 });
if ((await app.query({ role: "textfield", name: "API key" })).length) throw new Error("saved key is editable");
await app.typeText("Work", { role: "textfield", name: "Name" });
await app.click({ role: "button", name: "Save" });
await app.waitFor({ role: "button", name: "Add API key", timeout_ms: 5000 });
await app.click({ role: "button", name: "Back" });
await app.waitFor({ role: "button", name: "Edit OpenAI", value: "2 connections · " + single, timeout_ms: 5000 });
if ((await app.query({ role: "button", name: "Edit OpenAI" })).length !== 1) throw new Error("accounts created duplicate provider rows");
await add("Anthropic");
const order = async () => (await app.query({ role: "button" })).filter(row => /^Edit (OpenAI|Anthropic)$/.test(row.label)).map(row => row.label);
if (JSON.stringify(await order()) !== JSON.stringify(["Edit OpenAI", "Edit Anthropic"])) throw new Error("new provider didn't append");
await app.click({ role: "button", name: "Model" });
await app.typeText("gpt-5", { role: "textfield", name: "Search models" });
const models = (await app.query({ role: "button" })).filter(row => row.label?.startsWith("Gpt 5") && row.label.endsWith(" OpenAI"));
if (!models.length || new Set(models.map(row => row.label)).size !== models.length) throw new Error("account model lists weren't deduplicated");
await app.click({ role: "button", name: models[0].label });
app = await cua.getApp(APP);
async function noArrows() {
    if ((await app.query({ role: "button" })).some(row => /^Move (OpenAI|Anthropic) (up|down)$/.test(row.label))) {
        throw new Error("reorder buttons visible outside reorder mode");
    }
}
await noArrows();
await app.click({ role: "button", name: "Reorder providers" });
for (let attempt = 0; attempt < 100; attempt++) {
    const [button] = await app.query({ role: "button", name: "Move Anthropic up" });
    if (button?.enabled && button.element_token) break;
    if (attempt === 99) throw new Error("reorder mode did not start");
    await cua.sleep(100);
}
await app.click({ role: "button", name: "Move Anthropic up" });
await app.waitFor({ role: "button", name: "Move OpenAI down", timeout_ms: 5000 });
if (JSON.stringify(await order()) !== JSON.stringify(["Edit Anthropic", "Edit OpenAI"])) throw new Error("move up didn't change priority");
await app.click({ role: "button", name: "Save provider order" });
await app.waitFor({ role: "button", name: "Reorder providers", timeout_ms: 5000 });
await app.click({ role: "button", name: "Close Settings" });
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Models" });
await app.waitFor({ role: "button", name: "Edit OpenAI", value: "2 connections · " + single, timeout_ms: 5000 });
if (JSON.stringify(await order()) !== JSON.stringify(["Edit Anthropic", "Edit OpenAI"])) throw new Error("priority wasn't saved");
await noArrows();
await app.click({ role: "button", name: "Reorder providers" });
await app.click({ role: "button", name: "Move Anthropic down" });
if (JSON.stringify(await order()) !== JSON.stringify(["Edit OpenAI", "Edit Anthropic"])) throw new Error("move down didn't change priority");
await app.click({ role: "button", name: "Cancel reordering" });
await app.waitFor({ role: "button", name: "Reorder providers", timeout_ms: 5000 });
if (JSON.stringify(await order()) !== JSON.stringify(["Edit Anthropic", "Edit OpenAI"])) throw new Error("cancel didn't restore priority");
await app.click({ role: "button", name: "Reorder providers" });
await app.click({ role: "button", name: "Move Anthropic down" });
await app.click({ role: "button", name: "Save provider order" });
await app.waitFor({ role: "button", name: "Reorder providers", timeout_ms: 5000 });
await app.click({ role: "button", name: "Edit OpenAI" });
await app.waitFor({ role: "button", name: "Edit API key 1", timeout_ms: 5000 });
await app.click({ role: "button", name: "Edit Work" });
await app.waitFor({ role: "textfield", name: "Name", value: "Work", timeout_ms: 5000 });
await app.click({ role: "button", name: "Back" });
await app.click({ role: "button", name: "Remove Work" });
await confirm("Remove");
await app.waitFor({ role: "button", name: "Remove Work", gone: true, timeout_ms: 5000 });
await app.waitFor({ role: "button", name: "Edit API key 1", timeout_ms: 5000 });
await app.click({ role: "button", name: "Back" });
await app.waitFor({ role: "button", name: "Edit OpenAI", value: single, timeout_ms: 5000 });
for (const name of ["OpenAI", "Anthropic"]) {
    await app.click({ role: "button", name: "Edit " + name });
    await app.click({ role: "button", name: "Remove API key 1" });
    await confirm("Remove");
    await app.waitFor({ text: "No API keys yet.", timeout_ms: 5000 });
    await app.click({ role: "button", name: "Back" });
    await app.waitFor({ role: "button", name: "Edit " + name, gone: true, timeout_ms: 5000 });
}
await app.click({ role: "button", name: "Close Settings" });
return "ok";
