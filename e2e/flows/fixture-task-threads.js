// @fixture --verify-fixture --verify-profile
// A bot's tasks open in their own thread, stay out of the chat list, and finish from their row.
let app = await cua.getApp(APP);
let rows = [];
for (let i = 0; i < 60 && !rows.length; i++) {
  rows = (await app.query({ role: "button", name: "Chief" })).filter((item) => item.label.startsWith("Chief, "));
  if (!rows.length) await cua.sleep(500);
}
if (!rows.length) throw new Error("Chief row is missing");
await app.click(rows[0].element_token);
await app.waitFor({ text: "Profile fixture reply", timeout_ms: 10000 });
await app.click({ role: "button", name: "Edit Chief’s profile" });
await app.waitFor({ text: "Launch report", timeout_ms: 10000 });
for (const name of ["Open task Visa renewal, needs attention", "Open task Venue shortlist, new reply", "Open done task Archive photos"]) {
  if (!(await app.query({ role: "button", name })).length) throw new Error(`${name} is missing`);
}
const list = await app.query({ role: "button", name: "Launch report" });
if (list.some((item) => item.label.startsWith("Launch report, "))) throw new Error("a task is in the chat list");
await app.click({ role: "button", name: "Open task Venue shortlist, new reply" });
await app.waitFor({ text: "Venue shortlist", timeout_ms: 5000 });
await app.waitFor({ role: "button", name: "Back", timeout_ms: 5000 });
await app.click({ role: "button", name: "Back" });
await app.waitFor({ text: "Profile fixture reply", timeout_ms: 5000 });
await app.waitFor({ role: "button", name: "Open task Venue shortlist", timeout_ms: 5000 });
const more = { role: "menubutton", name: "More actions for Launch report" };
for (const item of ["Pin", "Unpin", "Finish"]) {
  await pick(app, item, more);
  // A closing menu keeps its window for a moment.
  await cua.sleep(750);
  app = await cua.getApp(APP);
}
await app.waitFor({ role: "button", name: "Open done task Launch report", timeout_ms: 5000 });
return "ok";
