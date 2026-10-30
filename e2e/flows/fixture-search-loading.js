// @fixture --verify-fixture --verify-profile --verify-search-slow
// Downloaded messages remain searchable while an eight-second server search is still running.
const app = await cua.getApp(APP);
let rows = [];
for (let i = 0; i < 60 && !rows.length; i++) {
  rows = (await app.query({ role: "button", name: "Chief" })).filter(item => item.label.startsWith("Chief, "));
  if (!rows.length) await cua.sleep(500);
}
if (!rows.length) throw new Error("Chief row is missing");
await app.click(rows[0].element_token);
await app.waitFor({ text: "Profile fixture reply", timeout_ms: 10000 });
await app.click({ role: "button", name: "Search" });
const opened = Date.now();
await app.typeText("fixture", { role: "textfield", name: "Search everything" });
await app.waitFor({ role: "button", name: "Profile fixture reply", timeout_ms: 2000 });
const firstMs = Date.now() - opened;
await app.click({ role: "button", name: "Close search" });
await app.click({ role: "button", name: "Search" });
await app.typeText("fixture", { role: "textfield", name: "Search everything" });
await app.waitFor({ role: "button", name: "Profile fixture reply", timeout_ms: 2000 });
await app.click({ role: "button", name: "Close search" });
await app.typeText("Typing stays available", { role: "textfield", name: "Message" });
await app.waitFor({ role: "textfield", name: "Message", value: "Typing stays available", timeout_ms: 2000 });
return { firstMs, cachedReopening: true };
