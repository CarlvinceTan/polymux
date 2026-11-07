// @fixture --verify-fixture --verify-tools --verify-send-slow
// A slow send must not leave a quote selected or lock the next draft.
const app = await cua.getApp(APP);
const source = "Second reply from the same turn.";
let rows = [];
for (let i = 0; i < 60 && !rows.length; i++) {
    rows = (await app.query({ role: "button", name: "Chief" })).filter((item) => item.label.startsWith("Chief, "));
    if (!rows.length) await cua.sleep(500);
}
if (!rows.length) throw new Error("Chief did not load");
await app.click(rows[0].element_token);
await app.waitFor({ role: "button", name: source });
await app.rightClick({ role: "button", name: source });
await pick(app, "Reply", open => open.rightClick({ role: "button", name: source }));
await app.waitFor({ role: "button", name: "Cancel reply" });
await app.click({ role: "button", name: "Cancel reply" });
await app.waitFor({ role: "button", name: "Cancel reply", gone: true });
await app.rightClick({ role: "button", name: source });
await pick(app, "Reply", open => open.rightClick({ role: "button", name: source }));
await app.typeText("Reply while the server is slow", { role: "textfield", name: "Message" });
await app.click({ role: "button", name: "Send message" });
await app.waitFor({ role: "button", name: "Cancel reply", gone: true, timeout_ms: 1000 });
const [composer] = await app.query({ role: "textfield", name: "Message" });
if (!composer?.enabled) throw new Error("The send locked the next draft");
await app.typeText("Next draft", { role: "textfield", name: "Message" });
await app.waitFor({ role: "textfield", name: "Message", value: "Next draft" });
await app.waitFor({ role: "button", name: "Reply to Chief: " + source });
return "ok";
