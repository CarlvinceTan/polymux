// @fixture --verify-fixture --verify-tools
// An unsent draft stays with its chat when you switch away and back, and survives a crash along with
// a group draft's mention, the open chat, the message being replied to, the side panel and its unsaved profile edit.
const app = await cua.getApp(APP);
const source = "Second reply from the same turn.";
async function open(name) {
  let rows = [];
  for (let i = 0; i < 40 && !rows.length; i++) {
    rows = (await app.query({ role: "button", name })).filter((row) => row.label.startsWith(name + ", "));
    if (!rows.length) await cua.sleep(250);
  }
  if (!rows.length) throw new Error("Missing chat " + name);
  await app.click(rows[0].element_token);
}
await open("grp-trip");
await app.typeText("Hi @Ar", { role: "textfield", name: "Message" });
await app.click({ role: "button", name: "Mention Aria" });
await app.waitFor({ role: "textfield", name: "Message", value: "Hi @Aria ", timeout_ms: 5000 });
await open("Chief");
await app.typeText("Draft for Chief", { role: "textfield", name: "Message" });
await open("Aria");
await cua.sleep(500);
const [other] = await app.query({ role: "textfield", name: "Message" });
if (other?.value === "Draft for Chief") throw new Error("Chief's draft showed in Aria's chat: " + JSON.stringify(other));
await open("Chief");
await app.waitFor({ role: "textfield", name: "Message", value: "Draft for Chief", timeout_ms: 5000 });
await app.rightClick({ role: "button", name: source });
await app.click({ role: "menuitem", name: "Reply" });
await app.waitFor({ role: "button", name: "Cancel reply" });
await app.click({ role: "button", name: "Edit Chief’s profile" });
await app.waitFor({ role: "textfield", name: "Name", value: "Chief", timeout_ms: 5000 });
await app.typeText(" Two", { role: "textfield", name: "Name" });
const [field] = await app.query({ role: "textfield", name: "Name" });
if (!field?.value?.includes("Two")) throw new Error("The name edit didn't take: " + JSON.stringify(field));
await cua.sleep(200);
return field.value;
// @crash
const app = await cua.getApp(APP);
async function open(name) {
  let rows = [];
  for (let i = 0; i < 40 && !rows.length; i++) {
    rows = (await app.query({ role: "button", name })).filter((row) => row.label.startsWith(name + ", "));
    if (!rows.length) await cua.sleep(250);
  }
  if (!rows.length) throw new Error("Missing chat " + name);
  await app.click(rows[0].element_token);
}
await app.waitFor({ role: "textfield", name: "Message", value: "Draft for Chief", timeout_ms: 10000 });
await app.waitFor({ role: "button", name: "Cancel reply", timeout_ms: 5000 });
// The fixture server forgets everything on a crash, so the edit can only come back from this device.
await app.waitFor({ role: "textfield", name: "Name", value: PREVIOUS, timeout_ms: 5000 });
await open("grp-trip");
await app.waitFor({ role: "textfield", name: "Message", value: "Hi @Aria ", timeout_ms: 5000 });
return "ok";
