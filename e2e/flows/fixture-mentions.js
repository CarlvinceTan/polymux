// @fixture --verify-fixture --verify-tools
// In a group, arrow keys choose among mentions and Return inserts the chosen one.
const app = await cua.getApp(APP);
let row;
for (let i = 0; i < 40 && !row; i++) {
    [row] = (await app.query({ role: "button", name: "grp-trip" })).filter(item => item.label.startsWith("grp-trip, "));
    if (!row) await cua.sleep(250);
}
if (!row) throw new Error("Group row is missing");
await app.click(row.element_token);
const field = { role: "textfield", name: "Message" };
await app.waitFor(field, { timeout_ms: 5000 });
await app.typeText("@", field);
await app.waitFor({ role: "button", name: "Mention Chief", timeout_ms: 5000 });
const top = async () => (await app.query({ role: "button", name: "Mention" })).find(item => item.value === "Top match")?.label;
const start = await top();
if (start !== "Mention everyone") throw new Error("List starts on " + start);
await app.pressKey("down", field);
await app.pressKey("down", field);
const next = await top();
if (next === start) throw new Error("Arrow keys didn't move the choice");
await app.pressKey("return", field);
await app.waitFor({ role: "button", name: "Mention Chief", gone: true });
const [message] = await app.query(field);
if (!message.value.includes("@" + next.replace("Mention ", ""))) throw new Error("Inserted " + message.value + " for " + next);
await app.pressKey("cmd+a", field);
await app.pressKey("delete", field);
return "ok";
