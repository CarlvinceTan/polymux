// @fixture --verify-fixture --verify-settings --verify-reply-error --verify-dictation --verify-dictation-unavailable --verify-still -serverAddress http://127.0.0.1:1
const app = await cua.getApp(APP);
for (let i = 0; ; i++) {
    const rows = (await app.query({ role: "button", name: "Chief" })).filter(row => row.label.startsWith("Chief, "));
    if (rows.length) { await app.click(rows[0].element_token); break; }
    if (i >= 30) throw new Error("Chief's chat did not load");
    await cua.sleep(250);
}

const reply = { role: "statictext", name: "A server-reported problem." };
const dictation = { role: "statictext", name: "Dictation isn’t available right now." };
const input = { role: "textfield", name: "Message" };
const dismiss = { role: "button", name: "Dismiss warning" };
await app.waitFor({ ...reply, timeout_ms: 10000 });
const [before] = await app.query(input);
await cua.sleep(2000);
await app.click({ role: "button", name: "Start dictation" });
await app.waitFor({ ...dictation, timeout_ms: 3000 });
await app.waitFor({ ...reply, gone: true, timeout_ms: 3000 });
const buttons = await app.query(dismiss);
const [front] = buttons;
const [after] = await app.query(input);
if (buttons.length !== 1 || front?.value !== `${dictation.name}, 1 more` ||
    !front?.frame || !before?.frame ||
    front.frame.y + front.frame.h >= after?.frame?.y ||
    JSON.stringify(before.frame) !== JSON.stringify(after?.frame)) {
    throw new Error("Expected one warning with a count of one more, above the unmoved input");
}

for (let i = 0; ; i++) {
    const [active] = await app.query(dismiss);
    if (active?.value === dictation.name) {
        if (JSON.stringify(active.frame) !== JSON.stringify(front.frame)) {
            throw new Error("Clearing the count moved the warning");
        }
        break;
    }
    if (!active || i >= 40) throw new Error("The count did not clear when the older warning expired");
    await cua.sleep(150);
}
await app.waitFor({ ...dictation, gone: true, timeout_ms: 5000 });
return "ok";
