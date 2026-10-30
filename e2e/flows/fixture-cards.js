// @fixture --verify-fixture --verify-cards
// Live cards keep their controls across chats, send a linked JSON reply, and contain script failures.
const app = await cua.getApp(APP);
async function open(name) {
  const rows = (await app.query({ role: "button", name })).filter((row) => row.label.startsWith(name + ", "));
  if (!rows.length) throw new Error("Missing chat " + name);
  await app.click(rows[0].element_token);
}
await app.waitFor({ role: "button", name: "Chief", timeout_ms: 10000 });
await open("Chief");
await app.typeText("Card", { role: "textfield", name: "Message" });
await app.pressKey("return", { role: "textfield", name: "Message" });
await app.waitFor({ role: "heading", name: "Weekly focus", timeout_ms: 10000 });
await app.waitFor({ text: "week · 4 h · notify", timeout_ms: 10000 });
await app.click({ role: "checkbox", name: "Day" });
await app.click({ role: "checkbox", name: "Notify" });
await app.waitFor({ text: "day · 4 h · quiet", timeout_ms: 10000 });
await open("Aria");
await open("Chief");
await app.waitFor({ text: "day · 4 h · quiet", timeout_ms: 10000 });
await app.click({ role: "button", name: "Theme" });
await app.waitFor({ text: "Theme: dark", timeout_ms: 10000 });
await app.click({ role: "button", name: "Theme" });
await app.waitFor({ text: "Theme: light", timeout_ms: 10000 });
await app.waitFor({ text: "day · 4 h · quiet", timeout_ms: 10000 });
await app.click({ role: "button", name: "Send" });
await app.waitFor({ text: '{"range":"day","hours":4,"notify":false}', timeout_ms: 10000 });
await app.waitFor({ text: "Received your card reply.", timeout_ms: 10000 });
await app.click({ role: "button", name: "Attempt" });
await cua.sleep(700);
if ((await app.query({ role: "button", name: "Scroll to latest" })).length) {
  await app.click({ role: "button", name: "Scroll to latest" });
}
await app.waitFor({ text: '{"range":"day","hours":4,"notify":false}', timeout_ms: 10000 });
if ((await app.query({ text: "Unsolicited" })).length || (await app.query({ text: "Unexpected card message." })).length) {
  throw new Error("Card sent without a fresh user interaction");
}
// A sent bubble reads as a button labelled with its text.
const reply = '{"range":"day","hours":4,"notify":false}';
const replies = [...await app.query({ role: "statictext", name: reply }), ...await app.query({ role: "button", name: reply })];
if (replies.length !== 1) throw new Error(`Card reply shown ${replies.length} times; expected once per interaction`);
await app.click({ role: "button", name: "Break" });
await app.waitFor({ text: "Part of this card didn’t work.", timeout_ms: 10000 });
await app.waitFor({ role: "heading", name: "Weekly focus", timeout_ms: 10000 });
return "ok";
