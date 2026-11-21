// Logins and authenticators support replacement without stored-value readback.
let app = await cua.getApp(APP);
for (let i = 0; i < 20; i++) {
    if ((await app.query({ role: "button", name: "Skip for now" })).length) {
        await app.click({ role: "button", name: "Skip for now" });
        break;
    }
    await cua.sleep(250);
}
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Vault" });
await app.waitFor({ role: "textfield", name: "Search vault" });
const entries = [
    { kind: "Login", title: "Service account", field: "Password", value: "synthetic-service-token",
      seed: "JBSWY3DPEHPK3PXP" },
    { kind: "TOTP", title: "Browser authenticator", field: "TOTP seed or URI", value: "JBSWY3DPEHPK3PXP" },
];
for (const entry of entries) {
    app = await pick(app, entry.kind, { role: "menubutton", name: "Add vault item" });
    await app.waitFor({ role: "textfield", name: "Vault title" });
    await app.typeText(entry.title, { role: "textfield", name: "Vault title" });
    await app.typeText(entry.value, { role: "textfield", name: entry.field });
    if (entry.seed) await app.typeText(entry.seed, { role: "textfield", name: "TOTP seed or URI" });
    await app.click({ role: "button", name: "Save vault item" });
    await app.waitFor({ role: "button", name: entry.title });
}
// A login carrying a seed and a standalone code item both surface under TOTP.
await app.click({ role: "button", name: "TOTP" });
for (const entry of entries) {
    await app.waitFor({ role: "button", name: entry.title });
    await app.click({ role: "button", name: entry.title });
    await app.waitFor({ role: "textfield", name: entry.field });
    const [field] = await app.query({ role: "textfield", name: entry.field });
    if ((field.value ?? "").includes(entry.value)) throw new Error("Stored value returned to editor");
    if (entry.seed) {
        const [seed] = await app.query({ role: "textfield", name: "TOTP seed or URI" });
        if ((seed.value ?? "").includes(entry.seed)) throw new Error("Stored seed returned to editor");
    }
    if ((await app.query({ name: "Reveal" })).length) throw new Error("Reveal control remains");
    if ((await app.query({ name: "Vault One-time code" })).length) throw new Error("Authenticator code was exposed");
    await app.click({ role: "button", name: "Delete vault item" });
    await app.waitFor({ role: "button", name: entry.title, gone: true });
}
await app.click({ role: "button", name: "Close Settings" });
return "ok";
