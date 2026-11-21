// @fixture --verify-fixture --verify-plugins
// A WeChat group renames through WeChat itself, and offers only what can really reach it: pictures, not voice notes.
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Hub", timeout_ms: 10000 });
await app.click({ role: "button", name: "Hub" });
await app.waitFor({ role: "button", name: "Family" });
await app.click({ role: "button", name: "Family" });
await app.waitFor({ role: "button", name: "Rename group" });
await app.waitFor({ role: "textfield", name: "Message" });
await app.click({ role: "button", name: "Add attachment" });
await app.waitFor({ role: "button", name: "Emoji" });
for (const name of ["Stickers", "Photos", "Camera"]) await app.waitFor({ role: "button", name });
if ((await app.query({ role: "button", name: "Record voice message" })).length) throw new Error("Voice notes are offered but wouldn't reach WeChat");
return "ok";
