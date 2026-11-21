// @fixture --verify-fixture --verify-permissions --verify-plugins --verify-onboarding
// The first-run walkthrough chooses where bots run, then shows the permissions they need.
let app = await cua.getApp(APP);
await app.waitFor({ role: "statictext", name: "Get Started with Polymux", timeout_ms: 10000 });
await app.click({ role: "button", name: "Continue" });
await app.waitFor({ role: "button", name: "On another device", timeout_ms: 5000 });
await app.click({ role: "button", name: "Continue" });
await app.waitFor({ role: "statictext", name: "System Permissions", timeout_ms: 5000 });
await app.waitFor({ role: "button", name: "Grant Full Disk Access", timeout_ms: 5000 });

await app.click({ role: "button", name: "Back" });
await app.waitFor({ role: "button", name: "On another device", timeout_ms: 5000 });
await app.click({ role: "button", name: "Continue" });
await app.waitFor({ role: "statictext", name: "System Permissions", timeout_ms: 5000 });
await app.click({ role: "button", name: "Continue" });
await app.waitFor({ role: "statictext", name: "Plugins", timeout_ms: 5000 });
await app.waitFor({ role: "statictext", name: "Hub", timeout_ms: 10000 });
await app.click({ role: "button", name: "Done" });
await cua.sleep(1000);
app = await cua.getApp(APP);
for (let i = 0; i < 20 && (await app.query({ role: "statictext", name: "System Permissions" })).length; i++) await cua.sleep(250);
if ((await app.query({ role: "statictext", name: "System Permissions" })).length) throw new Error("The walkthrough stayed open");

// Settings lists the same permissions and distinguishes granted access from pending grants.
await app.click({ role: "button", name: "Settings" });
await app.waitFor({ role: "button", name: "Grant Full Disk Access", timeout_ms: 10000 });
await app.waitFor({ name: "Screen & System Audio Recording: Granted", timeout_ms: 5000 });
await app.waitFor({ role: "button", name: "Manage permissions", timeout_ms: 10000 });
await app.waitFor({ text: "Device Access", timeout_ms: 5000 });
await app.waitFor({ role: "button", name: "Run Setup Again", timeout_ms: 5000 });
await app.click({ role: "button", name: "Run Setup Again" });
await cua.sleep(1000);
app = await cua.getApp(APP);
await app.waitFor({ role: "statictext", name: "Get Started with Polymux", timeout_ms: 5000 });
await app.click({ role: "button", name: "Skip for now" });
