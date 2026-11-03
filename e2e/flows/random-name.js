// Creating a bot without a name assigns a short random name that survives relaunch.
let app = await cua.getApp(APP);
await app.click({ role: "button", name: "New chat" });
app = await pick(app, "New Bot");
await app.waitFor({ role: "textfield", name: "Name", timeout_ms: 10000 });
await app.click({ role: "button", name: "Create" });
await app.waitFor({ role: "button", name: "Close New Bot", gone: true, timeout_ms: 10000 });
await app.waitFor({ role: "textfield", name: "Message", timeout_ms: 10000 });
const headers = await app.query({ role: "button", name: "profile" });
const header = headers.find(item => /^Edit .+’s profile$/.test(item.label));
const match = header?.label.match(/^Edit ([A-Z][a-z]{3,4})’s profile$/);
if (!match) throw new Error("The generated name is not 4–5 letters: " + header?.label);
await app.waitFor({ role: "button", name: match[1] });
return match[1];

// @relaunch
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: `Edit ${PREVIOUS}’s profile`, timeout_ms: 10000 });
await app.click({ role: "button", name: `Edit ${PREVIOUS}’s profile` });
await app.waitFor({ role: "textfield", name: "Name", value: PREVIOUS, timeout_ms: 10000 });
return "ok";
