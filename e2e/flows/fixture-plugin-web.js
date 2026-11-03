// @fixture --verify-fixture --verify-plugins --verify-plugin-web
// A web plugin's page fills the side panel and reaches its service; the panel's edge and close button stay the app's.
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
await app.waitFor({ role: "button", name: "Close side panel" });

const resize = { role: "slider", name: "Resize side panel" };
await app.setValue(resize, "400");
await app.waitFor({ ...resize, value: "400" });

await app.click({ role: "button", name: "Ping" });
await app.waitFor({ text: "Service answered in t-chief", timeout_ms: 5000 });

await app.click({ role: "button", name: "Finish" });
await app.waitFor({ ...resize, gone: true, timeout_ms: 5000 });
await app.click({ role: "button", name: "Canvas" });
await app.waitFor({ text: "Fixture page", timeout_ms: 10000 });
await app.click({ role: "button", name: "Close side panel" });
await app.waitFor({ ...resize, gone: true, timeout_ms: 5000 });
return "ok";
