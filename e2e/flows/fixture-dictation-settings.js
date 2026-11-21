// @fixture --verify-fixture --verify-dictation
// Dictation settings reach the composer: fillers stay when off, and a sounds-like word becomes its dictionary term.
const app = await cua.getApp(APP);
await app.click({ role: "button", name: "Settings" });
await app.waitFor({ role: "button", name: "Voice", timeout_ms: 5000 });
await app.click({ role: "button", name: "Voice" });
await app.waitFor({ role: "button", name: "Add dictionary word", timeout_ms: 5000 });
await app.click({ role: "button", name: "Add dictionary word" });
await app.typeText("Quen", { role: "textfield", name: "Sounds like" });
await app.typeText("Qwen", { role: "textfield", name: "Dictionary word" });
await app.click({ role: "checkbox", name: "Filler words" });
await app.waitFor({ role: "checkbox", name: "Filler words", value: "0", timeout_ms: 5000 });
const [polish] = await app.query({ role: "checkbox", name: "On-device polish" });
if (polish?.value === "1") {
  await app.click({ role: "checkbox", name: "On-device polish" });
  await app.waitFor({ role: "checkbox", name: "On-device polish", value: "0", timeout_ms: 5000 });
}
await app.click({ role: "button", name: "Close Settings" });

let rows = [];
for (let i = 0; i < 60 && !rows.length; i++) {
  rows = (await app.query({ role: "button", name: "Chief" })).filter(item => item.label.startsWith("Chief, "));
  if (!rows.length) await cua.sleep(500);
}
await app.click(rows[0].element_token);
await app.click({ role: "button", name: "Start dictation" });
await app.waitFor({ role: "button", name: "Pause dictation", timeout_ms: 6000 });
await cua.sleep(2500);
await app.click({ role: "button", name: "Pause dictation" });

let text = "";
for (let i = 0; i < 60; i++) {
  const [field] = await app.query({ role: "textfield", name: "Message" });
  text = field?.value ?? "";
  if (text.includes("ship it")) break;
  await cua.sleep(250);
}
if (!text.includes("um")) throw new Error("Turning filler words off did not keep the hesitation: " + JSON.stringify(text));
if (!text.includes("Qwen") || text.includes("Quen")) throw new Error("The dictionary term did not replace its sounds-like: " + JSON.stringify(text));

await app.click({ role: "button", name: "Settings" });
await app.waitFor({ role: "button", name: "Voice", timeout_ms: 5000 });
await app.click({ role: "button", name: "Voice" });
await app.click({ role: "checkbox", name: "Filler words" });
await app.waitFor({ role: "checkbox", name: "Filler words", value: "1", timeout_ms: 5000 });
if (polish?.value === "1") {
  await app.click({ role: "checkbox", name: "On-device polish" });
  await app.waitFor({ role: "checkbox", name: "On-device polish", value: "1", timeout_ms: 5000 });
}
await app.click({ role: "button", name: "Remove Qwen" });
await app.click({ role: "button", name: "Close Settings" });
return text;
