// A chat and its messages survive an app relaunch. The harness relaunches the app at the marker and passes the first part's result as PREVIOUS.
let app = await cua.getApp(APP);
const name = "Keep" + Date.now();
const text = "Still here, " + name;
await app.click({ role: "button", name: "New chat" });
app = await pick(app, "New Bot");
await app.typeText(name, { role: "textfield", name: "Name" });
await app.click({ role: "button", name: "Create" });
await app.typeText(text, { role: "textfield", name: "Message" });
await app.pressKey("return", { role: "textfield", name: "Message" });
await app.waitFor({ text: text, timeout_ms: 10000 });
await app.click({ role: "button", name: "Edit " + name + "’s profile" });
await app.waitFor({ role: "textfield", name: "Role" });
return { name, text };
// @relaunch
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: PREVIOUS.name, timeout_ms: 10000 });
await app.waitFor({ role: "textfield", name: "Role", timeout_ms: 10000 });
await app.click({ role: "button", name: PREVIOUS.name, nth: 0 });
await app.waitFor({ text: PREVIOUS.text, timeout_ms: 10000 });
return "ok";
