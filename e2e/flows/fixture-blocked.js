// @fixture --verify-fixture --verify-blocked
// Both pending approvals and completed questions keep their attention marker, even when read.
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Chief, pinned chat", value: "Needs attention", timeout_ms: 10000 });
const waiting = (await app.query({ role: "button", name: "Aria" })).find((item) => item.label.startsWith("Aria, "));
if (!waiting?.value?.includes("Needs attention")) throw new Error("completed question has no attention marker");
await app.click(waiting.element_token);
await app.waitFor({ role: "button", name: "Aria", value: "Active, Needs attention, Waiting for your input", timeout_ms: 10000 });
const ready = (await app.query({ role: "button", name: "Milo" })).find((item) => item.label.startsWith("Milo, "));
if (!ready) throw new Error("ready bot is missing");
if (ready.value?.includes("Needs attention")) throw new Error("ready bot has an attention marker");
return "ok";
