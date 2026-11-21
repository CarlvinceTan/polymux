// @fixture --verify-fixture --verify-plugins --verify-plugin-brands
// Transport plugins installed without a manifest symbol show their brand marks, not the generic piece.
const app = await cua.getApp(APP);
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Plugins" });
for (const name of ["Tailscale", "OpenTunnel"]) {
  await app.waitFor({ role: "button", name, timeout_ms: 10000 });
  const [row] = await app.query({ role: "button", name });
  // A mark drawn at its source size would swell the row far past one line.
  if (row.frame.height > 48) throw new Error(`${name} row is ${row.frame.height}pt tall`);
}
return "ok";
