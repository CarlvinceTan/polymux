// @fixture --verify-fixture --verify-profile
// First focus selects an inline value; later caret movement remains ordinary editing.
const app = await cua.getApp(APP);
let chief;
for (let i = 0; i < 40 && !chief; i++) {
    [chief] = (await app.query({ role: "button", name: "Chief" })).filter(item => item.label.startsWith("Chief, "));
    if (!chief) await cua.sleep(250);
}
if (!chief) throw new Error("Chief chat missing");
await app.click(chief.element_token);
await app.click({ role: "button", name: "Edit Chief’s profile" });
await app.waitFor({ role: "textfield", name: "Role" });
const name = { role: "textfield", name: "Name" };
const role = { role: "textfield", name: "Role" };
await app.typeText("Linus", name);
await app.waitFor({ ...name, value: "Linus" });
await app.pressKey("left", name);
await app.typeText("X");
await app.waitFor({ ...name, value: "LinuXs" });
await app.typeText("Maintainer", role);
await app.waitFor({ ...role, value: "Maintainer" });
await app.typeText("Milo", name);
await app.waitFor({ ...name, value: "Milo" });
return "ok";
