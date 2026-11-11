// @fixture --verify-fixture --verify-plugins --verify-hub-mail
// A platform with two accounts narrows its list through a dropdown, a group both are in stays in each, and one account shows no dropdown.
let app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Hub", timeout_ms: 10000 });
await app.click({ role: "button", name: "Hub" });
await app.waitFor({ role: "button", name: "WhatsApp" });
await app.click({ role: "button", name: "WhatsApp" });
await app.waitFor({ role: "button", name: "Office group" });
const account = { role: "popupbutton", name: "Account" };
const shown = async name => (await app.query({ role: "button", name })).length > 0;
await app.click(account);
app = await pick(app, "Work (2)", account);
await app.waitFor({ role: "button", name: "Home group", gone: true });
if (!await shown("Office group") || !await shown("Family and friends")) throw new Error("Work's chats are missing");
await app.click(account);
app = await pick(app, "Personal", account);
await app.waitFor({ role: "button", name: "Office group", gone: true });
if (!await shown("Home group") || !await shown("Family and friends")) throw new Error("Personal's chats are missing");
await app.click(account);
app = await pick(app, "All accounts", account);
await app.waitFor({ role: "button", name: "Office group" });
if (!await shown("Home group")) throw new Error("All accounts doesn't show everything");
await app.click({ role: "button", name: "Slack" });
await app.waitFor({ role: "button", name: "Launch team" });
if ((await app.query(account)).length) throw new Error("A platform with one account offers a dropdown");
return "ok";
