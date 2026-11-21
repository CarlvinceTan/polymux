// @fixture --verify-fixture --verify-profile
// Tasks contains delegated child work, with active tasks first and external agents' logos.
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
await app.waitFor({ text: "Reviewing calendar", timeout_ms: 10000 });
await app.waitFor({ text: "Finished task 5" });
if ((await app.query({ text: "Profile fixture reply" })).length && !(await app.query({ name: "Handled by Claude Agent" })).length) {
  throw new Error("Tasks is missing the newest task");
}
const state = await app.getState({ include_screenshot: false, timeout_ms: 5000, max_elements: 500 });
if (state.truncated) throw new Error("Tasks lookup is incomplete");
const heading = state.tree_markdown.search(/AX(?:StaticText = "Tasks"|Heading \(Tasks\))/);
if (heading < 0) throw new Error("Tasks section heading is missing");
const tree = state.tree_markdown.slice(heading);
if (!tree.includes("Reviewing calendar") || tree.indexOf("Reviewing calendar") > tree.indexOf("Finished task 4")) {
  throw new Error("active work is not first");
}
// A registry agent shows its logo; a command the registry doesn't list shows the generic one.
for (const name of ["Claude", "local-agent"]) {
  if (!(await app.query({ name: `Handled by ${name}` })).length) throw new Error(`Handled by ${name} is missing`);
}
return "ok";
