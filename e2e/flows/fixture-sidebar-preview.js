// @fixture --verify-fixture --verify-stream-preview
// A streaming reply leaves the sidebar preview on the last finished message.
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Chief" });
const [chief] = (await app.query({ role: "button" })).filter(row => String(row.label).startsWith("Chief, "));
if (!chief) throw new Error("Chief row is missing");
await app.click(chief.element_token);
for (let i = 0; i < 40; i++) {
  const [row] = (await app.query({ role: "button" })).filter(item => String(item.label).startsWith("Chief, "));
  const label = row ? String(row.label) : "";
  if (label.includes("Earlier reply")) {
    if (label.includes("Half-written") || label.includes("Composing reply")) {
      throw new Error("Sidebar preview showed the streaming reply: " + label);
    }
    return "ok";
  }
  await cua.sleep(250);
}
throw new Error("Sidebar preview did not keep the last finished message");
