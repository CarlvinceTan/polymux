// @cua
// Four sidebar tabs stay in sync, and reopening returns to General.
const app = await cua.getApp(APP);
await app.click({ role: "button", name: "Settings" });
await app.waitFor({ role: "button", name: "Theme", timeout_ms: 5000 });
await app.waitFor({ name: "Allow notifications", timeout_ms: 5000 });
for (const name of ["Vault", "Updates"]) {
    if ((await app.query({ role: "statictext", name })).length) throw new Error("General includes " + name + " settings");
}
if ((await app.query({ role: "button", name: "Connections" })).length) throw new Error("Old Connections tab remains");
await app.click({ role: "button", name: "Models" });
await app.waitFor({ role: "button", name: "Model", timeout_ms: 5000 });
await app.waitFor({ role: "button", name: "Voice provider", timeout_ms: 5000 });
if ((await app.query({ role: "button", name: "Theme" })).length) throw new Error("Models includes appearance settings");
await app.click({ role: "button", name: "Vault" });
await app.waitFor({ role: "textfield", name: "Search vault", timeout_ms: 5000 });
if ((await app.query({ role: "textfield", name: "Vault master password" })).length) throw new Error("Vault browsing is gated by a password");
if ((await app.query({ role: "button", name: "Theme" })).length) throw new Error("Vault includes appearance settings");
await app.click({ role: "button", name: "Updates" });
await app.waitFor({ role: "button", name: "Check for updates", timeout_ms: 5000 });
await app.waitFor({ text: "Version", timeout_ms: 5000 });
await app.waitFor({ role: "button", name: "Server channel", value: "Nightly", timeout_ms: 5000 });
await app.click({ role: "button", name: "Server channel" });
await app.click({ role: "button", name: "Stable" });
await app.waitFor({ role: "button", name: "Server channel", value: "Stable", timeout_ms: 5000 });
await app.click({ role: "button", name: "General" });
await app.click({ role: "button", name: "Updates" });
await app.waitFor({ role: "button", name: "Server channel", value: "Stable", timeout_ms: 5000 });
const rows = await app.query({ role: "statictext" });
for (const prefix of ["Client", "Server"]) {
    if (!rows.some(row => (row.value || row.name || "").startsWith(prefix))) throw new Error("Version section is missing the " + prefix + " row");
}
for (const name of ["General", "Vault", "Updates", "General", "Models"]) {
    await app.click({ role: "button", name });
}
await app.waitFor({ role: "button", name: "Model", timeout_ms: 5000 });
if ((await app.query({ role: "button", name: "Theme" })).length) throw new Error("Rapid switching left the wrong sections");
await app.click({ role: "button", name: "Close Settings" });
await app.click({ role: "button", name: "Settings" });
await app.waitFor({ role: "button", name: "Theme", timeout_ms: 5000 });
await app.click({ role: "button", name: "Close Settings" });
return "ok";
