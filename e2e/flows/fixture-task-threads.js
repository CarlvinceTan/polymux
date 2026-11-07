// @fixture --verify-fixture --verify-profile
// A bot's tasks show only their state in its profile: they don't open, have no actions, and stay out of the chat list.
const app = await cua.getApp(APP);
let rows = [];
for (let i = 0; i < 60 && !rows.length; i++) {
  rows = (await app.query({ role: "button", name: "Chief" })).filter((item) => item.label.startsWith("Chief, "));
  if (!rows.length) await cua.sleep(500);
}
if (!rows.length) throw new Error("Chief row is missing");
await app.click(rows[0].element_token);
await app.waitFor({ text: "Profile fixture reply", timeout_ms: 10000 });
await app.click({ role: "button", name: "Edit Chief’s profile" });
await app.waitFor({ text: "Archive photos", timeout_ms: 10000 });
for (const name of ["Launch report", "Visa renewal", "Venue shortlist", "Archive photos"]) {
  if ((await app.query({ role: "button", name })).some((item) => item.label.includes(name))) throw new Error(`${name} can be pressed`);
}
if ((await app.query({ role: "menubutton", name: "More actions for Launch report" })).length) throw new Error("a task has actions");
const state = await app.getState({ include_screenshot: false, timeout_ms: 5000, max_elements: 500 });
if (state.truncated) throw new Error("Tasks lookup is incomplete");
const heading = state.tree_markdown.search(/AX(?:StaticText = "Tasks"|Heading \(Tasks\))/);
if (heading < 0) throw new Error("Tasks section heading is missing");
const tree = state.tree_markdown.slice(heading);
// Each task's state comes just before its title.
const order = ["Open", "Venue shortlist", "Open", "Visa renewal", "In progress", "Launch report", "Done", "Archive photos"];
let at = 0;
for (const text of order) {
  const next = tree.indexOf(text, at);
  if (next < 0) throw new Error(`${text} is missing or out of order`);
  at = next + text.length;
}
return "ok";
