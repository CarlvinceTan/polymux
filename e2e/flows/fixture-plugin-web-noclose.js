// @fixture --verify-fixture --verify-plugins --verify-plugin-web --verify-plugin-noclose
// A web plugin can hide the close button, and its panel still resizes and collapses.
const app = await cua.getApp(APP);
let rows = [];
for (let i = 0; i < 60 && !rows.length; i++) {
  rows = (await app.query({ role: "button", name: "Chief" })).filter((item) => item.label.startsWith("Chief, "));
  if (!rows.length) await cua.sleep(500);
}
if (!rows.length) throw new Error("Chief row is missing");
await app.click(rows[0].element_token);
await app.click({ role: "button", name: "Canvas" });
await app.waitFor({ text: "Fixture page", timeout_ms: 10000 });
if ((await app.query({ role: "button", name: "Close side panel" })).length) throw new Error("The hidden close button is showing");

const resize = { role: "slider", name: "Resize side panel" };
await app.setValue(resize, "400");
await app.waitFor({ ...resize, value: "400" });
await app.click({ role: "button", name: "Finish" });
await app.waitFor({ ...resize, gone: true, timeout_ms: 5000 });
return "ok";
