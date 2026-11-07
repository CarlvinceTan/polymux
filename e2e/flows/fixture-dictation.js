// @fixture --verify-fixture --verify-dictation
const app = await cua.getApp(APP);
let rows = [];
for (let i = 0; i < 60 && !rows.length; i++) {
  rows = (await app.query({ role: "button", name: "Chief" })).filter(item => item.label.startsWith("Chief, "));
  if (!rows.length) await cua.sleep(500);
}
await app.click(rows[0].element_token);

async function preparing() {
  await app.click({ role: "button", name: "Start dictation" });
  await app.waitFor({ role: "button", name: "Cancel dictation", timeout_ms: 2000 });
  const fields = await app.query({ role: "textfield", name: "Message" });
  if (!fields.length) throw new Error("Permission setup replaced the message input with a waveform");
}

await preparing();
await app.click({ role: "button", name: "Cancel dictation" });
await cua.sleep(3500);
await app.waitFor({ role: "button", name: "Start dictation", timeout_ms: 2000 });

await preparing();
await app.waitFor({ role: "button", name: "Pause dictation", timeout_ms: 6000 });
if ((await app.query({ role: "textfield", name: "Message" })).length) {
  throw new Error("Recording did not replace the message input with a waveform");
}
// Let the fixture's sentence finish, so pausing leaves a draft the composer can send.
await cua.sleep(1200);
await app.click({ role: "button", name: "Pause dictation" });
await app.waitFor({ role: "textfield", name: "Message", timeout_ms: 3000 });
await app.waitFor({ role: "button", name: "Send message", timeout_ms: 3000 });
return "ok";
