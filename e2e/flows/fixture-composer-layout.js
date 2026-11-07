// @fixture --verify-fixture --verify-history --verify-panel t-long
// Composer growth keeps the newest message visible without moving the input's bottom edge or losing a reading position.
const app = await cua.getApp(APP);
const source = "History message 150";
for (let i = 0; ; i++) {
    const rows = (await app.query({ role: "button", name: "t-long" })).filter(row => row.label.startsWith("t-long, "));
    if (rows.length) { await app.click(rows[0].element_token); break; }
    if (i >= 40) throw new Error("History chat missing");
    await cua.sleep(250);
}
await app.waitFor({ role: "button", name: source });
await app.waitFor({ role: "button", name: "Scroll to latest", gone: true });
const [initial] = await app.query({ role: "textfield", name: "Message" });
const bottom = initial.frame.y + initial.frame.h;
async function settled(quoted) {
    for (let i = 0; i < 30; i++) {
        const [input] = await app.query({ role: "textfield", name: "Message" });
        const [last] = await app.query({ role: "button", name: source });
        const [cancel] = await app.query({ role: "button", name: "Cancel reply" });
        const top = quoted ? cancel?.frame?.y : input?.frame?.y;
        if (input?.frame && last?.frame && top &&
            Math.abs(input.frame.y + input.frame.h - bottom) <= (quoted ? 6 : 1) &&
            last.frame.y + last.frame.h <= top &&
            !(await app.query({ role: "button", name: "Scroll to latest" })).length) return;
        await cua.sleep(50);
    }
    throw new Error("Composer growth hid the newest message, moved its bottom edge, or showed a false latest button");
}
for (let i = 0; i < 3; i++) {
    await app.rightClick({ role: "button", name: source });
    await pick(app, "Reply", open => open.rightClick({ role: "button", name: source }));
    await app.waitFor({ role: "button", name: "Cancel reply" });
    await settled(true);
    await app.click({ role: "button", name: "Cancel reply" });
    await settled(false);
    if (i === 0) {
        await app.click({ role: "button", name: "Close side panel" });
        await app.waitFor({ role: "button", name: "Close side panel", gone: true });
        await settled(false);
    }
}
await app.setValue({ role: "textfield", name: "Message" }, "First line\nSecond line\nThird line\nFourth line");
await settled(false);
await app.setValue({ role: "textfield", name: "Message" }, "");
await settled(false);
await app.scroll({ role: "button", name: source }, "up", 12);
await app.waitFor({ role: "button", name: "Scroll to latest" });
const [input] = await app.query({ role: "textfield", name: "Message" });
const [latest] = await app.query({ role: "button", name: "Scroll to latest" });
if (!latest?.frame || latest.frame.y + latest.frame.h >= input.frame.y) {
    throw new Error("Latest button overlaps the message input");
}
const messages = await app.query({ role: "button", name: "History message" });
const anchor = messages.find(item => item.frame && item.frame.y > initial.frame.y - 350 && item.frame.y + item.frame.h < input.frame.y - 150);
if (!anchor) throw new Error("No visible history message to check");
await app.setValue({ role: "textfield", name: "Message" }, "Another\nmultiline\ndraft");
await cua.sleep(400);
const [after] = await app.query({ role: "button", name: anchor.label });
if (!after?.frame || Math.abs(after.frame.y - anchor.frame.y) > 2) throw new Error("Growing the composer lost the reading position");
await app.waitFor({ role: "button", name: "Scroll to latest" });
return "ok";
