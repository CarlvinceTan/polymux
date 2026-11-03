// @fixture --verify-fixture --verify-profile --verify-profile-loading
// Work renders independently of slow profile/setup requests and stays warm between visits.
const app = await cua.getApp(APP);
let rows = [];
for (let i = 0; i < 60 && !rows.length; i++) {
  rows = (await app.query({ role: "button", name: "Chief" })).filter(item => item.label.startsWith("Chief, "));
  if (!rows.length) await cua.sleep(500);
}
if (!rows.length) throw new Error("Chief row is missing");
await app.click(rows[0].element_token);
await app.waitFor({ text: "Profile fixture reply", timeout_ms: 10000 });
const started = Date.now();
await app.click({ role: "button", name: "Edit Chief’s profile" });
await app.waitFor({ text: "Reviewing calendar", timeout_ms: 2500 });
await app.waitFor({ text: "Scheduled check 1", timeout_ms: 2500 });
const firstOpenMs = Date.now() - started;
if (firstOpenMs >= 5000) throw new Error("Work waited for the deliberately slow profile requests");
await app.click({ role: "button", name: "Close side panel" });
await app.waitFor({ role: "button", name: "Work", gone: true });
const reopened = Date.now();
await app.click({ role: "button", name: "Edit Chief’s profile" });
await app.waitFor({ text: "Scheduled check 1", timeout_ms: 2500 });
const reopenMs = Date.now() - reopened;
for (let attempt = 0; attempt < 40; attempt++) {
  if ((await app.query({ role: "button", name: "Cancel Updated scheduled check 1" })).length) break;
  if (attempt === 39) throw new Error("Schedule did not refresh in the background");
  await cua.sleep(1500);
}
await app.waitFor({ text: "Reviewing calendar" });
await app.click({ role: "button", name: "Capabilities" });
await app.waitFor({ text: "polymux", timeout_ms: 10000 });
await app.click({ role: "button", name: "Work" });
await app.waitFor({ text: "Updated scheduled check 1", timeout_ms: 2500 });
return { firstOpenMs, reopenMs, backgroundRefresh: true };
