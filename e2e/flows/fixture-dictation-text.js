// @fixture --verify-fixture --verify-dictation
const app = await cua.getApp(APP);
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
  if (text.includes("ship it") && !text.includes("um")) break;
  await cua.sleep(250);
}
if (!text.includes("ship it")) throw new Error("The dictated sentence never reached the composer: " + JSON.stringify(text));
if (text.includes("um")) throw new Error("The hesitation survived cleanup: " + JSON.stringify(text));
return text;
