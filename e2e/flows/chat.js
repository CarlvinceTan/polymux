// Creating a bot opens its chat, and a sent message shows in the transcript.
let app = await cua.getApp(APP);
const name = "Chat" + Date.now();
const text = "Hello from " + name;
await app.click({ role: "button", name: "New chat" });
app = await pick(app, "New Bot");
// Every fresh form starts on Shapes, even after another category was selected.
for (let attempt = 0; attempt < 3; attempt++) {
  await app.waitFor({ role: "textfield", name: "Name", timeout_ms: 10000 });
  const [shapes] = await app.query({ role: "button", name: "Shapes" });
  if (!shapes?.selected) throw new Error("New Bot did not start on Shapes");
  await app.click({ role: "button", name: "Animals" });
  await app.click({ role: "button", name: "Cat" });
  await app.click({ role: "button", name: "Close New Bot" });
  await app.click({ role: "button", name: "New chat" });
  app = await pick(app, "New Bot");
}
await app.waitFor({ role: "textfield", name: "Name", timeout_ms: 10000 });
const [shapes] = await app.query({ role: "button", name: "Shapes" });
if (!shapes?.selected) throw new Error("Reopening New Bot did not reset to Shapes");
await app.click({ role: "button", name: "Hexagon" });
await app.waitFor({ role: "button", name: "Hexagon", value: "Selected" });
await app.click({ role: "button", name: "Grey" });
await app.waitFor({ role: "button", name: "Grey", value: "Selected" });
// Type rather than set values: SwiftUI text fields ignore accessibility value sets.
await app.typeText(name, { role: "textfield", name: "Name" });
await app.click({ role: "button", name: "Create" });
await app.waitFor({ role: "button", name: name, timeout_ms: 10000 });
await app.typeText(text, { role: "textfield", name: "Message" });
await app.pressKey("return", { role: "textfield", name: "Message" });
await app.waitFor({ text: text, timeout_ms: 10000 });
return "ok";
