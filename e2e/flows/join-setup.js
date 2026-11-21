// @fresh
// @second-server
// Setup on a Mac that joins a running server skips this Mac's permissions, and asks for a model
// only because that server has none yet.
let app = await cua.getApp(APP);
for (let i = 0; i < 80 && !(await app.query({ role: "statictext", name: "Get Started with Polymux" })).length; i++) await cua.sleep(250);
await app.click({ role: "button", name: "Continue" });
await app.click({ role: "button", name: "On another device" });
await app.waitFor({ role: "textfield", name: "Pairing link", timeout_ms: 5000 });
await app.typeText(SECOND_LINK, { role: "textfield", name: "Pairing link" });
await app.click({ role: "button", name: "Connect" });
for (let i = 0; i < 80 && !(await app.query({ text: "Connected" })).length; i++) await cua.sleep(250);
await app.waitFor({ text: "Connected", timeout_ms: 1000 });
await app.click({ role: "button", name: "Continue" });

await app.waitFor({ role: "statictext", name: "Connect a Model", timeout_ms: 10000 });
await app.click({ role: "button", name: "Set up a model later" });
await app.waitFor({ role: "statictext", name: "Plugins", timeout_ms: 10000 });
if ((await app.query({ role: "statictext", name: "System Permissions" })).length) throw new Error("Permissions showed for another server");
await app.click({ role: "button", name: "Continue" });
await app.waitFor({ role: "statictext", name: "You’re All Set", timeout_ms: 5000 });
await app.click({ role: "button", name: "Start" });
await cua.sleep(1000);
app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Settings", timeout_ms: 10000 });
return "ok";
