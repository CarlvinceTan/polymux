// Provider rows scroll independently of the fixed header and its controls.
let app;
for (let i = 0; ; i++) {
    try { app = await cua.getApp(APP); break; }
    catch (error) { if (i >= 20) throw error; await cua.sleep(500); }
}
async function bounds(name) {
    const rows = (await app.query({ role: "button", name })).filter(row => row.label === name);
    if (rows.length !== 1 || !rows[0].frame) throw new Error("Missing or ambiguous bounds: " + name);
    return rows[0].frame;
}
const first = "Amazon Bedrock · AWS credentials or bearer token";
const header = ["Back", "Add custom provider", "Close Add provider", "All", "API key", "Sign in"];
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Models" });
await app.click({ role: "button", name: "Add provider" });
await app.waitFor({ role: "button", name: first, timeout_ms: 10000 });
const initial = await bounds(first);
const positions = [];
for (const name of header) positions.push(await bounds(name));
await app.scroll({ role: "button", name: first }, "down", 4);
const scrolled = await bounds(first);
if (scrolled.y >= initial.y) throw new Error("Provider list did not scroll");
for (let i = 0; i < header.length; i++) {
    const current = await bounds(header[i]);
    if (current.y !== positions[i].y) throw new Error("Provider header moved with its rows: " + header[i]);
}
await app.scroll({ role: "button", name: "All" }, "down", 4);
if ((await bounds(first)).y !== scrolled.y) throw new Error("Scrolling over tabs moved the provider list");
await app.click({ role: "button", name: "API key" });
if ((await bounds(first)).y !== initial.y) throw new Error("Switching tabs did not reset the provider list");
await app.scroll({ role: "button", name: first }, "down", 4);
await app.click({ role: "button", name: "Add custom provider" });
await app.waitFor({ role: "textfield", name: "Base URL", timeout_ms: 5000 });
await app.click({ role: "button", name: "Back" });
await app.waitFor({ role: "button", name: "Add provider", timeout_ms: 5000 });
await app.click({ role: "button", name: "Close Settings" });
return "ok";
