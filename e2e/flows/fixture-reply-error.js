// @fixture --verify-fixture --verify-settings --verify-reply-error -serverAddress http://127.0.0.1:1
// Error metadata is a dismissible notice, never a fabricated assistant message.
const app = await cua.getApp(APP);
for (let i = 0; ; i++) {
    const rows = (await app.query({ role: "button", name: "Chief" })).filter(row => row.label.startsWith("Chief, "));
    if (rows.length) { await app.click(rows[0].element_token); break; }
    if (i >= 30) throw new Error("Chief's chat did not load");
    await cua.sleep(250);
}
await app.waitFor({ text: "Server-authored reply.", timeout_ms: 10000 });
await app.waitFor({ text: "A server-reported problem.", timeout_ms: 10000 });
for (const text of ["Reply failed", "Reply stopped", "Couldn’t complete this reply", "Private provider diagnostics"]) {
    if ((await app.query({ text })).length) throw new Error("Unexpected client reply or private detail: " + text);
}
const [before] = await app.query({ role: "textfield", name: "Message" });
await app.click({ role: "button", name: "Dismiss warning" });
await app.waitFor({ role: "statictext", name: "A server-reported problem.", gone: true, timeout_ms: 3000 });
await app.waitFor({ text: "Server-authored reply." });
const [after] = await app.query({ role: "textfield", name: "Message" });
if (!before?.frame || JSON.stringify(before.frame) !== JSON.stringify(after?.frame)) throw new Error("Warning dismissal moved the message input");
return "ok";
