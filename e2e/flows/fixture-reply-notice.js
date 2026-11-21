// @fixture --verify-fixture --verify-settings --verify-reply-error --verify-reply-notice -serverAddress http://127.0.0.1:1
// A reply from another model keeps its text and says why in a dismissible warning.
const app = await cua.getApp(APP);
for (let i = 0; ; i++) {
    const rows = (await app.query({ role: "button", name: "Chief" })).filter(row => row.label.startsWith("Chief, "));
    if (rows.length) { await app.click(rows[0].element_token); break; }
    if (i >= 30) throw new Error("Chief's chat did not load");
    await cua.sleep(250);
}
await app.waitFor({ text: "Server-authored reply.", timeout_ms: 10000 });
const notice = "DeepSeek V4.1 Flash on DeepSeek is unavailable, so OpenCode Go replied instead.";
await app.waitFor({ text: notice, timeout_ms: 10000 });
await app.click({ role: "button", name: "Dismiss warning" });
await app.waitFor({ role: "statictext", name: notice, gone: true, timeout_ms: 3000 });
await app.waitFor({ text: "Server-authored reply." });
return "ok";
