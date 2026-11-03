// @fixture --verify-fixture --verify-plugins --verify-graph
// A plugin that's both Global and Scoped says so, and keeps a Scoped plugin's list of chats.
const app = await cua.getApp(APP);
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Plugins" });
const rows = await app.query({ role: "button", name: "Graph Visualiser" });
await app.click(rows[rows.length - 1].element_token);
await app.waitFor({ text: "Global and Scoped" });
await app.waitFor({ text: "Repositories", gone: false });
return "ok";
