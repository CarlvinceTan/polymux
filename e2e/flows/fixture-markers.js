// @fixture --verify-fixture --verify-blocked --verify-markers --verify-still
// Waiting wins over unread; pinned and expanded chats expose only the chosen marker.
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Chief, pinned chat", value: "Needs attention", timeout_ms: 10000 });
await app.waitFor({ role: "button", name: "Milo, pinned chat", value: "Unread", timeout_ms: 10000 });
await app.waitFor({ role: "button", name: "Group, pinned chat", value: "Unread", timeout_ms: 10000 });
const row = async (name) => (await app.query({ role: "button", name })).find((item) => item.label.startsWith(name + ", "));
const aria = await row("Aria");
if (!aria?.value?.includes("Needs attention") || aria.value.includes("Unread")) throw new Error("waiting does not replace unread");
const group = await row("grp-trip");
if (group?.value !== "Needs attention") throw new Error("group marker has the wrong priority");
const working = await row("Approval");
if (working?.value !== "Active, Working") throw new Error("working bot has a marker instead of only its animation");
const ready = await row("Fern");
if (ready?.value !== "Active, Ready") throw new Error("idle bot has an online marker");
const nova = await row("Nova");
if (!nova?.value?.includes("Unread")) throw new Error("expanded unread marker is missing");
await app.click(nova.element_token);
await app.waitFor({ role: "button", name: "Nova", value: "Active, Ready", timeout_ms: 10000 });
await app.click((await row("Aria")).element_token);
await app.waitFor({ role: "button", name: "Aria", value: "Active, Needs attention, Waiting for your input", timeout_ms: 10000 });
return "ok";
