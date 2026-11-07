// @fixture --verify-fixture
// The + menu opens above the composer from +'s leading edge, Escape keeps the draft, and its rows act.
const app = await cua.getApp(APP);
const plus = { role: "button", name: "Add attachment" };
const camera = { role: "button", name: "Camera" };
const message = { role: "textfield", name: "Message" };
await app.waitFor({ ...plus, timeout_ms: 10000 });
const [button] = await app.query(plus);
await app.typeText("a", message);

await app.click(plus);
await app.waitFor({ ...camera, timeout_ms: 5000 });
const [files] = await app.query({ role: "button", name: "Files" });
// Menu rows sit 4pt inside the menu.
if (Math.abs(files.frame.x - button.frame.x - 4) > 1) throw new Error("The menu isn't on +'s leading edge: " + JSON.stringify(files.frame));
if (files.frame.y >= button.frame.y) throw new Error("The menu isn't above the composer");
await app.pressKey("escape");
await app.waitFor({ ...camera, gone: true, timeout_ms: 5000 });
await app.pressKey("b");
await cua.sleep(500);
const [draft] = await app.query(message);
if (draft?.value === "b") throw new Error("Typing after the menu replaced the draft");

await app.click(plus);
await app.click({ role: "button", name: "Dismiss menu" });
await app.waitFor({ ...camera, gone: true, timeout_ms: 5000 });

await app.click(plus);
await app.click({ role: "button", name: "Files" });
await app.waitFor({ ...camera, gone: true, timeout_ms: 5000 });
await app.click({ role: "button", name: "Cancel" });
await app.waitFor({ role: "button", name: "Cancel", gone: true, timeout_ms: 5000 });

// Emoji opens in the same menu, goes in at the caret, and typing carries on from there.
const smile = { role: "button", name: "Insert 😀" };
await app.setValue(message, "");
await app.typeText("Hello", message);
await app.pressKey("left", message);
await app.pressKey("left", message);
await app.click(plus);
await app.click({ role: "button", name: "Emoji" });
await app.waitFor({ ...smile, timeout_ms: 5000 });
const [picked] = await app.query(smile);
if (picked.frame.y >= button.frame.y) throw new Error("The emoji picker isn't above the composer");
await app.click(picked.element_token);
await app.pressKey("escape");
await app.waitFor({ ...smile, gone: true, timeout_ms: 5000 });
await app.waitFor({ ...message, value: "Hel😀lo", timeout_ms: 5000 });
await app.pressKey("x");
await app.waitFor({ ...message, value: "Hel😀xlo", timeout_ms: 5000 });
return "ok";
