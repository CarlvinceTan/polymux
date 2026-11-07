// @fixture --verify-fixture --verify-send-offline
// A send the server never accepted says it isn't sent yet and offers to delete it before it goes out.
const app = await cua.getApp(APP);
let rows = [];
for (let i = 0; i < 60 && !rows.length; i++) {
  rows = (await app.query({ role: "button", name: "Chief" })).filter((item) => item.label.startsWith("Chief, "));
  if (!rows.length) await cua.sleep(500);
}
await app.click(rows[0].element_token);
await app.typeText("Sent while offline", { role: "textfield", name: "Message" });
await app.pressKey("return", { role: "textfield", name: "Message" });
await app.waitFor({ text: "Not sent yet", timeout_ms: 10000 });
// Buttons inside the transcript don't take accessibility presses, so deleting is covered by
// ChatStore's unit test; this checks the option is offered.
await app.waitFor({ role: "button", name: "Delete unsent message", timeout_ms: 10000 });
return "ok";
