// Secrets and authenticators support replacement without stored-value readback.
let app = await cua.getApp(APP);
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Vault" });
await app.waitFor({ role: "textfield", name: "Search vault" });
for (const entry of [
    { kind: "Secret", title: "Service secret", field: "Secret", value: "synthetic-service-token" },
    { kind: "TOTP", title: "Browser authenticator", field: "TOTP seed or URI", value: "JBSWY3DPEHPK3PXP" },
]) {
    app = await pick(app, entry.kind, { role: "menubutton", name: "Add vault item" });
    await app.waitFor({ role: "textfield", name: "Vault title" });
    await app.typeText(entry.title, { role: "textfield", name: "Vault title" });
    await app.typeText(entry.value, { role: "textfield", name: entry.field });
    await app.click({ role: "button", name: "Save vault item" });
    await app.waitFor({ role: "button", name: entry.title });
    await app.click({ role: "button", name: entry.title });
    await app.waitFor({ role: "textfield", name: entry.field });
    const [field] = await app.query({ role: "textfield", name: entry.field });
    if ((field.value ?? "").includes(entry.value)) throw new Error("Stored value returned to editor");
    if ((await app.query({ name: "Reveal" })).length) throw new Error("Reveal control remains");
    if ((await app.query({ name: "Vault One-time code" })).length) throw new Error("Authenticator code was exposed");
    await app.click({ role: "button", name: "Delete vault item" });
    await app.waitFor({ role: "button", name: entry.title, gone: true });
}
await app.click({ role: "button", name: "Close Settings" });
return "ok";
