// @fixture --verify-fixture --verify-pins --verify-sidebar-collapsed
// Revealing a collapsed sidebar restores usable chats and titlebar controls without disturbing the composer.
const app = await cua.getApp(APP);
async function inputMatches(check, message) {
    let field;
    for (let i = 0; i < 30; i++) {
        [field] = await app.query({ role: "textfield", name: "Message" });
        if (field?.frame && check(field)) return field;
        await cua.sleep(50);
    }
    throw new Error(message + ": " + JSON.stringify(field));
}
await app.waitFor({ role: "button", name: "Show sidebar" });
await app.click({ role: "button", name: "Show sidebar" });
await app.waitFor({ role: "button", name: "Show sidebar", gone: true });
await app.waitFor({ role: "button", name: "Settings" });
const [chief] = (await app.query({ role: "button", name: "Chief" })).filter(row => row.label === "Chief, pinned chat");
if (!chief?.frame || chief.frame.w <= 0) throw new Error("Revealing the sidebar did not show its chats");
await app.click(chief.element_token);
await app.waitFor({ role: "button", name: "Edit Chief’s profile" });
for (let i = 0; i < 3; i++) {
    await app.click({ role: "button", name: "Settings" });
    await app.waitFor({ role: "button", name: "Close Settings" });
    await app.waitFor({ role: "button", name: "Theme" });
    await app.click({ role: "button", name: "Close Settings" });
    await app.waitFor({ role: "textfield", name: "Message" });
}
for (const draft of ["", "Keep this draft while opening the profile"]) {
    if (draft) {
        await app.typeText(draft, { role: "textfield", name: "Message" });
        await app.waitFor({ role: "textfield", name: "Message", value: draft });
    }
    for (let i = 0; i < 3; i++) {
        const [before] = await app.query({ role: "textfield", name: "Message" });
        const preserves = field => field.value === draft || (!draft && field.value === "Message");
        await app.click({ role: "button", name: "Edit Chief’s profile" });
        await app.waitFor({ role: "button", name: "Close side panel" });
        const open = await inputMatches(field => field.frame.w < before.frame.w && preserves(field),
                                        "Opening the right panel did not preserve and resize the message input");
        await app.click({ role: "button", name: "Close side panel" });
        await app.waitFor({ role: "button", name: "Close side panel", gone: true });
        await inputMatches(field => field.frame.w > open.frame.w && field.frame.w >= before.frame.w - 2 && preserves(field),
                           "Closing the right panel did not preserve and restore the message input");
    }
}
return "ok";
// @relaunch
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Show sidebar" });
await app.click({ role: "button", name: "Show sidebar" });
await app.waitFor({ role: "button", name: "Show sidebar", gone: true });
await app.waitFor({ role: "button", name: "Settings" });
await app.waitFor({ role: "textfield", name: "Message", value: "Keep this draft while opening the profile" });
return "ok";
