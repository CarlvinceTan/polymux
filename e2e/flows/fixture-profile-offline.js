// @fixture --verify-fixture --verify-tools --verify-profile-offline
// Profile sections that can't load show the offline icon instead of loading forever.
const app = await cua.getApp(APP);
let rows = [];
for (let i = 0; i < 60 && !rows.length; i++) {
    rows = (await app.query({ role: "button", name: "Chief" })).filter(row => row.label.startsWith("Chief, "));
    if (!rows.length) await cua.sleep(500);
}
if (rows.length !== 1) throw new Error("Chief row missing");
await app.click(rows[0].element_token);
await app.click({ role: "button", name: "Edit Chief’s profile" });
await app.click({ role: "button", name: "Capabilities" });
await app.waitFor({ name: "Offline", timeout_ms: 10000 });
if ((await app.query({ name: "Offline" })).length < 2) throw new Error("Skills and MCPs should both show the offline icon");
if ((await app.query({ name: "Loading" })).length) throw new Error("A failed section is still loading");
await app.click({ role: "button", name: "Work" });
await app.waitFor({ name: "Offline", timeout_ms: 5000 });
return "ok";
