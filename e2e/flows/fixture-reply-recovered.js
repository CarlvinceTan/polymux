// @fixture --verify-fixture --verify-settings --verify-reply-error --verify-reply-recovered -serverAddress http://127.0.0.1:1
// A quiet successful run clears an older failure without rewriting the failed reply.
const app = await cua.getApp(APP);
for (let i = 0; ; i++) {
    const rows = (await app.query({ role: "button", name: "Chief" })).filter(row => row.label.startsWith("Chief, "));
    if (rows.length) { await app.click(rows[0].element_token); break; }
    if (i >= 30) throw new Error("Chief's chat did not load");
    await cua.sleep(250);
}
await app.waitFor({ text: "Server-authored reply.", timeout_ms: 10000 });
await app.waitFor({ text: "Ready", timeout_ms: 10000 });
for (const text of ["A server-reported problem.", "Reply failed", "Private provider diagnostics"]) {
    if ((await app.query({ text })).length) throw new Error("Stale failure remained after recovery: " + text);
}
if ((await app.query({ role: "button", name: "Dismiss warning" })).length) {
    throw new Error("Recovered failure still needs manual dismissal");
}
return "ok";
