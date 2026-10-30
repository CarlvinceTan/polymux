// @fixture --verify-fixture --verify-history
// The latest button stays visible while reading back and returns to the newest message.
const app = await cua.getApp(APP);
let rows = [];
for (let i = 0; i < 60 && !rows.length; i++) {
    rows = (await app.query({ role: "button", name: "t-long" })).filter(row => row.label.startsWith("t-long, "));
    if (!rows.length) await cua.sleep(500);
}
if (rows.length !== 1) throw new Error("History chat missing");
await app.click(rows[0].element_token);
await app.waitFor({ role: "button", name: "History message 150", timeout_ms: 10000 });
await app.waitFor({ role: "button", name: "Scroll to latest", gone: true });
await app.scroll({ role: "button", name: "History message 150" }, "up", 12);
await app.waitFor({ role: "button", name: "Scroll to latest" });
for (let i = 0; i < 5; i++) {
    if ((await app.query({ role: "button", name: "Scroll to latest" })).length !== 1) throw new Error("Latest button flickered");
    await cua.sleep(100);
}
await app.click({ role: "button", name: "Scroll to latest" });
await app.waitFor({ role: "button", name: "Scroll to latest", gone: true });
const [last] = await app.query({ role: "button", name: "History message 150" });
const [composer] = await app.query({ role: "textfield", name: "Message" });
if (!last?.frame || !composer?.frame || last.frame.y + last.frame.h > composer.frame.y) {
    throw new Error("Newest message is below the composer");
}
return "ok";
