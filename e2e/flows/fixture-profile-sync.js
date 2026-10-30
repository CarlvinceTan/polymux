// @fixture --verify-fixture --verify-profile --verify-profile-update
// Opening Chief's profile arms an external rename on the next sidebar refresh.
const app = await cua.getApp(APP);
const [chief] = (await app.query({ role: "button", name: "Chief" })).filter(item => item.label.startsWith("Chief, "));
if (!chief) throw new Error("Chief row is missing");
await app.click(chief.element_token);
await app.click({ role: "button", name: "Edit Chief’s profile" });
await app.waitFor({ role: "textfield", name: "Name", value: "Alan", timeout_ms: 10000 });
await app.waitFor({ role: "textfield", name: "Role", value: "Researcher" });
await app.waitFor({ role: "button", name: "Edit Alan’s profile" });
await app.waitFor({ role: "button", name: "Close side panel" });
return "ok";
