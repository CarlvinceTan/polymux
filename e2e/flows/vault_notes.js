// Standalone secrets and recovery codes can be saved together under Notes.
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
for (const name of ["All", "Logins", "Cards", "TOTP", "Passkeys", "Wi-Fi", "Notes"]) {
    await app.waitFor({ role: "button", name });
}
for (const name of ["Secrets", "Recovery codes"]) {
    if ((await app.query({ role: "button", name })).length) throw new Error(`Unexpected ${name} filter`);
}
app = await pick(app, "Note", { role: "menubutton", name: "Add vault item" });
await app.typeText("Standalone note", { role: "textfield", name: "Vault title" });
await app.typeText("synthetic-token", { role: "textfield", name: "Secret" });
await app.click({ role: "button", name: "Add recovery code" });
await app.typeText("synthetic-recovery", { role: "textfield", name: "Recovery code" });
await app.click({ role: "button", name: "Save vault item" });
await app.waitFor({ role: "button", name: "Standalone note" });
await app.click({ role: "button", name: "Logins" });
await app.waitFor({ role: "button", name: "Standalone note", gone: true });
await app.click({ role: "button", name: "Notes" });
await app.click({ role: "button", name: "Standalone note" });
await app.waitFor({ role: "button", name: "Clear Secret" });
for (const name of ["Secret", "Recovery code"]) {
    const [field] = await app.query({ role: "textfield", name });
    if (!field || /synthetic-/.test(field.value ?? "")) throw new Error(`Missing or exposed ${name}`);
}
await app.pressKey("cmd+a", { role: "textfield", name: "Vault title" });
await app.typeText("Renamed note", { role: "textfield", name: "Vault title" });
await app.click({ role: "button", name: "Save vault item" });
await app.click({ role: "button", name: "Renamed note" });
await app.waitFor({ role: "button", name: "Clear Secret" });
await app.waitFor({ role: "textfield", name: "Recovery code" });
await app.click({ role: "button", name: "Delete vault item" });
await app.waitFor({ role: "button", name: "Renamed note", gone: true });
await app.click({ role: "button", name: "Close Settings" });
return "ok";
