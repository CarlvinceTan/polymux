// @fixture --verify-fixture --verify-profile
// Built-in skills stay visible, and MCP tools expand inside the shared Tools card.
const app = await cua.getApp(APP);
let rows = [];
for (let i = 0; i < 60 && !rows.length; i++) {
    rows = (await app.query({ role: "button", name: "Chief" })).filter(row => row.label.startsWith("Chief, "));
    if (!rows.length) await cua.sleep(500);
}
if (rows.length !== 1) throw new Error("Chief row missing");
await app.click(rows[0].element_token);
await app.click({ role: "button", name: "Edit Chief’s profile" });
await app.click({ role: "button", name: "Capabilities" });
for (const name of ["Polymux", "Report", "HTML Cards", "Skill 1"]) await app.waitFor({ text: name, timeout_ms: 10000 });
async function connection() {
    const state = await app.getState({ query: "Connection 1", include_screenshot: false,
        tree_format: "elements", max_elements: 500, timeout_ms: 5000 });
    const matches = state.elements.filter(row => row.role === "AXDisclosureTriangle" && row.label.startsWith("Connection 1,"));
    if (state.truncated || matches.length !== 1) throw new Error("MCP disclosure missing");
    return matches[0];
}
if ((await connection()).value !== "0") throw new Error("MCP starts expanded");
await app.click((await connection()).element_token);
await app.waitFor({ text: "connection_1_tool_1" });
await app.waitFor({ text: "connection_1_tool_3" });
await app.click((await connection()).element_token);
await app.waitFor({ text: "connection_1_tool_1", gone: true });
if ((await connection()).value !== "0") throw new Error("MCP did not collapse");
for (const name of ["MCPs", "Disabled connection", "Hidden connection"]) {
    if ((await app.query({ text: name })).length) throw new Error("Unexpected capability row: " + name);
}
return "ok";
