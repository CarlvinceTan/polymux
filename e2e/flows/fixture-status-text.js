// @fixture --verify-fixture --verify-busy --verify-status-text
// A model-authored status replaces the tool phrase in the activity line.
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Chief" });
const [row] = (await app.query({ role: "button", name: "Chief" })).filter(item => item.label.startsWith("Chief, "));
if (!row) throw new Error("Chief row is missing");
await app.click(row.element_token);
await app.waitFor({ text: "Searching for Artemis II dates", timeout_ms: 10000 });
return "ok";
