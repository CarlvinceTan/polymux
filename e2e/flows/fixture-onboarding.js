// @fixture --verify-fixture --verify-permissions --verify-plugins --verify-providers --verify-settings --verify-onboarding
// The first-run walkthrough chooses where bots run, connects a model, shows the permissions bots need,
// and ends ready to chat.
let app = await cua.getApp(APP);
await app.waitFor({ role: "statictext", name: "Get Started with Polymux", timeout_ms: 10000 });
await app.click({ role: "button", name: "Continue" });
await app.waitFor({ role: "button", name: "On another device", timeout_ms: 5000 });
await app.click({ role: "button", name: "Continue" });

// Without a provider, setup can only move on by putting the model off.
await app.waitFor({ role: "statictext", name: "Connect a Model", timeout_ms: 5000 });
await app.waitFor({ role: "button", name: "Set up a model later", timeout_ms: 5000 });
// Choosing a provider happens on the page itself, and Back steps out of it.
await app.click({ role: "button", name: "Add a provider" });
await app.waitFor({ role: "textfield", name: "Search providers", timeout_ms: 5000 });
if ((await app.query({ role: "button", name: "Close Add provider" })).length) throw new Error("Setup opened a panel");
await app.click({ role: "button", name: "Back" });
await app.waitFor({ role: "button", name: "Add a provider", timeout_ms: 5000 });
await app.click({ role: "button", name: "Add a provider" });
await app.click({ role: "button", name: "Subscription · Subscription sign-in" });
await app.waitFor({ text: "TEST-1234", timeout_ms: 10000 });
await app.typeText("http://localhost/callback?code=test", { role: "textfield", name: "http://localhost/callback" });
await app.click({ role: "button", name: "Continue" });
await app.waitFor({ role: "button", name: "Personal", timeout_ms: 5000 });
await app.click({ role: "button", name: "Personal" });
// The sign-in finishes on the page and the walkthrough lists the provider with a default model to pick.
await app.waitFor({ role: "statictext", name: "Subscription", timeout_ms: 5000 });
await app.click({ role: "button", name: "Default model" });
await app.click({ role: "button", name: "Subscription Chat Subscription" });
await app.waitFor({ role: "button", name: "Default model", value: "Subscription Chat Subscription", timeout_ms: 5000 });
if ((await app.query({ role: "button", name: "Set up a model later" })).length) throw new Error("Later stayed after a provider connected");
await app.click({ role: "button", name: "Continue" });

await app.waitFor({ role: "statictext", name: "System Permissions", timeout_ms: 5000 });
await app.waitFor({ role: "button", name: "Grant Full Disk Access", timeout_ms: 5000 });
await app.click({ role: "button", name: "Back" });
await app.waitFor({ role: "statictext", name: "Connect a Model", timeout_ms: 5000 });
await app.click({ role: "button", name: "Continue" });
await app.waitFor({ role: "statictext", name: "System Permissions", timeout_ms: 5000 });
await app.click({ role: "button", name: "Continue" });
await app.waitFor({ role: "statictext", name: "Plugins", timeout_ms: 5000 });
await app.waitFor({ role: "statictext", name: "Hub", timeout_ms: 10000 });
await app.click({ role: "button", name: "Continue" });

// The last page sums up what is ready and offers the iPhone pairing code.
await app.waitFor({ role: "statictext", name: "You’re All Set", timeout_ms: 5000 });
await app.waitFor({ role: "button", name: "Model", value: "Subscription Chat Subscription", timeout_ms: 5000 });
await app.waitFor({ role: "button", name: "Copy link", timeout_ms: 10000 });
await app.click({ role: "button", name: "Start" });
await cua.sleep(1000);
app = await cua.getApp(APP);
for (let i = 0; i < 20 && (await app.query({ role: "statictext", name: "You’re All Set" })).length; i++) await cua.sleep(250);
if ((await app.query({ role: "statictext", name: "You’re All Set" })).length) throw new Error("The walkthrough stayed open");

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
