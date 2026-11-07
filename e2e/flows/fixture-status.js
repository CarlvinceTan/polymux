// @fixture --verify-fixture --verify-status
// Dismissing an outage keeps it hidden until recovery; a later outage can warn again.
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Disconnect", timeout_ms: 10000 });

async function hidden() {
  for (let i = 0; i < 20; i++) {
    const rows = await app.query({ role: "statictext", name: "Can’t reach server" });
    if (rows.length === 0) return;
    await cua.sleep(100);
  }
  throw new Error("The connection status stayed visible after recovery");
}

await hidden();
await app.click({ role: "button", name: "Disconnect" });
await app.waitFor({ text: "Can’t reach server", timeout_ms: 10000 });
await app.click({ role: "button", name: "Connect" });
await hidden();
for (let i = 0; i < 2; i++) {
  await app.click({ role: "button", name: "Disconnect" });
  await app.waitFor({ text: "Can’t reach server", timeout_ms: 10000 });
  await app.waitFor({ text: "Cached chat" });
  await app.click({ role: "button", name: "Dismiss warning" });
  await hidden();
  await cua.sleep(300);
  await hidden();
  await app.click({ role: "button", name: "Connect" });
  await hidden();
}
return "ok";
