// @fixture --verify-fixture --verify-plugins --verify-permissions
// A chat's rules are added and removed in the side panel, and say when commands can get around them.
const app = await cua.getApp(APP);
let rows = [];
for (let i = 0; i < 60 && !rows.length; i++) {
  rows = (await app.query({ role: "button", name: "Chief" })).filter((item) => item.label.startsWith("Chief, "));
  if (!rows.length) await cua.sleep(500);
}
if (!rows.length) throw new Error("Chief row is missing");
await app.click(rows[0].element_token);

await app.click({ role: "button", name: "Permissions" });
await app.waitFor({ text: "No rules yet." });
const add = await app.query({ role: "button", name: "Add rule" });
if (!add.length) throw new Error("Add rule is missing");

await app.typeText("~/.ssh/**", { role: "textfield", name: "Path" });
await app.click({ role: "button", name: "Add rule" });
await app.waitFor({ text: "~/.ssh/**" });
await app.waitFor({ text: "Deny reading" });
await app.waitFor({ text: "Commands can still reach these files." });
if ((await app.query({ text: "No rules yet." })).length) throw new Error("The empty state stayed");

// Denying every command closes the way around the path rule.
await app.click({ role: "button", name: "Action" });
await app.click({ role: "button", name: "Commands" });
await app.waitFor({ role: "textfield", name: "Command" });
await app.click({ role: "button", name: "Add rule" });
await app.waitFor({ text: "Everything" });
for (let i = 0; i < 20 && (await app.query({ text: "Commands can still reach these files." })).length; i++) await cua.sleep(250);
if ((await app.query({ text: "Commands can still reach these files." })).length) throw new Error("The warning outlived the command rule");

await app.click({ role: "button", name: "Remove Deny commands, Everything" });
await app.waitFor({ text: "Commands can still reach these files." });
await app.click({ role: "button", name: "Remove Deny reading, ~/.ssh/**" });
await app.waitFor({ text: "No rules yet." });
return "ok";
