// @fixture --verify-fixture --verify-busy --verify-actions
// The activity line follows the action instead of falling back to generic working text.
const app = await cua.getApp(APP);
for (const [name, text] of [
  ["Chief", "Digging around"],
  ["Aria", "Reading the file"],
  ["Milo", "Running a few commands"],
  ["Nova", "Jotting it down"],
  ["Chief", "Digging around"],
]) {
  await app.waitFor({ role: "button", name });
  const [row] = (await app.query({ role: "button", name })).filter(item => item.label.startsWith(name + ", "));
  if (!row) throw new Error(name + " row is missing");
  await app.click(row.element_token);
  await app.waitFor({ text, timeout_ms: 10000 });
}
return "ok";
