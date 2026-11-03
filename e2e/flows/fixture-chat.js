// @fixture --verify-fixture --verify-tools
// Opening a chat shows its messages.
const app = await cua.getApp(APP);
let rows = [];
for (let i = 0; i < 60 && !rows.length; i++) {
  rows = (await app.query({ role: "button", name: "Chief" })).filter((item) => item.label.startsWith("Chief, "));
  if (!rows.length) await cua.sleep(500);
}
await app.click(rows[0].element_token);
await app.waitFor({ text: "Second reply from the same turn.", timeout_ms: 10000 });
return "ok";
