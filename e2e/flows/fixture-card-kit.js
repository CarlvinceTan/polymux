// @fixture --verify-fixture --verify-cards --verify-card-kit
// Kit-only layouts preserve form state and initialize controls inserted by local interaction.
const app = await cua.getApp(APP);
async function open(name) {
  const rows = (await app.query({ role: "button", name })).filter((row) => row.label.startsWith(name + ", "));
  if (!rows.length) throw new Error("Missing chat " + name);
  await app.click(rows[0].element_token);
}
await app.waitFor({ role: "button", name: "Chief", timeout_ms: 10000 });
await open("Chief");
await app.typeText("Card", { role: "textfield", name: "Message" });
await app.pressKey("return", { role: "textfield", name: "Message" });
await app.waitFor({ role: "heading", name: "Project workspace", timeout_ms: 10000 });
await app.waitFor({ text: "All channels are within budget.", timeout_ms: 10000 });
await app.click({ role: "checkbox", name: "Form", exact: true });
await app.click({ role: "button", name: "Save", exact: true });
await app.waitFor({ text: "Enter a project name.", timeout_ms: 10000 });
await app.click({ role: "button", name: "Fill", exact: true });
await app.click({ role: "button", name: "Save", exact: true });
await app.waitFor({ text: "Saved Quarterly research and customer feedback", timeout_ms: 10000 });
await open("Aria");
await open("Chief");
await app.waitFor({ role: "textfield", name: "Project", timeout_ms: 10000 });
const fields = await app.query({ role: "textfield", name: "Project", exact: true });
if (!fields.some((field) => field.value === "Quarterly research and customer feedback")) {
  throw new Error("Card form lost its value after switching chats");
}
await app.click({ role: "checkbox", name: "Activity", exact: true });
await app.click({ role: "button", name: "Advance", exact: true });
await app.waitFor({ text: "Analysis complete", timeout_ms: 10000 });
await app.click({ role: "button", name: "Filter", exact: true });
await app.waitFor({ role: "checkbox", name: "All", timeout_ms: 10000 });
await app.click({ role: "checkbox", name: "Done", exact: true });
await app.waitFor({ text: "Filter: done", timeout_ms: 10000 });
await app.click({ role: "checkbox", name: "States", exact: true });
await app.waitFor({ role: "heading", name: "No results", timeout_ms: 10000 });
await app.waitFor({ text: "The example report couldn't be saved.", timeout_ms: 10000 });
if ((await app.query({ text: "Part of this card didn’t work." })).length) throw new Error("Kit script failed");
return "ok";
