// @fixture --verify-fixture --verify-plugins
// New chat opens a direct chat or a named group on one account, and a platform takes a second account.
let app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Hub", timeout_ms: 10000 });
await app.click({ role: "button", name: "Hub" });
await app.waitFor({ role: "button", name: "Priya Raman" });

app = await pick(app, "New chat");
await app.click({ role: "button", name: "Select Tom Chen" });
await app.waitFor({ role: "button", name: "Remove Tom Chen" });
await app.click({ role: "button", name: "Select Sam" }).catch(() => {});
if ((await app.query({ role: "button", name: "Remove Sam" })).length) throw new Error("A group mixed two accounts");
await app.click({ role: "button", name: "Select Ana Ruiz" });
await app.waitFor({ text: "Name this group." });
await app.typeText("Trip", { role: "textfield", name: "Group name" });
await app.click({ role: "button", name: "Create" });
await app.waitFor({ role: "button", name: "Rename group" });
await app.waitFor({ role: "textfield", name: "To", gone: true });

app = await pick(app, "New chat");
await app.typeText("Sa", { role: "textfield", name: "To" });
await app.waitFor({ role: "button", name: "Select Tom Chen", gone: true });
await app.click({ role: "button", name: "Select Sam" });
await app.click({ role: "button", name: "Create" });
await app.waitFor({ role: "button", name: "Rename group", gone: true });
await app.waitFor({ role: "textfield", name: "To", gone: true });

app = await pick(app, "Add account");
const rows = await app.query({ role: "button", name: "WhatsApp" });
await app.click(rows.find((row) => row.value === "Personal").element_token);
await app.click({ role: "button", name: "Connect Phone number" });
await app.typeText("555", { role: "textfield", name: "Phone" });
await app.click({ role: "button", name: "Continue" });
await app.waitFor({ role: "button", name: "Disconnect Work" });
await app.click({ role: "button", name: "Disconnect Work" });
await app.waitFor({ role: "button", name: "Disconnect Work", gone: true });
await app.waitFor({ role: "button", name: "Disconnect Personal" });
await app.click({ role: "button", name: "Close WhatsApp" });
return "ok";
