// @fixture --verify-fixture --verify-plugins --verify-graph --verify-graph-down
// With the plugin's service gone, the graph still shows Polymux's own work, and says nothing is wrong with it.
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Graph Visualiser" });
await app.click({ role: "button", name: "Graph Visualiser" });
await app.waitFor({ role: "button", name: "Show compact view" });
await app.waitFor({ role: "button", name: "Claude Code" });
await app.waitFor({ role: "button", name: "Task from Chief to Aria" });
if ((await app.query({ role: "button", name: "Safari" })).length) throw new Error("A report appeared with no service");
if ((await app.query({ name: "No activity yet." })).length) throw new Error("The graph claims to be empty");
return "ok";
