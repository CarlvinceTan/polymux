// @fixture --verify-fixture --verify-tools
// Reaction palettes expose meaningful actions and group names to accessibility clients.
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Chief", timeout_ms: 10000 });
const chief = (await app.query({ role: "button", name: "Chief" })).find(row => row.label.startsWith("Chief, "));
if (!chief) throw new Error("Chief's chat is missing");
await app.click(chief.element_token);
const source = "Second reply from the same turn.";
await app.waitFor({ role: "button", name: source });
await app.rightClick({ role: "button", name: source });
await app.waitFor({ role: "menuitem", name: "React ❤️" });
const items = await app.query({ role: "menuitem" });
if (items.some(row => row.label?.startsWith("ID(id:"))) throw new Error("Reaction menu exposes internal group names");
// Background keys are refused while a menu holds the window, so close it by choosing.
await app.click({ role: "menuitem", name: "React ❤️" });
await app.waitFor({ role: "menuitem", name: "React ❤️", gone: true, timeout_ms: 5000 });
return "ok";
