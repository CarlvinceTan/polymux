// Search finds a message sent earlier in a chat.
let app = await cua.getApp(APP);
const word = "find" + Date.now();
await app.click({ role: "button", name: "New chat" });
app = await pick(app, "New Bot");
await app.typeText("Search" + word, { role: "textfield", name: "Name" });
await app.click({ role: "button", name: "Create" });
await app.typeText("Remember " + word, { role: "textfield", name: "Message" });
await app.pressKey("return", { role: "textfield", name: "Message" });
await app.waitFor({ text: "Remember " + word, timeout_ms: 10000 });
await app.click({ role: "button", name: "Search" });
await app.typeText(word, { role: "textfield", name: "Search everything" });
await app.waitFor({ role: "button", name: "Remember " + word, timeout_ms: 10000 });
await app.click({ role: "button", name: "Close search" });
return "ok";
